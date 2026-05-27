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
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/takalawang/corporate-catering-system/services/api/internal/payroll"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/storage"
)

// UserLookup resolves a user_id to the subset of "user" columns the HR CSV
// needs. Kept as an interface so tests can stub it without a real Postgres.
type UserLookup interface {
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

// AuditWriter is the shape Settler needs to record audit events inside the
// same tx that persists export info.
type AuditWriter interface {
	WriteTx(ctx context.Context, tx pgx.Tx, actorID, actorRole *string, action, targetKind, targetID string, payload map[string]any, requestID string) error
}

// OutboxAppender appends an outbox row inside the export-info tx. Letting the
// existing relay publish it keeps export_ready delivery exactly-once.
type OutboxAppender interface {
	AppendTx(ctx context.Context, tx pgx.Tx, aggregateType, aggregateID, subject string, payload map[string]any, headers map[string]any) error
}

// ExceptionLister loads a batch's payroll exceptions so the CSV can flag
// affected entries and drop the ones a welfare admin marked excluded.
type ExceptionLister interface {
	ListByBatch(ctx context.Context, batchID string) ([]*payroll.Exception, error)
}

// Settler wires together the NATS consumer, repos, storage, and audit/outbox
// dependencies needed to process payroll.batch_locked.v1 events.
type Settler struct {
	JS         jetstream.JetStream
	Pool       *pgxpool.Pool
	Batches    payroll.BatchRepository
	Entries    payroll.EntryRepository
	Users      UserLookup
	Exceptions ExceptionLister
	Storage    *storage.S3Client
	Logger     *slog.Logger
	Audit      AuditWriter
	Outbox     OutboxAppender
}

// Run blocks until ctx is cancelled or an unrecoverable error occurs. The
// consumer is a durable pull consumer named "payroll-settler" so it survives
// worker restarts and resumes from the last acked sequence.
func (s *Settler) Run(ctx context.Context) error {
	stream, err := s.JS.Stream(ctx, "PAYROLL_V1")
	if err != nil {
		return fmt.Errorf("get stream: %w", err)
	}

	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          "payroll-settler",
		Durable:       "payroll-settler",
		FilterSubject: "payroll.batch_locked.v1",
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    5,
	})
	if err != nil {
		return fmt.Errorf("create consumer: %w", err)
	}

	s.Logger.Info("settler started, waiting for batch_locked events")

	it, err := cons.Messages()
	if err != nil {
		return fmt.Errorf("messages: %w", err)
	}
	defer it.Stop()

	// it.Next blocks until a message arrives or the iterator is stopped. We
	// spawn a goroutine to Stop() it on ctx cancellation so Run returns
	// promptly during shutdown.
	go func() {
		<-ctx.Done()
		it.Stop()
	}()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg, err := it.Next()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, jetstream.ErrMsgIteratorClosed) {
				return ctx.Err()
			}
			s.Logger.Warn("consumer next", "err", err)
			// Brief backoff before re-trying — we don't want to busy-loop on a
			// transient NATS hiccup.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}
		if err := s.handle(ctx, msg.Data()); err != nil {
			s.Logger.Error("handle event", "err", err)
			_ = msg.Nak()
			continue
		}
		_ = msg.Ack()
	}
}

// handle processes a single payroll.batch_locked.v1 event end-to-end:
//  1. Load batch + entries; short-circuit if already exported (idempotent).
//  2. Render UTF-8 BOM + CSV with one row per entry.
//  3. Upload to S3 at payroll/<batch_id>.csv.
//  4. In a single tx: SetExportInfo + outbox payroll.export_ready.v1 + audit.
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
		return s.Audit.WriteTx(ctx, tx, nil, &sysRole, "payroll.export", "payroll_batch", batch.ID, payload, "")
	})
}

// renderCSV produces the bytes uploaded to S3. Format:
//   - UTF-8 BOM (so Excel auto-detects encoding and renders 中文 correctly)
//   - Header row
//   - One row per entry, with user details resolved via Users.GetByID
//
// The trailing `exception` column flags entries that carry a payroll
// exception (departed employee / failed deduction). Entries whose exception
// a welfare admin marked `excluded` are dropped from the file entirely — HR
// must not deduct them this period.
//
// A failed user lookup logs a warning and writes a "?" placeholder row rather
// than aborting the whole batch — HR can still reconcile from order_ids.
func (s *Settler) renderCSV(ctx context.Context, batch *payroll.Batch, entries []*payroll.Entry) ([]byte, error) {
	var buf bytes.Buffer
	// UTF-8 BOM: bytes 0xEF 0xBB 0xBF.
	buf.WriteByte(0xEF)
	buf.WriteByte(0xBB)
	buf.WriteByte(0xBF)

	// Group the batch's exceptions by entry: excluded -> drop the row,
	// otherwise surface the kinds in the exception column.
	excluded := map[string]bool{}
	flagged := map[string][]string{}
	if s.Exceptions != nil {
		exs, err := s.Exceptions.ListByBatch(ctx, batch.ID)
		if err != nil {
			return nil, fmt.Errorf("list exceptions: %w", err)
		}
		for _, ex := range exs {
			if ex.Status == payroll.ExceptionExcluded {
				excluded[ex.EntryID] = true
			}
			flagged[ex.EntryID] = append(flagged[ex.EntryID], string(ex.Kind))
		}
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
		if err := w.Write([]string{
			empID,
			u.PrimaryEmail,
			u.DisplayName,
			plant,
			dept,
			fmt.Sprintf("%d", e.AmountMinor),
			fmt.Sprintf("%d", e.RefundedMinor),
			fmt.Sprintf("%d", e.AmountMinor-e.RefundedMinor),
			period,
			strings.Join(flagged[e.ID], ";"),
		}); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
