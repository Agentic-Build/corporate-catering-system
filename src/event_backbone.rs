use std::str::FromStr;
use std::sync::Arc;
use std::sync::OnceLock;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use async_nats::jetstream;
use async_nats::jetstream::consumer::{pull, AckPolicy};
use async_nats::jetstream::stream::Config as StreamConfig;
use async_nats::jetstream::AckKind;
use futures::StreamExt;
use opentelemetry::global;
use opentelemetry::metrics::{Counter, UpDownCounter};
use opentelemetry::KeyValue;
use serde::{Deserialize, Serialize};
use sqlx::{PgPool, Row};
use tokio::time::{self, MissedTickBehavior};

pub const DEFAULT_ORDER_EVENT_STREAM_NAME: &str = "CATERING_ORDER_EVENTS";
pub const DEFAULT_ORDER_EVENT_SUBJECT: &str = "catering.order.state.changed.v1";
pub const DEFAULT_ORDER_EVENT_DLQ_SUBJECT: &str = "catering.order.state.changed.v1.dlq";
pub const DEFAULT_ORDER_EVENT_CONSUMER_NAME: &str = "catering-order-state-projection";
pub const DLQ_ROWS_TOTAL_METRIC_NAME: &str = "jetstream_dead_letter_rows_total";
pub const DLQ_BACKLOG_METRIC_NAME: &str = "jetstream_dead_letter_backlog";

const NATS_URL_ENV: &str = "NATS_URL";
const JETSTREAM_STREAM_NAME_ENV: &str = "PRELAUNCH_JETSTREAM_STREAM_NAME";
const JETSTREAM_ORDER_SUBJECT_ENV: &str = "PRELAUNCH_JETSTREAM_ORDER_SUBJECT";
const JETSTREAM_ORDER_DLQ_SUBJECT_ENV: &str = "PRELAUNCH_JETSTREAM_ORDER_DLQ_SUBJECT";
const JETSTREAM_ORDER_CONSUMER_ENV: &str = "PRELAUNCH_JETSTREAM_ORDER_CONSUMER_NAME";
const JETSTREAM_MAX_ACK_PENDING_ENV: &str = "PRELAUNCH_JETSTREAM_MAX_ACK_PENDING";
const JETSTREAM_MAX_DELIVER_ENV: &str = "PRELAUNCH_JETSTREAM_MAX_DELIVER";
const JETSTREAM_ACK_WAIT_SECONDS_ENV: &str = "PRELAUNCH_JETSTREAM_ACK_WAIT_SECONDS";
const EVENT_OUTBOX_POLL_INTERVAL_MS_ENV: &str = "PRELAUNCH_EVENT_OUTBOX_POLL_INTERVAL_MS";
const EVENT_OUTBOX_BATCH_SIZE_ENV: &str = "PRELAUNCH_EVENT_OUTBOX_BATCH_SIZE";

const DEFAULT_NATS_URL: &str = "nats://127.0.0.1:4222";
const DEFAULT_JETSTREAM_MAX_ACK_PENDING: i64 = 256;
const DEFAULT_JETSTREAM_MAX_DELIVER: i64 = 5;
const DEFAULT_JETSTREAM_ACK_WAIT_SECONDS: u64 = 30;
const DEFAULT_EVENT_OUTBOX_POLL_INTERVAL_MS: u64 = 500;
const DEFAULT_EVENT_OUTBOX_BATCH_SIZE: i64 = 64;
const DEFAULT_TELEMETRY_SERVICE_NAME: &str = "catering-http-api";

