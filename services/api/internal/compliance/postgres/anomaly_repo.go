package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance"
)

type AnomalyRepo struct{ pool *pgxpool.Pool }

func NewAnomalyRepo(p *pgxpool.Pool) *AnomalyRepo { return &AnomalyRepo{pool: p} }

const anomalyCols = `id, kind, target_kind, target_id, severity, status, payload, evidence_uri,
       triaged_at, triaged_by, closed_at, closed_by, notes, created_at, updated_at`

// Open inserts a new open anomaly or upserts the existing open row matching
// (kind, target_kind, target_id) via the partial unique index. After the call,
// the receiver is populated with id/created_at/updated_at from the DB.
func (r *AnomalyRepo) Open(ctx context.Context, a *compliance.Anomaly) error {
	severity := a.Severity
	if severity == "" {
		severity = compliance.SeverityMedium
	}
	payload := a.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal anomaly payload: %w", err)
	}
	evidence := a.EvidenceURI
	if evidence == nil {
		evidence = []string{}
	}
	err = r.pool.QueryRow(ctx, `
INSERT INTO anomaly_alert (kind, target_kind, target_id, severity, status, payload, evidence_uri)
VALUES ($1, $2, $3, $4::anomaly_severity, 'open', $5::jsonb, $6)
ON CONFLICT (kind, target_kind, target_id) WHERE status='open' DO UPDATE
SET severity = EXCLUDED.severity,
    payload = EXCLUDED.payload,
    evidence_uri = EXCLUDED.evidence_uri,
    updated_at = now()
RETURNING id, created_at, updated_at`,
		a.Kind, a.TargetKind, a.TargetID, string(severity), payloadJSON, evidence,
	).Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return fmt.Errorf("open anomaly: %w", err)
	}
	a.Severity = severity
	a.Status = compliance.AnomalyOpen
	a.Payload = payload
	a.EvidenceURI = evidence
	return nil
}

func (r *AnomalyRepo) GetByID(ctx context.Context, id string) (*compliance.Anomaly, error) {
	var a compliance.Anomaly
	var severity, status string
	var payloadJSON []byte
	err := r.pool.QueryRow(ctx, `SELECT `+anomalyCols+` FROM anomaly_alert WHERE id=$1`, id).Scan(
		&a.ID, &a.Kind, &a.TargetKind, &a.TargetID, &severity, &status,
		&payloadJSON, &a.EvidenceURI,
		&a.TriagedAt, &a.TriagedBy, &a.ClosedAt, &a.ClosedBy, &a.Notes,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, compliance.ErrAnomalyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan anomaly: %w", err)
	}
	a.Severity = compliance.AnomalySeverity(severity)
	a.Status = compliance.AnomalyStatus(status)
	if len(payloadJSON) > 0 {
		_ = json.Unmarshal(payloadJSON, &a.Payload)
	}
	return &a, nil
}

func (r *AnomalyRepo) List(ctx context.Context, statuses []compliance.AnomalyStatus, severities []compliance.AnomalySeverity) ([]*compliance.Anomaly, error) {
	var b strings.Builder
	args := []any{}
	clauses := []string{}
	if len(statuses) > 0 {
		ph := make([]string, len(statuses))
		for i, s := range statuses {
			args = append(args, string(s))
			ph[i] = fmt.Sprintf("$%d::anomaly_status", len(args))
		}
		clauses = append(clauses, "status IN ("+strings.Join(ph, ",")+")")
	}
	if len(severities) > 0 {
		ph := make([]string, len(severities))
		for i, s := range severities {
			args = append(args, string(s))
			ph[i] = fmt.Sprintf("$%d::anomaly_severity", len(args))
		}
		clauses = append(clauses, "severity IN ("+strings.Join(ph, ",")+")")
	}
	b.WriteString(`SELECT ` + anomalyCols + ` FROM anomaly_alert`)
	if len(clauses) > 0 {
		b.WriteString(" WHERE " + strings.Join(clauses, " AND "))
	}
	b.WriteString(" ORDER BY created_at DESC")

	rows, err := r.pool.Query(ctx, b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*compliance.Anomaly
	for rows.Next() {
		var a compliance.Anomaly
		var severity, status string
		var payloadJSON []byte
		if err := rows.Scan(&a.ID, &a.Kind, &a.TargetKind, &a.TargetID, &severity, &status,
			&payloadJSON, &a.EvidenceURI,
			&a.TriagedAt, &a.TriagedBy, &a.ClosedAt, &a.ClosedBy, &a.Notes,
			&a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		a.Severity = compliance.AnomalySeverity(severity)
		a.Status = compliance.AnomalyStatus(status)
		if len(payloadJSON) > 0 {
			_ = json.Unmarshal(payloadJSON, &a.Payload)
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

func (r *AnomalyRepo) Triage(ctx context.Context, id, by, notes string) error {
	return triageAnomaly(ctx, r.pool, id, by, notes)
}

// TriageTx is the transactional variant of Triage.
func (r *AnomalyRepo) TriageTx(ctx context.Context, tx pgx.Tx, id, by, notes string) error {
	return triageAnomaly(ctx, tx, id, by, notes)
}

func triageAnomaly(ctx context.Context, q pgxQuerier, id, by, notes string) error {
	tag, err := q.Exec(ctx, `
UPDATE anomaly_alert SET status='triaged', triaged_at=now(), triaged_by=$2, notes=$3, updated_at=now()
WHERE id=$1 AND status='open'`, id, by, notes)
	if err != nil {
		return fmt.Errorf("triage anomaly: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return compliance.ErrInvalidStatus
	}
	return nil
}

func (r *AnomalyRepo) Close(ctx context.Context, id, by, notes string) error {
	return closeAnomaly(ctx, r.pool, id, by, notes)
}

// CloseTx is the transactional variant of Close.
func (r *AnomalyRepo) CloseTx(ctx context.Context, tx pgx.Tx, id, by, notes string) error {
	return closeAnomaly(ctx, tx, id, by, notes)
}

func closeAnomaly(ctx context.Context, q pgxQuerier, id, by, notes string) error {
	tag, err := q.Exec(ctx, `
UPDATE anomaly_alert SET status='closed', closed_at=now(), closed_by=$2,
notes = CASE WHEN $3 = '' THEN notes ELSE $3 END, updated_at=now()
WHERE id=$1 AND status IN ('open', 'triaged')`, id, by, notes)
	if err != nil {
		return fmt.Errorf("close anomaly: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return compliance.ErrInvalidStatus
	}
	return nil
}
