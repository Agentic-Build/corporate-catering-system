// Command lunch-flow provisions and runs the two-stage lunch pickup load model:
//
//  1. setup resources: Gaussian-distributed employees, pickup points, vendors,
//     menu items, meal supply, and one cutoff order per employee.
//  2. prepare food: merchants call POST /api/merchant/orders/mark-ready in
//     batches.
//  3. take food: employees run the frontend pickup path, including the page
//     load / invalidation reads plus POST /api/employee/orders/{id}/pickup.
//  4. optional cleanup of only this run's synthetic rows.
//  5. print a report with distribution and latency details.
//
// Setup writes directly to Postgres so the test isolates lunch operations from
// order-placement load. Stage 1 and stage 2 deliberately go through the real API
// so auth, handlers, service logic, and database transactions are exercised.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idredis "github.com/takalawang/corporate-catering-system/services/api/internal/identity/redis"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/cache"
)

const (
	defaultPickupWindow = "11:50-12:10"
	defaultPriceMinor   = int64(100)
	emptyJSON           = "{}"
)

type config struct {
	BaseURL           string        `json:"base_url"`
	DBURL             string        `json:"-"`
	RedisURL          string        `json:"-"`
	RunID             string        `json:"run_id"`
	Mode              string        `json:"mode"`
	CleanupMode       string        `json:"cleanup_mode"`
	Replace           bool          `json:"replace"`
	Employees         int           `json:"employees"`
	Merchants         int           `json:"merchants"`
	PickupPoints      int           `json:"pickup_points"`
	ItemsPerVendor    int           `json:"items_per_vendor"`
	PickupSigma       float64       `json:"pickup_sigma"`
	MerchantSigma     float64       `json:"merchant_sigma"`
	Stage1BatchSize   int           `json:"stage1_batch_size"`
	Stage1Concurrency int           `json:"stage1_concurrency"`
	Stage1RPS         float64       `json:"stage1_rps"`
	Stage2Concurrency int           `json:"stage2_concurrency"`
	Stage2RPS         float64       `json:"stage2_rps"`
	HTTPTimeout       time.Duration `json:"http_timeout"`
	SupplyDate        string        `json:"supply_date"`
	ReportFile        string        `json:"report_file,omitempty"`
}

type report struct {
	RunID    string         `json:"run_id"`
	Config   config         `json:"config"`
	Setup    *setupReport   `json:"setup,omitempty"`
	Stage1   *metricsReport `json:"stage_1_prepare_food,omitempty"`
	Stage2   *metricsReport `json:"stage_2_take_food,omitempty"`
	Cleanup  *cleanupReport `json:"cleanup,omitempty"`
	Started  time.Time      `json:"started_at"`
	Finished time.Time      `json:"finished_at"`
	Errors   []string       `json:"errors,omitempty"`
	Warnings []string       `json:"warnings,omitempty"`
}

type setupReport struct {
	Employees           int          `json:"employees"`
	MerchantUsers       int          `json:"merchant_users"`
	Vendors             int          `json:"vendors"`
	PickupPoints        int          `json:"pickup_points"`
	MenuItems           int          `json:"menu_items"`
	MealSupplyRows      int          `json:"meal_supply_rows"`
	Orders              int          `json:"orders"`
	VendorPlantMappings int          `json:"vendor_plant_mappings"`
	PickupDistribution  distribution `json:"pickup_distribution"`
	MerchantTotals      distribution `json:"merchant_totals"`
	Stage1Batches       int          `json:"stage_1_batches"`
}

type distribution struct {
	Min      int            `json:"min"`
	Max      int            `json:"max"`
	Mean     float64        `json:"mean"`
	Total    int            `json:"total"`
	ByIndex  map[string]int `json:"by_index,omitempty"`
	Deciles  map[string]int `json:"deciles,omitempty"`
	Examples map[string]int `json:"examples,omitempty"`
}

type cleanupReport struct {
	Mode                  string        `json:"mode"`
	Duration              time.Duration `json:"duration"`
	DeletedOrders         int64         `json:"deleted_orders"`
	DeletedOrderItems     int64         `json:"deleted_order_items"`
	DeletedStateEvents    int64         `json:"deleted_state_events"`
	DeletedAuditEvents    int64         `json:"deleted_audit_events"`
	DeletedOutboxEvents   int64         `json:"deleted_outbox_events"`
	DeletedUsers          int64         `json:"deleted_users"`
	DeletedVendors        int64         `json:"deleted_vendors"`
	DeletedPlants         int64         `json:"deleted_plants"`
	DeletedMealSupplyRows int64         `json:"deleted_meal_supply_rows"`
	DeletedMenuItems      int64         `json:"deleted_menu_items"`
	RevokedSessionUsers   int           `json:"revoked_session_users"`
}

type plantRecord struct {
	ID    int
	Code  string
	Label string
	Count int
}

type vendorRecord struct {
	ID        string
	Index     int
	Name      string
	UserID    string
	UserEmail string
	Token     string
	Total     int
	ItemIDs   []string
}

type employeeRecord struct {
	ID     string
	Index  int
	Email  string
	Plant  string
	Token  string
	Order  string
	Vendor string
	Status string
}

type orderRecord struct {
	ID       string
	UserID   string
	VendorID string
	Plant    string
	Status   string
	Token    string
}

type scenarioData struct {
	Plants    []plantRecord
	Vendors   []vendorRecord
	Employees []employeeRecord
	Orders    []orderRecord
	Matrix    [][]int
}

type readyBatch struct {
	VendorID string
	Token    string
	OrderIDs []string
}

type pickupTask struct {
	OrderID string
	Token   string
}

type taskResult struct {
	Operation string
	Status    int
	Latency   time.Duration
	Error     string
	TaskSize  int
}

type metricsReport struct {
	Name            string         `json:"name"`
	Requests        int            `json:"requests"`
	Orders          int            `json:"orders"`
	Success         int            `json:"success"`
	Failed          int            `json:"failed"`
	Duration        time.Duration  `json:"duration"`
	ThroughputRPS   float64        `json:"throughput_rps"`
	OrderRatePerSec float64        `json:"order_rate_per_sec"`
	LatencyP50      time.Duration  `json:"latency_p50"`
	LatencyP95      time.Duration  `json:"latency_p95"`
	LatencyP99      time.Duration  `json:"latency_p99"`
	LatencyMax      time.Duration  `json:"latency_max"`
	ByStatus        map[string]int `json:"by_status"`
	ByOperation     map[string]int `json:"by_operation,omitempty"`
	Errors          map[string]int `json:"errors,omitempty"`
}

type pickupRequestStep struct {
	Method      string
	Route       string
	Path        func(pickupTask) string
	Body        []byte
	CountsOrder bool
}

func (s pickupRequestStep) Operation() string {
	return s.Method + " " + s.Route
}

func staticPickupPath(path string) func(pickupTask) string {
	return func(pickupTask) string {
		return path
	}
}

type requestLimiter struct {
	ticker *time.Ticker
	tokens <-chan time.Time
}

func newRequestLimiter(rps float64) *requestLimiter {
	if rps <= 0 {
		return &requestLimiter{}
	}
	interval := time.Duration(float64(time.Second) / rps)
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	return &requestLimiter{ticker: ticker, tokens: ticker.C}
}

