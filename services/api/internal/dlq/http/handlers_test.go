package dlqhttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/dlq"
	dlqhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/dlq/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
)

const (
	msgID    = "11111111-1111-1111-1111-111111111111"
	otherMsg = "22222222-2222-2222-2222-222222222222"
)

// === Fakes ===

// fakeRepo is an in-memory dlq.Repository. Errors can be forced per-method.
type fakeRepo struct {
	byID map[string]*dlq.Message

	listErr     error
	getErr      error
	replayErr   error
	resolveErr  error
	replayCalls []string // ids passed to MarkReplayed
}

func newFakeRepo() *fakeRepo { return &fakeRepo{byID: map[string]*dlq.Message{}} }

func (r *fakeRepo) Write(context.Context, *dlq.Message) error { return nil }

func (r *fakeRepo) GetByID(_ context.Context, id string) (*dlq.Message, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	if m, ok := r.byID[id]; ok {
		clone := *m
		return &clone, nil
	}
	return nil, dlq.ErrMessageNotFound
}

func (r *fakeRepo) ListPending(_ context.Context, stream string, limit int) ([]*dlq.Message, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	out := make([]*dlq.Message, 0, len(r.byID))
	for _, m := range r.byID {
		if m.ReplayedAt != nil || m.ResolvedAt != nil {
			continue
		}
		if stream != "" && m.SourceStream != stream {
			continue
		}
		clone := *m
		out = append(out, &clone)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (r *fakeRepo) MarkReplayed(_ context.Context, id, _ string) error {
	r.replayCalls = append(r.replayCalls, id)
	if r.replayErr != nil {
		return r.replayErr
	}
	return nil
}

func (r *fakeRepo) MarkResolved(_ context.Context, id, _, _ string) error {
	if r.resolveErr != nil {
		return r.resolveErr
	}
	if _, ok := r.byID[id]; !ok {
		return dlq.ErrMessageNotFound
	}
	return nil
}

func (r *fakeRepo) seed(m *dlq.Message) {
	clone := *m
	r.byID[m.ID] = &clone
}

// fakeJS embeds jetstream.JetStream so the (nil) embedded interface satisfies
// every method; only Publish is overridden for the tests.
type fakeJS struct {
	jetstream.JetStream
	pubErr   error
	subjects []string
}

func (j *fakeJS) Publish(_ context.Context, subject string, _ []byte, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	j.subjects = append(j.subjects, subject)
	if j.pubErr != nil {
		return nil, j.pubErr
	}
	return &jetstream.PubAck{Stream: "S", Sequence: 1}, nil
}

// === Harness ===

func adminUser() *identity.User {
	return &identity.User{ID: "admin-1", Role: identity.RoleWelfareAdmin}
}

// buildHandler wires the DLQ API onto a chi router. When user != nil a
// middleware injects it into the request context exactly like AuthMiddleware
// does. When js != nil it is wired as the JetStream client.
func buildHandler(t *testing.T, user *identity.User, js jetstream.JetStream) (*httptest.Server, *fakeRepo) {
	t.Helper()
	repo := newFakeRepo()
	api := &dlqhttp.API{Repo: repo, JS: js}

	r := chi.NewRouter()
	if user != nil {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(idhttp.ContextWithUser(req.Context(), user)))
			})
		})
	}
	h := humachi.New(r, huma.DefaultConfig("test", "0.0.0"))
	api.Register(h)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, repo
}

