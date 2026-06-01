package settler

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll"
	plaudit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/storage"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- jetstream fakes ---------------------------------------------------------
//
// Each fake embeds the corresponding interface so the unused remainder of the
// method set is satisfied; only the methods the settler calls are overridden.

type fakeMsg struct {
	jetstream.Msg
	data    []byte
	metaErr error
	acked   *int32
}

func (m fakeMsg) Data() []byte { return m.data }
func (m fakeMsg) Ack() error {
	if m.acked != nil {
		atomic.AddInt32(m.acked, 1)
	}
	return nil
}
func (m fakeMsg) Metadata() (*jetstream.MsgMetadata, error) {
	if m.metaErr != nil {
		return nil, m.metaErr
	}
	return &jetstream.MsgMetadata{NumDelivered: 1, Stream: "PAYROLL_V1"}, nil
}
func (m fakeMsg) Nak() error      { return nil }
func (m fakeMsg) Subject() string { return "payroll.batch_locked.v1" }

// nextResult is one scripted return value of MessagesContext.Next.
type nextResult struct {
	msg jetstream.Msg
	err error
}

type fakeMsgsCtx struct {
	jetstream.MessagesContext
	mu      sync.Mutex
	script  []nextResult
	idx     int
	stopped int32
	// onNext, if set, is invoked (with the 0-based call index) before each
	// Next returns — lets a test cancel the ctx at a precise moment.
	onNext func(i int)
}

func (c *fakeMsgsCtx) Next(_ ...jetstream.NextOpt) (jetstream.Msg, error) {
	c.mu.Lock()
	i := c.idx
	c.idx++
	var r nextResult
	if i < len(c.script) {
		r = c.script[i]
	} else {
		r = nextResult{err: jetstream.ErrMsgIteratorClosed}
	}
	c.mu.Unlock()
	if c.onNext != nil {
		c.onNext(i)
	}
	return r.msg, r.err
}

func (c *fakeMsgsCtx) Stop() { atomic.AddInt32(&c.stopped, 1) }

type fakeConsumer struct {
	jetstream.Consumer
	mctx        jetstream.MessagesContext
	messagesErr error
}

func (c *fakeConsumer) Messages(_ ...jetstream.PullMessagesOpt) (jetstream.MessagesContext, error) {
	if c.messagesErr != nil {
		return nil, c.messagesErr
	}
	return c.mctx, nil
}

type fakeStream struct {
	jetstream.Stream
	cons    *fakeConsumer
	consErr error
}

func (s *fakeStream) CreateOrUpdateConsumer(_ context.Context, _ jetstream.ConsumerConfig) (jetstream.Consumer, error) {
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

// --- repo / collaborator fakes ----------------------------------------------

type fakeBatches struct {
	payroll.BatchRepository
	batch        *payroll.Batch
	err          error
	setExportErr error
}

func (f *fakeBatches) GetByID(_ context.Context, _ string) (*payroll.Batch, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.batch, nil
}

// setExportErr, when non-nil, makes the in-tx SetExportInfoTx fail.
func (f *fakeBatches) SetExportInfoTx(_ context.Context, _ pgx.Tx, _, _ string, _ time.Time) error {
	return f.setExportErr
}

type fakeEntries struct {
	payroll.EntryRepository
	entries []*payroll.Entry
	err     error
}

func (f *fakeEntries) ListByBatch(_ context.Context, _ string) ([]*payroll.Entry, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.entries, nil
}

type fakeUsers struct {
	user *PayrollUser
	err  error
}

func (f *fakeUsers) GetByID(_ context.Context, id string) (*PayrollUser, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.user != nil {
		return f.user, nil
	}
	return &PayrollUser{ID: id, PrimaryEmail: "u@test", DisplayName: "U"}, nil
}

type fakeExceptions struct {
	exs []*payroll.Exception
	err error
}

func (f *fakeExceptions) ListByBatch(_ context.Context, _ string) ([]*payroll.Exception, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.exs, nil
}

type fakeOutbox struct{ err error }

func (f *fakeOutbox) AppendTx(_ context.Context, _ pgx.Tx, _, _, _ string, _ map[string]any, _ map[string]any) error {
	return f.err
}

type fakeAudit struct{ err error }

func (f *fakeAudit) WriteTx(_ context.Context, _ pgx.Tx, _ plaudit.Entry) error { return f.err }

func sampleBatch() *payroll.Batch {
	return &payroll.Batch{
		ID:          "batch-1",
		PeriodStart: time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2027, 3, 31, 0, 0, 0, 0, time.UTC),
		Status:      payroll.BatchStatusLocked,
	}
}