func (l *requestLimiter) Wait(ctx context.Context) error {
	if l == nil || l.tokens == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-l.tokens:
		return nil
	}
}

func (l *requestLimiter) Stop() {
	if l != nil && l.ticker != nil {
		l.ticker.Stop()
	}
}

func newHTTPClient(timeout time.Duration, concurrency int) *http.Client {
	if concurrency < 1 {
		concurrency = 1
	}
	poolSize := concurrency
	if poolSize < 100 {
		poolSize = 100
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxConnsPerHost:       concurrency,
			MaxIdleConns:          poolSize,
			MaxIdleConnsPerHost:   poolSize,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
			DisableCompression:    true,
		},
	}
}

var frontendPickupSteps = []pickupRequestStep{
	{
		Method: http.MethodGet,
		Route:  "/me",
		Path:   staticPickupPath("/me"),
	},
	{
		Method: http.MethodGet,
		Route:  "/api/employee/orders",
		Path:   staticPickupPath("/api/employee/orders"),
	},
	{
		Method: http.MethodGet,
		Route:  "/api/plants",
		Path:   staticPickupPath("/api/plants"),
	},
	{
		Method: http.MethodGet,
		Route:  "/api/employee/home",
		Path:   staticPickupPath("/api/employee/home"),
	},
	{
		Method: http.MethodGet,
		Route:  "/me",
		Path:   staticPickupPath("/me"),
	},
	{
		Method: http.MethodGet,
		Route:  "/me",
		Path:   staticPickupPath("/me"),
	},
	{
		Method: http.MethodPost,
		Route:  "/api/employee/orders/{id}/pickup",
		Path: func(t pickupTask) string {
			return "/api/employee/orders/" + t.OrderID + "/pickup"
		},
		Body:        []byte(emptyJSON),
		CountsOrder: true,
	},
	{
		Method: http.MethodGet,
		Route:  "/me",
		Path:   staticPickupPath("/me"),
	},
	{
		Method: http.MethodGet,
		Route:  "/api/employee/orders",
		Path:   staticPickupPath("/api/employee/orders"),
	},
	{
		Method: http.MethodGet,
		Route:  "/api/plants",
		Path:   staticPickupPath("/api/plants"),
	},
}

func main() {
	cfg := parseFlags()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	rep := report{RunID: cfg.RunID, Config: cfg, Started: time.Now().UTC()}

	if err := run(ctx, cfg, logger, &rep); err != nil {
		rep.Errors = append(rep.Errors, err.Error())
		rep.Finished = time.Now().UTC()
		_ = writeReport(cfg.ReportFile, &rep)
		printReport(&rep)
		os.Exit(1)
	}
	rep.Finished = time.Now().UTC()
	if err := writeReport(cfg.ReportFile, &rep); err != nil {
		rep.Warnings = append(rep.Warnings, err.Error())
	}
	printReport(&rep)
}

func parseFlags() config {
	var cfg config
	flag.StringVar(&cfg.BaseURL, "base-url", envOr("LUNCH_FLOW_BASE_URL", envOr("STRESS_BASE_URL", "http://localhost:8080")), "API base URL")
	flag.StringVar(&cfg.DBURL, "db", envOr("DATABASE_RW_URL", envOr("DATABASE_URL", "postgres://tbite:tbite@localhost:5432/tbite?sslmode=disable")), "Postgres DSN")
	flag.StringVar(&cfg.RedisURL, "redis", envOr("REDIS_URL", "redis://localhost:6379"), "Redis URL")
	flag.StringVar(&cfg.RunID, "run-id", "", "Synthetic run id. Defaults to lf-YYYYMMDDHHMMSS.")
	flag.StringVar(&cfg.Mode, "mode", "all", "all|setup|prepare|pickup|cleanup")
	flag.StringVar(&cfg.CleanupMode, "cleanup", "keep", "keep|delete. In mode=all, delete runs after stage 2.")
	flag.BoolVar(&cfg.Replace, "replace", false, "Cleanup an existing run-id before setup.")
	flag.IntVar(&cfg.Employees, "employees", 50000, "Employee/order count")
	flag.IntVar(&cfg.Merchants, "merchants", 100, "Merchant/vendor count")
	flag.IntVar(&cfg.PickupPoints, "pickup-points", 100, "Pickup location count")
	flag.IntVar(&cfg.ItemsPerVendor, "items-per-vendor", 10, "Demo menu item count per vendor")
	flag.Float64Var(&cfg.PickupSigma, "pickup-sigma", 25, "Gaussian sigma for employee distribution across pickup points")
	flag.Float64Var(&cfg.MerchantSigma, "merchant-sigma", 3, "Gaussian sigma for merchant-to-pickup assignment")
	flag.IntVar(&cfg.Stage1BatchSize, "stage1-batch-size", 100, "Orders per merchant mark-ready request")
	flag.IntVar(&cfg.Stage1Concurrency, "stage1-concurrency", 20, "Concurrent stage 1 API workers")
	flag.Float64Var(&cfg.Stage1RPS, "stage1-rps", 20, "Global stage 1 request rate. 0 disables throttling.")
	flag.IntVar(&cfg.Stage2Concurrency, "stage2-concurrency", 200, "Concurrent stage 2 API workers")
	flag.Float64Var(&cfg.Stage2RPS, "stage2-rps", 100, "Global stage 2 backend API request rate. 0 disables throttling.")
	flag.DurationVar(&cfg.HTTPTimeout, "http-timeout", 15*time.Second, "Per-request HTTP timeout")
	flag.StringVar(&cfg.SupplyDate, "supply-date", defaultSupplyDate(), "Supply date YYYY-MM-DD")
	flag.StringVar(&cfg.ReportFile, "report-file", "", "Optional JSON report file path")
	flag.Parse()

	if cfg.RunID == "" {
		cfg.RunID = "lf-" + time.Now().In(taipei()).Format("20060102150405")
	}
	cfg.RunID = sanitizeRunID(cfg.RunID)
	cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	cfg.CleanupMode = strings.ToLower(strings.TrimSpace(cfg.CleanupMode))
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return cfg
}

