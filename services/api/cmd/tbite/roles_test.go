package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBackgroundDependencyCheckerTracksReadiness(t *testing.T) {
	dep := newBackgroundDependency("jetstream-board-consumer")
	checker := dep.checker()

	if err := checker.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "starting") {
		t.Fatalf("initial Check() error = %v, want starting", err)
	}

	dep.setReady()
	if err := checker.Check(context.Background()); err != nil {
		t.Fatalf("ready Check() error = %v, want nil", err)
	}

	dep.setNotReady(errors.New("stream is offline"))
	if err := checker.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "stream is offline") {
		t.Fatalf("offline Check() error = %v, want stream is offline", err)
	}
}