// --- setupSettlerConsumer ----------------------------------------------------

func TestSetupSettlerConsumer_StreamError(t *testing.T) {
	want := errors.New("no stream")
	s := &Settler{JS: &fakeJS{streamErr: want}, Logger: quietLogger()}
	_, err := s.setupSettlerConsumer(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want wrap of %v", err, want)
	}
}

func TestSetupSettlerConsumer_CreateConsumerError(t *testing.T) {
	want := errors.New("no consumer")
	s := &Settler{JS: &fakeJS{stream: &fakeStream{consErr: want}}, Logger: quietLogger()}
	_, err := s.setupSettlerConsumer(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want wrap of %v", err, want)
	}
}

func TestSetupSettlerConsumer_OK(t *testing.T) {
	cons := &fakeConsumer{}
	s := &Settler{JS: &fakeJS{stream: &fakeStream{cons: cons}}, Logger: quietLogger()}
	got, err := s.setupSettlerConsumer(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != cons {
		t.Fatalf("got consumer %v, want %v", got, cons)
	}
}

// --- nextSettlerMsg ----------------------------------------------------------

func TestNextSettlerMsg_Success(t *testing.T) {
	s := &Settler{Logger: quietLogger()}
	m := fakeMsg{data: []byte("x")}
	it := &fakeMsgsCtx{script: []nextResult{{msg: m}}}
	msg, cont := s.nextSettlerMsg(context.Background(), it)
	if !cont || msg == nil {
		t.Fatalf("want (msg,true), got (%v,%v)", msg, cont)
	}
}

func TestNextSettlerMsg_IteratorClosed(t *testing.T) {
	s := &Settler{Logger: quietLogger()}
	it := &fakeMsgsCtx{script: []nextResult{{err: jetstream.ErrMsgIteratorClosed}}}
	msg, cont := s.nextSettlerMsg(context.Background(), it)
	if cont || msg != nil {
		t.Fatalf("want (nil,false) on closed iterator, got (%v,%v)", msg, cont)
	}
}

func TestNextSettlerMsg_CtxCancelled(t *testing.T) {
	s := &Settler{Logger: quietLogger()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	it := &fakeMsgsCtx{script: []nextResult{{err: errors.New("boom")}}}
	msg, cont := s.nextSettlerMsg(ctx, it)
	if cont || msg != nil {
		t.Fatalf("want (nil,false) when ctx already cancelled, got (%v,%v)", msg, cont)
	}
}

func TestNextSettlerMsg_TransientWarnThenRetry(t *testing.T) {
	s := &Settler{Logger: quietLogger()}
	// Non-fatal error with a live ctx: warns, waits ~500ms, returns (nil,true).
	it := &fakeMsgsCtx{script: []nextResult{{err: errors.New("transient")}}}
	start := time.Now()
	msg, cont := s.nextSettlerMsg(context.Background(), it)
	if !cont || msg != nil {
		t.Fatalf("want (nil,true) on transient error, got (%v,%v)", msg, cont)
	}
	if time.Since(start) < 400*time.Millisecond {
		t.Fatalf("expected a backoff wait, returned too fast")
	}
}

func TestNextSettlerMsg_CtxCancelDuringBackoff(t *testing.T) {
	s := &Settler{Logger: quietLogger()}
	ctx, cancel := context.WithCancel(context.Background())
	it := &fakeMsgsCtx{script: []nextResult{{err: errors.New("transient")}}}
	// Cancel shortly after Next returns so the select hits <-ctx.Done().
	it.onNext = func(int) { go func() { time.Sleep(50 * time.Millisecond); cancel() }() }
	msg, cont := s.nextSettlerMsg(ctx, it)
	if cont || msg != nil {
		t.Fatalf("want (nil,false) when ctx cancels during backoff, got (%v,%v)", msg, cont)
	}
}

// --- Run ---------------------------------------------------------------------

func TestRun_SetupConsumerError(t *testing.T) {
	want := errors.New("no stream")
	s := &Settler{JS: &fakeJS{streamErr: want}, Logger: quietLogger()}
	if err := s.Run(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Run err = %v, want wrap of %v", err, want)
	}
}

func TestRun_MessagesError(t *testing.T) {
	want := errors.New("messages boom")
	cons := &fakeConsumer{messagesErr: want}
	s := &Settler{JS: &fakeJS{stream: &fakeStream{cons: cons}}, Logger: quietLogger()}
	if err := s.Run(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Run err = %v, want wrap of %v", err, want)
	}
}

func TestRun_HandleErrorThenAckThenClose(t *testing.T) {
	var acked int32
	started := make(chan struct{})
	// 1st msg: bad JSON -> handle fails -> DLQ path (Pool nil -> Nak).
	// 2nd msg: nil with err=nil is impossible; instead 2nd entry is a good msg
	//   whose handle ALSO fails (no Batches set) but exercises the ack? No —
	//   to reach Ack we need handle success which needs a pool. So we just
	//   drive: bad msg (handle err) then iterator-closed to exit cleanly.
	bad := fakeMsg{data: []byte("not json"), acked: &acked}
	it := &fakeMsgsCtx{script: []nextResult{
		{msg: bad},
		{err: jetstream.ErrMsgIteratorClosed},
	}}
	cons := &fakeConsumer{mctx: it}
	s := &Settler{
		JS:        &fakeJS{stream: &fakeStream{cons: cons}},
		Pool:      nil, // DLQOnExhaustion tolerates nil pool (Naks).
		Logger:    quietLogger(),
		OnStarted: func() { close(started) },
	}
	done := make(chan error, 1)
	go func() { done <- s.Run(context.Background()) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("OnStarted not called")
	}
	select {
	case err := <-done:
		// iterator closed with live ctx -> nextSettlerMsg returns (nil,false)
		// -> Run returns ctx.Err() which is nil.
		if err != nil {
			t.Fatalf("Run returned %v, want nil on clean iterator close", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after iterator closed")
	}
	if atomic.LoadInt32(&it.stopped) == 0 {
		t.Fatal("expected iterator Stop to have been called")
	}
}

func TestRun_TransientNextThenClose(t *testing.T) {
	// 1st Next: transient error -> nextSettlerMsg returns (nil,true) -> Run hits
	// the `if msg == nil { continue }` branch. 2nd Next: iterator closed -> exit.
	it := &fakeMsgsCtx{script: []nextResult{
		{err: errors.New("transient")},
		{err: jetstream.ErrMsgIteratorClosed},
	}}
	cons := &fakeConsumer{mctx: it}
	s := &Settler{JS: &fakeJS{stream: &fakeStream{cons: cons}}, Logger: quietLogger()}
	done := make(chan error, 1)
	go func() { done <- s.Run(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run err = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
	}
}

func TestRun_CtxCancelExitsViaTopGuard(t *testing.T) {
	// Pre-cancelled ctx: the loop's `if ctx.Err() != nil` guard returns
	// immediately with context.Canceled. The blocking iterator's Next is never
	// reached (loop guard fires first); the Stop goroutine also fires.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	it := &blockingMsgsCtx{release: make(chan struct{})}
	cons := &fakeConsumer{mctx: it}
	s := &Settler{JS: &fakeJS{stream: &fakeStream{cons: cons}}, Logger: quietLogger()}
	err := s.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run err = %v, want context.Canceled", err)
	}
}

// blockingMsgsCtx.Next blocks until release is closed; used to prove the loop's
// ctx.Err() top-guard exits without ever calling Next.
type blockingMsgsCtx struct {
	jetstream.MessagesContext
	release chan struct{}
}

func (b *blockingMsgsCtx) Next(_ ...jetstream.NextOpt) (jetstream.Msg, error) {
	<-b.release
	return nil, jetstream.ErrMsgIteratorClosed
}
func (b *blockingMsgsCtx) Stop() {}

// --- handle (pre-tx error paths) --------------------------------------------

func TestHandle_DecodeError(t *testing.T) {
	s := &Settler{Logger: quietLogger()}
	err := s.handle(context.Background(), []byte("not json"))
	if err == nil {
		t.Fatal("want decode error")
	}
}

func TestHandle_MissingBatchID(t *testing.T) {
	s := &Settler{Logger: quietLogger()}
	err := s.handle(context.Background(), []byte(`{"batch_id":""}`))
	if err == nil || err.Error() != "event missing batch_id" {
		t.Fatalf("want missing batch_id error, got %v", err)
	}
}

func TestHandle_GetBatchError(t *testing.T) {
	s := &Settler{
		Logger:  quietLogger(),
		Batches: &fakeBatches{err: errors.New("db down")},
	}
	err := s.handle(context.Background(), []byte(`{"batch_id":"b1"}`))
	if err == nil {
		t.Fatal("want get batch error")
	}
}

func TestHandle_AlreadyExportedShortCircuits(t *testing.T) {
	b := sampleBatch()
	b.Status = payroll.BatchStatusExported
	s := &Settler{Logger: quietLogger(), Batches: &fakeBatches{batch: b}}
	if err := s.handle(context.Background(), []byte(`{"batch_id":"b1"}`)); err != nil {
		t.Fatalf("already-exported batch should short-circuit, got %v", err)
	}
}

func TestHandle_ListEntriesError(t *testing.T) {
	s := &Settler{
		Logger:  quietLogger(),
		Batches: &fakeBatches{batch: sampleBatch()},
		Entries: &fakeEntries{err: errors.New("list boom")},
	}
	err := s.handle(context.Background(), []byte(`{"batch_id":"b1"}`))
	if err == nil {
		t.Fatal("want list entries error")
	}
}

func TestHandle_RenderCSVError(t *testing.T) {
	// renderCSV fails when loadExceptionGroups' ListByBatch errors.
	s := &Settler{
		Logger:     quietLogger(),
		Batches:    &fakeBatches{batch: sampleBatch()},
		Entries:    &fakeEntries{entries: []*payroll.Entry{}},
		Exceptions: &fakeExceptions{err: errors.New("ex boom")},
	}
	err := s.handle(context.Background(), []byte(`{"batch_id":"b1"}`))
	if err == nil {
		t.Fatal("want render csv error")
	}
}

func TestHandle_UploadError(t *testing.T) {
	// Point the S3 client at a server that always 500s so PutObject fails.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	s3c, err := storage.NewS3(context.Background(), storage.S3Config{
		Endpoint:        srv.URL,
		Region:          "us-east-1",
		AccessKeyID:     "x",
		SecretAccessKey: "y",
		Bucket:          "tbite",
		UsePathStyle:    true,
	})
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	s := &Settler{
		Logger:  quietLogger(),
		Batches: &fakeBatches{batch: sampleBatch()},
		Entries: &fakeEntries{entries: []*payroll.Entry{
			{ID: "e1", UserID: "u1", AmountMinor: 100},
		}},
		Users:   &fakeUsers{},
		Storage: s3c,
	}
	err = s.handle(context.Background(), []byte(`{"batch_id":"b1"}`))
	if err == nil {
		t.Fatal("want upload error")
	}
}

// --- loadExceptionGroups -----------------------------------------------------

func TestLoadExceptionGroups_NilLister(t *testing.T) {
	s := &Settler{Logger: quietLogger()}
	excl, flagged, err := s.loadExceptionGroups(context.Background(), "b1")
	if err != nil || len(excl) != 0 || len(flagged) != 0 {
		t.Fatalf("nil lister should yield empty maps, got excl=%v flagged=%v err=%v", excl, flagged, err)
	}
}

func TestLoadExceptionGroups_Error(t *testing.T) {
	s := &Settler{Logger: quietLogger(), Exceptions: &fakeExceptions{err: errors.New("boom")}}
	_, _, err := s.loadExceptionGroups(context.Background(), "b1")
	if err == nil {
		t.Fatal("want list exceptions error")
	}
}

func TestLoadExceptionGroups_ExcludedAndFlagged(t *testing.T) {
	exs := []*payroll.Exception{
		{EntryID: "e1", Kind: payroll.ExceptionDeductionFailed, Status: payroll.ExceptionExcluded},
		{EntryID: "e2", Kind: payroll.ExceptionEmployeeDeparted, Status: payroll.ExceptionOpen},
		{EntryID: "e2", Kind: payroll.ExceptionDeductionFailed, Status: payroll.ExceptionOpen},
	}
	s := &Settler{Logger: quietLogger(), Exceptions: &fakeExceptions{exs: exs}}
	excl, flagged, err := s.loadExceptionGroups(context.Background(), "b1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !excl["e1"] {
		t.Fatal("e1 should be excluded")
	}
	if len(flagged["e2"]) != 2 {
		t.Fatalf("e2 should have 2 flags, got %v", flagged["e2"])
	}
}

// --- renderCSV / writeCSVRow -------------------------------------------------

func parseCSV(t *testing.T, b []byte) [][]string {
	t.Helper()
	if len(b) < 3 {
		t.Fatalf("csv too short: %d bytes", len(b))
	}
	rows, err := csv.NewReader(bytes.NewReader(b[3:])).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	return rows
}

func TestRenderCSV_ExcludedRowDropped(t *testing.T) {
	s := &Settler{
		Logger: quietLogger(),
		Users:  &fakeUsers{},
		Exceptions: &fakeExceptions{exs: []*payroll.Exception{
			{EntryID: "e1", Kind: payroll.ExceptionDeductionFailed, Status: payroll.ExceptionExcluded},
			{EntryID: "e2", Kind: payroll.ExceptionDeductionFailed, Status: payroll.ExceptionOpen},
		}},
	}
	entries := []*payroll.Entry{
		{ID: "e1", UserID: "u1", AmountMinor: 100},
		{ID: "e2", UserID: "u2", AmountMinor: 200, RefundedMinor: 50},
	}
	out, err := s.renderCSV(context.Background(), sampleBatch(), entries)
	if err != nil {
		t.Fatalf("renderCSV: %v", err)
	}
	rows := parseCSV(t, out)
	// header + 1 row (e1 excluded).
	if len(rows) != 2 {
		t.Fatalf("want header+1 row, got %d rows: %v", len(rows), rows)
	}
	// net column (index 7) = 200-50 = 150; exception column flagged.
	if rows[1][7] != "150" {
		t.Fatalf("net = %q, want 150", rows[1][7])
	}
	if rows[1][9] != "deduction_failed" {
		t.Fatalf("exception = %q, want deduction_failed", rows[1][9])
	}
}

func TestWriteCSVRow_UserLookupFails_PlaceholderRow(t *testing.T) {
	s := &Settler{
		Logger: quietLogger(),
		Users:  &fakeUsers{err: errors.New("no user")},
	}
	entries := []*payroll.Entry{{ID: "e1", UserID: "u1", AmountMinor: 100}}
	out, err := s.renderCSV(context.Background(), sampleBatch(), entries)
	if err != nil {
		t.Fatalf("renderCSV: %v", err)
	}
	rows := parseCSV(t, out)
	if len(rows) != 2 {
		t.Fatalf("want header+1 row, got %d", len(rows))
	}
	// Placeholder user => primary_email "?" and display_name "?".
	if rows[1][1] != "?" || rows[1][2] != "?" {
		t.Fatalf("expected placeholder row, got %v", rows[1])
	}
}

func TestWriteCSVRow_AllOptionalFieldsPopulated(t *testing.T) {
	emp, plant, dept := "E9", "F12", "RD"
	s := &Settler{
		Logger: quietLogger(),
		Users: &fakeUsers{user: &PayrollUser{
			ID: "u1", EmployeeID: &emp, PrimaryEmail: "a@b", DisplayName: "Amy",
			Plant: &plant, Department: &dept,
		}},
	}
	entries := []*payroll.Entry{{ID: "e1", UserID: "u1", AmountMinor: 300, RefundedMinor: 100}}
	out, err := s.renderCSV(context.Background(), sampleBatch(), entries)
	if err != nil {
		t.Fatalf("renderCSV: %v", err)
	}
	rows := parseCSV(t, out)
	got := rows[1]
	if got[0] != "E9" || got[3] != "F12" || got[4] != "RD" || got[5] != "300" || got[6] != "100" || got[7] != "200" {
		t.Fatalf("row = %v, want populated optional fields and net=200", got)
	}
}

// --- tx-body error paths (real pool, faked repos) ---------------------------

func setupPoolInternal(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("tbite"),
		tcpostgres.WithUsername("tbite"),
		tcpostgres.WithPassword("tbite"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("postgres container: %v", err)
	}
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("dsn: %v", err)
	}
	_, thisFile, _, _ := runtime.Caller(0)
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "..", "migrations")
	m, err := migrate.New("file://"+migrationsDir, dsn)
	if err != nil {
		t.Fatalf("migrate new: %v", err)
	}
	if err := m.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	return pool, func() {
		pool.Close()
		_ = container.Terminate(ctx)
	}
}