func run(ctx context.Context, cfg config, log *slog.Logger, rep *report) error {
	if err := validateConfig(cfg); err != nil {
		return err
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DBURL)
	if err != nil {
		return fmt.Errorf("postgres config: %w", err)
	}
	poolCfg.AfterConnect = registerCustomTypes
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("postgres connect: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres ping: %w", err)
	}

	rdb, err := cache.NewClient(ctx, cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("redis connect: %w", err)
	}
	defer rdb.Close()
	sessions := idredis.NewSessionStore(rdb, 24*time.Hour)

	if cfg.Replace && (cfg.Mode == "setup" || cfg.Mode == "all") {
		log.Info("cleanup before setup", "run_id", cfg.RunID)
		cr, err := cleanupRun(ctx, pool, sessions, cfg.RunID, "replace")
		if err != nil {
			return err
		}
		rep.Cleanup = cr
	}

	var data *scenarioData
	switch cfg.Mode {
	case "setup":
		data, err = setupRun(ctx, pool, sessions, cfg, log)
		if err != nil {
			return err
		}
		rep.Setup = buildSetupReport(cfg, data)
	case "prepare":
		data, err = loadRun(ctx, pool, sessions, cfg.RunID)
		if err != nil {
			return err
		}
		rep.Setup = buildSetupReport(cfg, data)
		rep.Stage1, err = runPrepareStage(ctx, cfg, data, log)
		if err != nil {
			return err
		}
	case "pickup":
		data, err = loadRun(ctx, pool, sessions, cfg.RunID)
		if err != nil {
			return err
		}
		rep.Setup = buildSetupReport(cfg, data)
		rep.Stage2, err = runPickupStage(ctx, cfg, data, log)
		if err != nil {
			return err
		}
	case "cleanup":
		rep.Cleanup, err = cleanupRun(ctx, pool, sessions, cfg.RunID, cfg.CleanupMode)
		if err != nil {
			return err
		}
	case "all":
		data, err = setupRun(ctx, pool, sessions, cfg, log)
		if err != nil {
			return err
		}
		rep.Setup = buildSetupReport(cfg, data)
		rep.Stage1, err = runPrepareStage(ctx, cfg, data, log)
		if err != nil {
			return err
		}
		data, err = loadRun(ctx, pool, sessions, cfg.RunID)
		if err != nil {
			return err
		}
		rep.Stage2, err = runPickupStage(ctx, cfg, data, log)
		if err != nil {
			return err
		}
		if cfg.CleanupMode == "delete" {
			rep.Cleanup, err = cleanupRun(ctx, pool, sessions, cfg.RunID, cfg.CleanupMode)
			if err != nil {
				return err
			}
		} else {
			rep.Cleanup = &cleanupReport{Mode: "keep"}
		}
	default:
		return fmt.Errorf("unknown mode %q", cfg.Mode)
	}
	return nil
}

func registerCustomTypes(ctx context.Context, conn *pgx.Conn) error {
	for _, name := range []string{
		"user_role",
		"user_status",
		"vendor_status",
		"vendor_operator_status",
		"menu_item_status",
		"order_status",
	} {
		t, err := conn.LoadType(ctx, name)
		if err != nil {
			return fmt.Errorf("load postgres type %s: %w", name, err)
		}
		conn.TypeMap().RegisterType(t)
	}
	return nil
}

func validateConfig(cfg config) error {
	var errs []string
	if cfg.Employees <= 0 {
		errs = append(errs, "employees must be > 0")
	}
	if cfg.Merchants <= 0 {
		errs = append(errs, "merchants must be > 0")
	}
	if cfg.PickupPoints <= 0 {
		errs = append(errs, "pickup-points must be > 0")
	}
	if cfg.ItemsPerVendor <= 0 {
		errs = append(errs, "items-per-vendor must be > 0")
	}
	if cfg.PickupSigma <= 0 {
		errs = append(errs, "pickup-sigma must be > 0")
	}
	if cfg.MerchantSigma <= 0 {
		errs = append(errs, "merchant-sigma must be > 0")
	}
	if cfg.Stage1BatchSize <= 0 {
		errs = append(errs, "stage1-batch-size must be > 0")
	}
	if cfg.Stage1Concurrency <= 0 {
		errs = append(errs, "stage1-concurrency must be > 0")
	}
	if cfg.Stage2Concurrency <= 0 {
		errs = append(errs, "stage2-concurrency must be > 0")
	}
	if cfg.CleanupMode != "keep" && cfg.CleanupMode != "delete" {
		errs = append(errs, "cleanup must be keep or delete")
	}
	if _, err := time.Parse("2006-01-02", cfg.SupplyDate); err != nil {
		errs = append(errs, "supply-date must be YYYY-MM-DD")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func setupRun(ctx context.Context, pool *pgxpool.Pool, sessions identity.SessionStore, cfg config, log *slog.Logger) (*scenarioData, error) {
	log.Info("setup starting",
		"run_id", cfg.RunID,
		"employees", cfg.Employees,
		"merchants", cfg.Merchants,
		"pickup_points", cfg.PickupPoints,
	)
	counts := gaussianCounts(cfg.Employees, cfg.PickupPoints, cfg.PickupSigma)
	matrix := merchantPickupMatrix(counts, cfg.Merchants, cfg.MerchantSigma)
	plants := makePlants(cfg.RunID, counts)
	vendors := makeVendors(cfg.RunID, cfg.Merchants, cfg.ItemsPerVendor, matrix)
	employees := makeEmployees(cfg.RunID, plants)
	assignOrders(cfg, plants, vendors, employees, matrix)

	if err := insertScenario(ctx, pool, cfg, plants, vendors, employees, matrix); err != nil {
		return nil, err
	}

	if err := mintSessions(ctx, sessions, vendors, employees); err != nil {
		return nil, err
	}

	orders := make([]orderRecord, 0, len(employees))
	for _, e := range employees {
		orders = append(orders, orderRecord{
			ID:       e.Order,
			UserID:   e.ID,
			VendorID: e.Vendor,
			Plant:    e.Plant,
			Status:   "cutoff",
			Token:    e.Token,
		})
	}
	log.Info("setup complete", "orders", len(orders), "menu_items", cfg.Merchants*cfg.ItemsPerVendor)
	return &scenarioData{Plants: plants, Vendors: vendors, Employees: employees, Orders: orders, Matrix: matrix}, nil
}

func insertScenario(ctx context.Context, pool *pgxpool.Pool, cfg config, plants []plantRecord, vendors []vendorRecord, employees []employeeRecord, matrix [][]int) error {
	supplyDay, _ := time.Parse("2006-01-02", cfg.SupplyDate)
	cutoffAt := time.Date(supplyDay.Year(), supplyDay.Month(), supplyDay.Day(), 11, 0, 0, 0, taipei()).UTC()
	now := time.Now().UTC()
	runNote := runNote(cfg.RunID)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin setup tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"plant"}, []string{"code", "label", "address", "active", "sort_order"}, pgx.CopyFromSlice(len(plants), func(i int) ([]any, error) {
		p := plants[i]
		return []any{p.Code, p.Label, "load-test", true, p.ID}, nil
	})); err != nil {
		return fmt.Errorf("copy plants: %w", err)
	}

	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"vendor"}, []string{"id", "display_name", "legal_name", "contact_email", "status", "approved_at", "cutoff_hour", "preorder_window_days"}, pgx.CopyFromSlice(len(vendors), func(i int) ([]any, error) {
		v := vendors[i]
		return []any{v.ID, v.Name, v.Name + " Ltd.", merchantEmail(cfg.RunID, v.Index), "approved", now, 17, 7}, nil
	})); err != nil {
		return fmt.Errorf("copy vendors: %w", err)
	}

	userRows := make([][]any, 0, len(employees)+len(vendors))
	for _, e := range employees {
		userRows = append(userRows, []any{e.ID, e.Email, fmt.Sprintf("測試員工 %05d", e.Index), "employee", "active", fmt.Sprintf("LF-%05d", e.Index), nil, e.Plant, "load-test"})
	}
	for _, v := range vendors {
		userRows = append(userRows, []any{v.UserID, v.UserEmail, fmt.Sprintf("測試商家帳號 %03d", v.Index), "vendor_operator", "active", nil, v.ID, nil, nil})
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"user"}, []string{"id", "primary_email", "display_name", "role", "status", "employee_id", "vendor_id", "plant", "department"}, pgx.CopyFromRows(userRows)); err != nil {
		return fmt.Errorf("copy users: %w", err)
	}

	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"vendor_operator_account"}, []string{"vendor_id", "email", "display_name", "provider", "external_subject", "status", "last_synced_at"}, pgx.CopyFromSlice(len(vendors), func(i int) ([]any, error) {
		v := vendors[i]
		return []any{v.ID, v.UserEmail, fmt.Sprintf("測試商家帳號 %03d", v.Index), "load-test", "lunch-flow:" + cfg.RunID + ":" + fmt.Sprint(v.Index), "active", now}, nil
	})); err != nil {
		return fmt.Errorf("copy vendor operators: %w", err)
	}

	mappings := make([][]any, 0, cfg.Merchants*cfg.PickupPoints)
	for pIdx, row := range matrix {
		for mIdx, n := range row {
			if n > 0 {
				mappings = append(mappings, []any{vendors[mIdx].ID, plants[pIdx].Code, true, defaultPickupWindow})
			}
		}
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"vendor_plant_mapping"}, []string{"vendor_id", "plant", "active", "service_window"}, pgx.CopyFromRows(mappings)); err != nil {
		return fmt.Errorf("copy vendor plant mappings: %w", err)
	}

	menuRows := make([][]any, 0, cfg.Merchants*cfg.ItemsPerVendor)
	for _, v := range vendors {
		for i, itemID := range v.ItemIDs {
			menuRows = append(menuRows, []any{
				itemID,
				v.ID,
				fmt.Sprintf("測試餐點 %03d-%02d", v.Index, i+1),
				"load-test lunch-flow item",
				defaultPriceMinor,
				[]string{"load-test"},
				[]string{},
				"active",
			})
		}
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"menu_item"}, []string{"id", "vendor_id", "name", "description", "price_minor", "tags", "badges", "status"}, pgx.CopyFromRows(menuRows)); err != nil {
		return fmt.Errorf("copy menu items: %w", err)
	}

	itemOrderCounts := map[string]int{}
	for _, e := range employees {
		itemOrderCounts[orderItemForEmployee(vendors, e)]++
	}
	supplyRows := make([][]any, 0, len(menuRows))
	for _, v := range vendors {
		for _, itemID := range v.ItemIDs {
			capacity := itemOrderCounts[itemID]
			if capacity == 0 {
				capacity = 1
			}
			supplyRows = append(supplyRows, []any{itemID, supplyDay, capacity, 0, defaultPickupWindow, defaultPickupWindow, cutoffAt, false})
		}
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"meal_supply"}, []string{"menu_item_id", "supply_date", "capacity", "remain", "pickup_window", "eta_label", "cutoff_at", "sold_out"}, pgx.CopyFromRows(supplyRows)); err != nil {
		return fmt.Errorf("copy meal supply: %w", err)
	}

	orderRows := make([][]any, 0, len(employees))
	itemRows := make([][]any, 0, len(employees))
	placedAt := now.Add(-2 * time.Hour)
	for _, e := range employees {
		itemID := orderItemForEmployee(vendors, e)
		orderRows = append(orderRows, []any{e.Order, e.ID, e.Vendor, e.Plant, supplyDay, "cutoff", defaultPriceMinor, runNote, placedAt, cutoffAt})
		itemRows = append(itemRows, []any{uuid.NewString(), e.Order, itemID, 1, defaultPriceMinor})
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"order"}, []string{"id", "user_id", "vendor_id", "plant", "supply_date", "status", "total_price_minor", "notes", "placed_at", "cutoff_at"}, pgx.CopyFromRows(orderRows)); err != nil {
		return fmt.Errorf("copy orders: %w", err)
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"order_item"}, []string{"id", "order_id", "menu_item_id", "qty", "unit_price_minor"}, pgx.CopyFromRows(itemRows)); err != nil {
		return fmt.Errorf("copy order items: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit setup: %w", err)
	}
	return nil
}

