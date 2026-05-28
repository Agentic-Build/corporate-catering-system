// Shared service-graph wiring for the RoleAPI and RoleMCPStdio cases — single
// source of truth so the two roles can't drift again. services_test.go uses
// reflection to assert every field is non-nil (cmd/ isn't in the test suite).
package main

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

// coreServices bundles the shared services + repos both roles need.
type coreServices struct {
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

	Order      *order.Service
	Vendor     *vendor.Service
	Menu       *menu.Service
	Payroll    *payroll.Service
	Compliance *compliance.Service
	Feedback   *feedback.Service
	Settlement *settlement.Service
}

// buildCoreServices constructs the shared service graph. Pure construction
// (no DB calls) so it's safe to call with nil pool/rdb in tests. s3 may be
// nil — compliance.Storage is then left at the interface zero value rather
// than wrapping a typed-nil pointer.
func buildCoreServices(_ context.Context, pool *pgxpool.Pool, rdb *cache.Client, cfg config.Config, s3 *storage.S3Client, _ *slog.Logger) (*coreServices, error) {
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

	// Both roles get the full wiring; the previous mcp-stdio inline copy had
	// drifted (omitted payroll.Exceptions / compliance.Vendors / feedback.Reverser).
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
	if s3 != nil { // avoid wrapping a typed-nil pointer in the interface
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