// okS3 returns an S3Client whose PutObject succeeds against a 200-OK httptest
// server — no MinIO container needed for the upload step.
func okS3(t *testing.T) (*storage.S3Client, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	s3c, err := storage.NewS3(context.Background(), storage.S3Config{
		Endpoint:        srv.URL,
		Region:          "us-east-1",
		AccessKeyID:     "x",
		SecretAccessKey: "y",
		Bucket:          "tbite",
		UsePathStyle:    true,
	})
	if err != nil {
		srv.Close()
		t.Fatalf("NewS3: %v", err)
	}
	return s3c, srv.Close
}

func txTestSettler(t *testing.T, pool *pgxpool.Pool, s3c *storage.S3Client) *Settler {
	return &Settler{
		Pool:    pool,
		Logger:  quietLogger(),
		Batches: &fakeBatches{batch: sampleBatch()},
		Entries: &fakeEntries{entries: []*payroll.Entry{{ID: "e1", UserID: "u1", AmountMinor: 100}}},
		Users:   &fakeUsers{},
		Storage: s3c,
	}
}

func TestHandle_SetExportInfoTxError(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPoolInternal(t)
	defer cleanup()
	s3c, s3Close := okS3(t)
	defer s3Close()

	s := txTestSettler(t, pool, s3c)
	s.Batches = &fakeBatches{batch: sampleBatch(), setExportErr: errors.New("set export boom")}
	s.Outbox = &fakeOutbox{}
	s.Audit = &fakeAudit{}

	err := s.handle(context.Background(), []byte(`{"batch_id":"b1"}`))
	if err == nil {
		t.Fatal("want SetExportInfoTx error to propagate out of BeginFunc")
	}
}