func do(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// === list: auth ===

func TestList_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/dlq", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestList_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, &identity.User{ID: "e-1", Role: identity.RoleEmployee}, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/dlq", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// === list: happy / filters / errors ===

func TestList_OK(t *testing.T) {
	srv, repo := buildHandler(t, adminUser(), nil)
	seen := time.Date(2026, 5, 14, 8, 30, 0, 0, time.UTC)
	repo.seed(&dlq.Message{
		ID:             msgID,
		SourceStream:   "ORDERS",
		SourceSubject:  "orders.created",
		SourceConsumer: "projector",
		Payload:        map[string]any{"order_id": "abc"},
		Headers:        map[string]any{"Nats-Msg-Id": "x"},
		LastError:      "boom",
		FirstSeenAt:    seen,
	})

	resp := do(t, http.MethodGet, srv.URL+"/api/admin/dlq", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID            string         `json:"id"`
			SourceStream  string         `json:"source_stream"`
			SourceSubject string         `json:"source_subject"`
			Payload       map[string]any `json:"payload"`
			LastError     string         `json:"last_error"`
			FirstSeenAt   string         `json:"first_seen_at"`
			ReplayedAt    *string        `json:"replayed_at"`
			ResolvedAt    *string        `json:"resolved_at"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, msgID, out.Items[0].ID)
	assert.Equal(t, "ORDERS", out.Items[0].SourceStream)
	assert.Equal(t, "orders.created", out.Items[0].SourceSubject)
	assert.Equal(t, "abc", out.Items[0].Payload["order_id"])
	assert.Equal(t, "boom", out.Items[0].LastError)
	assert.Equal(t, "2026-05-14T08:30:00Z", out.Items[0].FirstSeenAt)
	assert.Nil(t, out.Items[0].ReplayedAt)
	assert.Nil(t, out.Items[0].ResolvedAt)
}

func TestList_Empty(t *testing.T) {
	srv, _ := buildHandler(t, adminUser(), nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/dlq", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []any `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Empty(t, out.Items)
}

func TestList_StreamFilter(t *testing.T) {
	srv, repo := buildHandler(t, adminUser(), nil)
	repo.seed(&dlq.Message{ID: msgID, SourceStream: "ORDERS", SourceSubject: "orders.created", FirstSeenAt: time.Now()})
	repo.seed(&dlq.Message{ID: otherMsg, SourceStream: "PAYMENTS", SourceSubject: "payments.charged", FirstSeenAt: time.Now()})

	resp := do(t, http.MethodGet, srv.URL+"/api/admin/dlq?stream=PAYMENTS", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, otherMsg, out.Items[0].ID)
}

// limit clamping: a limit > 200 falls back to the default (100) and is accepted.
func TestList_LimitClamped(t *testing.T) {
	srv, _ := buildHandler(t, adminUser(), nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/dlq?limit=9999", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestList_RepoError_500(t *testing.T) {
	srv, repo := buildHandler(t, adminUser(), nil)
	repo.listErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/dlq", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// === replay ===

func TestReplay_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil, &fakeJS{})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/dlq/"+msgID+"/replay", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestReplay_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, &identity.User{ID: "e-1", Role: identity.RoleEmployee}, &fakeJS{})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/dlq/"+msgID+"/replay", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestReplay_BadUUID_422(t *testing.T) {
	srv, _ := buildHandler(t, adminUser(), &fakeJS{})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/dlq/not-a-uuid/replay", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestReplay_NoJetStream_503(t *testing.T) {
	srv, _ := buildHandler(t, adminUser(), nil) // JS not wired
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/dlq/"+msgID+"/replay", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestReplay_NotFound_404(t *testing.T) {
	srv, _ := buildHandler(t, adminUser(), &fakeJS{}) // message not seeded
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/dlq/"+msgID+"/replay", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestReplay_AlreadyReplayed_409(t *testing.T) {
	srv, repo := buildHandler(t, adminUser(), &fakeJS{})
	now := time.Now()
	repo.seed(&dlq.Message{ID: msgID, SourceSubject: "orders.created", FirstSeenAt: now, ReplayedAt: &now})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/dlq/"+msgID+"/replay", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestReplay_AlreadyResolved_409(t *testing.T) {
	srv, repo := buildHandler(t, adminUser(), &fakeJS{})
	now := time.Now()
	repo.seed(&dlq.Message{ID: msgID, SourceSubject: "orders.created", FirstSeenAt: now, ResolvedAt: &now})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/dlq/"+msgID+"/replay", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestReplay_OK(t *testing.T) {
	js := &fakeJS{}
	srv, repo := buildHandler(t, adminUser(), js)
	repo.seed(&dlq.Message{
		ID:            msgID,
		SourceSubject: "orders.created",
		Payload:       map[string]any{"order_id": "abc"},
		FirstSeenAt:   time.Now(),
	})

	resp := do(t, http.MethodPost, srv.URL+"/api/admin/dlq/"+msgID+"/replay", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Replayed bool `json:"replayed"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.True(t, out.Replayed)
	assert.Equal(t, []string{"orders.created"}, js.subjects, "payload published to original subject")
	assert.Equal(t, []string{msgID}, repo.replayCalls, "row stamped replayed")
}

func TestReplay_PublishError_500(t *testing.T) {
	js := &fakeJS{pubErr: errors.New("nats down")}
	srv, repo := buildHandler(t, adminUser(), js)
	repo.seed(&dlq.Message{ID: msgID, SourceSubject: "orders.created", FirstSeenAt: time.Now()})

	resp := do(t, http.MethodPost, srv.URL+"/api/admin/dlq/"+msgID+"/replay", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.Empty(t, repo.replayCalls, "row not stamped when publish fails")
}

func TestReplay_GetError_500(t *testing.T) {
	srv, repo := buildHandler(t, adminUser(), &fakeJS{})
	repo.getErr = errors.New("db down")
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/dlq/"+msgID+"/replay", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestReplay_MarkReplayedError_500(t *testing.T) {
	js := &fakeJS{}
	srv, repo := buildHandler(t, adminUser(), js)
	repo.seed(&dlq.Message{ID: msgID, SourceSubject: "orders.created", FirstSeenAt: time.Now()})
	repo.replayErr = errors.New("db down")

	resp := do(t, http.MethodPost, srv.URL+"/api/admin/dlq/"+msgID+"/replay", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// MarkReplayed reporting a race-loss (already resolved) maps to 409.
func TestReplay_MarkReplayedAlreadyResolved_409(t *testing.T) {
	js := &fakeJS{}
	srv, repo := buildHandler(t, adminUser(), js)
	repo.seed(&dlq.Message{ID: msgID, SourceSubject: "orders.created", FirstSeenAt: time.Now()})
	repo.replayErr = dlq.ErrAlreadyResolved

	resp := do(t, http.MethodPost, srv.URL+"/api/admin/dlq/"+msgID+"/replay", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

// === resolve ===

func TestResolve_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/dlq/"+msgID+"/resolve", `{"notes":"junk"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestResolve_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, &identity.User{ID: "v-1", Role: identity.RoleVendorOperator}, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/dlq/"+msgID+"/resolve", `{"notes":"junk"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestResolve_BadUUID_422(t *testing.T) {
	srv, _ := buildHandler(t, adminUser(), nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/dlq/not-a-uuid/resolve", `{"notes":"junk"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestResolve_OK_204(t *testing.T) {
	srv, repo := buildHandler(t, adminUser(), nil)
	repo.seed(&dlq.Message{ID: msgID, SourceSubject: "orders.created", FirstSeenAt: time.Now()})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/dlq/"+msgID+"/resolve", `{"notes":"genuinely garbage"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestResolve_NotFound_404(t *testing.T) {
	srv, _ := buildHandler(t, adminUser(), nil) // message not seeded
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/dlq/"+msgID+"/resolve", `{"notes":"junk"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestResolve_AlreadyResolved_409(t *testing.T) {
	srv, repo := buildHandler(t, adminUser(), nil)
	repo.seed(&dlq.Message{ID: msgID, SourceSubject: "orders.created", FirstSeenAt: time.Now()})
	repo.resolveErr = dlq.ErrAlreadyResolved
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/dlq/"+msgID+"/resolve", `{"notes":"junk"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestResolve_RepoError_500(t *testing.T) {
	srv, repo := buildHandler(t, adminUser(), nil)
	repo.seed(&dlq.Message{ID: msgID, SourceSubject: "orders.created", FirstSeenAt: time.Now()})
	repo.resolveErr = errors.New("db down")
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/dlq/"+msgID+"/resolve", `{"notes":"junk"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}