func mintSessions(ctx context.Context, sessions identity.SessionStore, vendors []vendorRecord, employees []employeeRecord) error {
	for i := range vendors {
		s, err := sessions.Create(ctx, vendors[i].UserID, identity.RoleVendorOperator)
		if err != nil {
			return fmt.Errorf("merchant session %d: %w", vendors[i].Index, err)
		}
		vendors[i].Token = s.Token
	}
	for i := range employees {
		s, err := sessions.Create(ctx, employees[i].ID, identity.RoleEmployee)
		if err != nil {
			return fmt.Errorf("employee session %d: %w", employees[i].Index, err)
		}
		employees[i].Token = s.Token
	}
	return nil
}

func loadRun(ctx context.Context, pool *pgxpool.Pool, sessions identity.SessionStore, runID string) (*scenarioData, error) {
	plants, err := loadPlants(ctx, pool, runID)
	if err != nil {
		return nil, err
	}
	vendors, err := loadVendors(ctx, pool, runID)
	if err != nil {
		return nil, err
	}
	employees, err := loadEmployees(ctx, pool, runID)
	if err != nil {
		return nil, err
	}
	if err := loadOrdersIntoEmployees(ctx, pool, runID, employees); err != nil {
		return nil, err
	}
	if err := mintSessions(ctx, sessions, vendors, employees); err != nil {
		return nil, err
	}
	orders := make([]orderRecord, 0, len(employees))
	for _, e := range employees {
		if e.Order == "" {
			continue
		}
		orders = append(orders, orderRecord{ID: e.Order, UserID: e.ID, VendorID: e.Vendor, Plant: e.Plant, Status: e.Status, Token: e.Token})
	}
	matrix := rebuildMatrix(plants, vendors, orders)
	return &scenarioData{Plants: plants, Vendors: vendors, Employees: employees, Orders: orders, Matrix: matrix}, nil
}

func loadPlants(ctx context.Context, pool *pgxpool.Pool, runID string) ([]plantRecord, error) {
	prefix := plantCode(runID, 0)
	prefix = strings.TrimSuffix(prefix, "000")
	rows, err := pool.Query(ctx, `SELECT code, label FROM plant WHERE code LIKE $1 ORDER BY code`, prefix+"%")
	if err != nil {
		return nil, fmt.Errorf("load plants: %w", err)
	}
	defer rows.Close()
	var out []plantRecord
	for rows.Next() {
		var p plantRecord
		if err := rows.Scan(&p.Code, &p.Label); err != nil {
			return nil, err
		}
		p.ID = len(out) + 1
		out = append(out, p)
	}
	return out, rows.Err()
}

