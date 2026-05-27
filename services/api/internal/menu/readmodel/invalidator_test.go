package readmodel

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// --- minimal jetstream fakes -------------------------------------------------
//
// Each fake embeds the corresponding interface so the (unused) remainder of
// the method set is satisfied; only the methods RunOrderInvalidator calls are
// overridden.

type fakeMsg struct {
	jetstream.Msg
	data []byte
}

func (m fakeMsg) Data() []byte { return m.data }

type fakeConsumeCtx struct {
	jetstream.ConsumeContext
	stopped bool
}

func (c *fakeConsumeCtx) Stop() { c.stopped = true }

type fakeConsumer struct {
	jetstream.Consumer
	msgs       [][]byte
	consumeErr error
	cc         *fakeConsumeCtx
}

func (c *fakeConsumer) Consume(handler jetstream.MessageHandler, _ ...jetstream.PullConsumeOpt) (jetstream.ConsumeContext, error) {
	if c.consumeErr != nil {
		return nil, c.consumeErr
	}
	for _, d := range c.msgs {
		handler(fakeMsg{data: d})
	}
	c.cc = &fakeConsumeCtx{}
	return c.cc, nil
}

type fakeStream struct {
	jetstream.Stream
	cons       *fakeConsumer
	consErr    error
	cfgCapture *jetstream.ConsumerConfig
}

func (s *fakeStream) CreateOrUpdateConsumer(_ context.Context, cfg jetstream.ConsumerConfig) (jetstream.Consumer, error) {
	if s.cfgCapture != nil {
		*s.cfgCapture = cfg
	}
	if s.consErr != nil {
		return nil, s.consErr
	}
	return s.cons, nil
}

type fakeJS struct {
	jetstream.JetStream
	stream    *fakeStream
	streamErr error
}

func (j *fakeJS) Stream(_ context.Context, _ string) (jetstream.Stream, error) {
	if j.streamErr != nil {
		return nil, j.streamErr
	}
	return j.stream, nil
}

// recordCache records every Invalidate pattern; Get/Set are unused here.
type recordCache struct {
	mu       sync.Mutex
	patterns []string
	err      error
}

func (c *recordCache) Get(_ context.Context, _ string) ([]byte, error) { return nil, ErrCacheMiss }
func (c *recordCache) Set(_ context.Context, _ string, _ []byte, _ time.Duration) error {
	return nil
}
func (c *recordCache) Invalidate(_ context.Context, pattern string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.patterns = append(c.patterns, pattern)
	return c.err
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSanitizeConsumerToken(t *testing.T) {
	cases := map[string]string{
		"host.local":     "host-local",
		"Abc_123-xyz":    "Abc_123-xyz",
		"a b/c:d":        "a-b-c-d",
		"":               "",
		"全部中文":           "----",
		"mix.ED/Name_99": "mix-ED-Name_99",
	}
	for in, want := range cases {
		if got := sanitizeConsumerToken(in); got != want {
			t.Errorf("sanitizeConsumerToken(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRunOrderInvalidatorStreamError(t *testing.T) {
	want := errors.New("no stream")
	js := &fakeJS{streamErr: want}
	err := RunOrderInvalidator(context.Background(), js, &recordCache{}, quietLogger())
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestRunOrderInvalidatorConsumerError(t *testing.T) {
	want := errors.New("no consumer")
	js := &fakeJS{stream: &fakeStream{consErr: want}}
	err := RunOrderInvalidator(context.Background(), js, &recordCache{}, quietLogger())
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestRunOrderInvalidatorConsumeError(t *testing.T) {
	want := errors.New("consume failed")
	js := &fakeJS{stream: &fakeStream{cons: &fakeConsumer{consumeErr: want}}}
	err := RunOrderInvalidator(context.Background(), js, &recordCache{}, quietLogger())
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestRunOrderInvalidatorInvalidatesOnEvent(t *testing.T) {
	cache := &recordCache{}
	var cfg jetstream.ConsumerConfig
	cons := &fakeConsumer{msgs: [][]byte{
		[]byte(`{"order_id":"o1","plant":"plant-a","supply_date":"2026-05-26"}`), // valid -> invalidate
		[]byte(`{bad json`),                            // unmarshal error -> skipped
		[]byte(`{"order_id":"o2","plant":""}`),         // empty plant -> skipped
		[]byte(`{"plant":"plant-b","supply_date":""}`), // empty date -> skipped
	}}
	js := &fakeJS{stream: &fakeStream{cons: cons, cfgCapture: &cfg}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunOrderInvalidator(ctx, js, cache, quietLogger()) }()

	// The handler runs synchronously inside Consume before the block on
	// ctx.Done(); cancel to release RunOrderInvalidator.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunOrderInvalidator: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunOrderInvalidator did not return after cancel")
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	// The valid event (no user_id) invalidates home + popularity, not affinity.
	if len(cache.patterns) != 2 {
		t.Fatalf("invalidate patterns = %v, want home + popularity for the valid event", cache.patterns)
	}
	got := map[string]bool{}
	for _, p := range cache.patterns {
		got[p] = true
	}
	if !got["home:*:plant-a:2026-05-26"] || !got["pop:plant-a:2026-05-26"] {
		t.Errorf("patterns = %v, want home + popularity", cache.patterns)
	}
	if cons.cc == nil || !cons.cc.stopped {
		t.Error("consume context Stop() not called on return")
	}
	if cfg.FilterSubject != "order.>" || cfg.AckPolicy != jetstream.AckNonePolicy || cfg.DeliverPolicy != jetstream.DeliverNewPolicy {
		t.Errorf("consumer config = %+v", cfg)
	}
}

func TestRunOrderInvalidatorLogsInvalidateError(t *testing.T) {
	cache := &recordCache{err: errors.New("scan failed")} // exercises logger.Warn branch
	cons := &fakeConsumer{msgs: [][]byte{
		[]byte(`{"order_id":"o1","plant":"plant-a","supply_date":"2026-05-26"}`),
	}}
	js := &fakeJS{stream: &fakeStream{cons: cons}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunOrderInvalidator(ctx, js, cache, quietLogger()) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunOrderInvalidator: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunOrderInvalidator did not return after cancel")
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	// Both home + popularity invalidations are attempted (and both error).
	if len(cache.patterns) != 2 {
		t.Fatalf("invalidate attempted = %d, want 2 (home + popularity)", len(cache.patterns))
	}
}
