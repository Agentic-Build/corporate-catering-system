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
}

func FromEnv() (Config, error) {
	c := Config{
		HTTPAddr: getenv("HTTP_ADDR", ":8080"),
		LogLevel: getenv("LOG_LEVEL", "info"),
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
