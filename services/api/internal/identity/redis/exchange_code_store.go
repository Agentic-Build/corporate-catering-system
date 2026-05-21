package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ExchangeCodeStore backs the PKCE-style one-time code → session-token
// handshake used by the native mobile app (B4). After OIDC completes the
// backend mints a random code, stores it here with a short TTL, and
// redirects the deep link with the code instead of the session token. The
// app then POSTs the code to /auth/exchange to swap it for the long-lived
// session token.
//
// Consume is atomic (GETDEL) so a code can only be redeemed once.
type ExchangeCodeStore struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewExchangeCodeStore(rdb *redis.Client, ttl time.Duration) *ExchangeCodeStore {
	return &ExchangeCodeStore{rdb: rdb, ttl: ttl}
}

func exchangeKey(code string) string { return "oidc:exchange:" + code }

func (s *ExchangeCodeStore) Put(ctx context.Context, code, token string) error {
	if err := s.rdb.Set(ctx, exchangeKey(code), token, s.ttl).Err(); err != nil {
		return fmt.Errorf("redis set: %w", err)
	}
	return nil
}

// Consume returns (token, true, nil) on the first successful exchange and
// (zero, false, nil) when the code is unknown or already redeemed. Any
// non-miss Redis error is returned with ok=false.
func (s *ExchangeCodeStore) Consume(ctx context.Context, code string) (string, bool, error) {
	tok, err := s.rdb.GetDel(ctx, exchangeKey(code)).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("redis getdel: %w", err)
	}
	return tok, true, nil
}
