// Package leader provides K8s Lease-based leader election so multi-replica
// scheduler deployments only run jobs on a single active leader. Outside of
// K8s (e.g. local dev) RunWithLease falls back to invoking the workload
// directly so the scheduler still functions without coordination.
package leader

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// Config controls the K8s Lease lock used for leader election.
type Config struct {
	Namespace string
	LeaseName string
	Identity  string // unique per pod, e.g. POD_NAME env
	Logger    *slog.Logger
}

// RunWithLease acquires a K8s Lease and invokes onLeading once leadership
// is held. The lease is released on context cancellation.
//
// If there is no in-cluster config (local dev), onLeading is invoked directly
// without any locking.
func RunWithLease(ctx context.Context, cfg Config, onLeading func(ctx context.Context) error) error {
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	k8sCfg, err := rest.InClusterConfig()
	if err != nil {
		cfg.Logger.Info("not in-cluster, running scheduler without leader election", "err", err)
		return onLeading(ctx)
	}

	client, err := kubernetes.NewForConfig(k8sCfg)
	if err != nil {
		return fmt.Errorf("k8s client: %w", err)
	}

	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      cfg.LeaseName,
			Namespace: cfg.Namespace,
		},
		Client: client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: cfg.Identity,
		},
	}

	runErr := make(chan error, 1)
	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   15 * time.Second,
		RenewDeadline:   10 * time.Second,
		RetryPeriod:     2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(leadCtx context.Context) {
				cfg.Logger.Info("acquired lease, starting scheduler jobs", "lease", cfg.LeaseName, "identity", cfg.Identity)
				if err := onLeading(leadCtx); err != nil {
					cfg.Logger.Error("leading workload exited", "err", err)
					runErr <- err
					return
				}
				runErr <- nil
			},
			OnStoppedLeading: func() {
				cfg.Logger.Info("lost lease", "identity", cfg.Identity)
			},
			OnNewLeader: func(identity string) {
				if identity != cfg.Identity {
					cfg.Logger.Info("another replica is leading", "leader", identity)
				}
			},
		},
	})

	select {
	case err := <-runErr:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
