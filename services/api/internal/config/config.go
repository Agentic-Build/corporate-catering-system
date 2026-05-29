package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Role string

const (
	// Application HTTP role.
	RoleAPI Role = "api"

	// MCP stdio transport for local AI clients.
	RoleMCPStdio Role = "mcp-stdio"

	// Cloud-native split worker roles (architecture #56): each runs as an
	// independent Deployment with its own scaling rule and DLQ behaviour.
	RoleOutboxRelay      Role = "outbox-relay"
	RolePayrollSettler   Role = "payroll-settler"
	RoleOnTimeEvaluator  Role = "on-time-evaluator"
	RoleCutoffSweeper    Role = "cutoff-sweeper"
	RoleNoShowSweeper    Role = "no-show-sweeper"
	RoleDocExpiryScanner Role = "document-expiry-scanner"
	RoleFeedbackScanner  Role = "feedback-scanner"

	// Realtime SSE gateway (architecture #58): serves only the long-lived
	// SSE endpoints, consuming from JetStream, so API request pods don't
	// carry the long-connection load.
	RoleRealtimeGateway Role = "realtime-gateway"

	// One-shot provisioning role (architecture #62): Helm hook Job that
	// declares JetStream streams + consumers and exits.
	RoleProvisionStreams Role = "provision-streams"
)

type Config struct {
	Role     Role
	HTTPAddr string
	LogLevel string

	// DatabaseRW = primary write conn string; DatabaseRO = replica fan-out
	// (falls back to RW when empty for small deployments). ADR-0007 / #54.
	DatabaseRW            string
	DatabaseRO            string
	DBMaxConns            int32
	DBMinConns            int32
	DBMaxConnsRO          int32
	DBMinConnsRO          int32
	DBConnectRetryTimeout time.Duration

	RedisURL                 string
	RedisConnectRetryTimeout time.Duration
	NATSURL                  string

	AuthProviders []AuthProviderConfig

	// OIDCCallbackBaseURL is the Go API's own externally reachable base URL.
	OIDCCallbackBaseURL string

	AuthentikBaseURL             string
	AuthentikAPIToken            string
	AuthentikVendorOperatorGroup string

	// Per-app SPA base URLs used to build the post-login landing redirect.
	AppBaseURLEmployee string
	AppBaseURLMerchant string
	AppBaseURLAdmin    string

	// S3-compatible object storage. Optional at boot — only worker roles require it.
	S3Endpoint        string
	S3Region          string
	S3AccessKeyID     string
	S3SecretAccessKey string
	S3Bucket          string
	S3UsePathStyle    bool
	S3PublicBaseURL   string

	// Hydra sidecar URLs fronting our OAuth surface so MCP clients get RFC 7591
	// DCR (Authentik 2026.2 lacks it). Empty HydraPublicURL → MCP metadata
	// advertises Authentik directly (no DCR).
	HydraPublicURL string
	HydraAdminURL  string

	// NATSStreamReplicas sets the JetStream stream replica count for HA deployments.
	// Defaults to 1 (single-node dev); set to 3 in clustered production.
	NATSStreamReplicas int

	RealtimePreStopDrainSeconds int
}

type AuthProviderConfig struct {
	Slug         string
	DisplayName  string
	IssuerURL    string
	ClientID     string
	ClientSecret string
	Scopes       []string
}