static EVENT_BACKBONE_METRICS: OnceLock<EventBackboneMetrics> = OnceLock::new();

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct OrderStateChangedEvent {
    pub event_id: String,
    pub order_id: String,
    pub vendor_id: String,
    pub plant_id: String,
    pub order_state: String,
    pub operation_id: String,
    pub actor_id: String,
    pub occurred_at_epoch_millis: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct OrderStateChangedDeadLetter {
    pub consumer_name: String,
    pub source_subject: String,
    pub delivery_attempt: u64,
    pub failure_reason: String,
    pub event: serde_json::Value,
    pub failed_at_epoch_millis: i64,
}

#[derive(Debug, Clone)]
pub struct EventBackboneConfig {
    pub nats_url: String,
    pub stream_name: String,
    pub order_subject: String,
    pub order_dlq_subject: String,
    pub order_consumer_name: String,
    pub max_ack_pending: i64,
    pub max_deliver: i64,
    pub ack_wait: Duration,
    pub outbox_poll_interval: Duration,
    pub outbox_batch_size: i64,
}

impl Default for EventBackboneConfig {
    fn default() -> Self {
        Self {
            nats_url: DEFAULT_NATS_URL.to_owned(),
            stream_name: DEFAULT_ORDER_EVENT_STREAM_NAME.to_owned(),
            order_subject: DEFAULT_ORDER_EVENT_SUBJECT.to_owned(),
            order_dlq_subject: DEFAULT_ORDER_EVENT_DLQ_SUBJECT.to_owned(),
            order_consumer_name: DEFAULT_ORDER_EVENT_CONSUMER_NAME.to_owned(),
            max_ack_pending: DEFAULT_JETSTREAM_MAX_ACK_PENDING,
            max_deliver: DEFAULT_JETSTREAM_MAX_DELIVER,
            ack_wait: Duration::from_secs(DEFAULT_JETSTREAM_ACK_WAIT_SECONDS),
            outbox_poll_interval: Duration::from_millis(DEFAULT_EVENT_OUTBOX_POLL_INTERVAL_MS),
            outbox_batch_size: DEFAULT_EVENT_OUTBOX_BATCH_SIZE,
        }
    }
}

impl EventBackboneConfig {
    pub fn from_env() -> Result<Self, String> {
        let default = Self::default();
        Ok(Self {
            nats_url: load_non_empty_env(NATS_URL_ENV).unwrap_or_else(|_| default.nats_url),
            stream_name: load_non_empty_env(JETSTREAM_STREAM_NAME_ENV)
                .unwrap_or_else(|_| default.stream_name),
            order_subject: load_non_empty_env(JETSTREAM_ORDER_SUBJECT_ENV)
                .unwrap_or_else(|_| default.order_subject),
            order_dlq_subject: load_non_empty_env(JETSTREAM_ORDER_DLQ_SUBJECT_ENV)
                .unwrap_or_else(|_| default.order_dlq_subject),
            order_consumer_name: load_non_empty_env(JETSTREAM_ORDER_CONSUMER_ENV)
                .unwrap_or_else(|_| default.order_consumer_name),
            max_ack_pending: parse_positive_i64_env(
                JETSTREAM_MAX_ACK_PENDING_ENV,
                DEFAULT_JETSTREAM_MAX_ACK_PENDING,
            )?,
            max_deliver: parse_positive_i64_env(
                JETSTREAM_MAX_DELIVER_ENV,
                DEFAULT_JETSTREAM_MAX_DELIVER,
            )?,
            ack_wait: Duration::from_secs(parse_positive_u64_env(
                JETSTREAM_ACK_WAIT_SECONDS_ENV,
                DEFAULT_JETSTREAM_ACK_WAIT_SECONDS,
            )?),
            outbox_poll_interval: Duration::from_millis(parse_positive_u64_env(
                EVENT_OUTBOX_POLL_INTERVAL_MS_ENV,
                DEFAULT_EVENT_OUTBOX_POLL_INTERVAL_MS,
            )?),
            outbox_batch_size: parse_positive_i64_env(
                EVENT_OUTBOX_BATCH_SIZE_ENV,
                DEFAULT_EVENT_OUTBOX_BATCH_SIZE,
            )?,
        })
    }
}

#[derive(Debug)]
pub struct OrderEventBackbone {
    pool: PgPool,
    jetstream: jetstream::Context,
    config: EventBackboneConfig,
}

#[derive(Debug)]
struct EventBackboneMetrics {
    service_name: String,
    dead_letter_rows_total: Counter<u64>,
    dead_letter_backlog: UpDownCounter<i64>,
}

impl EventBackboneMetrics {
    fn global() -> &'static Self {
        EVENT_BACKBONE_METRICS.get_or_init(Self::new)
    }

    fn new() -> Self {
        let service_name = std::env::var("OTEL_SERVICE_NAME")
            .ok()
            .map(|value| value.trim().to_owned())
            .filter(|value| !value.is_empty())
            .unwrap_or_else(|| DEFAULT_TELEMETRY_SERVICE_NAME.to_owned());
        let meter = global::meter(service_name.clone());
        let dead_letter_rows_total = meter
            .u64_counter(DLQ_ROWS_TOTAL_METRIC_NAME)
            .with_description("Persisted JetStream dead-letter rows")
            .init();
        let dead_letter_backlog = meter
            .i64_up_down_counter(DLQ_BACKLOG_METRIC_NAME)
            .with_description("JetStream dead-letter backlog size approximation")
            .init();
        Self {
            service_name,
            dead_letter_rows_total,
            dead_letter_backlog,
        }
    }

    fn record_dead_letter_persisted(&self, consumer_name: &str) {
        let attributes = [
            KeyValue::new("service_name", self.service_name.clone()),
            KeyValue::new("consumer_name", consumer_name.to_owned()),
        ];
        self.dead_letter_rows_total.add(1, &attributes);
        self.dead_letter_backlog.add(1, &attributes);
    }
}