func loadVendors(ctx context.Context, pool *pgxpool.Pool, runID string) ([]vendorRecord, error) {
	rows, err := pool.Query(ctx, `
SELECT v.id::text, v.display_name, u.id::text, u.primary_email
  FROM vendor v
  JOIN "user" u ON u.vendor_id = v.id AND u.role='vendor_operator'
 WHERE v.contact_email LIKE $1
 ORDER BY v.contact_email`, merchantEmailPrefix(runID)+"%")
	if err != nil {
		return nil, fmt.Errorf("load vendors: %w", err)
	}
	defer rows.Close()
	var out []vendorRecord
	for rows.Next() {
		var v vendorRecord
		if err := rows.Scan(&v.ID, &v.Name, &v.UserID, &v.UserEmail); err != nil {
			return nil, err
		}
		v.Index = len(out) + 1
		out = append(out, v)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	for i := range out {
		itemRows, err := pool.Query(ctx, `SELECT id::text FROM menu_item WHERE vendor_id=$1 ORDER BY name`, out[i].ID)
		if err != nil {
			return nil, fmt.Errorf("load items: %w", err)
		}
		for itemRows.Next() {
			var itemID string
			if err := itemRows.Scan(&itemID); err != nil {
				itemRows.Close()
				return nil, err
			}
			out[i].ItemIDs = append(out[i].ItemIDs, itemID)
		}
		itemRows.Close()
	}
	return out, nil
}

func loadEmployees(ctx context.Context, pool *pgxpool.Pool, runID string) ([]employeeRecord, error) {
	rows, err := pool.Query(ctx, `
SELECT id::text, primary_email, plant
  FROM "user"
 WHERE primary_email LIKE $1 AND role='employee'
 ORDER BY primary_email`, employeeEmailPrefix(runID)+"%")
	if err != nil {
		return nil, fmt.Errorf("load employees: %w", err)
	}
	defer rows.Close()
	var out []employeeRecord
	for rows.Next() {
		var e employeeRecord
		if err := rows.Scan(&e.ID, &e.Email, &e.Plant); err != nil {
			return nil, err
		}
		e.Index = len(out) + 1
		out = append(out, e)
	}
	return out, rows.Err()
}

func loadOrdersIntoEmployees(ctx context.Context, pool *pgxpool.Pool, runID string, employees []employeeRecord) error {
	byUser := make(map[string]int, len(employees))
	for i := range employees {
		byUser[employees[i].ID] = i
	}
	rows, err := pool.Query(ctx, `
SELECT id::text, user_id::text, vendor_id::text, status::text
  FROM "order"
 WHERE notes=$1
 ORDER BY user_id`, runNote(runID))
	if err != nil {
		return fmt.Errorf("load orders: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var orderID, userID, vendorID, status string
		if err := rows.Scan(&orderID, &userID, &vendorID, &status); err != nil {
			return err
		}
		if idx, ok := byUser[userID]; ok {
			employees[idx].Order = orderID
			employees[idx].Vendor = vendorID
			employees[idx].Status = status
		}
	}
	return rows.Err()
}

func loadRunUserIDs(ctx context.Context, pool *pgxpool.Pool, runID string) ([]string, error) {
	rows, err := pool.Query(ctx, `
SELECT id::text
  FROM "user"
 WHERE primary_email LIKE $1 OR primary_email LIKE $2`,
		employeeEmailPrefix(runID)+"%", merchantUserEmailPrefix(runID)+"%")
	if err != nil {
		return nil, fmt.Errorf("load cleanup users: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func runPrepareStage(ctx context.Context, cfg config, data *scenarioData, log *slog.Logger) (*metricsReport, error) {
	batches := buildReadyBatches(data, cfg.Stage1BatchSize)
	log.Info("stage 1 prepare starting", "batches", len(batches), "batch_size", cfg.Stage1BatchSize, "rps", cfg.Stage1RPS)
	client := newHTTPClient(cfg.HTTPTimeout, cfg.Stage1Concurrency)
	start := time.Now()
	results := runRateLimited(ctx, len(batches), cfg.Stage1Concurrency, cfg.Stage1RPS, func(ctx context.Context, idx int) taskResult {
		b := batches[idx]
		body := map[string]any{"order_ids": b.OrderIDs}
		status, latency, err := postJSON(ctx, client, cfg.BaseURL+"/api/merchant/orders/mark-ready", b.Token, body)
		res := taskResult{Operation: "POST /api/merchant/orders/mark-ready", Status: status, Latency: latency, TaskSize: len(b.OrderIDs)}
		if err != nil {
			res.Error = err.Error()
		}
		return res
	})
	m := summarizeMetrics("stage_1_prepare_food", results, time.Since(start))
	log.Info("stage 1 prepare complete", "requests", m.Requests, "orders", m.Orders, "success", m.Success, "failed", m.Failed)
	return m, nil
}

func runPickupStage(ctx context.Context, cfg config, data *scenarioData, log *slog.Logger) (*metricsReport, error) {
	tasks := buildPickupTasks(data)
	expectedRequests := len(tasks) * len(frontendPickupSteps)
	log.Info("stage 2 pickup starting",
		"orders", len(tasks),
		"requests", expectedRequests,
		"requests_per_order", len(frontendPickupSteps),
		"rps", cfg.Stage2RPS,
		"concurrency", cfg.Stage2Concurrency,
	)
	client := newHTTPClient(cfg.HTTPTimeout, cfg.Stage2Concurrency)
	limiter := newRequestLimiter(cfg.Stage2RPS)
	defer limiter.Stop()
	start := time.Now()
	results := runPickupFrontendFlows(ctx, tasks, cfg.Stage2Concurrency, client, cfg.BaseURL, limiter)
	m := summarizeMetrics("stage_2_take_food", results, time.Since(start))
	log.Info("stage 2 pickup complete", "requests", m.Requests, "orders", m.Orders, "success", m.Success, "failed", m.Failed)
	return m, nil
}

func runPickupFrontendFlows(ctx context.Context, tasks []pickupTask, concurrency int, client *http.Client, baseURL string, limiter *requestLimiter) []taskResult {
	if len(tasks) == 0 {
		return nil
	}
	jobs := make(chan pickupTask)
	results := make(chan taskResult, len(tasks)*len(frontendPickupSteps))
	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range jobs {
				runPickupFrontendFlow(ctx, task, client, baseURL, limiter, results)
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, task := range tasks {
			select {
			case <-ctx.Done():
				return
			case jobs <- task:
			}
		}
	}()

	wg.Wait()
	close(results)

	out := make([]taskResult, 0, len(tasks)*len(frontendPickupSteps))
	for result := range results {
		out = append(out, result)
	}
	return out
}

func runPickupFrontendFlow(ctx context.Context, task pickupTask, client *http.Client, baseURL string, limiter *requestLimiter, results chan<- taskResult) {
	for _, step := range frontendPickupSteps {
		if err := limiter.Wait(ctx); err != nil {
			results <- taskResult{Operation: step.Operation(), Error: err.Error()}
			return
		}
		status, latency, err := doAPIRequest(ctx, client, step.Method, baseURL+step.Path(task), task.Token, step.Body)
		result := taskResult{Operation: step.Operation(), Status: status, Latency: latency}
		if step.CountsOrder {
			result.TaskSize = 1
		}
		if err != nil {
			result.Error = err.Error()
			results <- result
			return
		}
		results <- result
	}
}

func postJSON(ctx context.Context, client *http.Client, url, token string, body any) (int, time.Duration, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, 0, err
	}
	return doAPIRequest(ctx, client, http.MethodPost, url, token, raw)
}

func doAPIRequest(ctx context.Context, client *http.Client, method, url, token string, body []byte) (int, time.Duration, error) {
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return 0, 0, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return 0, latency, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, latency, fmt.Errorf("http_%d", resp.StatusCode)
	}
	return resp.StatusCode, latency, nil
}

func runRateLimited(ctx context.Context, n, concurrency int, rps float64, fn func(context.Context, int) taskResult) []taskResult {
	if n == 0 {
		return nil
	}
	jobs := make(chan int)
	results := make([]taskResult, n)
	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				results[idx] = fn(ctx, idx)
			}
		}()
	}

	var ticker *time.Ticker
	if rps > 0 {
		interval := time.Duration(float64(time.Second) / rps)
		if interval < time.Millisecond {
			interval = time.Millisecond
		}
		ticker = time.NewTicker(interval)
		defer ticker.Stop()
	}

	for i := 0; i < n; i++ {
		if ticker != nil {
			select {
			case <-ctx.Done():
				close(jobs)
				wg.Wait()
				return results
			case <-ticker.C:
			}
		}
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return results
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()
	return results
}

