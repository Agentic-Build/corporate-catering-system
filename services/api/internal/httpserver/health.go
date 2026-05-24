package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"
)

// Checker reports the health of a single dependency. Checkers must be
// cheap and bounded: the readiness handler enforces a 2-second deadline
// for the entire check fan-out so kubelet probes do not stall.
type Checker interface {
	// Name is the dependency label surfaced in the readiness JSON
	// (e.g. "postgres-rw", "nats", "valkey").
	Name() string
	// Check returns nil when the dependency is reachable. The
	// context carries the shared per-request deadline.
	Check(ctx context.Context) error
}

// CheckerFunc adapts a function to the Checker interface.
type CheckerFunc struct {
	N string
	F func(ctx context.Context) error
}

func (c CheckerFunc) Name() string                            { return c.N }
func (c CheckerFunc) Check(ctx context.Context) error         { return c.F(ctx) }

// Health holds the set of dependency checkers for a role. The
// per-role binary (api / realtime-gateway / outbox-relay / ...) wires
// only the checkers that match its actual runtime dependencies, so a
// pod can never become Ready while a hard dependency is unreachable
// (see architecture issue #62).
type Health struct {
	live  atomic.Bool
	deps  []Checker
}

// NewHealth constructs a Health with the provided dependency
// checkers. The liveness gauge starts true; call SetLive(false) to
// force the /healthz probe to fail (e.g. during graceful shutdown).
func NewHealth(deps ...Checker) *Health {
	h := &Health{}
	h.live.Store(true)
	h.deps = deps
	return h
}

// SetLive controls the liveness gauge. Failed liveness causes
// kubelet to restart the pod; readiness is computed from deps.
func (h *Health) SetLive(v bool) { h.live.Store(v) }

// LivenessHandler implements /healthz. It is a fast pass-through
// independent of dependency state — kubelet uses liveness to detect
// stuck processes, not dependency outages.
func (h *Health) LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if !h.live.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"draining"}`))
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}

// ReadinessHandler implements /readyz. It fans out to each Checker
// under a shared 2-second deadline; any failure surfaces as 503 and
// the failing dependency name is included in the response body so
// operators can diagnose at a glance.
func (h *Health) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		w.Header().Set("content-type", "application/json")

		type depResult struct {
			Name  string `json:"name"`
			OK    bool   `json:"ok"`
			Error string `json:"error,omitempty"`
		}
		results := make([]depResult, 0, len(h.deps))
		ok := true
		for _, dep := range h.deps {
			err := dep.Check(ctx)
			res := depResult{Name: dep.Name(), OK: err == nil}
			if err != nil {
				res.Error = err.Error()
				ok = false
			}
			results = append(results, res)
		}

		status := "ready"
		code := http.StatusOK
		if !ok {
			status = "not_ready"
			code = http.StatusServiceUnavailable
		}
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": status,
			"deps":   results,
		})
	}
}

// legacy shallow handlers retained for code paths that still wire the
// HTTP server through the old constructor signature. New roles must
// use the Health type above.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func readyHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}