impl OrderEventBackbone {
    pub async fn connect(pool: PgPool, config: EventBackboneConfig) -> Result<Arc<Self>, String> {
        let client = async_nats::connect(config.nats_url.as_str())
            .await
            .map_err(|error| format!("failed to connect to NATS `{}`: {error}", config.nats_url))?;
        let jetstream = jetstream::new(client);
        let backbone = Arc::new(Self {
            pool,
            jetstream,
            config,
        });
        backbone.ensure_topology().await?;
        Ok(backbone)
    }

    pub fn config(&self) -> &EventBackboneConfig {
        &self.config
    }

    pub fn spawn_background_workers(self: &Arc<Self>) {
        let publisher = Arc::clone(self);
        tokio::spawn(async move {
            publisher.run_outbox_publish_loop().await;
        });

        let consumer = Arc::clone(self);
        tokio::spawn(async move {
            consumer.run_order_consumer_loop().await;
        });
    }

    pub async fn enqueue_order_state_changed_event(
        &self,
        event: &OrderStateChangedEvent,
    ) -> Result<(), String> {
        let payload = serde_json::to_value(event)
            .map_err(|error| format!("failed to serialize order event payload: {error}"))?;
        sqlx::query(
            r#"
INSERT INTO domain_event_outbox (
    event_id,
    subject,
    payload
)
VALUES ($1, $2, $3)
ON CONFLICT (event_id) DO NOTHING
            "#,
        )
        .bind(event.event_id.as_str())
        .bind(self.config.order_subject.as_str())
        .bind(payload)
        .execute(&self.pool)
        .await
        .map_err(|error| format!("failed to enqueue order event into outbox: {error}"))?;
        Ok(())
    }

    async fn ensure_topology(&self) -> Result<(), String> {
        let stream = self
            .jetstream
            .get_or_create_stream(StreamConfig {
                name: self.config.stream_name.clone(),
                subjects: vec![
                    self.config.order_subject.clone(),
                    self.config.order_dlq_subject.clone(),
                ],
                ..Default::default()
            })
            .await
            .map_err(|error| {
                format!(
                    "failed to create/get JetStream stream `{}`: {error}",
                    self.config.stream_name
                )
            })?;
        stream
            .get_or_create_consumer(
                self.config.order_consumer_name.as_str(),
                pull::Config {
                    durable_name: Some(self.config.order_consumer_name.clone()),
                    filter_subject: self.config.order_subject.clone(),
                    ack_policy: AckPolicy::Explicit,
                    ack_wait: self.config.ack_wait,
                    max_deliver: self.config.max_deliver,
                    max_ack_pending: self.config.max_ack_pending,
                    ..Default::default()
                },
            )
            .await
            .map_err(|error| {
                format!(
                    "failed to create/get JetStream consumer `{}`: {error}",
                    self.config.order_consumer_name
                )
            })?;
        Ok(())
    }

