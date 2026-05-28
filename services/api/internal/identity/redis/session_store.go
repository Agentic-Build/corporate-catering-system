package redis

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
)

const tokenPrefix = "tb_"

type SessionStore struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewSessionStore(rdb *redis.Client, ttl time.Duration) *SessionStore {
	return &SessionStore{rdb: rdb, ttl: ttl}
}

type sessionDoc struct {
	UserID     string        `json:"user_id"`
	Role       identity.Role `json:"role"`
	CreatedAt  time.Time     `json:"created_at"`
	LastSeenAt time.Time     `json:"last_seen_at"`
}

func (s *SessionStore) Create(ctx context.Context, userID string, role identity.Role) (*identity.Session, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("rand: %w", err)
	}
	token := tokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	now := time.Now().UTC()
	doc := sessionDoc{UserID: userID, Role: role, CreatedAt: now, LastSeenAt: now}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode session: %w", err)
	}
	if err := s.rdb.Set(ctx, key(token), raw, s.ttl).Err(); err != nil {
		return nil, fmt.Errorf("redis set: %w", err)
	}
	if err := s.rdb.SAdd(ctx, userKey(userID), token).Err(); err != nil {
		return nil, fmt.Errorf("redis sadd: %w", err)
	}
	return &identity.Session{
		Token:      token,
		UserID:     userID,
		Role:       role,
		CreatedAt:  now,
		LastSeenAt: now,
	}, nil
}

func (s *SessionStore) Get(ctx context.Context, token string) (*identity.Session, error) {
	raw, err := s.rdb.Get(ctx, key(token)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, identity.ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("redis get: %w", err)
	}
	var d sessionDoc
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("decode session: %w", err)
	}
	return &identity.Session{
		Token:      token,
		UserID:     d.UserID,
		Role:       d.Role,
		CreatedAt:  d.CreatedAt,
		LastSeenAt: d.LastSeenAt,
	}, nil
}

func (s *SessionStore) Touch(ctx context.Context, token string) error {
	raw, err := s.rdb.Get(ctx, key(token)).Bytes()
	if errors.Is(err, redis.Nil) {
		return identity.ErrSessionNotFound
	}
	if err != nil {
		return fmt.Errorf("redis get: %w", err)
	}
	var d sessionDoc
	if err := json.Unmarshal(raw, &d); err != nil {
		return fmt.Errorf("decode session: %w", err)
	}
	d.LastSeenAt = time.Now().UTC()
	out, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}
	return s.rdb.Set(ctx, key(token), out, s.ttl).Err()
}

func (s *SessionStore) Revoke(ctx context.Context, token string) error {
	return s.rdb.Del(ctx, key(token)).Err()
}

func (s *SessionStore) RevokeAllForUser(ctx context.Context, userID string) error {
	tokens, err := s.rdb.SMembers(ctx, userKey(userID)).Result()
	if err != nil {
		return fmt.Errorf("redis smembers: %w", err)
	}
	if len(tokens) == 0 {
		return nil
	}
	keys := make([]string, len(tokens))
	for i, t := range tokens {
		keys[i] = key(t)
	}
	if err := s.rdb.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("redis del sessions: %w", err)
	}
	return s.rdb.Del(ctx, userKey(userID)).Err()
}

func key(token string) string       { return "sess:" + token }
func userKey(userID string) string  { return "sess-user:" + userID }
func handoffKey(code string) string { return "handoff:" + code }

// handoffTTL bounds how long the login-redirect code is valid; the app server
// redeems it within one redirect, so a short window is plenty.
const handoffTTL = 2 * time.Minute

// IssueCode mints a single-use code mapping to token, valid for handoffTTL.
func (s *SessionStore) IssueCode(ctx context.Context, token string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	code := base64.RawURLEncoding.EncodeToString(b)
	if err := s.rdb.Set(ctx, handoffKey(code), token, handoffTTL).Err(); err != nil {
		return "", fmt.Errorf("redis set handoff: %w", err)
	}
	return code, nil
}

// RedeemCode atomically returns and deletes the token for code (single use).
func (s *SessionStore) RedeemCode(ctx context.Context, code string) (string, error) {
	token, err := s.rdb.GetDel(ctx, handoffKey(code)).Result()
	if errors.Is(err, redis.Nil) {
		return "", identity.ErrHandoffNotFound
	}
	if err != nil {
		return "", fmt.Errorf("redis getdel handoff: %w", err)
	}
	return token, nil
}
