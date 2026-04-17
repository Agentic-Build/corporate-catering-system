use std::time::Duration;

use corporate_catering_system::cache_backbone::{
    RuntimeStateCacheTtls, ValkeyRuntimeStateCache, ANOMALY_ALERT_STATE_KEY,
    DELIVERY_POLICY_STATE_KEY, MENU_SUPPLY_STATE_KEY, OPERATIONS_ANALYTICS_STATE_KEY,
    PAYROLL_LEDGER_STATE_KEY, VENDOR_FULFILLMENT_STATE_KEY,
};
use serde::{Deserialize, Serialize};
use testcontainers_modules::testcontainers::core::WaitFor;
use testcontainers_modules::testcontainers::runners::AsyncRunner;
use testcontainers_modules::testcontainers::GenericImage;

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
struct CacheProbeSnapshot {
    version: u32,
    status: String,
}

#[tokio::test]
async fn valkey_cache_keys_ttls_and_invalidation_are_observable() {
    let redis = GenericImage::new("redis", "7-alpine")
        .with_wait_for(WaitFor::message_on_stdout("Ready to accept connections"))
        .start()
        .await
        .expect("redis container should start");
    let redis_url = format!(
        "redis://{}:{}",
        redis.get_host().await.expect("redis host should resolve"),
        redis
            .get_host_port_ipv4(6379)
            .await
            .expect("redis mapped port should resolve")
    );

    let cache = ValkeyRuntimeStateCache::connect(
        redis_url,
        "integration:test:runtime-state",
        RuntimeStateCacheTtls {
            menu_supply_seconds: 3,
            vendor_fulfillment_seconds: 3,
            payroll_ledger_seconds: 3,
            anomaly_alert_seconds: 3,
            delivery_policy_seconds: 3,
            operations_analytics_seconds: 3,
        },
    )
    .await
    .expect("valkey cache should connect");

    let initial = CacheProbeSnapshot {
        version: 1,
        status: "initial".to_owned(),
    };
    cache
        .write_through_snapshot(MENU_SUPPLY_STATE_KEY, &initial)
        .await
        .expect("initial snapshot write-through should succeed");

    let observed = cache
        .observe_state_key(MENU_SUPPLY_STATE_KEY)
        .await
        .expect("cache key observation should succeed");
    assert!(observed.exists, "menu supply cache key should exist");
    let ttl = observed
        .ttl_seconds
        .expect("menu supply cache key should have TTL");
    assert!(
        (1..=3).contains(&ttl),
        "menu supply TTL should be within configured bounds, got {ttl}"
    );
    let loaded = cache
        .load_snapshot::<CacheProbeSnapshot>(MENU_SUPPLY_STATE_KEY)
        .await
        .expect("cache read should succeed");
    assert_eq!(loaded, Some(initial.clone()));

    tokio::time::sleep(Duration::from_millis(1100)).await;

    let updated = CacheProbeSnapshot {
        version: 2,
        status: "updated".to_owned(),
    };
    cache
        .write_through_snapshot(MENU_SUPPLY_STATE_KEY, &updated)
        .await
        .expect("updated snapshot write-through should succeed");

    let observed_after_update = cache
        .observe_state_key(MENU_SUPPLY_STATE_KEY)
        .await
        .expect("cache key observation should succeed after update");
    let refreshed_ttl = observed_after_update
        .ttl_seconds
        .expect("updated key should still have TTL");
    assert!(
        refreshed_ttl >= 2,
        "write-through should reset TTL close to configured value, got {refreshed_ttl}"
    );
    let loaded_after_update = cache
        .load_snapshot::<CacheProbeSnapshot>(MENU_SUPPLY_STATE_KEY)
        .await
        .expect("cache read after update should succeed");
    assert_eq!(loaded_after_update, Some(updated));

    cache
        .invalidate_state(MENU_SUPPLY_STATE_KEY)
        .await
        .expect("explicit invalidation should succeed");
    let observed_after_invalidate = cache
        .observe_state_key(MENU_SUPPLY_STATE_KEY)
        .await
        .expect("cache key observation should succeed after invalidation");
    assert!(
        !observed_after_invalidate.exists,
        "menu supply key should be deleted after invalidation"
    );
    assert_eq!(
        observed_after_invalidate.ttl_seconds, None,
        "invalidated key should not report a TTL"
    );

    for state_key in [
        VENDOR_FULFILLMENT_STATE_KEY,
        PAYROLL_LEDGER_STATE_KEY,
        ANOMALY_ALERT_STATE_KEY,
        DELIVERY_POLICY_STATE_KEY,
        OPERATIONS_ANALYTICS_STATE_KEY,
    ] {
        cache
            .write_through_snapshot(
                state_key,
                &CacheProbeSnapshot {
                    version: 1,
                    status: state_key.to_owned(),
                },
            )
            .await
            .expect("state cache write-through should succeed for all managed keys");
        let observation = cache
            .observe_state_key(state_key)
            .await
            .expect("state cache observation should succeed");
        assert!(observation.exists, "cache key `{state_key}` should exist");
        assert!(
            observation.ttl_seconds.is_some(),
            "cache key `{state_key}` should have TTL configured"
        );
    }
}