    async fn run_outbox_publish_loop(self: Arc<Self>) {
        let mut interval = time::interval(self.config.outbox_poll_interval);
        interval.set_missed_tick_behavior(MissedTickBehavior::Skip);
        loop {
            interval.tick().await;
            if let Err(error) = self.publish_pending_outbox_batch().await {
                tracing::warn!(
                    error = %error,
                    "order event outbox publish batch failed"
                );
            }
        }
    }

    async fn publish_pending_outbox_batch(&self) -> Result<(), String> {
        let rows = sqlx::query(
            r#"
SELECT event_id, subject, payload
FROM domain_event_outbox
WHERE published_at_utc IS NULL
ORDER BY created_at_utc ASC
LIMIT $1
            "#,
        )
        .bind(self.config.outbox_batch_size)
        .fetch_all(&self.pool)
        .await
        .map_err(|error| format!("failed to load outbox rows for publishing: {error}"))?;

        for row in rows {
            let event_id: String = row
                .try_get("event_id")
                .map_err(|error| format!("failed to decode outbox event_id: {error}"))?;
            let subject: String = row
                .try_get("subject")
                .map_err(|error| format!("failed to decode outbox subject: {error}"))?;
            let payload: serde_json::Value = row
                .try_get("payload")
                .map_err(|error| format!("failed to decode outbox payload: {error}"))?;

            let publish_result = self
                .publish_with_msg_id(
                    subject.as_str(),
                    event_id.as_str(),
                    serde_json::to_vec(&payload).map_err(|error| {
                        format!("failed to encode outbox payload for publish: {error}")
                    })?,
                )
                .await;

            match publish_result {
                Ok(()) => {
                    sqlx::query(
                        r#"
UPDATE domain_event_outbox
SET
    published_at_utc = CURRENT_TIMESTAMP,
    publish_attempts = publish_attempts + 1,
    last_publish_error = NULL
WHERE event_id = $1
                        "#,
                    )
                    .bind(event_id.as_str())
                    .execute(&self.pool)
                    .await
                    .map_err(|error| {
                        format!("failed to mark outbox event `{event_id}` as published: {error}")
                    })?;
                }
                Err(error) => {
                    sqlx::query(
                        r#"
UPDATE domain_event_outbox
SET
    publish_attempts = publish_attempts + 1,
    last_publish_error = LEFT($2, 2048)
WHERE event_id = $1
                        "#,
                    )
                    .bind(event_id.as_str())
                    .bind(error.as_str())
                    .execute(&self.pool)
                    .await
                    .map_err(|query_error| {
                        format!(
                            "failed to record outbox publish error for `{event_id}`: {query_error}"
                        )
                    })?;
                }
            }
        }

        Ok(())
    }

    async fn run_order_consumer_loop(self: Arc<Self>) {
        loop {
            let result = self.consume_order_messages_once().await;
            if let Err(error) = result {
                tracing::warn!(
                    error = %error,
                    "order event consumer loop iteration failed"
                );
                time::sleep(Duration::from_secs(1)).await;
            }
        }
    }

    async fn consume_order_messages_once(&self) -> Result<(), String> {
        let stream = self
            .jetstream
            .get_stream(self.config.stream_name.as_str())
            .await
            .map_err(|error| {
                format!(
                    "failed to load JetStream stream `{}`: {error}",
                    self.config.stream_name
                )
            })?;
        let consumer = stream
            .get_consumer::<pull::Config>(self.config.order_consumer_name.as_str())
            .await
            .map_err(|error| {
                format!(
                    "failed to load JetStream consumer `{}`: {error}",
                    self.config.order_consumer_name
                )
            })?;
        let mut messages = consumer
            .messages()
            .await
            .map_err(|error| format!("failed to open JetStream message stream: {error}"))?;
        while let Some(next) = messages.next().await {
            let message =
                next.map_err(|error| format!("failed to receive JetStream message: {error}"))?;
            self.handle_order_message(message).await?;
        }
        Ok(())
    }