func summarizeMetrics(name string, results []taskResult, duration time.Duration) *metricsReport {
	m := &metricsReport{
		Name:        name,
		Requests:    len(results),
		ByStatus:    map[string]int{},
		ByOperation: map[string]int{},
		Errors:      map[string]int{},
	}
	var latencies []time.Duration
	for _, r := range results {
		if r.Status == 0 && r.Latency == 0 && r.Error == "" {
			r.Error = "not_sent"
		}
		m.Orders += r.TaskSize
		if r.Operation != "" {
			m.ByOperation[r.Operation]++
		}
		if r.Latency > 0 {
			latencies = append(latencies, r.Latency)
		}
		key := "not_sent"
		if r.Status > 0 {
			key = fmt.Sprintf("%d", r.Status)
		}
		m.ByStatus[key]++
		if r.Error == "" {
			m.Success++
		} else {
			m.Failed++
			m.Errors[r.Error]++
		}
	}
	if len(m.Errors) == 0 {
		m.Errors = nil
	}
	if len(m.ByOperation) == 0 {
		m.ByOperation = nil
	}
	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		m.LatencyP50 = percentile(latencies, 0.50)
		m.LatencyP95 = percentile(latencies, 0.95)
		m.LatencyP99 = percentile(latencies, 0.99)
		m.LatencyMax = latencies[len(latencies)-1]
	}
	m.Duration = duration
	if m.Duration.Seconds() > 0 {
		m.ThroughputRPS = float64(m.Requests) / m.Duration.Seconds()
		m.OrderRatePerSec = float64(m.Orders) / m.Duration.Seconds()
	}
	return m
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func buildReadyBatches(data *scenarioData, batchSize int) []readyBatch {
	vendorByID := map[string]vendorRecord{}
	for _, v := range data.Vendors {
		vendorByID[v.ID] = v
	}
	grouped := map[string][]string{}
	for _, o := range data.Orders {
		if o.Status != "" && o.Status != "cutoff" && o.Status != "placed" {
			continue
		}
		grouped[o.VendorID] = append(grouped[o.VendorID], o.ID)
	}
	var out []readyBatch
	vendorIDs := make([]string, 0, len(grouped))
	for id := range grouped {
		vendorIDs = append(vendorIDs, id)
	}
	sort.Strings(vendorIDs)
	for _, vendorID := range vendorIDs {
		ids := grouped[vendorID]
		sort.Strings(ids)
		token := vendorByID[vendorID].Token
		for start := 0; start < len(ids); start += batchSize {
			end := start + batchSize
			if end > len(ids) {
				end = len(ids)
			}
			out = append(out, readyBatch{VendorID: vendorID, Token: token, OrderIDs: ids[start:end]})
		}
	}
	return out
}

func buildPickupTasks(data *scenarioData) []pickupTask {
	out := make([]pickupTask, 0, len(data.Orders))
	for _, o := range data.Orders {
		if o.Status != "" && o.Status != "ready" {
			continue
		}
		out = append(out, pickupTask{OrderID: o.ID, Token: o.Token})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OrderID < out[j].OrderID })
	return out
}

func cleanupRun(ctx context.Context, pool *pgxpool.Pool, sessions identity.SessionStore, runID, mode string) (*cleanupReport, error) {
	start := time.Now()
	cr := &cleanupReport{Mode: mode}
	if mode != "delete" && mode != "replace" {
		cr.Duration = time.Since(start)
		return cr, nil
	}

	userIDs, err := loadRunUserIDs(ctx, pool, runID)
	if err != nil {
		return nil, err
	}
	for _, id := range userIDs {
		if err := sessions.RevokeAllForUser(ctx, id); err != nil {
			return nil, fmt.Errorf("revoke sessions for user %s: %w", id, err)
		}
		cr.RevokedSessionUsers++
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin cleanup: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `ALTER TABLE order_state_event DISABLE TRIGGER order_state_event_no_delete`); err != nil {
		return nil, fmt.Errorf("disable order_state_event delete trigger: %w", err)
	}
	if _, err := tx.Exec(ctx, `ALTER TABLE audit_event DISABLE TRIGGER audit_event_no_delete`); err != nil {
		return nil, fmt.Errorf("disable audit_event delete trigger: %w", err)
	}

	runNote := runNote(runID)
	orderIDs := `SELECT id FROM "order" WHERE notes=$1`
	cleanupUserIDs := `SELECT id FROM "user" WHERE primary_email LIKE $2 OR primary_email LIKE $3`

	tag, err := tx.Exec(ctx, `DELETE FROM audit_event
 WHERE target_id IN (SELECT id::text FROM "order" WHERE notes=$1)
    OR actor_id IN (`+cleanupUserIDs+`)`, runNote, employeeEmailPrefix(runID)+"%", merchantUserEmailPrefix(runID)+"%")
	if err != nil {
		return nil, fmt.Errorf("delete audit events: %w", err)
	}
	cr.DeletedAuditEvents = tag.RowsAffected()

	tag, err = tx.Exec(ctx, `DELETE FROM order_state_event WHERE order_id IN (`+orderIDs+`)`, runNote)
	if err != nil {
		return nil, fmt.Errorf("delete state events: %w", err)
	}
	cr.DeletedStateEvents = tag.RowsAffected()

	tag, err = tx.Exec(ctx, `DELETE FROM outbox_event WHERE aggregate_type='order' AND aggregate_id IN (`+orderIDs+`)`, runNote)
	if err != nil {
		return nil, fmt.Errorf("delete outbox events: %w", err)
	}
	cr.DeletedOutboxEvents = tag.RowsAffected()

	tag, err = tx.Exec(ctx, `DELETE FROM order_item WHERE order_id IN (`+orderIDs+`)`, runNote)
	if err != nil {
		return nil, fmt.Errorf("delete order items: %w", err)
	}
	cr.DeletedOrderItems = tag.RowsAffected()

	tag, err = tx.Exec(ctx, `DELETE FROM "order" WHERE notes=$1`, runNote)
	if err != nil {
		return nil, fmt.Errorf("delete orders: %w", err)
	}
	cr.DeletedOrders = tag.RowsAffected()

	tag, err = tx.Exec(ctx, `DELETE FROM meal_supply WHERE menu_item_id IN (
 SELECT mi.id FROM menu_item mi JOIN vendor v ON v.id=mi.vendor_id WHERE v.contact_email LIKE $1
)`, merchantEmailPrefix(runID)+"%")
	if err != nil {
		return nil, fmt.Errorf("delete meal supply: %w", err)
	}
	cr.DeletedMealSupplyRows = tag.RowsAffected()

	tag, err = tx.Exec(ctx, `DELETE FROM menu_item WHERE vendor_id IN (
 SELECT id FROM vendor WHERE contact_email LIKE $1
)`, merchantEmailPrefix(runID)+"%")
	if err != nil {
		return nil, fmt.Errorf("delete menu items: %w", err)
	}
	cr.DeletedMenuItems = tag.RowsAffected()

	tag, err = tx.Exec(ctx, `DELETE FROM "user" WHERE primary_email LIKE $1 OR primary_email LIKE $2`, employeeEmailPrefix(runID)+"%", merchantUserEmailPrefix(runID)+"%")
	if err != nil {
		return nil, fmt.Errorf("delete users: %w", err)
	}
	cr.DeletedUsers = tag.RowsAffected()

	tag, err = tx.Exec(ctx, `DELETE FROM vendor WHERE contact_email LIKE $1`, merchantEmailPrefix(runID)+"%")
	if err != nil {
		return nil, fmt.Errorf("delete vendors: %w", err)
	}
	cr.DeletedVendors = tag.RowsAffected()

	tag, err = tx.Exec(ctx, `DELETE FROM plant WHERE code LIKE $1`, strings.TrimSuffix(plantCode(runID, 0), "000")+"%")
	if err != nil {
		return nil, fmt.Errorf("delete plants: %w", err)
	}
	cr.DeletedPlants = tag.RowsAffected()

	if _, err := tx.Exec(ctx, `ALTER TABLE order_state_event ENABLE TRIGGER order_state_event_no_delete`); err != nil {
		return nil, fmt.Errorf("enable order_state_event delete trigger: %w", err)
	}
	if _, err := tx.Exec(ctx, `ALTER TABLE audit_event ENABLE TRIGGER audit_event_no_delete`); err != nil {
		return nil, fmt.Errorf("enable audit_event delete trigger: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit cleanup: %w", err)
	}
	cr.Duration = time.Since(start)
	return cr, nil
}

