package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type Client struct {
	NC *nats.Conn
	JS jetstream.JetStream
}

func New(ctx context.Context, url string) (*Client, error) {
	nc, err := nats.Connect(url,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("jetstream: %w", err)
	}
	return &Client{NC: nc, JS: js}, nil
}

// ProvisionStreams creates or updates the streams expected by the platform.
// Safe to call repeatedly (CreateOrUpdate semantics).
func (c *Client) ProvisionStreams(ctx context.Context) error {
	_, err := c.JS.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        "ORDERS_V1",
		Description: "Order domain events (place/cutoff/cancel/...)",
		Subjects:    []string{"order.>"},
		Storage:     jetstream.FileStorage,
		Replicas:    1,
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
		Replicas:    1,
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
