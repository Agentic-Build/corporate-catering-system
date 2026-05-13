// Package dlq models the synthetic dead-letter-queue: messages workers gave up
// on (MaxDeliver exceeded, unrecoverable schema mismatch, etc.) are written
// into the dlq_message table via messaging.WriteDLQ. Admins can list pending
// rows, replay them back onto the original NATS subject, or mark them resolved
// without replay (i.e. the message is genuinely garbage and should be dropped).
package dlq

import "time"

// Message is the in-memory representation of a row in dlq_message.
type Message struct {
	ID             string
	SourceStream   string
	SourceSubject  string
	SourceConsumer string
	Payload        map[string]any
	Headers        map[string]any
	LastError      string
	FirstSeenAt    time.Time
	ReplayedAt     *time.Time
	ReplayedBy     *string
	ResolvedAt     *time.Time
	ResolvedBy     *string
	ResolvedNotes  string
}