func gaussianCounts(total, n int, sigma float64) []int {
	mu := float64(n+1) / 2
	raw := make([]float64, n)
	sum := 0.0
	for i := range raw {
		x := float64(i + 1)
		w := math.Exp(-math.Pow(x-mu, 2) / (2 * sigma * sigma))
		raw[i] = w
		sum += w
	}
	counts := make([]int, n)
	fractions := make([]struct {
		idx  int
		frac float64
	}, n)
	assigned := 0
	for i, w := range raw {
		v := float64(total) * w / sum
		counts[i] = int(math.Floor(v))
		assigned += counts[i]
		fractions[i] = struct {
			idx  int
			frac float64
		}{i, v - float64(counts[i])}
	}
	sort.Slice(fractions, func(i, j int) bool { return fractions[i].frac > fractions[j].frac })
	for i := 0; i < total-assigned; i++ {
		counts[fractions[i].idx]++
	}
	return counts
}

func merchantPickupMatrix(pickupCounts []int, merchants int, sigma float64) [][]int {
	matrix := make([][]int, len(pickupCounts))
	for pIdx, total := range pickupCounts {
		raw := make([]float64, merchants)
		sum := 0.0
		p := float64(pIdx + 1)
		for m := 1; m <= merchants; m++ {
			w := math.Exp(-math.Pow(float64(m)-p, 2) / (2 * sigma * sigma))
			raw[m-1] = w
			sum += w
		}
		row := make([]int, merchants)
		fractions := make([]struct {
			idx  int
			frac float64
		}, merchants)
		assigned := 0
		for i, w := range raw {
			v := float64(total) * w / sum
			row[i] = int(math.Floor(v))
			assigned += row[i]
			fractions[i] = struct {
				idx  int
				frac float64
			}{i, v - float64(row[i])}
		}
		sort.Slice(fractions, func(i, j int) bool { return fractions[i].frac > fractions[j].frac })
		for i := 0; i < total-assigned; i++ {
			row[fractions[i].idx]++
		}
		matrix[pIdx] = row
	}
	return matrix
}

func makePlants(runID string, counts []int) []plantRecord {
	out := make([]plantRecord, len(counts))
	for i, c := range counts {
		out[i] = plantRecord{
			ID:    i + 1,
			Code:  plantCode(runID, i+1),
			Label: fmt.Sprintf("測試領餐點 %03d", i+1),
			Count: c,
		}
	}
	return out
}

func makeVendors(runID string, n, itemsPerVendor int, matrix [][]int) []vendorRecord {
	totals := make([]int, n)
	for _, row := range matrix {
		for m, c := range row {
			totals[m] += c
		}
	}
	out := make([]vendorRecord, n)
	for i := range out {
		itemIDs := make([]string, itemsPerVendor)
		for j := range itemIDs {
			itemIDs[j] = uuid.NewString()
		}
		out[i] = vendorRecord{
			ID:        uuid.NewString(),
			Index:     i + 1,
			Name:      fmt.Sprintf("測試商家 %03d", i+1),
			UserID:    uuid.NewString(),
			UserEmail: merchantUserEmail(runID, i+1),
			Total:     totals[i],
			ItemIDs:   itemIDs,
		}
	}
	return out
}

func makeEmployees(runID string, plants []plantRecord) []employeeRecord {
	total := 0
	for _, p := range plants {
		total += p.Count
	}
	out := make([]employeeRecord, 0, total)
	idx := 1
	for _, p := range plants {
		for i := 0; i < p.Count; i++ {
			out = append(out, employeeRecord{
				ID:    uuid.NewString(),
				Index: idx,
				Email: employeeEmail(runID, idx),
				Plant: p.Code,
				Order: uuid.NewString(),
			})
			idx++
		}
	}
	return out
}

func assignOrders(cfg config, plants []plantRecord, vendors []vendorRecord, employees []employeeRecord, matrix [][]int) {
	byPlantStart := make([]int, len(plants))
	cursor := 0
	for i, p := range plants {
		byPlantStart[i] = cursor
		cursor += p.Count
	}
	for pIdx, row := range matrix {
		pos := byPlantStart[pIdx]
		for mIdx, n := range row {
			for i := 0; i < n; i++ {
				employees[pos].Vendor = vendors[mIdx].ID
				pos++
			}
		}
	}
}

func orderItemForEmployee(vendors []vendorRecord, e employeeRecord) string {
	for _, v := range vendors {
		if v.ID == e.Vendor {
			return v.ItemIDs[(e.Index-1)%len(v.ItemIDs)]
		}
	}
	return vendors[0].ItemIDs[0]
}

func rebuildMatrix(plants []plantRecord, vendors []vendorRecord, orders []orderRecord) [][]int {
	pIdx := map[string]int{}
	for i, p := range plants {
		pIdx[p.Code] = i
	}
	vIdx := map[string]int{}
	for i, v := range vendors {
		vIdx[v.ID] = i
	}
	matrix := make([][]int, len(plants))
	for i := range matrix {
		matrix[i] = make([]int, len(vendors))
	}
	for _, o := range orders {
		pi, pok := pIdx[o.Plant]
		vi, vok := vIdx[o.VendorID]
		if pok && vok {
			matrix[pi][vi]++
		}
	}
	for i := range plants {
		total := 0
		for _, n := range matrix[i] {
			total += n
		}
		plants[i].Count = total
	}
	for i := range vendors {
		total := 0
		for p := range matrix {
			total += matrix[p][i]
		}
		vendors[i].Total = total
	}
	return matrix
}

