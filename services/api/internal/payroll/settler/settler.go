// Package settler implements the payroll-settler worker. It subscribes to
// `payroll.batch_locked.v1` events on the PAYROLL_V1 JetStream stream,
// generates the HR-friendly CSV (UTF-8 BOM so Excel renders 中文 correctly),
// uploads it to object storage, marks the batch as exported, and emits
// `payroll.export_ready.v1` so downstream consumers (notifications etc.)
// can react.
package settler

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	plaudit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/messaging"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/storage"
)

const consumerName = "payroll-settler"

// UserGetter resolves a user_id to the subset of "user" columns the HR CSV needs.
type UserGetter interface {
	GetByID(ctx context.Context, id string) (*PayrollUser, error)
}

// PayrollUser is the projection used for HR CSV rows. Pointers represent
// nullable columns (employee_id / plant / department).
type PayrollUser struct {
	ID           string
	EmployeeID   *string
	PrimaryEmail string
	DisplayName  string
	Plant        *string
	Department   *string
}

// AuditWriter records audit events inside the export-info tx.
type AuditWriter interface {
	WriteTx(ctx context.Context, tx pgx.Tx, e plaudit.Entry) error
}

// OutboxAppender appends an outbox row inside the export-info tx. Reusing the
// outbox relay keeps export_ready delivery exactly-once.
type OutboxAppender interface {
	AppendTx(ctx context.Context, tx pgx.Tx, aggregateType, aggregateID, subject string, payload map[string]any, headers map[string]any) error
}

// ExceptionLister loads a batch's payroll exceptions so the CSV can flag
// affected entries and drop the ones a welfare admin marked excluded.
type ExceptionLister interface {
	ListByBatch(ctx context.Context, batchID string) ([]*payroll.Exception, error)
}

// Settler processes payroll.batch_locked.v1 events: render CSV, upload, mark
// batch exported, emit payroll.export_ready.v1.
type Settler struct {
	JS         jetstream.JetStream
	Pool       *pgxpool.Pool
	Batches    payroll.BatchRepository
	Entries    payroll.EntryRepository
	Users      UserGetter
	Exceptions ExceptionLister
	Storage    *storage.S3Client
	Logger     *slog.Logger
	Audit      AuditWriter
	Outbox     OutboxAppender
	OnStarted  func()
}

// setupSettlerConsumer resolves the durable payroll-settler consumer.
func (s *Settler) setupSettlerConsumer(ctx context.Context) (jetstream.Consumer, error) {
	stream, err := s.JS.Stream(ctx, "PAYROLL_V1")
	if err != nil {
		return nil, fmt.Errorf("get stream: %w", err)
	}
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          consumerName,
		Durable:       consumerName,
		FilterSubject: "payroll.batch_locked.v1",
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    5,
	})
	if err != nil {
		return nil, fmt.Errorf("create consumer: %w", err)
	}
	return cons, nil
}

// nextSettlerMsg returns (msg, true) on success or (nil, shouldExit) when the
// iterator closes / ctx cancels.
func (s *Settler) nextSettlerMsg(ctx context.Context, it jetstream.MessagesContext) (jetstream.Msg, bool) {
	msg, err := it.Next()
	if err == nil {
		return msg, true
	}
	if ctx.Err() != nil || errors.Is(err, jetstream.ErrMsgIteratorClosed) {
		return nil, false
	}
	s.Logger.Warn("consumer next", "err", err)
	select {
	case <-ctx.Done():
		return nil, false
	case <-time.After(500 * time.Millisecond):
	}
	return nil, true
}

// Run blocks until ctx is cancelled or an unrecoverable error occurs. The
// consumer is a durable pull consumer named "payroll-settler" so it survives
// worker restarts and resumes from the last acked sequence.
func (s *Settler) Run(ctx context.Context) error {
	cons, err := s.setupSettlerConsumer(ctx)
	if err != nil {
		return err
	}
	s.Logger.Info("settler started, waiting for batch_locked events")
	it, err := cons.Messages()
	if err != nil {
		return fmt.Errorf("messages: %w", err)
	}
	defer it.Stop()
	if s.OnStarted != nil {
		s.OnStarted()
	}
	go func() {
		<-ctx.Done()
		it.Stop()
	}()
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg, cont := s.nextSettlerMsg(ctx, it)
		if !cont {
			return ctx.Err()
		}
		if msg == nil {
			continue
		}
		if err := s.handle(ctx, msg.Data()); err != nil {
			s.Logger.Error("handle event", "err", err)
			messaging.DLQOnExhaustion(ctx, msg, s.Pool, consumerName, 5, err)
			continue
		}
		_ = msg.Ack()
	}
}