    async fn handle_order_message(&self, message: jetstream::Message) -> Result<(), String> {
        let delivery_attempt = message.info().map(|info| info.delivered).unwrap_or(1);
        let payload_value = match serde_json::from_slice::<serde_json::Value>(
            message.payload.as_ref(),
        ) {
            Ok(payload) => payload,
            Err(error) => {
                return self
                    .handle_consumer_failure(
                        &message,
                        delivery_attempt,
                        format!("order event payload is invalid JSON: {error}"),
                        serde_json::json!({
                            "rawPayloadUtf8": String::from_utf8_lossy(message.payload.as_ref()).to_string()
                        }),
                    )
                    .await;
            }
        };
        let event = match serde_json::from_value::<OrderStateChangedEvent>(payload_value.clone()) {
            Ok(event) => event,
            Err(error) => {
                return self
                    .handle_consumer_failure(
                        &message,
                        delivery_attempt,
                        format!("order event payload shape is invalid: {error}"),
                        payload_value,
                    )
                    .await;
            }
        };

        match self.apply_order_event_projection(&event).await {
            Ok(()) => {
                message
                    .ack()
                    .await
                    .map_err(|error| format!("failed to ack order event message: {error}"))?;
            }
            Err(error) => {
                self.handle_consumer_failure(&message, delivery_attempt, error, payload_value)
                    .await?;
            }
        }

        Ok(())
    }

    async fn handle_consumer_failure(
        &self,
        message: &jetstream::Message,
        delivery_attempt: i64,
        failure_reason: String,
        payload: serde_json::Value,
    ) -> Result<(), String> {
        if delivery_attempt >= self.config.max_deliver {
            self.route_to_dead_letter(
                message.subject.to_string(),
                delivery_attempt,
                failure_reason.as_str(),
                payload,
            )
            .await?;
            message.ack().await.map_err(|ack_error| {
                format!(
                    "failed to ack max-delivery order event message after DLQ routing: {ack_error}"
                )
            })?;
        } else {
            message
                .ack_with(AckKind::Nak(None))
                .await
                .map_err(|nak_error| {
                    format!("failed to NAK order event message for retry: {nak_error}")
                })?;
        }
        Ok(())
    }

    async fn apply_order_event_projection(
        &self,
        event: &OrderStateChangedEvent,
    ) -> Result<(), String> {
        let mut transaction = self.pool.begin().await.map_err(|error| {
            format!("failed to begin consumer idempotency transaction: {error}")
        })?;

        let dedup_insert = sqlx::query(
            r#"
INSERT INTO jetstream_consumer_dedup (
    consumer_name,
    event_id
)
VALUES ($1, $2)
ON CONFLICT (consumer_name, event_id) DO NOTHING
            "#,
        )
        .bind(self.config.order_consumer_name.as_str())
        .bind(event.event_id.as_str())
        .execute(&mut *transaction)
        .await
        .map_err(|error| format!("failed to write consumer dedup record: {error}"))?;
        if dedup_insert.rows_affected() == 0 {
            transaction.rollback().await.map_err(|error| {
                format!("failed to rollback duplicate-event transaction: {error}")
            })?;
            return Ok(());
        }

        sqlx::query(
            r#"
INSERT INTO order_state_event_projection (
    order_id,
    vendor_id,
    plant_id,
    order_state,
    event_id
)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (order_id)
DO UPDATE
SET
    vendor_id = EXCLUDED.vendor_id,
    plant_id = EXCLUDED.plant_id,
    order_state = EXCLUDED.order_state,
    event_id = EXCLUDED.event_id,
    updated_at_utc = CURRENT_TIMESTAMP
            "#,
        )
        .bind(event.order_id.as_str())
        .bind(event.vendor_id.as_str())
        .bind(event.plant_id.as_str())
        .bind(event.order_state.as_str())
        .bind(event.event_id.as_str())
        .execute(&mut *transaction)
        .await
        .map_err(|error| format!("failed to project order event in SQL: {error}"))?;

        transaction.commit().await.map_err(|error| {
            format!("failed to commit order event projection transaction: {error}")
        })?;
        Ok(())
    }

