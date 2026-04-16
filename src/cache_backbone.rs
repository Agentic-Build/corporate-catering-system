use redis::aio::ConnectionManager;
use redis::{AsyncCommands, Client};
use serde::de::DeserializeOwned;
use serde::Serialize;

pub const MENU_SUPPLY_STATE_KEY: &str = "menu_supply_policy";
pub const PAYROLL_LEDGER_STATE_KEY: &str = "payroll_ledger_service";
pub const ANOMALY_ALERT_STATE_KEY: &str = "anomaly_alert_workflow";
pub const DELIVERY_POLICY_STATE_KEY: &str = "vendor_delivery_policy";
pub const OPERATIONS_ANALYTICS_STATE_KEY: &str = "operations_analytics_warehouse";

const CACHE_TTL_MENU_SUPPLY_SECONDS_ENV: &str = "PRELAUNCH_CACHE_TTL_MENU_SUPPLY_SECONDS";
const CACHE_TTL_PAYROLL_LEDGER_SECONDS_ENV: &str = "PRELAUNCH_CACHE_TTL_PAYROLL_LEDGER_SECONDS";
const CACHE_TTL_ANOMALY_ALERT_SECONDS_ENV: &str = "PRELAUNCH_CACHE_TTL_ANOMALY_ALERT_SECONDS";
const CACHE_TTL_DELIVERY_POLICY_SECONDS_ENV: &str = "PRELAUNCH_CACHE_TTL_DELIVERY_POLICY_SECONDS";
const CACHE_TTL_OPERATIONS_ANALYTICS_SECONDS_ENV: &str =
    "PRELAUNCH_CACHE_TTL_OPERATIONS_ANALYTICS_SECONDS";

const DEFAULT_TTL_MENU_SUPPLY_SECONDS: u64 = 30;
const DEFAULT_TTL_PAYROLL_LEDGER_SECONDS: u64 = 30;
const DEFAULT_TTL_ANOMALY_ALERT_SECONDS: u64 = 15;
const DEFAULT_TTL_DELIVERY_POLICY_SECONDS: u64 = 60;
const DEFAULT_TTL_OPERATIONS_ANALYTICS_SECONDS: u64 = 30;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct RuntimeStateCacheTtls {
    pub menu_supply_seconds: u64,
    pub payroll_ledger_seconds: u64,
    pub anomaly_alert_seconds: u64,
    pub delivery_policy_seconds: u64,
    pub operations_analytics_seconds: u64,
}

impl Default for RuntimeStateCacheTtls {
    fn default() -> Self {
        Self {
            menu_supply_seconds: DEFAULT_TTL_MENU_SUPPLY_SECONDS,
            payroll_ledger_seconds: DEFAULT_TTL_PAYROLL_LEDGER_SECONDS,
            anomaly_alert_seconds: DEFAULT_TTL_ANOMALY_ALERT_SECONDS,
            delivery_policy_seconds: DEFAULT_TTL_DELIVERY_POLICY_SECONDS,
            operations_analytics_seconds: DEFAULT_TTL_OPERATIONS_ANALYTICS_SECONDS,
        }
    }
}

impl RuntimeStateCacheTtls {
    pub fn from_env() -> Result<Self, String> {
        Ok(Self {
            menu_supply_seconds: parse_positive_u64_env(
                CACHE_TTL_MENU_SUPPLY_SECONDS_ENV,
                DEFAULT_TTL_MENU_SUPPLY_SECONDS,
            )?,
            payroll_ledger_seconds: parse_positive_u64_env(
                CACHE_TTL_PAYROLL_LEDGER_SECONDS_ENV,
                DEFAULT_TTL_PAYROLL_LEDGER_SECONDS,
            )?,
            anomaly_alert_seconds: parse_positive_u64_env(
                CACHE_TTL_ANOMALY_ALERT_SECONDS_ENV,
                DEFAULT_TTL_ANOMALY_ALERT_SECONDS,
            )?,
            delivery_policy_seconds: parse_positive_u64_env(
                CACHE_TTL_DELIVERY_POLICY_SECONDS_ENV,
                DEFAULT_TTL_DELIVERY_POLICY_SECONDS,
            )?,
            operations_analytics_seconds: parse_positive_u64_env(
                CACHE_TTL_OPERATIONS_ANALYTICS_SECONDS_ENV,
                DEFAULT_TTL_OPERATIONS_ANALYTICS_SECONDS,
            )?,
        })
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CacheStateObservation {
    pub key: String,
    pub exists: bool,
    pub ttl_seconds: Option<i64>,
}

#[derive(Clone)]
pub struct ValkeyRuntimeStateCache {
    connection_manager: ConnectionManager,
    key_prefix: String,
    ttls: RuntimeStateCacheTtls,
}

impl std::fmt::Debug for ValkeyRuntimeStateCache {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("ValkeyRuntimeStateCache")
            .field("key_prefix", &self.key_prefix)
            .field("ttls", &self.ttls)
            .finish_non_exhaustive()
    }
}

impl ValkeyRuntimeStateCache {
    pub async fn connect(
        redis_url: impl AsRef<str>,
        key_prefix: impl Into<String>,
        ttls: RuntimeStateCacheTtls,
    ) -> Result<Self, String> {
        let redis_url = redis_url.as_ref().trim();
        if redis_url.is_empty() {
            return Err("Valkey URL must not be empty".to_owned());
        }
        let client = Client::open(redis_url)
            .map_err(|error| format!("failed to parse Valkey URL `{redis_url}`: {error}"))?;
        let mut connection_manager = ConnectionManager::new(client)
            .await
            .map_err(|error| format!("failed to connect to Valkey at `{redis_url}`: {error}"))?;
        let response: String = redis::cmd("PING")
            .query_async(&mut connection_manager)
            .await
            .map_err(|error| format!("failed to ping Valkey at `{redis_url}`: {error}"))?;
        if response != "PONG" {
            return Err(format!(
                "unexpected Valkey ping response from `{redis_url}`: {response}"
            ));
        }

        Ok(Self {
            connection_manager,
            key_prefix: normalize_key_prefix(key_prefix.into()),
            ttls,
        })
    }

