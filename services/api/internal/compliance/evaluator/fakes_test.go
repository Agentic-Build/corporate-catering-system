package evaluator

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance"
)

// fakeAnomalyRepo is an in-process AnomalyRepository. Only Open is exercised by
// the evaluator; the rest satisfy the interface and panic if ever called.
type fakeAnomalyRepo struct {
	mu      sync.Mutex
	openErr error
	opened  []*compliance.Anomaly
}

func (r *fakeAnomalyRepo) Open(_ context.Context, a *compliance.Anomaly) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.openErr != nil {
		return r.openErr
	}
	r.opened = append(r.opened, a)
	return nil
}

func (r *fakeAnomalyRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.opened)
}

func (r *fakeAnomalyRepo) last() *compliance.Anomaly {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.opened) == 0 {
		return nil
	}
	return r.opened[len(r.opened)-1]
}

func (r *fakeAnomalyRepo) GetByID(context.Context, string) (*compliance.Anomaly, error) {
	panic("not used")
}

func (r *fakeAnomalyRepo) List(context.Context, []compliance.AnomalyStatus, []compliance.AnomalySeverity) ([]*compliance.Anomaly, error) {
	panic("not used")
}

func (r *fakeAnomalyRepo) Triage(context.Context, string, string, string) error { panic("not used") }
func (r *fakeAnomalyRepo) TriageTx(context.Context, pgx.Tx, string, string, string) error {
	panic("not used")
}
func (r *fakeAnomalyRepo) Close(context.Context, string, string, string) error { panic("not used") }
func (r *fakeAnomalyRepo) CloseTx(context.Context, pgx.Tx, string, string, string) error {
	panic("not used")
}

// === jetstream fakes for Run/setupConsumer/nextMsg ===

// fakeJS embeds jetstream.JetStream; only Stream is implemented.
type fakeJS struct {
	jetstream.JetStream
	streamErr error
	stream    *fakeStream
}

func (j *fakeJS) Stream(context.Context, string) (jetstream.Stream, error) {
	if j.streamErr != nil {
		return nil, j.streamErr
	}
	return j.stream, nil
}

// fakeStream embeds jetstream.Stream; only CreateOrUpdateConsumer is implemented.
type fakeStream struct {
	jetstream.Stream
	consumerErr error
	consumer    *fakeConsumer
}

func (s *fakeStream) CreateOrUpdateConsumer(context.Context, jetstream.ConsumerConfig) (jetstream.Consumer, error) {
	if s.consumerErr != nil {
		return nil, s.consumerErr
	}
	return s.consumer, nil
}

// fakeConsumer embeds jetstream.Consumer; only Messages is implemented.
type fakeConsumer struct {
	jetstream.Consumer
	messagesErr error
	msgs        *fakeMessages
}

func (c *fakeConsumer) Messages(...jetstream.PullMessagesOpt) (jetstream.MessagesContext, error) {
	if c.messagesErr != nil {
		return nil, c.messagesErr
	}
	return c.msgs, nil
}

// fakeMessages drives a scripted sequence of Next() results.
type fakeMessages struct {
	mu      sync.Mutex
	idx     int
	results []nextResult
	stopped bool
	stopCh  chan struct{}
}

type nextResult struct {
	msg *fakeMsg
	err error
	// block: if true, Next blocks until Stop() is called, then returns
	// ErrMsgIteratorClosed (simulates iterator drained on cancel).
	block bool
	// onNext, if set, runs synchronously when this result is delivered by
	// Next, before the value is returned to the caller.
	onNext func()
}

func newFakeMessages(results ...nextResult) *fakeMessages {
	return &fakeMessages{results: results, stopCh: make(chan struct{})}
}

func (m *fakeMessages) Next(...jetstream.NextOpt) (jetstream.Msg, error) {
	m.mu.Lock()
	if m.idx >= len(m.results) {
		m.mu.Unlock()
		<-m.stopCh
		return nil, jetstream.ErrMsgIteratorClosed
	}
	r := m.results[m.idx]
	m.idx++
	m.mu.Unlock()

	if r.onNext != nil {
		r.onNext()
	}
	if r.block {
		<-m.stopCh
		return nil, jetstream.ErrMsgIteratorClosed
	}
	if r.err != nil {
		return nil, r.err
	}
	return r.msg, nil
}

func (m *fakeMessages) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return
	}
	m.stopped = true
	close(m.stopCh)
}

func (m *fakeMessages) Drain() {}

// fakeMsg embeds jetstream.Msg; implements the methods the evaluator and
// messaging.DLQOnExhaustion touch.
type fakeMsg struct {
	jetstream.Msg
	subject string
	data    []byte

	mu      sync.Mutex
	acked   bool
	naked   bool
	metaErr error
}

func (m *fakeMsg) Subject() string { return m.subject }
func (m *fakeMsg) Data() []byte    { return m.data }

func (m *fakeMsg) Ack() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acked = true
	return nil
}

func (m *fakeMsg) Nak() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.naked = true
	return nil
}

func (m *fakeMsg) Metadata() (*jetstream.MsgMetadata, error) {
	if m.metaErr != nil {
		return nil, m.metaErr
	}
	return &jetstream.MsgMetadata{NumDelivered: 1}, nil
}

func (m *fakeMsg) wasAcked() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.acked
}

func (m *fakeMsg) wasNaked() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.naked
}

// compile-time check that fakeAnomalyRepo satisfies the interface.
var _ compliance.AnomalyRepository = (*fakeAnomalyRepo)(nil)

var _ = nats.Header{}
var _ = time.Second
