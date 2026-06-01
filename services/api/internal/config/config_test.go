package config

import (
	"testing"
	"time"
)

func TestFromEnvLoadsAuthProviderRegistry(t *testing.T) {
	t.Setenv("DATABASE_RW_URL", "postgres://tbite:tbite@localhost:5432/tbite?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("AUTH_PROVIDER_SLUGS", "authentik, keycloak")
	t.Setenv("AUTH_PROVIDER_AUTHENTIK_DISPLAY_NAME", "Authentik")
	t.Setenv("AUTH_PROVIDER_AUTHENTIK_ISSUER_URL", "http://auth.tbite.local/application/o/tbite/")
	t.Setenv("AUTH_PROVIDER_AUTHENTIK_CLIENT_ID", "tbite")
	t.Setenv("AUTH_PROVIDER_AUTHENTIK_CLIENT_SECRET", "secret")
	t.Setenv("AUTH_PROVIDER_AUTHENTIK_SCOPES", "openid email tbite")
	t.Setenv("AUTH_PROVIDER_KEYCLOAK_DISPLAY_NAME", "Keycloak")
	t.Setenv("AUTH_PROVIDER_KEYCLOAK_ISSUER_URL", "https://idp.example.test/realms/tbite")
	t.Setenv("AUTH_PROVIDER_KEYCLOAK_CLIENT_ID", "tbite-keycloak")
	t.Setenv("AUTH_PROVIDER_KEYCLOAK_CLIENT_SECRET", "secret-2")
	t.Setenv("AUTH_PROVIDER_KEYCLOAK_SCOPES", "openid,email,profile")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if len(cfg.AuthProviders) != 2 {
		t.Fatalf("AuthProviders len = %d, want 2", len(cfg.AuthProviders))
	}
	if got := cfg.AuthProviders[0]; got.Slug != "authentik" || got.DisplayName != "Authentik" ||
		got.IssuerURL != "http://auth.tbite.local/application/o/tbite/" ||
		got.ClientID != "tbite" || got.ClientSecret != "secret" {
		t.Fatalf("unexpected authentik config: %#v", got)
	}
	if scopes := cfg.AuthProviders[1].Scopes; len(scopes) != 3 || scopes[0] != "openid" || scopes[1] != "email" || scopes[2] != "profile" {
		t.Fatalf("unexpected keycloak scopes: %#v", scopes)
	}
}

func TestFromEnvRejectsDuplicateAuthProviderSlugs(t *testing.T) {
	t.Setenv("AUTH_PROVIDER_SLUGS", "authentik authentik")

	_, err := FromEnv()
	if err == nil {
		t.Fatal("FromEnv() error = nil, want duplicate provider error")
	}
}

func TestNATSStreamReplicasDefaultsToOne(t *testing.T) {
	t.Setenv("DATABASE_RW_URL", "postgres://tbite:tbite@localhost:5432/tbite?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.NATSStreamReplicas != 1 {
		t.Fatalf("NATSStreamReplicas = %d, want 1", cfg.NATSStreamReplicas)
	}
}

func TestNATSStreamReplicasOverrideFromEnv(t *testing.T) {
	t.Setenv("DATABASE_RW_URL", "postgres://tbite:tbite@localhost:5432/tbite?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("NATS_STREAM_REPLICAS", "3")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.NATSStreamReplicas != 3 {
		t.Fatalf("NATSStreamReplicas = %d, want 3", cfg.NATSStreamReplicas)
	}
}

func TestFromEnvLoadsRealtimePreStopDrainSeconds(t *testing.T) {
	t.Setenv("DATABASE_RW_URL", "postgres://tbite:tbite@localhost:5432/tbite?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("TBITE_REALTIME_PRESTOP_DRAIN_SECONDS", "12")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.RealtimePreStopDrainSeconds != 12 {
		t.Fatalf("RealtimePreStopDrainSeconds = %d, want 12", cfg.RealtimePreStopDrainSeconds)
	}
}

func TestFromEnvLoadsConnectRetryTimeouts(t *testing.T) {
	t.Setenv("DATABASE_RW_URL", "postgres://tbite:tbite@localhost:5432/tbite?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("DB_CONNECT_RETRY_TIMEOUT_SECONDS", "17")
	t.Setenv("REDIS_CONNECT_RETRY_TIMEOUT_SECONDS", "19")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.DBConnectRetryTimeout != 17*time.Second {
		t.Fatalf("DBConnectRetryTimeout = %s, want 17s", cfg.DBConnectRetryTimeout)
	}
	if cfg.RedisConnectRetryTimeout != 19*time.Second {
		t.Fatalf("RedisConnectRetryTimeout = %s, want 19s", cfg.RedisConnectRetryTimeout)
	}
}

func TestEffectiveDatabaseROFallsBackToRW(t *testing.T) {
	cfg := Config{DatabaseRW: "postgres://primary/tbite"}
	if got := cfg.EffectiveDatabaseRO(); got != cfg.DatabaseRW {
		t.Fatalf("EffectiveDatabaseRO() = %q, want RW %q when DATABASE_RO_URL is unset", got, cfg.DatabaseRW)
	}
}

func TestEffectiveDatabaseROPrefersReplica(t *testing.T) {
	cfg := Config{DatabaseRW: "postgres://primary/tbite", DatabaseRO: "postgres://replica/tbite"}
	if got := cfg.EffectiveDatabaseRO(); got != cfg.DatabaseRO {
		t.Fatalf("EffectiveDatabaseRO() = %q, want RO %q when DATABASE_RO_URL is set", got, cfg.DatabaseRO)
	}
}

func TestFromEnvRequiresDatabaseRW(t *testing.T) {
	t.Setenv("DATABASE_RW_URL", "")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")

	_, err := FromEnv()
	if err == nil {
		t.Fatal("FromEnv() error = nil, want DATABASE_RW_URL required error")
	}
	if got := err.Error(); got != "config: DATABASE_RW_URL is required" {
		t.Fatalf("FromEnv() error = %q, want DATABASE_RW_URL required", got)
	}
}

func TestFromEnvRequiresRedisURL(t *testing.T) {
	t.Setenv("DATABASE_RW_URL", "postgres://tbite:tbite@localhost:5432/tbite?sslmode=disable")
	t.Setenv("REDIS_URL", "")

	_, err := FromEnv()
	if err == nil {
		t.Fatal("FromEnv() error = nil, want REDIS_URL required error")
	}
	if got := err.Error(); got != "config: REDIS_URL is required" {
		t.Fatalf("FromEnv() error = %q, want REDIS_URL required", got)
	}
}

func TestFromEnvPropagatesAuthProviderError(t *testing.T) {
	// Duplicate slugs make authProvidersFromEnv fail before required-field checks.
	t.Setenv("DATABASE_RW_URL", "")
	t.Setenv("REDIS_URL", "")
	t.Setenv("AUTH_PROVIDER_SLUGS", "dup dup")

	_, err := FromEnv()
	if err == nil {
		t.Fatal("FromEnv() error = nil, want duplicate auth provider error")
	}
	if got := err.Error(); got != `config: duplicate auth provider "dup"` {
		t.Fatalf("FromEnv() error = %q, want duplicate provider error", got)
	}
}

func TestEnvInt32(t *testing.T) {
	t.Setenv("DATABASE_RW_URL", "postgres://tbite:tbite@localhost:5432/tbite?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")

	// Valid override.
	t.Setenv("DB_MAX_CONNS", "32")
	// Non-numeric -> default.
	t.Setenv("DB_MIN_CONNS", "notanumber")
	// Non-positive -> default.
	t.Setenv("DB_MAX_CONNS_RO", "0")
	t.Setenv("DB_MIN_CONNS_RO", "-5")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.DBMaxConns != 32 {
		t.Fatalf("DBMaxConns = %d, want 32 (valid override)", cfg.DBMaxConns)
	}
	if cfg.DBMinConns != 2 {
		t.Fatalf("DBMinConns = %d, want 2 (default on parse error)", cfg.DBMinConns)
	}
	if cfg.DBMaxConnsRO != 16 {
		t.Fatalf("DBMaxConnsRO = %d, want 16 (default on zero)", cfg.DBMaxConnsRO)
	}
	if cfg.DBMinConnsRO != 2 {
		t.Fatalf("DBMinConnsRO = %d, want 2 (default on negative)", cfg.DBMinConnsRO)
	}
}

func TestEnvPositiveIntInvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("DATABASE_RW_URL", "postgres://tbite:tbite@localhost:5432/tbite?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	// Parse error and non-positive both fall back to default 1.
	t.Setenv("NATS_STREAM_REPLICAS", "notanumber")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.NATSStreamReplicas != 1 {
		t.Fatalf("NATSStreamReplicas = %d, want 1 (default on parse error)", cfg.NATSStreamReplicas)
	}

	t.Setenv("NATS_STREAM_REPLICAS", "0")
	cfg, err = FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.NATSStreamReplicas != 1 {
		t.Fatalf("NATSStreamReplicas = %d, want 1 (default on non-positive)", cfg.NATSStreamReplicas)
	}
}

func TestEnvNonNegativeIntInvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("DATABASE_RW_URL", "postgres://tbite:tbite@localhost:5432/tbite?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	// Parse error -> default.
	t.Setenv("TBITE_REALTIME_PRESTOP_DRAIN_SECONDS", "notanumber")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.RealtimePreStopDrainSeconds != 30 {
		t.Fatalf("RealtimePreStopDrainSeconds = %d, want 30 (default on parse error)", cfg.RealtimePreStopDrainSeconds)
	}

	// Negative -> default; zero is allowed (boundary).
	t.Setenv("TBITE_REALTIME_PRESTOP_DRAIN_SECONDS", "-1")
	cfg, err = FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.RealtimePreStopDrainSeconds != 30 {
		t.Fatalf("RealtimePreStopDrainSeconds = %d, want 30 (default on negative)", cfg.RealtimePreStopDrainSeconds)
	}

	t.Setenv("TBITE_REALTIME_PRESTOP_DRAIN_SECONDS", "0")
	cfg, err = FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.RealtimePreStopDrainSeconds != 0 {
		t.Fatalf("RealtimePreStopDrainSeconds = %d, want 0 (zero allowed)", cfg.RealtimePreStopDrainSeconds)
	}
}

func TestAuthProvidersFromEnvSkipsEmptySlugTokens(t *testing.T) {
	t.Setenv("DATABASE_RW_URL", "postgres://tbite:tbite@localhost:5432/tbite?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	// Extra separators yield empty tokens that FieldsFunc drops; the real
	// driver of an empty post-trim slug is exercised via the unit call below.
	t.Setenv("AUTH_PROVIDER_SLUGS", "authentik,,  ,authentik2")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if len(cfg.AuthProviders) != 2 {
		t.Fatalf("AuthProviders len = %d, want 2 (empty tokens skipped)", len(cfg.AuthProviders))
	}
	if cfg.AuthProviders[0].Slug != "authentik" || cfg.AuthProviders[1].Slug != "authentik2" {
		t.Fatalf("unexpected slugs: %#v", cfg.AuthProviders)
	}
}

func TestAuthProvidersFromEnvEmptyReturnsNil(t *testing.T) {
	t.Setenv("AUTH_PROVIDER_SLUGS", "   ")
	providers, err := authProvidersFromEnv()
	if err != nil {
		t.Fatalf("authProvidersFromEnv() error = %v", err)
	}
	if providers != nil {
		t.Fatalf("authProvidersFromEnv() = %#v, want nil for blank slug list", providers)
	}
}

func TestParseRole(t *testing.T) {
	valid := []Role{
		RoleAPI, RoleMCPStdio,
		RoleOutboxRelay, RolePayrollSettler, RoleOnTimeEvaluator,
		RoleCutoffSweeper, RoleNoShowSweeper,
		RoleDocExpiryScanner, RoleFeedbackScanner,
		RoleRealtimeGateway, RoleProvisionStreams,
	}
	for _, want := range valid {
		got, err := ParseRole(string(want))
		if err != nil {
			t.Fatalf("ParseRole(%q) error = %v", want, err)
		}
		if got != want {
			t.Fatalf("ParseRole(%q) = %q, want %q", want, got, want)
		}
	}

	if _, err := ParseRole("not-a-role"); err == nil {
		t.Fatal("ParseRole(\"not-a-role\") error = nil, want invalid role error")
	}
}
