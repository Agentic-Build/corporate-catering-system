// Command stress drives realistic traffic against a running tbite API so the
// observability dashboards have something to display. Provisions synthetic
// users idempotently, mints Bearer sessions via the same Redis SessionStore
// the API consumes, then spawns N workers picking weighted scenarios.
//
// Usage:
//
//	go run ./services/api/cmd/stress --duration=5m --rps=20 --scenario=mixed
//	go run ./services/api/cmd/stress --scenario=lunch-crunch --duration=2m
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
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	pgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/postgres"
	idredis "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/redis"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/cache"
)

func main() {
	var (
		baseURL      = flag.String("base-url", envOr("STRESS_BASE_URL", "http://localhost:8080"), "API base URL")
		dbURL        = flag.String("db", envOr("DATABASE_RW_URL", "postgres://tbite:tbite@localhost:5432/tbite?sslmode=disable"), "Postgres DSN")
		redisURL     = flag.String("redis", envOr("REDIS_URL", "redis://localhost:6379"), "Redis URL")
		employees    = flag.Int("employees", 30, "Synthetic employees to provision (one per plant×N)")
		plantsCSV    = flag.String("plants", "hc-12a-1f,hc-12a-3f,hc-12b-1f,tc-15a-1f,tn-18p1-1f,tn-18p3-1f,tn-18p7-2f", "Comma-separated plant codes")
		duration     = flag.Duration("duration", 5*time.Minute, "Total run duration")
		concurrency  = flag.Int("concurrency", 8, "Concurrent worker count")
		rps          = flag.Float64("rps", 5, "Target requests-per-second per worker (overall ≈ rps × concurrency)")
		scenario     = flag.String("scenario", "mixed", "mixed|place-only|cancel-storm|browse|adjust-supply|lunch-crunch|modify-storm|pickup-flood")
		targetPlant  = flag.String("target-plant", "hc-12a-1f", "Plant focus for lunch-crunch / focused scenarios")
		targetVendor = flag.String("target-vendor", "a1111111-1111-1111-1111-111111111111", "Vendor focus for lunch-crunch")
		hotItem      = flag.String("hot-item", "4f26e612-b35f-5500-8f2a-63eded235675", "Single menu item that focused scenarios prefer (drives quota exhaustion)")
		quiet        = flag.Bool("quiet", false, "Suppress per-request log lines (still prints summary)")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if *quiet {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	summary := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	timed, cancelTimed := context.WithTimeout(ctx, *duration)
	defer cancelTimed()

	pool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		summary.Error("postgres connect", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	rdb, err := cache.NewClient(ctx, *redisURL)
	if err != nil {
		summary.Error("redis connect", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	plants := strings.Split(*plantsCSV, ",")
	for i := range plants {
		plants[i] = strings.TrimSpace(plants[i])
	}

	userRepo := pgrepo.NewUserRepo(pool)
	sess := idredis.NewSessionStore(rdb, 24*time.Hour)

	users, err := provisionUsers(ctx, pool, userRepo, sess, *employees, plants, summary)
	if err != nil {
		summary.Error("provision users", "err", err)
		os.Exit(1)
	}

	menus, err := loadMenus(ctx, pool)
	if err != nil {
		summary.Error("load menus", "err", err)
		os.Exit(1)
	}
	ready, err := loadReadyOrders(ctx, pool)
	if err != nil {
		summary.Error("load ready orders", "err", err)
		os.Exit(1)
	}
	ownerTokens, err := mintReadyOwnerSessions(ctx, pool, sess, ready)
	if err != nil {
		summary.Error("mint ready owner sessions", "err", err)
		os.Exit(1)
	}
	summary.Info("seed ready",
		"employees", len(users.employees),
		"merchants", len(users.merchants),
		"admins", 1,
		"vendors", len(menus),
		"ready_orders", len(ready),
		"ready_owner_tokens", len(ownerTokens),
	)

	picker := buildPicker(*scenario)
	if picker == nil {
		summary.Error("unknown scenario", "scenario", *scenario)
		os.Exit(2)
	}

	st := &stats{startedAt: time.Now()}
	client := &http.Client{Timeout: 15 * time.Second}
	state := &runState{
		baseURL:      strings.TrimRight(*baseURL, "/"),
		client:       client,
		users:        users,
		menus:        menus,
		plants:       plants,
		stats:        st,
		log:          logger,
		readyOrders:  ready,
		ownerTokens:  ownerTokens,
		targetPlant:  *targetPlant,
		targetVendor: *targetVendor,
		hotItem:      *hotItem,
		// recentOrders is a per-user ring of order IDs the user has placed but
		// not yet cancelled; cancel/modify scenarios pop from it.
		recentOrders: map[string]*orderRing{},
	}
	for _, u := range users.employees {
		state.recentOrders[u.user.ID] = newOrderRing(64)
	}

	summary.Info("stress starting",
		"scenario", *scenario,
		"duration", *duration,
		"concurrency", *concurrency,
		"rps_per_worker", *rps,
		"target_total_rps", float64(*concurrency)*(*rps),
	)

	var wg sync.WaitGroup
	for w := 0; w < *concurrency; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			interval := time.Duration(float64(time.Second) / *rps)
			if interval <= 0 {
				interval = time.Millisecond
			}
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-timed.Done():
					return
				case <-ticker.C:
					scenarioName := picker()
					runScenario(timed, state, scenarioName, workerID)
				}
			}
		}(w)
	}
	wg.Wait()
	st.print(summary, *scenario)
}

type userToken struct {
	user  *identity.User
	token string
}

type userPool struct {
	employees []*userToken
	// merchants maps vendor_id -> bearer-equipped vendor_operator user.
	merchants map[string]*userToken
	admin     *userToken
}

func provisionUsers(ctx context.Context, pool *pgxpool.Pool, userRepo *pgrepo.UserRepo, sess identity.SessionStore, nEmployees int, plants []string, log *slog.Logger) (*userPool, error) {
	out := &userPool{merchants: map[string]*userToken{}}

	for i := 0; i < nEmployees; i++ {
		plant := plants[i%len(plants)]
		empID := fmt.Sprintf("stress-emp-%03d", i)
		email := fmt.Sprintf("stress-employee-%03d@local.invalid", i)
		u, err := upsertUser(ctx, pool, userRepo, &identity.User{
			PrimaryEmail: email,
			DisplayName:  fmt.Sprintf("Stress Employee %03d", i),
			Role:         identity.RoleEmployee,
			Status:       identity.StatusActive,
			EmployeeID:   strPtr(empID),
			Plant:        strPtr(plant),
		})
		if err != nil {
			return nil, fmt.Errorf("employee %d: %w", i, err)
		}
		tok, err := sess.Create(ctx, u.ID, u.Role)
		if err != nil {
			return nil, fmt.Errorf("employee %d session: %w", i, err)
		}
		out.employees = append(out.employees, &userToken{user: u, token: tok.Token})
	}

	rows, err := pool.Query(ctx, `SELECT id FROM vendor WHERE status='approved' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list vendors: %w", err)
	}
	defer rows.Close()
	var vendorIDs []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		vendorIDs = append(vendorIDs, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, vid := range vendorIDs {
		email := fmt.Sprintf("stress-merchant-%s@local.invalid", vid[:8])
		u, err := upsertUser(ctx, pool, userRepo, &identity.User{
			PrimaryEmail: email,
			DisplayName:  "Stress Merchant " + vid[:8],
			Role:         identity.RoleVendorOperator,
			Status:       identity.StatusActive,
			VendorID:     strPtr(vid),
		})
		if err != nil {
			return nil, fmt.Errorf("merchant %s: %w", vid, err)
		}
		tok, err := sess.Create(ctx, u.ID, u.Role)
		if err != nil {
			return nil, err
		}
		out.merchants[vid] = &userToken{user: u, token: tok.Token}
	}

	adminUser, err := upsertUser(ctx, pool, userRepo, &identity.User{
		PrimaryEmail: "stress-admin@local.invalid",
		DisplayName:  "Stress Admin",
		Role:         identity.RoleWelfareAdmin,
		Status:       identity.StatusActive,
	})
	if err != nil {
		return nil, fmt.Errorf("admin: %w", err)
	}
	adminTok, err := sess.Create(ctx, adminUser.ID, adminUser.Role)
	if err != nil {
		return nil, err
	}
	out.admin = &userToken{user: adminUser, token: adminTok.Token}
	log.Info("provisioned",
		"employees", len(out.employees),
		"merchants", len(out.merchants),
		"admin", out.admin.user.PrimaryEmail,
	)
	return out, nil
}

func upsertUser(ctx context.Context, pool *pgxpool.Pool, repo *pgrepo.UserRepo, in *identity.User) (*identity.User, error) {
	if u, err := repo.GetByEmail(ctx, in.PrimaryEmail); err == nil {
		// Refresh plant/vendor in case seed params changed.
		if _, err := pool.Exec(ctx,
			`UPDATE "user" SET role=$2, status=$3, plant=$4, vendor_id=$5, employee_id=$6, display_name=$7, updated_at=now() WHERE id=$1`,
			u.ID, string(in.Role), string(in.Status), in.Plant, in.VendorID, in.EmployeeID, in.DisplayName,
		); err != nil {
			return nil, err
		}
		return repo.GetByID(ctx, u.ID)
	} else if !errors.Is(err, identity.ErrUserNotFound) {
		return nil, err
	}
	if err := repo.Create(ctx, in); err != nil {
		return nil, err
	}
	return in, nil
}

// vendorMenu keys vendors to the {plants, items} they cover so place-order
// scenarios can target valid combinations.
type vendorMenu struct {
	vendorID string
	plants   []string
	items    []menuItem
}

type menuItem struct {
	id    string
	price int64
}

// loadReadyOrders snapshots up to 1000 currently-READY orders. The pickup-flood
// scenario uses this list as a pool of valid order IDs to verify against, so
// it doesn't have to navigate the cutoff/mark-ready flow inside the stress
// loop. The snapshot is per-process — if external traffic transitions more
// orders mid-run we won't see them, but the bound keeps the slice cheap.
func loadReadyOrders(ctx context.Context, pool *pgxpool.Pool) ([]readyOrder, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, vendor_id, user_id FROM "order"
		WHERE status='ready'
		ORDER BY ready_at DESC NULLS LAST
		LIMIT 1000
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []readyOrder
	for rows.Next() {
		var r readyOrder
		if err := rows.Scan(&r.id, &r.vendorID, &r.userID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// mintReadyOwnerSessions mints a stress session for each unique user that owns
// a READY order so the pickup-flow scenario can authenticate AS the rightful
// owner and exercise the success path. Sessions for non-owners (foreign
// pickups → 403) still come from the stress employee pool.
func mintReadyOwnerSessions(ctx context.Context, pool *pgxpool.Pool, sess identity.SessionStore, ready []readyOrder) (map[string]string, error) {
	uniq := map[string]struct{}{}
	for _, r := range ready {
		uniq[r.userID] = struct{}{}
	}
	ids := make([]string, 0, len(uniq))
	for id := range uniq {
		ids = append(ids, id)
	}
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := pool.Query(ctx, `SELECT id, role FROM "user" WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, role string
		if err := rows.Scan(&id, &role); err != nil {
			return nil, err
		}
		s, err := sess.Create(ctx, id, identity.Role(role))
		if err != nil {
			return nil, err
		}
		out[id] = s.Token
	}
	return out, rows.Err()
}

func loadMenus(ctx context.Context, pool *pgxpool.Pool) ([]vendorMenu, error) {
	rows, err := pool.Query(ctx, `
		SELECT mi.id, mi.vendor_id, mi.price_minor,
		       COALESCE(array_agg(DISTINCT vpm.plant), ARRAY[]::text[]) AS plants
		FROM menu_item mi
		JOIN vendor_plant_mapping vpm ON vpm.vendor_id = mi.vendor_id
		WHERE mi.status = 'active'
		GROUP BY mi.id, mi.vendor_id, mi.price_minor
		ORDER BY mi.vendor_id, mi.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byVendor := map[string]*vendorMenu{}
	for rows.Next() {
		var id, vendorID string
		var price int64
		var plants []string
		if err := rows.Scan(&id, &vendorID, &price, &plants); err != nil {
			return nil, err
		}
		v, ok := byVendor[vendorID]
		if !ok {
			v = &vendorMenu{vendorID: vendorID, plants: plants}
			byVendor[vendorID] = v
		}
		v.items = append(v.items, menuItem{id: id, price: price})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]vendorMenu, 0, len(byVendor))
	for _, v := range byVendor {
		out = append(out, *v)
	}
	return out, nil
}

type runState struct {
	baseURL      string
	client       *http.Client
	users        *userPool
	menus        []vendorMenu
	plants       []string
	stats        *stats
	log          *slog.Logger
	mu           sync.Mutex
	recentOrders map[string]*orderRing
	// readyOrders is a snapshot of currently-READY orders, pulled directly
	// from Postgres at start-up by the pickup-flood scenario so workers can
	// hit /verify-pickup without first navigating cutoff/mark-ready.
	readyOrders []readyOrder
	// ownerTokens maps a READY order's owner user_id → minted bearer token.
	// pickup_self_owner authenticates as the rightful owner from this map.
	ownerTokens map[string]string
	// Focus knobs for lunch-crunch / modify-storm — narrowing pickers down
	// to a single plant×vendor×item turns a diffuse load test into a real
	// hotspot scenario.
	targetPlant  string
	targetVendor string
	hotItem      string
}

type readyOrder struct {
	id       string
	vendorID string
	userID   string
}

type stats struct {
	startedAt      time.Time
	requests       atomic.Int64
	httpStatus2xx  atomic.Int64
	httpStatus4xx  atomic.Int64
	httpStatus5xx  atomic.Int64
	netErrors      atomic.Int64
	scenarioCounts sync.Map // scenarioName -> *atomic.Int64
	outcomeCounts  sync.Map // domain outcome label -> *atomic.Int64
}

func (s *stats) recordScenario(name string) {
	v, _ := s.scenarioCounts.LoadOrStore(name, &atomic.Int64{})
	v.(*atomic.Int64).Add(1)
}

func (s *stats) recordOutcome(name string) {
	v, _ := s.outcomeCounts.LoadOrStore(name, &atomic.Int64{})
	v.(*atomic.Int64).Add(1)
}

func (s *stats) print(log *slog.Logger, scenarioMode string) {
	elapsed := time.Since(s.startedAt)
	total := s.requests.Load()
	log.Info("stress complete",
		"scenario_mode", scenarioMode,
		"elapsed", elapsed.Round(time.Second),
		"total_requests", total,
		"effective_rps", fmt.Sprintf("%.2f", float64(total)/elapsed.Seconds()),
		"2xx", s.httpStatus2xx.Load(),
		"4xx", s.httpStatus4xx.Load(),
		"5xx", s.httpStatus5xx.Load(),
		"net_errors", s.netErrors.Load(),
	)
	log.Info("--- scenario breakdown ---")
	s.scenarioCounts.Range(func(k, v any) bool {
		log.Info(" ", "scenario", k.(string), "count", v.(*atomic.Int64).Load())
		return true
	})
	log.Info("--- domain outcomes ---")
	s.outcomeCounts.Range(func(k, v any) bool {
		log.Info(" ", "outcome", k.(string), "count", v.(*atomic.Int64).Load())
		return true
	})
}

func buildPicker(mode string) func() string {
	type weight struct {
		name   string
		weight int
	}
	var mix []weight
	switch mode {
	case "mixed":
		mix = []weight{
			{"place_order", 45},
			{"browse_menu", 14},
			{"browse_orders", 9},
			{"cancel_order", 9},
			{"modify_order", 7},
			{"mark_ready", 6},
			{"adjust_supply", 4},
			{"list_supply", 3},
			{"pickup_self_owner", 2},
			{"pickup_foreign", 1},
		}
	case "place-only":
		mix = []weight{{"place_order", 1}}
	case "cancel-storm":
		mix = []weight{
			{"place_order", 30},
			{"cancel_order", 70},
		}
	case "browse":
		mix = []weight{
			{"browse_menu", 50},
			{"browse_orders", 30},
			{"list_supply", 20},
		}
	case "adjust-supply":
		mix = []weight{
			{"adjust_supply", 80},
			{"list_supply", 20},
		}
	case "lunch-crunch":
		// Simulates a sudden lunch peak focused on one vendor at one plant.
		// place_order_focused funnels every placement at a single hot item;
		// merchant_chase_supply races to add capacity. The mix is calibrated
		// so quota exhaustion outpaces capacity adjustments (~9:1 placement
		// vs. supply bumps), which is realistic for a flash-crowd lunch.
		mix = []weight{
			{"place_order_focused", 85},
			{"merchant_chase_supply", 10},
			{"browse_menu", 5},
		}
	case "modify-storm":
		// Heavy contention on a small pool of placed orders — every worker
		// races to modify/cancel the same set of recently placed orders,
		// driving up serialization failures (HTTP 409) on the order table.
		mix = []weight{
			{"place_order_focused", 30},
			{"modify_order", 35},
			{"cancel_order", 35},
		}
	case "pickup-flood":
		// Lunch-time self-service pickup peak. Mostly rightful owners scanning
		// their own QR (success path), with a tail of forbidden attempts from
		// employees scanning someone else's sticker (forbidden outcome) — the
		// real-world failure mode the new model exposes. Requires READY orders
		// in DB; see scripts/dev/promote-to-ready.sh.
		mix = []weight{
			{"pickup_self_owner", 65},
			{"pickup_foreign", 15},
			{"browse_orders", 20},
		}
	default:
		return nil
	}
	total := 0
	for _, w := range mix {
		total += w.weight
	}
	return func() string {
		n := rand.IntN(total)
		for _, w := range mix {
			if n < w.weight {
				return w.name
			}
			n -= w.weight
		}
		return mix[len(mix)-1].name
	}
}

func runScenario(ctx context.Context, st *runState, name string, workerID int) {
	st.stats.recordScenario(name)
	switch name {
	case "place_order":
		scenarioPlaceOrder(ctx, st)
	case "place_order_focused":
		scenarioPlaceOrderFocused(ctx, st)
	case "merchant_chase_supply":
		scenarioMerchantChaseSupply(ctx, st)
	case "pickup_self_owner":
		scenarioPickup(ctx, st, true)
	case "pickup_foreign":
		scenarioPickup(ctx, st, false)
	case "browse_menu":
		scenarioBrowseMenu(ctx, st)
	case "browse_orders":
		scenarioBrowseOrders(ctx, st)
	case "cancel_order":
		scenarioCancelOrder(ctx, st)
	case "modify_order":
		scenarioModifyOrder(ctx, st)
	case "adjust_supply":
		scenarioAdjustSupply(ctx, st)
	case "list_supply":
		scenarioListSupply(ctx, st)
	case "mark_ready":
		scenarioMarkReady(ctx, st)
	}
}

func scenarioPlaceOrder(ctx context.Context, st *runState) {
	emp := pickEmployee(st)
	plant := derefOr(emp.user.Plant, st.plants[0])
	vm := pickVendorForPlant(st, plant)
	if vm == nil {
		st.stats.recordOutcome("place:no_vendor")
		return
	}
	mi := vm.items[rand.IntN(len(vm.items))]
	supplyDate := pickFutureDate()
	body := map[string]any{
		"plant":       plant,
		"supply_date": supplyDate,
		"items":       []map[string]any{{"menu_item_id": mi.id, "qty": 1 + rand.IntN(2)}},
	}
	resp, status := postJSON(ctx, st, emp.token, "/api/employee/orders", body)
	switch {
	case status >= 200 && status < 300:
		var out struct {
			Order struct {
				ID string `json:"id"`
			} `json:"order"`
		}
		_ = json.Unmarshal(resp, &out)
		if out.Order.ID != "" {
			st.mu.Lock()
			st.recentOrders[emp.user.ID].push(out.Order.ID)
			st.mu.Unlock()
		}
		st.stats.recordOutcome("place:success")
	case status == 409:
		st.stats.recordOutcome("place:quota_or_conflict")
	case status == 422 || status == 400:
		st.stats.recordOutcome("place:cutoff_or_validation")
	default:
		st.stats.recordOutcome(fmt.Sprintf("place:http_%d", status))
	}
}

// scenarioPlaceOrderFocused funnels placement traffic at runState.targetPlant
// × runState.targetVendor × runState.hotItem. The narrow target deliberately
// hammers a single supply row so quota gets exhausted fast and the dashboards
// have something obviously hot to highlight.
//
// Only employees whose plant matches the target plant are eligible — the rest
// abstain (returning a "skipped" outcome) so traffic concentration follows
// the target rather than diluting across plants.
func scenarioPlaceOrderFocused(ctx context.Context, st *runState) {
	candidates := make([]*userToken, 0, len(st.users.employees))
	for _, e := range st.users.employees {
		if e.user.Plant != nil && *e.user.Plant == st.targetPlant {
			candidates = append(candidates, e)
		}
	}
	if len(candidates) == 0 {
		st.stats.recordOutcome("place_focused:no_plant_employee")
		return
	}
	emp := candidates[rand.IntN(len(candidates))]

	var vm *vendorMenu
	for i := range st.menus {
		if st.menus[i].vendorID == st.targetVendor {
			vm = &st.menus[i]
			break
		}
	}
	if vm == nil {
		st.stats.recordOutcome("place_focused:no_vendor")
		return
	}
	// Bias 70% toward the configured hot item; the remaining 30% spread
	// across the same vendor's other items so we still produce contrast
	// between "hot" and "cool" supply rows.
	var mi menuItem
	if rand.IntN(10) < 7 {
		for _, it := range vm.items {
			if it.id == st.hotItem {
				mi = it
				break
			}
		}
	}
	if mi.id == "" {
		mi = vm.items[rand.IntN(len(vm.items))]
	}

	supplyDate := pickFutureDate()
	body := map[string]any{
		"plant":       st.targetPlant,
		"supply_date": supplyDate,
		"items":       []map[string]any{{"menu_item_id": mi.id, "qty": 1 + rand.IntN(2)}},
	}
	resp, status := postJSON(ctx, st, emp.token, "/api/employee/orders", body)
	switch {
	case status >= 200 && status < 300:
		var out struct {
			Order struct {
				ID string `json:"id"`
			} `json:"order"`
		}
		_ = json.Unmarshal(resp, &out)
		if out.Order.ID != "" {
			st.mu.Lock()
			st.recentOrders[emp.user.ID].push(out.Order.ID)
			st.mu.Unlock()
		}
		st.stats.recordOutcome("place_focused:success")
	case status == 409:
		st.stats.recordOutcome("place_focused:quota_or_conflict")
	default:
		st.stats.recordOutcome(fmt.Sprintf("place_focused:http_%d", status))
	}
}

// scenarioMerchantChaseSupply is the merchant-side companion to lunch-crunch:
// the vendor whose item is being hammered tries to add capacity to keep up.
// Adjustments are small (+10..+30) to make the chase realistic — bigger jumps
// would solve the burst instantly and the dashboards would never see the
// quota-exhaustion fire.
func scenarioMerchantChaseSupply(ctx context.Context, st *runState) {
	merchant, ok := st.users.merchants[st.targetVendor]
	if !ok {
		st.stats.recordOutcome("merchant_chase:no_merchant")
		return
	}
	var vm *vendorMenu
	for i := range st.menus {
		if st.menus[i].vendorID == st.targetVendor {
			vm = &st.menus[i]
			break
		}
	}
	if vm == nil {
		return
	}
	mi := vm.items[rand.IntN(len(vm.items))]
	// Bias toward the hot item so the merchant chase is actually targeted
	// at the bottleneck.
	if rand.IntN(10) < 7 {
		for _, it := range vm.items {
			if it.id == st.hotItem {
				mi = it
				break
			}
		}
	}
	date := pickFutureDate()
	newCap := 80 + 10 + rand.IntN(20) // 90..110, small chase increment
	cutoffAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	body := map[string]any{
		"capacity":      newCap,
		"pickup_window": "11:50-12:10",
		"eta_label":     "11:50-12:10",
		"cutoff_at":     cutoffAt,
	}
	_, status := putJSON(ctx, st, merchant.token, fmt.Sprintf("/api/merchant/supply/%s/%s", mi.id, date), body)
	st.stats.recordOutcome(fmt.Sprintf("merchant_chase:http_%d", status))
}

// scenarioPickup exercises POST /api/employee/orders/{id}/pickup — the new
// employee self-service flow that replaced the vendor TOTP verify path in #26.
//
//	asOwner=true  : authenticate as the order's rightful owner (success path,
//	                or wrong_state if someone else already picked it up).
//	asOwner=false : authenticate as a random stress employee → 403 forbidden.
//
// Both modes drive the same metric (catering_order_pickup_verified_count_total)
// with different `outcome` labels so dashboards can distinguish a real scanner
// problem (forbidden spike) from a contention issue (concurrent_modification).
func scenarioPickup(ctx context.Context, st *runState, asOwner bool) {
	if len(st.readyOrders) == 0 {
		st.stats.recordOutcome("pickup:no_ready_order")
		return
	}
	st.mu.Lock()
	target := st.readyOrders[rand.IntN(len(st.readyOrders))]
	st.mu.Unlock()

	var token, modeTag string
	if asOwner {
		t, ok := st.ownerTokens[target.userID]
		if !ok {
			st.stats.recordOutcome("pickup:no_owner_token")
			return
		}
		token = t
		modeTag = "self"
	} else {
		if len(st.users.employees) == 0 {
			return
		}
		emp := st.users.employees[rand.IntN(len(st.users.employees))]
		token = emp.token
		modeTag = "foreign"
	}

	_, status := postJSON(ctx, st, token, "/api/employee/orders/"+target.id+"/pickup", map[string]any{})
	switch {
	case status >= 200 && status < 300:
		st.stats.recordOutcome("pickup_" + modeTag + ":success")
	case status == 403:
		st.stats.recordOutcome("pickup_" + modeTag + ":forbidden")
	case status == 409:
		st.stats.recordOutcome("pickup_" + modeTag + ":conflict_or_state")
	case status == 404:
		st.stats.recordOutcome("pickup_" + modeTag + ":not_found")
	default:
		st.stats.recordOutcome(fmt.Sprintf("pickup_%s:http_%d", modeTag, status))
	}
}

func scenarioBrowseMenu(ctx context.Context, st *runState) {
	emp := pickEmployee(st)
	plant := derefOr(emp.user.Plant, st.plants[0])
	date := pickFutureDate()
	_, status := getURL(ctx, st, emp.token, fmt.Sprintf("/api/employee/menu?plant=%s&date=%s", plant, date))
	st.stats.recordOutcome(fmt.Sprintf("browse_menu:http_%d", status))
}

func scenarioBrowseOrders(ctx context.Context, st *runState) {
	emp := pickEmployee(st)
	_, status := getURL(ctx, st, emp.token, "/api/employee/orders")
	st.stats.recordOutcome(fmt.Sprintf("browse_orders:http_%d", status))
}

func scenarioCancelOrder(ctx context.Context, st *runState) {
	emp := pickEmployee(st)
	st.mu.Lock()
	id, ok := st.recentOrders[emp.user.ID].pop()
	st.mu.Unlock()
	if !ok {
		st.stats.recordOutcome("cancel:no_order_to_cancel")
		return
	}
	_, status := postJSON(ctx, st, emp.token, "/api/employee/orders/"+id+"/cancel", map[string]any{})
	switch {
	case status >= 200 && status < 300:
		st.stats.recordOutcome("cancel:success")
	case status == 404:
		st.stats.recordOutcome("cancel:not_found")
	case status == 409 || status == 422:
		st.stats.recordOutcome("cancel:invalid_state")
	default:
		st.stats.recordOutcome(fmt.Sprintf("cancel:http_%d", status))
	}
}

func scenarioModifyOrder(ctx context.Context, st *runState) {
	emp := pickEmployee(st)
	st.mu.Lock()
	id, ok := st.recentOrders[emp.user.ID].peek()
	st.mu.Unlock()
	if !ok {
		st.stats.recordOutcome("modify:no_order_to_modify")
		return
	}
	plant := derefOr(emp.user.Plant, st.plants[0])
	vm := pickVendorForPlant(st, plant)
	if vm == nil {
		st.stats.recordOutcome("modify:no_vendor")
		return
	}
	mi := vm.items[rand.IntN(len(vm.items))]
	body := map[string]any{
		"items": []map[string]any{{"menu_item_id": mi.id, "qty": 1 + rand.IntN(2)}},
		"notes": "stress-modify",
	}
	_, status := putJSON(ctx, st, emp.token, "/api/employee/orders/"+id, body)
	switch {
	case status >= 200 && status < 300:
		st.stats.recordOutcome("modify:success")
	default:
		st.stats.recordOutcome(fmt.Sprintf("modify:http_%d", status))
	}
}

func scenarioAdjustSupply(ctx context.Context, st *runState) {
	if len(st.users.merchants) == 0 {
		return
	}
	vendorIDs := make([]string, 0, len(st.users.merchants))
	for k := range st.users.merchants {
		vendorIDs = append(vendorIDs, k)
	}
	vid := vendorIDs[rand.IntN(len(vendorIDs))]
	merchant := st.users.merchants[vid]
	var vm *vendorMenu
	for i := range st.menus {
		if st.menus[i].vendorID == vid {
			vm = &st.menus[i]
			break
		}
	}
	if vm == nil || len(vm.items) == 0 {
		return
	}
	mi := vm.items[rand.IntN(len(vm.items))]
	date := pickFutureDate()
	newCap := 40 + rand.IntN(160)
	cutoffAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	body := map[string]any{
		"capacity":      newCap,
		"pickup_window": "11:50-12:10",
		"eta_label":     "11:50-12:10",
		"cutoff_at":     cutoffAt,
	}
	url := fmt.Sprintf("/api/merchant/supply/%s/%s", mi.id, date)
	_, status := requestJSON(ctx, st, merchant.token, http.MethodPut, url, body)
	st.stats.recordOutcome(fmt.Sprintf("adjust_supply:http_%d", status))
}

func scenarioListSupply(ctx context.Context, st *runState) {
	if len(st.users.merchants) == 0 {
		return
	}
	vendorIDs := make([]string, 0, len(st.users.merchants))
	for k := range st.users.merchants {
		vendorIDs = append(vendorIDs, k)
	}
	vid := vendorIDs[rand.IntN(len(vendorIDs))]
	merchant := st.users.merchants[vid]
	date := pickFutureDate()
	_, status := getURL(ctx, st, merchant.token, "/api/merchant/supply?date="+date)
	st.stats.recordOutcome(fmt.Sprintf("list_supply:http_%d", status))
}

// scenarioMarkReady picks a random merchant, queries their PLACED orders for
// the next supply day, and marks up to 5 of them READY. This drives the
// catering_order_ready_count_total metric and the "Orders marked READY (24h)"
// pickup-floor stat — without it the pickup lifecycle has a missing link.
func scenarioMarkReady(ctx context.Context, st *runState) {
	if len(st.users.merchants) == 0 {
		return
	}
	vendorIDs := make([]string, 0, len(st.users.merchants))
	for k := range st.users.merchants {
		vendorIDs = append(vendorIDs, k)
	}
	vid := vendorIDs[rand.IntN(len(vendorIDs))]
	merchant := st.users.merchants[vid]
	date := pickFutureDate()
	body, status := getURL(ctx, st, merchant.token, "/api/merchant/orders?date="+date+"&status=placed")
	if status >= 400 {
		st.stats.recordOutcome(fmt.Sprintf("mark_ready:list_http_%d", status))
		return
	}
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil || len(list.Items) == 0 {
		st.stats.recordOutcome("mark_ready:no_placed")
		return
	}
	n := 1 + rand.IntN(5)
	if n > len(list.Items) {
		n = len(list.Items)
	}
	ids := make([]string, 0, n)
	for _, o := range list.Items[:n] {
		ids = append(ids, o.ID)
	}
	_, status = postJSON(ctx, st, merchant.token, "/api/merchant/orders/mark-ready", map[string]any{"order_ids": ids})
	switch {
	case status >= 200 && status < 300:
		st.stats.recordOutcome("mark_ready:success")
	case status == 409 || status == 422:
		st.stats.recordOutcome("mark_ready:invalid_state")
	default:
		st.stats.recordOutcome(fmt.Sprintf("mark_ready:http_%d", status))
	}
}

func postJSON(ctx context.Context, st *runState, token, path string, body any) ([]byte, int) {
	return requestJSON(ctx, st, token, http.MethodPost, path, body)
}

func putJSON(ctx context.Context, st *runState, token, path string, body any) ([]byte, int) {
	return requestJSON(ctx, st, token, http.MethodPut, path, body)
}

func getURL(ctx context.Context, st *runState, token, path string) ([]byte, int) {
	return requestJSON(ctx, st, token, http.MethodGet, path, nil)
}

func requestJSON(ctx context.Context, st *runState, token, method, path string, body any) ([]byte, int) {
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, st.baseURL+path, buf)
	if err != nil {
		st.stats.netErrors.Add(1)
		return nil, 0
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	st.stats.requests.Add(1)
	resp, err := st.client.Do(req)
	if err != nil {
		st.stats.netErrors.Add(1)
		return nil, 0
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	switch {
	case resp.StatusCode >= 500:
		st.stats.httpStatus5xx.Add(1)
	case resp.StatusCode >= 400:
		st.stats.httpStatus4xx.Add(1)
	default:
		st.stats.httpStatus2xx.Add(1)
	}
	return out, resp.StatusCode
}

func pickEmployee(st *runState) *userToken {
	return st.users.employees[rand.IntN(len(st.users.employees))]
}

func pickVendorForPlant(st *runState, plant string) *vendorMenu {
	candidates := make([]int, 0, len(st.menus))
	for i := range st.menus {
		for _, p := range st.menus[i].plants {
			if p == plant {
				candidates = append(candidates, i)
				break
			}
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	return &st.menus[candidates[rand.IntN(len(candidates))]]
}

// pickFutureDate returns a YYYY-MM-DD between today+1 and today+7. Today's
// supply has already passed the cutoff (vendor cutoff_hour=17 the previous
// evening), so we never target it from stress traffic.
func pickFutureDate() string {
	days := 1 + rand.IntN(7)
	t := time.Now().AddDate(0, 0, days)
	return t.Format("2006-01-02")
}

func strPtr(s string) *string { return &s }
func derefOr(p *string, d string) string {
	if p == nil {
		return d
	}
	return *p
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// orderRing is a tiny circular buffer of order IDs per user. Cancel/modify
// scenarios pull from it to act on orders the same user just placed.
type orderRing struct {
	buf  []string
	head int
	size int
}

func newOrderRing(cap int) *orderRing { return &orderRing{buf: make([]string, cap)} }

func (r *orderRing) push(id string) {
	r.buf[(r.head+r.size)%len(r.buf)] = id
	if r.size < len(r.buf) {
		r.size++
	} else {
		r.head = (r.head + 1) % len(r.buf)
	}
}

func (r *orderRing) pop() (string, bool) {
	if r.size == 0 {
		return "", false
	}
	r.size--
	id := r.buf[(r.head+r.size)%len(r.buf)]
	return id, true
}

func (r *orderRing) peek() (string, bool) {
	if r.size == 0 {
		return "", false
	}
	return r.buf[(r.head+r.size-1)%len(r.buf)], true
}
