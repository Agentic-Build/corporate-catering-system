package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrStateNotFound = errors.New("oidc: state not found or expired")
	ErrStateConsumed = errors.New("oidc: state already consumed")
)

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
	return s.rdb.Set(ctx, stateKey(state), raw, s.ttl).Err()
}

func (s *RedisStateStore) Get(ctx context.Context, state string) (*StatePayload, error) {
	raw, err := s.rdb.Get(ctx, stateKey(state)).Bytes()
	if errors.Is(err, redis.Nil) {
		consumedRaw, consumedErr := s.rdb.Get(ctx, consumedStateKey(state)).Bytes()
		if errors.Is(consumedErr, redis.Nil) {
			return nil, ErrStateNotFound
		}
		if consumedErr != nil {
			return nil, fmt.Errorf("redis get consumed state: %w", consumedErr)
		}
		p, decodeErr := decodeStatePayload(consumedRaw)
		if decodeErr != nil {
			return nil, decodeErr
		}
		return p, ErrStateConsumed
	}
	if err != nil {
		return nil, fmt.Errorf("redis get: %w", err)
	}
	return decodeStatePayload(raw)
}

func decodeStatePayload(raw []byte) (*StatePayload, error) {
	var p StatePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}
	return &p, nil
}

func (s *RedisStateStore) Consume(ctx context.Context, state string) error {
	raw, err := s.rdb.Get(ctx, stateKey(state)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("redis get state for consume: %w", err)
	}
	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, consumedStateKey(state), raw, s.ttl)
	pipe.Del(ctx, stateKey(state))
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis consume state: %w", err)
	}
	return nil
}

func stateKey(state string) string         { return "oidc:" + state }
func consumedStateKey(state string) string { return "oidc-consumed:" + state }
