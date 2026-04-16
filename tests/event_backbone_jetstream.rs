use std::time::{Duration, SystemTime, UNIX_EPOCH};

use async_nats::jetstream;
use async_nats::jetstream::consumer::pull;
use corporate_catering_system::event_backbone::{
    EventBackboneConfig, OrderEventBackbone, OrderStateChangedEvent,
};
use sqlx::postgres::PgPoolOptions;
use sqlx::Row;
use testcontainers_modules::postgres::Postgres;
use testcontainers_modules::testcontainers::core::WaitFor;
use testcontainers_modules::testcontainers::runners::AsyncRunner;
use testcontainers_modules::testcontainers::{GenericImage, ImageExt};

fn unique_suffix() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_nanos() as u64)
        .unwrap_or(0)
}

async fn wait_until<F, Fut>(timeout: Duration, mut predicate: F)
where
    F: FnMut() -> Fut,
    Fut: std::future::Future<Output = bool>,
{
    let started = std::time::Instant::now();
    while started.elapsed() < timeout {
        if predicate().await {
            return;
        }
        tokio::time::sleep(Duration::from_millis(100)).await;
    }
    panic!("condition was not met within {:?}", timeout);
}

#[tokio::test]
async fn jetstream_consumer_is_idempotent_and_routes_retries_to_dlq() {
    let suffix = unique_suffix();

    let postgres = Postgres::default()
        .with_tag("16-alpine")
        .start()
        .await
        .expect("postgres container should start");
    let database_url = format!(
        "postgres://postgres:postgres@{}:{}/postgres",
        postgres
            .get_host()
            .await
            .expect("postgres host should resolve"),
        postgres
            .get_host_port_ipv4(5432)
            .await
            .expect("postgres mapped port should resolve")
    );
    let pool = PgPoolOptions::new()
        .max_connections(10)
        .connect(database_url.as_str())
        .await
        .expect("postgres pool should connect");
    sqlx::migrate!("./migrations")
        .run(&pool)
        .await
        .expect("migrations should apply");

    let nats = GenericImage::new("nats", "2.10-alpine")
        .with_wait_for(WaitFor::message_on_stderr(
            "Listening for client connections on 0.0.0.0:4222",
        ))
        .with_wait_for(WaitFor::message_on_stderr("Server is ready"))
        .with_cmd(vec!["-js", "-sd", "/data", "-m", "8222"])
        .start()
        .await
        .expect("nats container should start");
    let nats_url = format!(
        "nats://{}:{}",
        nats.get_host().await.expect("nats host should resolve"),
        nats.get_host_port_ipv4(4222)
            .await
            .expect("nats mapped port should resolve")
    );

    let config = EventBackboneConfig {
        nats_url: nats_url.clone(),
        stream_name: format!("CATERING_ORDER_EVENTS_{suffix}"),
        order_subject: format!("catering.order.state.changed.v1.{suffix}"),
        order_dlq_subject: format!("catering.order.state.changed.v1.{suffix}.dlq"),
        order_consumer_name: format!("catering-order-state-projection-{suffix}"),
        max_ack_pending: 8,
        max_deliver: 2,
        ack_wait: Duration::from_millis(200),
        outbox_poll_interval: Duration::from_millis(50),
        outbox_batch_size: 64,
    };

    let backbone = OrderEventBackbone::connect(pool.clone(), config.clone())
        .await
        .expect("event backbone should initialize");
    backbone.spawn_background_workers();

    let nats_client = async_nats::connect(nats_url.as_str())
        .await
        .expect("nats connection should succeed");
    let js = jetstream::new(nats_client);
    let stream = js
        .get_stream(config.stream_name.as_str())
        .await
        .expect("jetstream stream should exist");
    let consumer = stream
        .get_consumer::<pull::Config>(config.order_consumer_name.as_str())
        .await
        .expect("jetstream consumer should exist");
    assert_eq!(
        consumer.cached_info().config.max_ack_pending,
        config.max_ack_pending,
        "consumer must use configured max_ack_pending"
    );

    let event = OrderStateChangedEvent {
        event_id: format!("evt-{suffix}"),
        order_id: format!("ord-{suffix}"),
        vendor_id: "ven-backbone".to_owned(),
        plant_id: "fab-a".to_owned(),
        order_state: "PLACED".to_owned(),
        operation_id: "createEmployeeOrder".to_owned(),
        actor_id: "emp-backbone".to_owned(),
        occurred_at_epoch_millis: 1_712_345_678_000,
    };
    backbone
        .enqueue_order_state_changed_event(&event)
        .await
        .expect("order event should enqueue into outbox");

    sqlx::query(
        r#"
INSERT INTO domain_event_outbox (event_id, subject, payload)
VALUES ($1, $2, $3)
        "#,
    )
    .bind(format!("dup-msg-{suffix}"))
    .bind(config.order_subject.as_str())
    .bind(serde_json::to_value(&event).expect("event payload should serialize"))
    .execute(&pool)
    .await
    .expect("duplicate payload should enqueue with unique transport message id");

    wait_until(Duration::from_secs(8), || {
        let pool = pool.clone();
        let order_id = event.order_id.clone();
        async move {
            let projection = sqlx::query(
                r#"
SELECT event_id
FROM order_state_event_projection
WHERE order_id = $1
                "#,
            )
            .bind(order_id.as_str())
            .fetch_optional(&pool)
            .await
            .expect("projection query should succeed");
            projection.is_some()
        }
    })
    .await;

    let dedup_count: i64 = sqlx::query_scalar(
        r#"
SELECT COUNT(*)::BIGINT
FROM jetstream_consumer_dedup
WHERE consumer_name = $1 AND event_id = $2
        "#,
    )
    .bind(config.order_consumer_name.as_str())
    .bind(event.event_id.as_str())
    .fetch_one(&pool)
    .await
    .expect("dedup count query should succeed");
    assert_eq!(
        dedup_count, 1,
        "duplicate message deliveries must be idempotent at the consumer boundary"
    );

    js.publish(
        config.order_subject.clone(),
        serde_json::to_vec(&serde_json::json!("poison-non-object-payload"))
            .expect("poison payload should encode")
            .into(),
    )
    .await
    .expect("poison payload should publish to JetStream")
    .await
    .expect("poison payload publish ack should succeed");

    wait_until(Duration::from_secs(8), || {
        let pool = pool.clone();
        let consumer_name = config.order_consumer_name.clone();
        async move {
            let dead_letter_count: i64 = sqlx::query_scalar(
                r#"
SELECT COUNT(*)::BIGINT
FROM jetstream_dead_letter
WHERE consumer_name = $1
                "#,
            )
            .bind(consumer_name.as_str())
            .fetch_one(&pool)
            .await
            .expect("dead-letter count query should succeed");
            dead_letter_count > 0
        }
    })
    .await;

    let dlq_row = sqlx::query(
        r#"
SELECT delivery_attempt, failure_reason, payload
FROM jetstream_dead_letter
WHERE consumer_name = $1
ORDER BY failed_at_utc DESC
LIMIT 1
        "#,
    )
    .bind(config.order_consumer_name.as_str())
    .fetch_one(&pool)
    .await
    .expect("latest dead-letter row should exist");
    let delivery_attempt: i32 = dlq_row
        .try_get("delivery_attempt")
        .expect("delivery attempt column should decode");
    let failure_reason: String = dlq_row
        .try_get("failure_reason")
        .expect("failure reason column should decode");
    let payload: serde_json::Value = dlq_row
        .try_get("payload")
        .expect("payload column should decode");
    assert!(
        delivery_attempt >= 2,
        "failed message should only route to DLQ after retries"
    );
    assert!(
        failure_reason.contains("payload shape is invalid")
            || failure_reason.contains("payload is invalid JSON"),
        "dead-letter reason should capture parse failure, got `{failure_reason}`"
    );
    assert_eq!(
        payload["rawPayloadType"],
        serde_json::Value::String("string".to_owned()),
        "DLQ persisted payload should normalize non-object poison values"
    );
    assert_eq!(
        payload["rawPayload"],
        serde_json::Value::String("poison-non-object-payload".to_owned()),
        "DLQ payload should retain original poison payload value"
    );
}