    async fn route_to_dead_letter(
        &self,
        source_subject: String,
        delivery_attempt: i64,
        failure_reason: &str,
        payload: serde_json::Value,
    ) -> Result<(), String> {
        let dead_letter = OrderStateChangedDeadLetter {
            consumer_name: self.config.order_consumer_name.clone(),
            source_subject,
            delivery_attempt: delivery_attempt.max(0) as u64,
            failure_reason: failure_reason.to_owned(),
            event: payload.clone(),
            failed_at_epoch_millis: now_epoch_millis(),
        };
        let serialized = serde_json::to_vec(&dead_letter)
            .map_err(|error| format!("failed to serialize dead-letter payload: {error}"))?;
        let dlq_message_id = format!(
            "dlq-{}-{}",
            self.config.order_consumer_name,
            now_epoch_millis()
        );
        self.publish_with_msg_id(
            self.config.order_dlq_subject.as_str(),
            dlq_message_id.as_str(),
            serialized,
        )
        .await?;

        sqlx::query(
            r#"
INSERT INTO jetstream_dead_letter (
    consumer_name,
    source_subject,
    delivery_attempt,
    failure_reason,
    payload
)
VALUES ($1, $2, $3, $4, $5)
            "#,
        )
        .bind(self.config.order_consumer_name.as_str())
        .bind(dead_letter.source_subject.as_str())
        .bind(delivery_attempt)
        .bind(dead_letter.failure_reason.as_str())
        .bind(payload)
        .execute(&self.pool)
        .await
        .map_err(|error| format!("failed to persist dead-letter record: {error}"))?;
        EventBackboneMetrics::global()
            .record_dead_letter_persisted(self.config.order_consumer_name.as_str());
        Ok(())
    }

    async fn publish_with_msg_id(
        &self,
        subject: &str,
        message_id: &str,
        payload: Vec<u8>,
    ) -> Result<(), String> {
        let mut headers = async_nats::HeaderMap::new();
        headers.insert(
            "Nats-Msg-Id",
            async_nats::HeaderValue::from_str(message_id).map_err(|error| {
                format!("failed to build Nats-Msg-Id header `{message_id}`: {error}")
            })?,
        );
        self.jetstream
            .publish_with_headers(subject.to_owned(), headers, payload.into())
            .await
            .map_err(|error| format!("failed to publish to subject `{subject}`: {error}"))?
            .await
            .map_err(|error| format!("failed to await publish ack on `{subject}`: {error}"))?;
        Ok(())
    }
}

fn load_non_empty_env(key: &str) -> Result<String, String> {
    let raw = std::env::var(key).map_err(|error| format!("{key} is invalid: {error}"))?;
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        return Err(format!("{key} must not be empty"));
    }
    Ok(trimmed.to_owned())
}

fn parse_positive_i64_env(key: &str, default: i64) -> Result<i64, String> {
    match std::env::var(key) {
        Ok(raw) => {
            let parsed = raw
                .trim()
                .parse::<i64>()
                .map_err(|error| format!("{key} must be a positive integer: {error}"))?;
            if parsed <= 0 {
                return Err(format!("{key} must be greater than zero"));
            }
            Ok(parsed)
        }
        Err(std::env::VarError::NotPresent) => Ok(default),
        Err(error) => Err(format!("{key} is invalid: {error}")),
    }
}

fn parse_positive_u64_env(key: &str, default: u64) -> Result<u64, String> {
    match std::env::var(key) {
        Ok(raw) => {
            let parsed = raw
                .trim()
                .parse::<u64>()
                .map_err(|error| format!("{key} must be a positive integer: {error}"))?;
            if parsed == 0 {
                return Err(format!("{key} must be greater than zero"));
            }
            Ok(parsed)
        }
        Err(std::env::VarError::NotPresent) => Ok(default),
        Err(error) => Err(format!("{key} is invalid: {error}")),
    }
}

fn now_epoch_millis() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_millis() as i64)
        .unwrap_or(0)
}