// handle processes one payroll.batch_locked.v1 event end-to-end. Idempotent:
// already-exported batches short-circuit before render/upload.
func (s *Settler) handle(ctx context.Context, data []byte) error {
	var ev struct {
		BatchID     string `json:"batch_id"`
		PeriodStart string `json:"period_start"`
		PeriodEnd   string `json:"period_end"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return fmt.Errorf("decode event: %w", err)
	}
	if ev.BatchID == "" {
		return fmt.Errorf("event missing batch_id")
	}
	s.Logger.Info("processing batch", "batch_id", ev.BatchID)

	batch, err := s.Batches.GetByID(ctx, ev.BatchID)
	if err != nil {
		return fmt.Errorf("get batch: %w", err)
	}
	if batch.Status == payroll.BatchStatusExported {
		s.Logger.Info("batch already exported, skipping", "batch_id", ev.BatchID)
		return nil
	}

	entries, err := s.Entries.ListByBatch(ctx, ev.BatchID)
	if err != nil {
		return fmt.Errorf("list entries: %w", err)
	}

	csvBytes, err := s.renderCSV(ctx, batch, entries)
	if err != nil {
		return fmt.Errorf("render csv: %w", err)
	}

	key := fmt.Sprintf("payroll/%s.csv", batch.ID)
	uri, err := s.Storage.PutObject(ctx, key, bytes.NewReader(csvBytes), "text/csv; charset=utf-8")
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	s.Logger.Info("csv uploaded", "uri", uri, "bytes", len(csvBytes), "entries", len(entries))

	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		now := time.Now().UTC()
		if err := s.Batches.SetExportInfoTx(ctx, tx, batch.ID, uri, now); err != nil {
			return err
		}
		sysRole := "welfare_admin"
		payload := map[string]any{
			"batch_id":   batch.ID,
			"export_uri": uri,
			"entries":    len(entries),
		}
		if err := s.Outbox.AppendTx(ctx, tx, "payroll_batch", batch.ID, "payroll.export_ready.v1", payload, map[string]any{}); err != nil {
			return err
		}
		return s.Audit.WriteTx(ctx, tx, plaudit.Entry{ActorID: nil, ActorRole: &sysRole, Action: "payroll.export", TargetKind: "payroll_batch", TargetID: batch.ID, Payload: payload, RequestID: ""})
	})
}

// loadExceptionGroups returns (excluded entry IDs, kinds per entry ID) for the
// batch's payroll exceptions.
func (s *Settler) loadExceptionGroups(ctx context.Context, batchID string) (map[string]bool, map[string][]string, error) {
	excluded := map[string]bool{}
	flagged := map[string][]string{}
	if s.Exceptions == nil {
		return excluded, flagged, nil
	}
	exs, err := s.Exceptions.ListByBatch(ctx, batchID)
	if err != nil {
		return nil, nil, fmt.Errorf("list exceptions: %w", err)
	}
	for _, ex := range exs {
		if ex.Status == payroll.ExceptionExcluded {
			excluded[ex.EntryID] = true
		}
		flagged[ex.EntryID] = append(flagged[ex.EntryID], string(ex.Kind))
	}
	return excluded, flagged, nil
}

// writeCSVRow writes one HR-CSV row for entry e, looking up the user (with a
// "?" placeholder fallback) and flattening the optional fields.
func (s *Settler) writeCSVRow(ctx context.Context, w *csv.Writer, e *payroll.Entry, period string, flagged []string) error {
	u, err := s.Users.GetByID(ctx, e.UserID)
	if err != nil {
		s.Logger.Warn("user lookup failed", "user_id", e.UserID, "err", err)
		u = &PayrollUser{ID: e.UserID, PrimaryEmail: "?", DisplayName: "?"}
	}
	empID := ""
	if u.EmployeeID != nil {
		empID = *u.EmployeeID
	}
	plant := ""
	if u.Plant != nil {
		plant = *u.Plant
	}
	dept := ""
	if u.Department != nil {
		dept = *u.Department
	}
	return w.Write([]string{
		empID,
		u.PrimaryEmail,
		u.DisplayName,
		plant,
		dept,
		fmt.Sprintf("%d", e.AmountMinor),
		fmt.Sprintf("%d", e.RefundedMinor),
		fmt.Sprintf("%d", e.AmountMinor-e.RefundedMinor),
		period,
		strings.Join(flagged, ";"),
	})
}

// renderCSV produces the bytes uploaded to S3: UTF-8 BOM (so Excel renders 中文)
// + header + one row per entry. The `exception` column flags entries with a
// payroll exception; rows marked `excluded` by a welfare admin are dropped
// entirely — HR must not deduct them this period. A failed user lookup writes
// a "?" placeholder row rather than aborting the batch.
func (s *Settler) renderCSV(ctx context.Context, batch *payroll.Batch, entries []*payroll.Entry) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte(0xEF)
	buf.WriteByte(0xBB)
	buf.WriteByte(0xBF)

	excluded, flagged, err := s.loadExceptionGroups(ctx, batch.ID)
	if err != nil {
		return nil, err
	}
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{
		"employee_id", "primary_email", "display_name", "plant", "department",
		"amount_ntd", "refunded_ntd", "net_ntd", "batch_period", "exception",
	}); err != nil {
		return nil, err
	}
	period := fmt.Sprintf("%s ~ %s",
		batch.PeriodStart.Format("2006-01-02"),
		batch.PeriodEnd.Format("2006-01-02"),
	)
	for _, e := range entries {
		if excluded[e.ID] {
			continue
		}
		if err := s.writeCSVRow(ctx, w, e, period, flagged[e.ID]); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