func FromEnv() (Config, error) {
	providers, err := authProvidersFromEnv()
	if err != nil {
		return Config{}, err
	}
	c := Config{
		HTTPAddr: getenv("HTTP_ADDR", ":8080"),
		LogLevel: getenv("LOG_LEVEL", "info"),

		DatabaseRW:               os.Getenv("DATABASE_RW_URL"),
		DatabaseRO:               os.Getenv("DATABASE_RO_URL"),
		DBMaxConns:               envInt32("DB_MAX_CONNS", 16),
		DBMinConns:               envInt32("DB_MIN_CONNS", 2),
		DBMaxConnsRO:             envInt32("DB_MAX_CONNS_RO", 16),
		DBMinConnsRO:             envInt32("DB_MIN_CONNS_RO", 2),
		DBConnectRetryTimeout:    time.Duration(envNonNegativeInt("DB_CONNECT_RETRY_TIMEOUT_SECONDS", 0)) * time.Second,
		RedisURL:                 os.Getenv("REDIS_URL"),
		RedisConnectRetryTimeout: time.Duration(envNonNegativeInt("REDIS_CONNECT_RETRY_TIMEOUT_SECONDS", 0)) * time.Second,
		NATSURL:                  os.Getenv("NATS_URL"),

		AuthProviders: providers,

		OIDCCallbackBaseURL: getenv("OIDC_CALLBACK_BASE_URL", "http://api.tbite.local"),
		AuthentikBaseURL:    getenv("AUTHENTIK_BASE_URL", "http://auth.tbite.local"),
		AuthentikAPIToken:   os.Getenv("AUTHENTIK_API_TOKEN"),
		AuthentikVendorOperatorGroup: getenv(
			"AUTHENTIK_VENDOR_OPERATOR_GROUP",
			"tbite:role:vendor_operator",
		),

		AppBaseURLEmployee: getenv("APP_BASE_URL_EMPLOYEE", "http://app.tbite.local"),
		AppBaseURLMerchant: getenv("APP_BASE_URL_MERCHANT", "http://merchant.tbite.local"),
		AppBaseURLAdmin:    getenv("APP_BASE_URL_ADMIN", "http://admin.tbite.local"),

		S3Endpoint:        os.Getenv("S3_ENDPOINT"),
		S3Region:          os.Getenv("S3_REGION"),
		S3AccessKeyID:     os.Getenv("S3_ACCESS_KEY_ID"),
		S3SecretAccessKey: os.Getenv("S3_SECRET_ACCESS_KEY"),
		S3Bucket:          getenv("S3_BUCKET", "tbite-dev"),
		S3UsePathStyle:    os.Getenv("S3_USE_PATH_STYLE") == "1",
		S3PublicBaseURL:   getenv("S3_PUBLIC_BASE_URL", "http://minio.tbite.local"),

		HydraPublicURL: strings.TrimRight(os.Getenv("HYDRA_PUBLIC_URL"), "/"),
		HydraAdminURL:  strings.TrimRight(os.Getenv("HYDRA_ADMIN_URL"), "/"),

		NATSStreamReplicas:          envPositiveInt("NATS_STREAM_REPLICAS", 1),
		RealtimePreStopDrainSeconds: envNonNegativeInt("TBITE_REALTIME_PRESTOP_DRAIN_SECONDS", 30),
	}
	if c.DatabaseRW == "" {
		return c, fmt.Errorf("config: DATABASE_RW_URL is required")
	}
	if c.RedisURL == "" {
		return c, fmt.Errorf("config: REDIS_URL is required")
	}
	return c, nil
}

func getenv(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}

// envInt32 parses an integer env var, returning def on unset or parse error.
// Used for DB pool budgets so HPA/KEDA scaling has a known total backend budget.
func envInt32(k string, def int32) int32 {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return int32(n)
}

func envPositiveInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func envNonNegativeInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}

func authProvidersFromEnv() ([]AuthProviderConfig, error) {
	raw := strings.TrimSpace(os.Getenv("AUTH_PROVIDER_SLUGS"))
	if raw == "" {
		return nil, nil
	}
	slugs := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	out := make([]AuthProviderConfig, 0, len(slugs))
	seen := map[string]struct{}{}
	for _, slug := range slugs {
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}
		if _, ok := seen[slug]; ok {
			return nil, fmt.Errorf("config: duplicate auth provider %q", slug)
		}
		seen[slug] = struct{}{}
		prefix := "AUTH_PROVIDER_" + envSlug(slug) + "_"
		cfg := AuthProviderConfig{
			Slug:         slug,
			DisplayName:  getenv(prefix+"DISPLAY_NAME", slug),
			IssuerURL:    os.Getenv(prefix + "ISSUER_URL"),
			ClientID:     os.Getenv(prefix + "CLIENT_ID"),
			ClientSecret: os.Getenv(prefix + "CLIENT_SECRET"),
			Scopes:       splitScopes(getenv(prefix+"SCOPES", "openid email profile tbite")),
		}
		out = append(out, cfg)
	}
	return out, nil
}

func envSlug(slug string) string {
	slug = strings.ToUpper(slug)
	slug = strings.ReplaceAll(slug, "-", "_")
	return strings.ReplaceAll(slug, ".", "_")
}

func splitScopes(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func ParseRole(s string) (Role, error) {
	switch Role(s) {
	case RoleAPI, RoleMCPStdio,
		RoleOutboxRelay, RolePayrollSettler, RoleOnTimeEvaluator,
		RoleCutoffSweeper, RoleNoShowSweeper,
		RoleDocExpiryScanner, RoleFeedbackScanner,
		RoleRealtimeGateway, RoleProvisionStreams:
		return Role(s), nil
	default:
		return "", fmt.Errorf("invalid role %q", s)
	}
}

// EffectiveDatabaseRO returns DatabaseRO when set, else DatabaseRW.
// Hot read paths (menu, home, recommendation) should ask for the RO pool by
// name; small single-DB deployments still get a working conn string.
func (c Config) EffectiveDatabaseRO() string {
	if c.DatabaseRO == "" {
		return c.DatabaseRW
	}
	return c.DatabaseRO
}
