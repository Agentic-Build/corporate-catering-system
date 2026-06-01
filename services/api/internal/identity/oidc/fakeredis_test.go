package oidc_test

import (
	"bufio"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// fakeRESP is a tiny in-process RESP (Redis) server that returns scripted
// replies keyed by command name. It exists to deterministically drive the
// error branches of RedisStateStore (e.g. a GET that returns an error, or a
// pipeline EXEC that fails) without an external dependency. Replies are raw
// RESP byte strings; "default" supplies the reply for unscripted commands.
type fakeRESP struct {
	t        *testing.T
	ln       net.Listener
	mu       sync.Mutex
	replies  map[string]string   // upper(cmd) -> raw RESP reply
	seqs     map[string][]string // upper(cmd) -> ordered replies (consumed one per call)
	defReply string
}

func newFakeRESP(t *testing.T) *fakeRESP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &fakeRESP{
		t:        t,
		ln:       ln,
		replies:  map[string]string{},
		seqs:     map[string][]string{},
		defReply: "+OK\r\n",
	}
	// go-redis v9 performs a RESP3 handshake on connect; satisfy it so the
	// connection is usable for the scripted command under test.
	f.replies["HELLO"] = "%0\r\n"
	f.replies["CLIENT"] = "+OK\r\n"
	f.replies["AUTH"] = "+OK\r\n"
	go f.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return f
}

func (f *fakeRESP) addr() string { return f.ln.Addr().String() }

// reply sets the RESP reply for the given command (case-insensitive).
func (f *fakeRESP) reply(cmd, raw string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[strings.ToUpper(cmd)] = raw
}

// replySeq sets an ordered sequence of replies for the given command. Each
// invocation of that command consumes the next reply; once exhausted it falls
// back to the static reply / default.
func (f *fakeRESP) replySeq(cmd string, raws ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seqs[strings.ToUpper(cmd)] = raws
}

func (f *fakeRESP) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *fakeRESP) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	for {
		cmd, ok := readCommand(r)
		if !ok {
			return
		}
		key := strings.ToUpper(cmd)
		f.mu.Lock()
		var raw string
		if seq := f.seqs[key]; len(seq) > 0 {
			raw = seq[0]
			f.seqs[key] = seq[1:]
		} else if r, found := f.replies[key]; found {
			raw = r
		} else {
			raw = f.defReply
		}
		f.mu.Unlock()
		if _, err := conn.Write([]byte(raw)); err != nil {
			return
		}
	}
}

// readCommand parses one RESP array command and returns its first token
// (the command verb). Returns ok=false on EOF / parse failure.
func readCommand(r *bufio.Reader) (string, bool) {
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
		// $<len>\r\n<bytes>\r\n
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
		buf := make([]byte, blen+2) // include trailing CRLF
		if _, err := readFull(r, buf); err != nil {
			return "", false
		}
		if i == 0 {
			verb = string(buf[:blen])
		}
	}
	return verb, true
}

func readFull(r *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func newFakeRedisClient(t *testing.T, f *fakeRESP) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{
		Addr:         f.addr(),
		DialTimeout:  time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
		MaxRetries:   -1,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}
