package compliance_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance"
	vendor "github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
)

// errReader fails on the first Read, exercising the io.ReadAll error path in
// UploadDocument (the "read upload" wrap).
type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

func TestUploadDocument_ReadError(t *testing.T) {
	svc := &compliance.Service{
		Pool:    fakeBeginner{},
		Docs:    &fakeDocRepo{},
		Storage: fakeStore{},
		Audit:   &recordingAudit{},
		Clock:   fixedClock{t: time.Now()},
	}
	_, err := svc.UploadDocument(context.Background(), compliance.UploadInput{
		VendorID: "v1", Kind: compliance.DocKindOther, Filename: "f.pdf",
		Body: errReader{err: errBoom},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, errBoom)
	assert.Contains(t, err.Error(), "read upload:")
}

// CreateTx failing inside the upload transaction propagates the repo error
// (the row insert + audit run in one tx).
func TestUploadDocument_CreateError(t *testing.T) {
	svc := &compliance.Service{
		Pool:    fakeBeginner{},
		Docs:    &fakeDocRepo{createErr: errBoom},
		Storage: fakeStore{},
		Audit:   &recordingAudit{},
		Clock:   fixedClock{t: time.Now()},
	}
	_, err := svc.UploadDocument(context.Background(), compliance.UploadInput{
		VendorID: "v1", Kind: compliance.DocKindOther, Filename: "f.pdf",
		Body: strings.NewReader("x"),
	})
	assert.ErrorIs(t, err, errBoom)
}

// === TriageAnomaly (fake-injection error paths) ===

// fakeGov is a VendorSuspender whose Suspend always returns a fixed error.
type fakeGov struct{ err error }

func (g fakeGov) Suspend(context.Context, string, string) error { return g.err }

func TestTriageAnomaly_GetByIDError(t *testing.T) {
	svc := &compliance.Service{
		Pool:    fakeBeginner{},
		Anomaly: &fakeAnomalyRepo{getErr: compliance.ErrAnomalyNotFound},
		Audit:   &recordingAudit{},
	}
	err := svc.TriageAnomaly(context.Background(), "an1", "by1", "notes", compliance.ActionNone)
	assert.ErrorIs(t, err, compliance.ErrAnomalyNotFound)
}

func TestTriageAnomaly_TriageTxError(t *testing.T) {
	svc := &compliance.Service{
		Pool: fakeBeginner{},
		Anomaly: &fakeAnomalyRepo{
			getByID:   &compliance.Anomaly{ID: "an1", TargetKind: "vendor", TargetID: "v1"},
			triageErr: errBoom,
		},
		Audit: &recordingAudit{},
	}
	err := svc.TriageAnomaly(context.Background(), "an1", "by1", "notes", compliance.ActionNone)
	assert.ErrorIs(t, err, errBoom)
}

// Suspend returning a non-ErrInvalidStatus error must propagate out of
// TriageAnomaly (the no-op success exemption applies only to ErrInvalidStatus).
func TestTriageAnomaly_SuspendHardError(t *testing.T) {
	audit := &recordingAudit{}
	svc := &compliance.Service{
		Pool: fakeBeginner{},
		Anomaly: &fakeAnomalyRepo{
			getByID: &compliance.Anomaly{ID: "an1", TargetKind: "vendor", TargetID: "v1"},
		},
		Audit:     audit,
		VendorGov: fakeGov{err: errBoom},
	}
	err := svc.TriageAnomaly(context.Background(), "an1", "by1", "notes", compliance.ActionSuspend)
	assert.ErrorIs(t, err, errBoom)
	// Triage audit was still written before the suspend failure.
	assert.Equal(t, []string{"anomaly.triage"}, audit.calls)
}

// Sanity: an ErrInvalidStatus from Suspend is swallowed as a no-op success even
// with fake injection (mirrors the container-backed governance test).
func TestTriageAnomaly_SuspendInvalidStatusIsNoOp(t *testing.T) {
	svc := &compliance.Service{
		Pool: fakeBeginner{},
		Anomaly: &fakeAnomalyRepo{
			getByID: &compliance.Anomaly{ID: "an1", TargetKind: "vendor", TargetID: "v1"},
		},
		Audit:     &recordingAudit{},
		VendorGov: fakeGov{err: vendor.ErrInvalidStatus},
	}
	require.NoError(t, svc.TriageAnomaly(context.Background(), "an1", "by1", "notes", compliance.ActionSuspend))
}
