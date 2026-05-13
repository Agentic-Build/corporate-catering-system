// Package dlqhttp exposes the synthetic DLQ admin surface:
//
//	GET  /api/admin/dlq              - list pending DLQ messages
//	POST /api/admin/dlq/{id}/replay  - re-publish payload to original subject + stamp replayed_at
//	POST /api/admin/dlq/{id}/resolve - mark resolved (drop) without replay
//
// All routes require the welfare_admin role.
package dlqhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/takalawang/corporate-catering-system/services/api/internal/dlq"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
)

// API wires the DLQ repository and (optionally) a JetStream client used by
// /replay. JS may be nil for binaries that don't talk to NATS — in that case
// /replay returns 503.
type API struct {
	Repo dlq.Repository
	JS   jetstream.JetStream
}

// ----- DTOs -----

type messageDTO struct {
	ID             string         `json:"id"`
	SourceStream   string         `json:"source_stream"`
	SourceSubject  string         `json:"source_subject"`
	SourceConsumer string         `json:"source_consumer"`
	Payload        map[string]any `json:"payload"`
	Headers        map[string]any `json:"headers"`
	LastError      string         `json:"last_error"`
	FirstSeenAt    string         `json:"first_seen_at"`
	ReplayedAt     *string        `json:"replayed_at,omitempty"`
	ResolvedAt     *string        `json:"resolved_at,omitempty"`
	ResolvedNotes  string         `json:"resolved_notes,omitempty"`
}

type listInput struct {
	Stream string `query:"stream"`
	Limit  int    `query:"limit"`
}

type listOutput struct {
	Body struct {
		Items []messageDTO `json:"items"`
	}
}

type idInput struct {
	ID string `path:"id" format:"uuid"`
}

type replayOutput struct {
	Body struct {
		Replayed bool `json:"replayed"`
	}
}

type resolveInput struct {
	ID   string `path:"id" format:"uuid"`
	Body struct {
		Notes string `json:"notes"`
	}
}

// ----- Registration -----

// Register wires the DLQ operations onto the given huma API.
func (a *API) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listDLQ",
		Method:      http.MethodGet,
		Path:        "/api/admin/dlq",
		Summary:     "List pending DLQ messages",
		Tags:        []string{"admin", "dlq"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.list)
	huma.Register(api, huma.Operation{
		OperationID: "replayDLQMessage",
		Method:      http.MethodPost,
		Path:        "/api/admin/dlq/{id}/replay",
		Summary:     "Re-publish a DLQ message to its original subject",
		Tags:        []string{"admin", "dlq"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.replay)
	huma.Register(api, huma.Operation{
		OperationID:   "resolveDLQMessage",
		Method:        http.MethodPost,
		Path:          "/api/admin/dlq/{id}/resolve",
		Summary:       "Mark a DLQ message resolved without replay",
		Tags:          []string{"admin", "dlq"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.resolve)
}

// ----- Auth -----

func (a *API) requireAdmin(ctx context.Context) (*identity.User, error) {
	u, ok := idhttp.UserFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	if u.Role != identity.RoleWelfareAdmin {
		return nil, huma.Error403Forbidden("admin role required")
	}
	return u, nil
}

// ----- Handlers -----

func (a *API) list(ctx context.Context, in *listInput) (*listOutput, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	limit := in.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	msgs, err := a.Repo.ListPending(ctx, in.Stream, limit)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listOutput
	resp.Body.Items = make([]messageDTO, 0, len(msgs))
	for _, m := range msgs {
		resp.Body.Items = append(resp.Body.Items, toDTO(m))
	}
	return &resp, nil
}

func (a *API) replay(ctx context.Context, in *idInput) (*replayOutput, error) {
	u, err := a.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if a.JS == nil {
		return nil, huma.Error503ServiceUnavailable("NATS not wired in this role")
	}
	msg, err := a.Repo.GetByID(ctx, in.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	if msg.ReplayedAt != nil || msg.ResolvedAt != nil {
		return nil, huma.Error409Conflict("already replayed or resolved")
	}
	payloadBytes, err := json.Marshal(msg.Payload)
	if err != nil {
		return nil, huma.Error500InternalServerError("marshal payload", err)
	}
	if _, err := a.JS.Publish(ctx, msg.SourceSubject, payloadBytes); err != nil {
		return nil, huma.Error500InternalServerError("publish failed", err)
	}
	if err := a.Repo.MarkReplayed(ctx, msg.ID, u.ID); err != nil {
		return nil, mapErr(err)
	}
	var resp replayOutput
	resp.Body.Replayed = true
	return &resp, nil
}

func (a *API) resolve(ctx context.Context, in *resolveInput) (*struct{}, error) {
	u, err := a.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Repo.MarkResolved(ctx, in.ID, u.ID, in.Body.Notes); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

// ----- Helpers -----

func toDTO(m *dlq.Message) messageDTO {
	payload := m.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	headers := m.Headers
	if headers == nil {
		headers = map[string]any{}
	}
	out := messageDTO{
		ID:             m.ID,
		SourceStream:   m.SourceStream,
		SourceSubject:  m.SourceSubject,
		SourceConsumer: m.SourceConsumer,
		Payload:        payload,
		Headers:        headers,
		LastError:      m.LastError,
		FirstSeenAt:    m.FirstSeenAt.UTC().Format(time.RFC3339),
		ResolvedNotes:  m.ResolvedNotes,
	}
	if m.ReplayedAt != nil {
		s := m.ReplayedAt.UTC().Format(time.RFC3339)
		out.ReplayedAt = &s
	}
	if m.ResolvedAt != nil {
		s := m.ResolvedAt.UTC().Format(time.RFC3339)
		out.ResolvedAt = &s
	}
	return out
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, dlq.ErrMessageNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, dlq.ErrAlreadyResolved):
		return huma.Error409Conflict(err.Error())
	}
	return huma.Error500InternalServerError("internal", err)
}