    pub fn cache_key_for_state(&self, state_key: &str) -> String {
        format!("{}{}", self.key_prefix, state_key)
    }

    pub fn ttl_seconds_for_state(&self, state_key: &str) -> Result<u64, String> {
        match state_key {
            MENU_SUPPLY_STATE_KEY => Ok(self.ttls.menu_supply_seconds),
            PAYROLL_LEDGER_STATE_KEY => Ok(self.ttls.payroll_ledger_seconds),
            ANOMALY_ALERT_STATE_KEY => Ok(self.ttls.anomaly_alert_seconds),
            DELIVERY_POLICY_STATE_KEY => Ok(self.ttls.delivery_policy_seconds),
            OPERATIONS_ANALYTICS_STATE_KEY => Ok(self.ttls.operations_analytics_seconds),
            _ => Err(format!("unsupported runtime state cache key `{state_key}`")),
        }
    }

    pub async fn load_snapshot<T>(&self, state_key: &str) -> Result<Option<T>, String>
    where
        T: DeserializeOwned,
    {
        let cache_key = self.cache_key_for_state(state_key);
        let mut connection = self.connection_manager.clone();
        let payload: Option<String> = connection
            .get(cache_key.as_str())
            .await
            .map_err(|error| format!("failed to load cache key `{cache_key}`: {error}"))?;
        match payload {
            Some(serialized) => {
                let snapshot = serde_json::from_str::<T>(serialized.as_str()).map_err(|error| {
                    format!("failed to deserialize cache key `{cache_key}` payload: {error}")
                })?;
                tracing::info!(
                    cache_backbone = "valkey",
                    cache_event = "hit",
                    cache_key = %cache_key,
                    state_key = %state_key,
                    "runtime state cache hit"
                );
                Ok(Some(snapshot))
            }
            None => {
                tracing::info!(
                    cache_backbone = "valkey",
                    cache_event = "miss",
                    cache_key = %cache_key,
                    state_key = %state_key,
                    "runtime state cache miss"
                );
                Ok(None)
            }
        }
    }

    pub async fn write_through_snapshot<T>(
        &self,
        state_key: &str,
        snapshot: &T,
    ) -> Result<(), String>
    where
        T: Serialize,
    {
        let cache_key = self.cache_key_for_state(state_key);
        let ttl_seconds = self.ttl_seconds_for_state(state_key)?;
        let payload = serde_json::to_string(snapshot).map_err(|error| {
            format!("failed to serialize cache payload for `{cache_key}`: {error}")
        })?;
        let mut connection = self.connection_manager.clone();
        let mut pipeline = redis::pipe();
        pipeline
            .atomic()
            .del(cache_key.as_str())
            .set_ex(cache_key.as_str(), payload, ttl_seconds);
        pipeline
            .query_async::<()>(&mut connection)
            .await
            .map_err(|error| {
                format!(
                    "failed to execute write-through invalidation for cache key `{cache_key}`: {error}"
                )
            })?;
        tracing::info!(
            cache_backbone = "valkey",
            cache_event = "write_through_invalidate",
            cache_key = %cache_key,
            state_key = %state_key,
            ttl_seconds = ttl_seconds,
            "runtime state cache write-through invalidation committed"
        );
        Ok(())
    }

    pub async fn invalidate_state(&self, state_key: &str) -> Result<(), String> {
        let cache_key = self.cache_key_for_state(state_key);
        let mut connection = self.connection_manager.clone();
        let _: u64 = connection
            .del(cache_key.as_str())
            .await
            .map_err(|error| format!("failed to invalidate cache key `{cache_key}`: {error}"))?;
        tracing::info!(
            cache_backbone = "valkey",
            cache_event = "invalidate",
            cache_key = %cache_key,
            state_key = %state_key,
            "runtime state cache invalidated"
        );
        Ok(())
    }

    pub async fn observe_state_key(
        &self,
        state_key: &str,
    ) -> Result<CacheStateObservation, String> {
        let cache_key = self.cache_key_for_state(state_key);
        let mut connection = self.connection_manager.clone();
        let exists: bool = connection
            .exists(cache_key.as_str())
            .await
            .map_err(|error| format!("failed to check existence for `{cache_key}`: {error}"))?;
        let ttl_raw: i64 = redis::cmd("TTL")
            .arg(cache_key.as_str())
            .query_async(&mut connection)
            .await
            .map_err(|error| format!("failed to fetch TTL for `{cache_key}`: {error}"))?;
        let ttl_seconds = match ttl_raw {
            ttl if ttl >= 0 => Some(ttl),
            _ => None,
        };
        Ok(CacheStateObservation {
            key: cache_key,
            exists,
            ttl_seconds,
        })
    }
}

fn normalize_key_prefix(mut prefix: String) -> String {
    if prefix.trim().is_empty() {
        prefix = "ccs:runtime-state:".to_owned();
    }
    if !prefix.ends_with(':') {
        prefix.push(':');
    }
    prefix
}

fn parse_positive_u64_env(key: &str, default_value: u64) -> Result<u64, String> {
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
        Err(std::env::VarError::NotPresent) => Ok(default_value),
        Err(error) => Err(format!("{key} is invalid: {error}")),
    }
}
