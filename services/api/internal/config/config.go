package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Role string

const (
	RoleAPI       Role = "api"
	RoleWorker    Role = "worker"
	RoleScheduler Role = "scheduler"
	RoleMCPStdio  Role = "mcp-stdio"
)

type Config struct {
	Role     Role
	HTTPAddr string
	LogLevel string

	DatabaseRW string
	RedisURL   string
	NATSURL    string

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

	// S3-compatible object storage (MinIO local / AWS S3 / GCS HMAC). Used by
	// the payroll settler worker. Optional at boot — only the worker role
	// actually requires it, so we don't validate here.
	S3Endpoint        string
	S3Region          string
	S3AccessKeyID     string
	S3SecretAccessKey string
	S3Bucket          string
	S3UsePathStyle    bool
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

		DatabaseRW: os.Getenv("DATABASE_RW_URL"),
		RedisURL:   os.Getenv("REDIS_URL"),
		NATSURL:    os.Getenv("NATS_URL"),

		AuthProviders: providers,

		OIDCCallbackBaseURL: getenv("OIDC_CALLBACK_BASE_URL", "http://api.tbite.test"),
		AuthentikBaseURL:    getenv("AUTHENTIK_BASE_URL", "http://localhost:9002"),
		AuthentikAPIToken:   os.Getenv("AUTHENTIK_API_TOKEN"),
		AuthentikVendorOperatorGroup: getenv(
			"AUTHENTIK_VENDOR_OPERATOR_GROUP",
			"tbite:role:vendor_operator",
		),

		AppBaseURLEmployee: getenv("APP_BASE_URL_EMPLOYEE", "http://app.tbite.test"),
		AppBaseURLMerchant: getenv("APP_BASE_URL_MERCHANT", "http://merchant.tbite.test"),
		AppBaseURLAdmin:    getenv("APP_BASE_URL_ADMIN", "http://admin.tbite.test"),

		S3Endpoint:        os.Getenv("S3_ENDPOINT"),
		S3Region:          os.Getenv("S3_REGION"),
		S3AccessKeyID:     os.Getenv("S3_ACCESS_KEY_ID"),
		S3SecretAccessKey: os.Getenv("S3_SECRET_ACCESS_KEY"),
		S3Bucket:          getenv("S3_BUCKET", "tbite"),
		S3UsePathStyle:    os.Getenv("S3_USE_PATH_STYLE") == "1",
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
	case RoleAPI, RoleWorker, RoleScheduler, RoleMCPStdio:
		return Role(s), nil
	default:
		return "", fmt.Errorf("invalid role %q (want api|worker|scheduler|mcp-stdio)", s)
	}
}

func MustParsePort(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		panic(err)
	}
	return n
}
