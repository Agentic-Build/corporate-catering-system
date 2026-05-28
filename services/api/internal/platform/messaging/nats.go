package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

type Client struct {
	NC             *nats.Conn
	JS             jetstream.JetStream
	StreamReplicas int // JetStream stream replica count; 1 = single-node default
}

// New dials NATS with a bounded retry on the initial connect so the caller is
// not blocked by transient EOFs when the server is still coming up (k8s pod
// ordering, testcontainers warm-up).
func New(ctx context.Context, url string) (*Client, error) {
	var (
		nc       *nats.Conn
		err      error
		deadline = time.Now().Add(10 * time.Second)
	)
	for {
		nc, err = nats.Connect(url,
			nats.MaxReconnects(-1),
			nats.ReconnectWait(2*time.Second),
		)
		if err == nil {
			break
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return nil, fmt.Errorf("nats connect: %w", err)
		}
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return nil, fmt.Errorf("nats connect: %w", ctx.Err())
		}
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("jetstream: %w", err)
	}
	return &Client{NC: nc, JS: js, StreamReplicas: 1}, nil
}

// ProvisionStreams creates or updates the streams expected by the platform.
// Safe to call repeatedly (CreateOrUpdate semantics).
func (c *Client) ProvisionStreams(ctx context.Context) error {
	_, err := c.JS.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        "ORDERS_V1",
		Description: "Order domain events (place/cutoff/cancel/...)",
		Subjects:    []string{"order.>"},
		Storage:     jetstream.FileStorage,
		Replicas:    c.StreamReplicas,
		MaxAge:      30 * 24 * time.Hour,
	})
	if err != nil {
		return fmt.Errorf("provision ORDERS_V1: %w", err)
	}
	_, err = c.JS.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        "PAYROLL_V1",
		Description: "Payroll domain events (batch_locked / export_ready / dispute_resolved)",
		Subjects:    []string{"payroll.>"},
		Storage:     jetstream.FileStorage,
		Replicas:    c.StreamReplicas,
		MaxAge:      90 * 24 * time.Hour,
	})
	if err != nil {
		return fmt.Errorf("provision PAYROLL_V1: %w", err)
	}
	return nil
}

func (c *Client) Close() {
	if c.NC != nil {
		c.NC.Close()
	}
}

// PublishTraced publishes a JetStream message and emits an OpenTelemetry span
// around the publish. When no tracer provider is configured (OTel disabled),
// the global no-op tracer makes this effectively free.
//
// dedupID, when non-empty, is sent as the Nats-Msg-Id header so JetStream's
// built-in dedup window collapses re-publishes of the same logical event (e.g.
// the outbox relay re-sending after a crash between publish and mark) into a
// single stream message. Pass "" to opt out.
func (c *Client) PublishTraced(ctx context.Context, subject string, data []byte, dedupID string) error {
	tracer := otel.Tracer("tbite.nats")
	ctx, span := tracer.Start(ctx, "nats.publish")
	defer span.End()
	span.SetAttributes(
		attribute.String("subject", subject),
		attribute.Int("size", len(data)),
	)
	var opts []jetstream.PublishOpt
	if dedupID != "" {
		opts = append(opts, jetstream.WithMsgID(dedupID))
	}
	_, err := c.JS.Publish(ctx, subject, data, opts...)
	if err != nil {
		span.RecordError(err)
	}
	return err
}