func buildSetupReport(cfg config, data *scenarioData) *setupReport {
	pickupCounts := make([]int, len(data.Plants))
	for i, p := range data.Plants {
		pickupCounts[i] = p.Count
	}
	merchantTotals := make([]int, len(data.Vendors))
	for i, v := range data.Vendors {
		merchantTotals[i] = v.Total
	}
	mappings := 0
	for _, row := range data.Matrix {
		for _, n := range row {
			if n > 0 {
				mappings++
			}
		}
	}
	return &setupReport{
		Employees:           len(data.Employees),
		MerchantUsers:       len(data.Vendors),
		Vendors:             len(data.Vendors),
		PickupPoints:        len(data.Plants),
		MenuItems:           len(data.Vendors) * cfg.ItemsPerVendor,
		MealSupplyRows:      len(data.Vendors) * cfg.ItemsPerVendor,
		Orders:              len(data.Orders),
		VendorPlantMappings: mappings,
		PickupDistribution:  summarizeDistribution("P", pickupCounts),
		MerchantTotals:      summarizeDistribution("M", merchantTotals),
		Stage1Batches:       len(buildReadyBatches(data, cfg.Stage1BatchSize)),
	}
}

func summarizeDistribution(prefix string, values []int) distribution {
	d := distribution{ByIndex: map[string]int{}, Deciles: map[string]int{}, Examples: map[string]int{}}
	if len(values) == 0 {
		return d
	}
	d.Min = values[0]
	d.Max = values[0]
	total := 0
	for i, v := range values {
		if v < d.Min {
			d.Min = v
		}
		if v > d.Max {
			d.Max = v
		}
		total += v
		d.ByIndex[fmt.Sprintf("%s%03d", prefix, i+1)] = v
	}
	d.Total = total
	d.Mean = float64(total) / float64(len(values))
	for start := 0; start < len(values); start += 10 {
		end := start + 10
		if end > len(values) {
			end = len(values)
		}
		sum := 0
		for _, v := range values[start:end] {
			sum += v
		}
		d.Deciles[fmt.Sprintf("%s%03d-%s%03d", prefix, start+1, prefix, end)] = sum
	}
	for _, idx := range []int{1, 10, 25, 50, 51, 75, 90, 100} {
		if idx <= len(values) {
			d.Examples[fmt.Sprintf("%s%03d", prefix, idx)] = values[idx-1]
		}
	}
	return d
}

func writeReport(path string, rep *report) error {
	if path == "" {
		return nil
	}
	raw, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		return fmt.Errorf("write report %s: %w", path, err)
	}
	return nil
}

func printReport(rep *report) {
	fmt.Println()
	fmt.Println("Lunch Gaussian Flow Report")
	fmt.Println("==========================")
	fmt.Printf("run_id: %s\n", rep.RunID)
	fmt.Printf("mode: %s, cleanup: %s\n", rep.Config.Mode, rep.Config.CleanupMode)
	if rep.Setup != nil {
		s := rep.Setup
		fmt.Printf("setup: employees=%s vendors=%s merchant_users=%s pickup_points=%s menu_items=%s orders=%s mappings=%s stage1_batches=%s\n",
			comma(s.Employees), comma(s.Vendors), comma(s.MerchantUsers), comma(s.PickupPoints), comma(s.MenuItems), comma(s.Orders), comma(s.VendorPlantMappings), comma(s.Stage1Batches))
		fmt.Printf("pickup distribution: total=%s min=%s max=%s mean=%.1f examples=%s\n",
			comma(s.PickupDistribution.Total), comma(s.PickupDistribution.Min), comma(s.PickupDistribution.Max), s.PickupDistribution.Mean, formatExamples(s.PickupDistribution.Examples))
		fmt.Printf("merchant totals: total=%s min=%s max=%s mean=%.1f examples=%s\n",
			comma(s.MerchantTotals.Total), comma(s.MerchantTotals.Min), comma(s.MerchantTotals.Max), s.MerchantTotals.Mean, formatExamples(s.MerchantTotals.Examples))
	}
	if rep.Stage1 != nil {
		printMetrics("stage 1 prepare food", rep.Stage1)
	}
	if rep.Stage2 != nil {
		printMetrics("stage 2 take food", rep.Stage2)
	}
	if rep.Cleanup != nil {
		c := rep.Cleanup
		fmt.Printf("cleanup: mode=%s orders=%s users=%s vendors=%s plants=%s audit=%s state_events=%s outbox=%s revoked_session_users=%s duration=%s\n",
			c.Mode, comma64(c.DeletedOrders), comma64(c.DeletedUsers), comma64(c.DeletedVendors), comma64(c.DeletedPlants), comma64(c.DeletedAuditEvents), comma64(c.DeletedStateEvents), comma64(c.DeletedOutboxEvents), comma(c.RevokedSessionUsers), c.Duration)
	}
	for _, w := range rep.Warnings {
		fmt.Printf("warning: %s\n", w)
	}
	for _, e := range rep.Errors {
		fmt.Printf("error: %s\n", e)
	}
}

func printMetrics(label string, m *metricsReport) {
	fmt.Printf("%s: requests=%s orders=%s success=%s failed=%s p50=%s p95=%s p99=%s max=%s statuses=%v\n",
		label, comma(m.Requests), comma(m.Orders), comma(m.Success), comma(m.Failed),
		m.LatencyP50, m.LatencyP95, m.LatencyP99, m.LatencyMax, m.ByStatus)
	if len(m.ByOperation) > 0 {
		fmt.Printf("%s operations: %v\n", label, m.ByOperation)
	}
	if len(m.Errors) > 0 {
		fmt.Printf("%s errors: %v\n", label, m.Errors)
	}
}

func formatExamples(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s:%s", k, comma(m[k])))
	}
	return strings.Join(parts, " ")
}

func comma(v int) string {
	return comma64(int64(v))
}

func comma64(v int64) string {
	s := fmt.Sprintf("%d", v)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre == 0 {
		pre = 3
	}
	b.WriteString(s[:pre])
	for i := pre; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func defaultSupplyDate() string {
	return time.Now().In(taipei()).Format("2006-01-02")
}

func taipei() *time.Location {
	return time.FixedZone("Asia/Taipei", 8*60*60)
}

var runIDRe = regexp.MustCompile(`[^a-z0-9-]+`)

func sanitizeRunID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = runIDRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "lf-" + time.Now().In(taipei()).Format("20060102150405")
	}
	if len(s) > 40 {
		s = strings.TrimRight(s[:40], "-")
	}
	return s
}

func runNote(runID string) string {
	return "lunch-flow:" + runID
}

func plantCode(runID string, idx int) string {
	return fmt.Sprintf("lf-%s-p%03d", runID, idx)
}

func employeeEmailPrefix(runID string) string {
	return "lunch-flow+" + runID + "-employee-"
}

func employeeEmail(runID string, idx int) string {
	return fmt.Sprintf("%s%05d@local.invalid", employeeEmailPrefix(runID), idx)
}

func merchantEmailPrefix(runID string) string {
	return "lunch-flow+" + runID + "-merchant-"
}

func merchantEmail(runID string, idx int) string {
	return fmt.Sprintf("%s%03d@local.invalid", merchantEmailPrefix(runID), idx)
}

func merchantUserEmailPrefix(runID string) string {
	return "lunch-flow+" + runID + "-operator-"
}

func merchantUserEmail(runID string, idx int) string {
	return fmt.Sprintf("%s%03d@local.invalid", merchantUserEmailPrefix(runID), idx)
}
