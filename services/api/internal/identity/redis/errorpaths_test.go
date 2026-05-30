package redis_test

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	idredis "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/redis"
)

// --- closed-client tests: every redis op returns a non-nil error ---

// closedStore returns a SessionStore whose underlying client is already closed,
// so any redis command fails with a real (non-redis.Nil) error. This drives the
// "redis <op>" error-wrap branches that a live server never exercises.
func closedStore(t *testing.T) *idredis.SessionStore {
	t.Helper()
	rdb, cleanup := setupRedis(t)
	t.Cleanup(cleanup)
	require.NoError(t, rdb.Close())
	return idredis.NewSessionStore(rdb, time.Hour)
}

func TestSessionStore_Create_SetError(t *testing.T) {
	s := closedStore(t)
	_, err := s.Create(context.Background(), "u", identity.RoleEmployee)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis set")
}

func TestSessionStore_Get_RedisError(t *testing.T) {
	s := closedStore(t)
	_, err := s.Get(context.Background(), "tb_x")
	require.Error(t, err)
	assert.NotErrorIs(t, err, identity.ErrSessionNotFound)
	assert.Contains(t, err.Error(), "redis get")
}

func TestSessionStore_Touch_RedisError(t *testing.T) {
	s := closedStore(t)
	err := s.Touch(context.Background(), "tb_x")
	require.Error(t, err)
	assert.NotErrorIs(t, err, identity.ErrSessionNotFound)
	assert.Contains(t, err.Error(), "redis get")
}

func TestSessionStore_Touch_NotFound(t *testing.T) {
	rdb, cleanup := setupRedis(t)
	defer cleanup()
	s := idredis.NewSessionStore(rdb, time.Hour)
	err := s.Touch(context.Background(), "tb_missing")
	assert.ErrorIs(t, err, identity.ErrSessionNotFound)
}

func TestSessionStore_RevokeAllForUser_SMembersError(t *testing.T) {
	s := closedStore(t)
	err := s.RevokeAllForUser(context.Background(), "u")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis smembers")
}

func TestSessionStore_IssueCode_SetError(t *testing.T) {
	s := closedStore(t)
	_, err := s.IssueCode(context.Background(), "tb_tok")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis set handoff")
}

func TestSessionStore_RedeemCode_RedisError(t *testing.T) {
	s := closedStore(t)
	_, err := s.RedeemCode(context.Background(), "code")
	require.Error(t, err)
	assert.NotErrorIs(t, err, identity.ErrHandoffNotFound)
	assert.Contains(t, err.Error(), "redis getdel handoff")
}

// --- corrupt-payload tests: GET succeeds but JSON decode fails ---

func TestSessionStore_Get_DecodeError(t *testing.T) {
	rdb, cleanup := setupRedis(t)
	defer cleanup()
	s := idredis.NewSessionStore(rdb, time.Hour)
	ctx := context.Background()
	require.NoError(t, rdb.Set(ctx, "sess:tb_bad", []byte("not-json"), time.Hour).Err())
	_, err := s.Get(ctx, "tb_bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode session")
}

func TestSessionStore_Touch_DecodeError(t *testing.T) {
	rdb, cleanup := setupRedis(t)
	defer cleanup()
	s := idredis.NewSessionStore(rdb, time.Hour)
	ctx := context.Background()
	require.NoError(t, rdb.Set(ctx, "sess:tb_bad", []byte("not-json"), time.Hour).Err())
	err := s.Touch(ctx, "tb_bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode session")
}

// --- selective-failure tests via a tiny scripted RESP server ---
// Create's SAdd-error branch and RevokeAllForUser's Del-error branch require
// some commands to succeed while a later one fails. A scripted in-process RESP
// server gives that precise control without an external dependency.

type scriptRESP struct {
	ln       net.Listener
	mu       sync.Mutex
	replies  map[string]string
	defReply string
}

func newScriptRESP(t *testing.T) *scriptRESP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	f := &scriptRESP{
		ln:       ln,
		replies:  map[string]string{"HELLO": "%0\r\n", "CLIENT": "+OK\r\n", "AUTH": "+OK\r\n"},
		defReply: "+OK\r\n",
	}
	go f.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return f
}

func (f *scriptRESP) reply(cmd, raw string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[strings.ToUpper(cmd)] = raw
}

func (f *scriptRESP) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *scriptRESP) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	for {
		cmd, ok := readScriptCommand(r)
		if !ok {
			return
		}
		f.mu.Lock()
		raw, found := f.replies[strings.ToUpper(cmd)]
		if !found {
			raw = f.defReply
		}
		f.mu.Unlock()
		if _, err := conn.Write([]byte(raw)); err != nil {
			return
		}
	}
}

func readScriptCommand(r *bufio.Reader) (string, bool) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", false
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 || line[0] != '*' {
		return "", false
	}
	n, err := strconv.Atoi(line[1:])
	if err != nil || n <= 0 {
		return "", false
	}
	var verb string
	for i := 0; i < n; i++ {
		hdr, err := r.ReadString('\n')
		if err != nil {
			return "", false
		}
		hdr = strings.TrimRight(hdr, "\r\n")
		if len(hdr) == 0 || hdr[0] != '$' {
			return "", false
		}
		blen, err := strconv.Atoi(hdr[1:])
		if err != nil || blen < 0 {
			return "", false
		}
		buf := make([]byte, blen+2)
		if err := readScriptFull(r, buf); err != nil {
			return "", false
		}
		if i == 0 {
			verb = string(buf[:blen])
		}
	}
	return verb, true
}

func readScriptFull(r *bufio.Reader, buf []byte) error {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return err
		}
	}
	return nil
}

func newScriptClient(t *testing.T, f *scriptRESP) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{
		Addr:         f.ln.Addr().String(),
		DialTimeout:  time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
		MaxRetries:   -1,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

func TestSessionStore_Create_SAddError(t *testing.T) {
	f := newScriptRESP(t)
	// SET succeeds (default +OK); SADD returns an error reply.
	f.reply("SADD", "-ERR sadd boom\r\n")
	s := idredis.NewSessionStore(newScriptClient(t, f), time.Hour)
	_, err := s.Create(context.Background(), "u", identity.RoleEmployee)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis sadd")
}

func TestSessionStore_RevokeAllForUser_DelSessionsError(t *testing.T) {
	f := newScriptRESP(t)
	// SMEMBERS returns one token; DEL of the session keys errors.
	f.reply("SMEMBERS", "*1\r\n$9\r\ntb_tokenA\r\n")
	f.reply("DEL", "-ERR del boom\r\n")
	s := idredis.NewSessionStore(newScriptClient(t, f), time.Hour)
	err := s.RevokeAllForUser(context.Background(), "u")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis del sessions")
}

func TestSessionStore_RevokeAllForUser_Empty(t *testing.T) {
	f := newScriptRESP(t)
	// SMEMBERS returns an empty set -> early return nil, no DEL.
	f.reply("SMEMBERS", "*0\r\n")
	s := idredis.NewSessionStore(newScriptClient(t, f), time.Hour)
	require.NoError(t, s.RevokeAllForUser(context.Background(), "u"))
}
