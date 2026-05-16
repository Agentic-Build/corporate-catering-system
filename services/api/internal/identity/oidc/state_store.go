package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrStateNotFound = errors.New("oidc: state not found or expired")

type StatePayload struct {
	App          string `json:"app"`
	Provider     string `json:"provider"`
	ReturnTo     string `json:"return_to"`
	PKCEVerifier string `json:"pkce_verifier"`
	Nonce        string `json:"nonce"`
}

type StateStore interface {
	Put(ctx context.Context, state string, p *StatePayload) error
	Get(ctx context.Context, state string) (*StatePayload, error)
	Consume(ctx context.Context, state string) error
}

type RedisStateStore struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewRedisStateStore(rdb *redis.Client, ttl time.Duration) *RedisStateStore {
	return &RedisStateStore{rdb: rdb, ttl: ttl}
}

func (s *RedisStateStore) Put(ctx context.Context, state string, p *StatePayload) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	return s.rdb.Set(ctx, "oidc:"+state, raw, s.ttl).Err()
}

func (s *RedisStateStore) Get(ctx context.Context, state string) (*StatePayload, error) {
	raw, err := s.rdb.Get(ctx, "oidc:"+state).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrStateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("redis get: %w", err)
	}
	var p StatePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}
	return &p, nil
}

func (s *RedisStateStore) Consume(ctx context.Context, state string) error {
	return s.rdb.Del(ctx, "oidc:"+state).Err()
}
