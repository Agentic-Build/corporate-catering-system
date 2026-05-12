package config

import (
	"fmt"
	"os"
	"strconv"
)

type Role string

const (
	RoleAPI       Role = "api"
	RoleWorker    Role = "worker"
	RoleScheduler Role = "scheduler"
)

type Config struct {
	Role     Role
	HTTPAddr string
	LogLevel string

	DatabaseRW string
	RedisURL   string

	GoogleClientID     string
	GoogleClientSecret string
	GitHubClientID     string
	GitHubClientSecret string

	// OIDCCallbackBaseURL is the Go API's own externally reachable base URL
	// (e.g. http://api.tbite.test). Provider callback URLs are derived from it.
	OIDCCallbackBaseURL string

	// Per-app SPA base URLs used to build the post-login landing redirect.
	AppBaseURLEmployee string
	AppBaseURLMerchant string
	AppBaseURLAdmin    string
}

func FromEnv() (Config, error) {
	c := Config{
		HTTPAddr: getenv("HTTP_ADDR", ":8080"),
		LogLevel: getenv("LOG_LEVEL", "info"),

		DatabaseRW: os.Getenv("DATABASE_RW_URL"),
		RedisURL:   os.Getenv("REDIS_URL"),

		GoogleClientID:     os.Getenv("OIDC_GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("OIDC_GOOGLE_CLIENT_SECRET"),
		GitHubClientID:     os.Getenv("OIDC_GITHUB_CLIENT_ID"),
		GitHubClientSecret: os.Getenv("OIDC_GITHUB_CLIENT_SECRET"),

		OIDCCallbackBaseURL: getenv("OIDC_CALLBACK_BASE_URL", "http://api.tbite.test"),

		AppBaseURLEmployee: getenv("APP_BASE_URL_EMPLOYEE", "http://app.tbite.test"),
		AppBaseURLMerchant: getenv("APP_BASE_URL_MERCHANT", "http://merchant.tbite.test"),
		AppBaseURLAdmin:    getenv("APP_BASE_URL_ADMIN", "http://admin.tbite.test"),
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

func ParseRole(s string) (Role, error) {
	switch Role(s) {
	case RoleAPI, RoleWorker, RoleScheduler:
		return Role(s), nil
	default:
		return "", fmt.Errorf("invalid role %q (want api|worker|scheduler)", s)
	}
}

func MustParsePort(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		panic(err)
	}
	return n
}