func TestHandle_OutboxAppendTxError(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPoolInternal(t)
	defer cleanup()
	s3c, s3Close := okS3(t)
	defer s3Close()

	s := txTestSettler(t, pool, s3c)
	s.Outbox = &fakeOutbox{err: errors.New("append boom")}
	s.Audit = &fakeAudit{}

	err := s.handle(context.Background(), []byte(`{"batch_id":"b1"}`))
	if err == nil {
		t.Fatal("want Outbox.AppendTx error to propagate out of BeginFunc")
	}
}

func TestHandle_HappyTxCommits(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPoolInternal(t)
	defer cleanup()
	s3c, s3Close := okS3(t)
	defer s3Close()

	s := txTestSettler(t, pool, s3c)
	s.Outbox = &fakeOutbox{}
	s.Audit = &fakeAudit{}

	if err := s.handle(context.Background(), []byte(`{"batch_id":"b1"}`)); err != nil {
		t.Fatalf("happy path should commit, got %v", err)
	}
}

// --- PgUserLookup.GetByID (real pool) ---------------------------------------

func TestPgUserLookup_GetByID(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPoolInternal(t)
	defer cleanup()
	ctx := context.Background()

	var id string
	err := pool.QueryRow(ctx, `
INSERT INTO "user" (primary_email, display_name, employee_id, plant, department, role)
VALUES ('lookup@test.com','Lookup User','E777','F1','RD','employee')
RETURNING id`).Scan(&id)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	l := NewPgUserLookup(pool)

	got, err := l.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.PrimaryEmail != "lookup@test.com" || got.EmployeeID == nil || *got.EmployeeID != "E777" {
		t.Fatalf("unexpected user: %+v", got)
	}

	// Not-found path: pgx.ErrNoRows -> wrapped "not found" error.
	_, err = l.GetByID(ctx, "00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Fatal("want not-found error for missing user")
	}

	// Generic (non-ErrNoRows) error path: a malformed UUID makes the query fail
	// at the DB level, exercising the `if err != nil` branch.
	_, err = l.GetByID(ctx, "not-a-uuid")
	if err == nil {
		t.Fatal("want query error for malformed id")
	}
}
