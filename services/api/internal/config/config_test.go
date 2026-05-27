package config

import "testing"

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
