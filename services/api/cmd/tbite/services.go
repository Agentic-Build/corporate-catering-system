package main

// This file de-duplicates the per-role service-graph wiring that used to live
// inline in main.go's RoleAPI and RoleMCPStdio cases. Both roles construct the
// same seven domain services (order/vendor/menu/payroll/compliance/feedback/
// settlement) plus their shared repos; previously the two inline copies had
// already drifted (mcp-stdio omitted payroll.Exceptions, compliance.Vendors,
// and feedback.Reverser). buildCoreServices is the single source of truth, so
// future changes can't drift again.
//
// Wiring correctness here is critical: a missing field is a runtime nil-deref
// the compiler can't catch, and the cmd package isn't part of the test
// coverage. services_test.go uses reflection over the returned struct to
// assert every field is non-nil — exercising the constructor catches a wiring
// slip before deploy.

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/compliance"
	cpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/compliance/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/config"
	"github.com/takalawang/corporate-catering-system/services/api/internal/feedback"
	fpg "github.com/takalawang/corporate-catering-system/services/api/internal/feedback/postgres"
	pgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/identity/postgres"
	idredis "github.com/takalawang/corporate-catering-system/services/api/internal/identity/redis"
	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
	mpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/menu/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	opgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/order/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/payroll"
	payrollpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/payroll/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/cache"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/clock"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/storage"
	qpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/quota/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/settlement"
	settlementpg "github.com/takalawang/corporate-catering-system/services/api/internal/settlement/postgres"
	vendor "github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
	vpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/vendors/postgres"
)

// coreServices bundles the seven domain services plus the shared repos that
// role-specific code (HTTP API, MCP stdio bootstrap, the per-role extras like
// Reorder/Favorites/Home) needs to construct further wiring on top.
type coreServices struct {
	// shared repos
	UserRepo       *pgrepo.UserRepo
	SessStore      *idredis.SessionStore
	OrderRepo      *opgrepo.OrderRepo
	StateEventRepo *opgrepo.StateEventRepo
	AuditRepo      *opgrepo.AuditRepo
	OutboxRepo     *opgrepo.OutboxRepo
	SupplyRepo     *qpgrepo.SupplyRepo
	ItemRepo       *mpgrepo.ItemRepo
	PlantRepo      *vpgrepo.PlantMappingRepo
	VendorRepo     *vpgrepo.VendorRepo

	// domain services
	Order      *order.Service
	Vendor     *vendor.Service
	Menu       *menu.Service
	Payroll    *payroll.Service
	Compliance *compliance.Service
	Feedback   *feedback.Service
	Settlement *settlement.Service
}

// buildCoreServices constructs the shared service graph used by both the api
// and mcp-stdio roles. Pure construction (no DB calls), so it's safe to call
// with nil pool/rdb under test for nil-field assertion. storage may be nil:
// when nil, compliance.Service.Storage is left as the interface zero value,
// matching the previous mcp-stdio behaviour where document upload paths were
// not wired.
func buildCoreServices(_ context.Context, pool *pgxpool.Pool, rdb *cache.Client, cfg config.Config, s3 *storage.S3Client, _ *slog.Logger) (*coreServices, error) {
	// repos (stateless wrappers around the pool/rdb)
	userRepo := pgrepo.NewUserRepo(pool)
	sessStore := idredis.NewSessionStore(rdb, 7*24*time.Hour)
	orderRepo := opgrepo.NewOrderRepo(pool)
	stateEventRepo := opgrepo.NewStateEventRepo(pool)
	auditRepo := opgrepo.NewAuditRepo(pool)
	outboxRepo := opgrepo.NewOutboxRepo(pool)
	supplyRepo := qpgrepo.NewSupplyRepo(pool)
	itemRepo := mpgrepo.NewItemRepo(pool)
	plantRepo := vpgrepo.NewPlantMappingRepo(pool)
	vendorRepo := vpgrepo.NewVendorRepo(pool)

	authentikProvisioner, err := newAuthentikProvisioner(cfg)
	if err != nil {
		return nil, fmt.Errorf("authentik provisioner: %w", err)
	}

	// domain services — both roles get the same full wiring (previous inline
	// MCP copy omitted payroll.Exceptions / compliance.Vendors / feedback
	// .Reverser; unifying here can't make MCP worse since none of those fields
	// were exercised by any MCP tool, and it removes the drift risk).
	orderService := &order.Service{
		Pool:        pool,
		Orders:      orderRepo,
		OrdersTx:    orderRepo,
		StateEvents: stateEventRepo,
		StateTx:     stateEventRepo,
		Audit:       auditRepo,
		AuditTx:     auditRepo,
		Outbox:      outboxRepo,
		OutboxTx:    outboxRepo,
		QuotaTx:     supplyRepo,
		Items:       itemRepo,
		Plants:      plantRepo,
		Vendors:     vendorRepo,
		Clock:       clock.SystemClock{},
		Location:    appLocation(),
	}
	vendorService := &vendor.Service{
		Vendors:     vendorRepo,
		Plants:      plantRepo,
		Operators:   vpgrepo.NewOperatorRepo(pool),
		Provisioner: authentikProvisioner,
		Users:       userRepo,
		Sessions:    sessStore,
		Audit:       auditRepo,
	}
	menuService := &menu.Service{
		Categories: mpgrepo.NewCategoryRepo(pool),
		Items:      itemRepo,
		Images:     mpgrepo.NewImageRepo(pool),
	}
	payrollService := &payroll.Service{
		Pool:       pool,
		Batches:    payrollpgrepo.NewBatchRepo(pool),
		Entries:    payrollpgrepo.NewEntryRepo(pool),
		Disputes:   payrollpgrepo.NewDisputeRepo(pool),
		Exceptions: payrollpgrepo.NewExceptionRepo(pool),
		Orders:     orderRepo,
		OrderTx:    orderRepo,
		Audit:      auditRepo,
		Outbox:     outboxRepo,
		Clock:      clock.SystemClock{},
	}
	complianceService := &compliance.Service{
		Pool:      pool,
		Docs:      cpgrepo.NewDocumentRepo(pool),
		Anomaly:   cpgrepo.NewAnomalyRepo(pool),
		Audit:     auditRepo,
		Outbox:    outboxRepo,
		AuditQry:  auditRepo,
		Vendors:   vendorRepo,
		VendorGov: vendorService,
		Clock:     clock.SystemClock{},
	}
	// Only assign Storage when non-nil: a typed-nil *S3Client wrapped in the
	// objectStore interface would compare != nil and panic on first call.
	if s3 != nil {
		complianceService.Storage = s3
	}
	feedbackService := &feedback.Service{
		Pool:       pool,
		Ratings:    fpg.NewRatingRepo(pool),
		Complaints: fpg.NewComplaintRepo(pool),
		Orders:     fpg.NewOrderReader(pool),
		Audit:      auditRepo,
		Clock:      clock.SystemClock{},
		Reverser:   payrollService,
	}
	settlementRepo := settlementpg.NewSettlementRepo(pool)
	settlementService := &settlement.Service{
		Pool:        pool,
		Settlements: settlementRepo,
		Orders:      settlementRepo,
		Audit:       auditRepo,
	}

	return &coreServices{
		UserRepo:       userRepo,
		SessStore:      sessStore,
		OrderRepo:      orderRepo,
		StateEventRepo: stateEventRepo,
		AuditRepo:      auditRepo,
		OutboxRepo:     outboxRepo,
		SupplyRepo:     supplyRepo,
		ItemRepo:       itemRepo,
		PlantRepo:      plantRepo,
		VendorRepo:     vendorRepo,

		Order:      orderService,
		Vendor:     vendorService,
		Menu:       menuService,
		Payroll:    payrollService,
		Compliance: complianceService,
		Feedback:   feedbackService,
		Settlement: settlementService,
	}, nil
}
