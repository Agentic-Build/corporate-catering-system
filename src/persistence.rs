use std::fmt;
use std::time::Duration;

use serde::de::DeserializeOwned;
use sqlx::postgres::PgPoolOptions;
use sqlx::{PgPool, Postgres, Transaction};

use crate::audit::ImmutableAuditTrail;
use crate::vendor_compliance::{
    HistoryRetentionPolicy, VendorComplianceError, VendorComplianceLifecycle,
    VendorComplianceLifecycleSnapshot,
};

const VENDOR_COMPLIANCE_STATE_KEY: &str = "vendor_compliance_lifecycle";
const DATABASE_URL_ENV: &str = "DATABASE_URL";
const PRELAUNCH_DB_POOL_MAX_CONNECTIONS_ENV: &str = "PRELAUNCH_DB_POOL_MAX_CONNECTIONS";
const PRELAUNCH_DB_POOL_MIN_CONNECTIONS_ENV: &str = "PRELAUNCH_DB_POOL_MIN_CONNECTIONS";
const PRELAUNCH_DB_POOL_ACQUIRE_TIMEOUT_MS_ENV: &str = "PRELAUNCH_DB_POOL_ACQUIRE_TIMEOUT_MS";
const PRELAUNCH_DB_POOL_IDLE_TIMEOUT_SECONDS_ENV: &str = "PRELAUNCH_DB_POOL_IDLE_TIMEOUT_SECONDS";
const PRELAUNCH_DB_POOL_MAX_LIFETIME_SECONDS_ENV: &str = "PRELAUNCH_DB_POOL_MAX_LIFETIME_SECONDS";
const DEFAULT_DB_POOL_MAX_CONNECTIONS: u32 = 32;
const DEFAULT_DB_POOL_MIN_CONNECTIONS: u32 = 4;
const DEFAULT_DB_POOL_ACQUIRE_TIMEOUT_MS: u64 = 5_000;
const DEFAULT_DB_POOL_IDLE_TIMEOUT_SECONDS: u64 = 300;
const DEFAULT_DB_POOL_MAX_LIFETIME_SECONDS: u64 = 1_800;
const MENU_SUPPLY_STATE_KEY: &str = "menu_supply_policy";
const PAYROLL_LEDGER_STATE_KEY: &str = "payroll_ledger_service";
const ANOMALY_ALERT_STATE_KEY: &str = "anomaly_alert_workflow";
const DELIVERY_POLICY_STATE_KEY: &str = "vendor_delivery_policy";
const OPERATIONS_ANALYTICS_STATE_KEY: &str = "operations_analytics_warehouse";

#[derive(Debug, Clone)]
pub struct OutboxEventRecord {
    pub event_id: String,
    pub subject: String,
    pub payload: serde_json::Value,
}

pub async fn build_operational_pg_pool_from_env() -> Result<PgPool, String> {
    let database_url = std::env::var(DATABASE_URL_ENV)
        .map_err(|_| format!("{DATABASE_URL_ENV} must be configured"))?;
    let max_connections = parse_positive_u32_env(
        PRELAUNCH_DB_POOL_MAX_CONNECTIONS_ENV,
        DEFAULT_DB_POOL_MAX_CONNECTIONS,
    )?;
    let min_connections = parse_positive_u32_env(
        PRELAUNCH_DB_POOL_MIN_CONNECTIONS_ENV,
        DEFAULT_DB_POOL_MIN_CONNECTIONS,
    )?;
    if min_connections > max_connections {
        return Err(format!(
            "{PRELAUNCH_DB_POOL_MIN_CONNECTIONS_ENV} ({min_connections}) cannot exceed {PRELAUNCH_DB_POOL_MAX_CONNECTIONS_ENV} ({max_connections})"
        ));
    }
    let acquire_timeout = Duration::from_millis(parse_positive_u64_env(
        PRELAUNCH_DB_POOL_ACQUIRE_TIMEOUT_MS_ENV,
        DEFAULT_DB_POOL_ACQUIRE_TIMEOUT_MS,
    )?);
    let idle_timeout = Duration::from_secs(parse_positive_u64_env(
        PRELAUNCH_DB_POOL_IDLE_TIMEOUT_SECONDS_ENV,
        DEFAULT_DB_POOL_IDLE_TIMEOUT_SECONDS,
    )?);
    let max_lifetime = Duration::from_secs(parse_positive_u64_env(
        PRELAUNCH_DB_POOL_MAX_LIFETIME_SECONDS_ENV,
        DEFAULT_DB_POOL_MAX_LIFETIME_SECONDS,
    )?);

    PgPoolOptions::new()
        .max_connections(max_connections)
        .min_connections(min_connections)
        .acquire_timeout(acquire_timeout)
        .idle_timeout(Some(idle_timeout))
        .max_lifetime(Some(max_lifetime))
        .connect(database_url.as_str())
        .await
        .map_err(|error| format!("failed to create PostgreSQL connection pool: {error}"))
}

pub async fn apply_sql_migrations(pool: &PgPool) -> Result<(), String> {
    sqlx::migrate!("./migrations")
        .run(pool)
        .await
        .map_err(|error| format!("failed to apply SQL migrations: {error}"))
}

pub async fn allocate_order_id_hex_from_postgres(pool: &PgPool) -> Result<String, String> {
    sqlx::query_scalar!(
        r#"
SELECT encode(gen_random_bytes(16), 'hex') AS "order_id_hex!"
        "#
    )
    .fetch_one(pool)
    .await
    .map_err(|error| format!("failed to allocate order id from PostgreSQL: {error}"))
}

fn parse_positive_u32_env(env_name: &str, default: u32) -> Result<u32, String> {
    match std::env::var(env_name) {
        Ok(raw) => {
            let parsed = raw
                .trim()
                .parse::<u32>()
                .map_err(|error| format!("{env_name} must be a positive integer: {error}"))?;
            if parsed == 0 {
                return Err(format!("{env_name} must be greater than zero"));
            }
            Ok(parsed)
        }
        Err(std::env::VarError::NotPresent) => Ok(default),
        Err(error) => Err(format!("{env_name} is invalid: {error}")),
    }
}

fn parse_positive_u64_env(env_name: &str, default: u64) -> Result<u64, String> {
    match std::env::var(env_name) {
        Ok(raw) => {
            let parsed = raw
                .trim()
                .parse::<u64>()
                .map_err(|error| format!("{env_name} must be a positive integer: {error}"))?;
            if parsed == 0 {
                return Err(format!("{env_name} must be greater than zero"));
            }
            Ok(parsed)
        }
        Err(std::env::VarError::NotPresent) => Ok(default),
        Err(error) => Err(format!("{env_name} is invalid: {error}")),
    }
}

#[derive(Debug)]
pub enum JsonStatePersistenceError<E = String> {
    Sqlx(sqlx::Error),
    Serialize(serde_json::Error),
    Domain(E),
}

impl<E> fmt::Display for JsonStatePersistenceError<E>
where
    E: fmt::Display,
{
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Sqlx(error) => write!(f, "sql persistence operation failed: {error}"),
            Self::Serialize(error) => write!(f, "snapshot serialization failed: {error}"),
            Self::Domain(message) => write!(f, "domain mutation failed: {message}"),
        }
    }
}

impl<E> std::error::Error for JsonStatePersistenceError<E> where
    E: fmt::Debug + fmt::Display + 'static
{
}

impl<E> From<sqlx::Error> for JsonStatePersistenceError<E> {
    fn from(value: sqlx::Error) -> Self {
        Self::Sqlx(value)
    }
}

impl<E> From<serde_json::Error> for JsonStatePersistenceError<E> {
    fn from(value: serde_json::Error) -> Self {
        Self::Serialize(value)
    }
}

#[derive(Debug, Clone)]
pub struct SqlJsonStateRepository {
    pool: PgPool,
    state_key: &'static str,
}

impl SqlJsonStateRepository {
    pub fn for_menu_supply(pool: PgPool) -> Self {
        Self {
            pool,
            state_key: MENU_SUPPLY_STATE_KEY,
        }
    }

    pub fn for_payroll_ledger(pool: PgPool) -> Self {
        Self {
            pool,
            state_key: PAYROLL_LEDGER_STATE_KEY,
        }
    }

    pub fn for_anomaly_alert(pool: PgPool) -> Self {
        Self {
            pool,
            state_key: ANOMALY_ALERT_STATE_KEY,
        }
    }

    pub fn for_delivery_policy(pool: PgPool) -> Self {
        Self {
            pool,
            state_key: DELIVERY_POLICY_STATE_KEY,
        }
    }

    pub fn for_operations_analytics(pool: PgPool) -> Self {
        Self {
            pool,
            state_key: OPERATIONS_ANALYTICS_STATE_KEY,
        }
    }

    pub fn state_key(&self) -> &'static str {
        self.state_key
    }

    pub async fn load_snapshot<T>(&self) -> Result<Option<T>, JsonStatePersistenceError>
    where
        T: DeserializeOwned,
    {
        let payload = load_payload(&self.pool, self.state_key).await?;
        payload
            .map(serde_json::from_value)
            .transpose()
            .map_err(JsonStatePersistenceError::from)
    }

    pub async fn save_snapshot<T>(&self, snapshot: &T) -> Result<(), JsonStatePersistenceError>
    where
        T: serde::Serialize,
    {
        let payload = serde_json::to_value(snapshot)?;
        save_payload(&self.pool, self.state_key, payload).await?;
        Ok(())
    }

    pub async fn mutate_snapshot<T, R, E, F>(
        &self,
        mutator: F,
    ) -> Result<(T, R), JsonStatePersistenceError<E>>
    where
        T: serde::Serialize + DeserializeOwned,
        F: FnOnce(Option<T>) -> Result<(T, R), E>,
    {
        let mut transaction = self.pool.begin().await?;
        let payload = load_payload_for_update(&mut transaction, self.state_key).await?;
        let current = payload
            .map(serde_json::from_value)
            .transpose()
            .map_err(JsonStatePersistenceError::<E>::from)?;
        let (snapshot, value) = mutator(current).map_err(JsonStatePersistenceError::Domain)?;
        let payload = serde_json::to_value(&snapshot)?;
        save_payload_in_transaction(&mut transaction, self.state_key, payload).await?;
        transaction.commit().await?;
        Ok((snapshot, value))
    }

    pub async fn mutate_snapshot_with_outbox<T, R, E, F>(
        &self,
        mutator: F,
    ) -> Result<(T, R), JsonStatePersistenceError<E>>
    where
        T: serde::Serialize + DeserializeOwned,
        F: FnOnce(Option<T>) -> Result<(T, R, Vec<OutboxEventRecord>), E>,
    {
        let mut transaction = self.pool.begin().await?;
        let payload = load_payload_for_update(&mut transaction, self.state_key).await?;
        let current = payload
            .map(serde_json::from_value)
            .transpose()
            .map_err(JsonStatePersistenceError::<E>::from)?;
        let (snapshot, value, outbox_events) =
            mutator(current).map_err(JsonStatePersistenceError::Domain)?;
        let payload = serde_json::to_value(&snapshot)?;
        save_payload_in_transaction(&mut transaction, self.state_key, payload).await?;
        for outbox_event in outbox_events {
            insert_outbox_event_in_transaction(&mut transaction, &outbox_event).await?;
        }
        transaction.commit().await?;
        Ok((snapshot, value))
    }
}

async fn load_payload(
    pool: &PgPool,
    state_key: &str,
) -> Result<Option<serde_json::Value>, sqlx::Error> {
    let row = sqlx::query!(
        r#"
SELECT payload
FROM vendor_compliance_state
WHERE state_key = $1
        "#,
        state_key
    )
    .fetch_optional(pool)
    .await?;
    Ok(row.map(|row| row.payload))
}

async fn load_payload_for_update(
    transaction: &mut Transaction<'_, Postgres>,
    state_key: &str,
) -> Result<Option<serde_json::Value>, sqlx::Error> {
    let row = sqlx::query!(
        r#"
SELECT payload
FROM vendor_compliance_state
WHERE state_key = $1
FOR UPDATE
        "#,
        state_key
    )
    .fetch_optional(&mut **transaction)
    .await?;
    Ok(row.map(|row| row.payload))
}

async fn save_payload(
    pool: &PgPool,
    state_key: &str,
    payload: serde_json::Value,
) -> Result<(), sqlx::Error> {
    sqlx::query!(
        r#"
INSERT INTO vendor_compliance_state (state_key, payload)
VALUES ($1, $2)
ON CONFLICT (state_key)
DO UPDATE
SET payload = EXCLUDED.payload,
    updated_at_utc = CURRENT_TIMESTAMP
        "#,
        state_key,
        payload
    )
    .execute(pool)
    .await?;
    Ok(())
}

async fn save_payload_in_transaction(
    transaction: &mut Transaction<'_, Postgres>,
    state_key: &str,
    payload: serde_json::Value,
) -> Result<(), sqlx::Error> {
    sqlx::query!(
        r#"
INSERT INTO vendor_compliance_state (state_key, payload)
VALUES ($1, $2)
ON CONFLICT (state_key)
DO UPDATE
SET payload = EXCLUDED.payload,
    updated_at_utc = CURRENT_TIMESTAMP
        "#,
        state_key,
        payload
    )
    .execute(&mut **transaction)
    .await?;
    Ok(())
}

async fn insert_outbox_event_in_transaction(
    transaction: &mut Transaction<'_, Postgres>,
    event: &OutboxEventRecord,
) -> Result<(), sqlx::Error> {
    sqlx::query(
        r#"
INSERT INTO domain_event_outbox (event_id, subject, payload)
VALUES ($1, $2, $3)
ON CONFLICT (event_id) DO NOTHING
        "#,
    )
    .bind(event.event_id.as_str())
    .bind(event.subject.as_str())
    .bind(&event.payload)
    .execute(&mut **transaction)
    .await?;
    Ok(())
}

#[derive(Debug)]
pub enum VendorCompliancePersistenceError {
    Sqlx(sqlx::Error),
    Serialize(serde_json::Error),
    Domain(VendorComplianceError),
}

impl fmt::Display for VendorCompliancePersistenceError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Sqlx(error) => write!(f, "sql persistence operation failed: {error}"),
            Self::Serialize(error) => write!(f, "snapshot serialization failed: {error}"),
            Self::Domain(error) => write!(f, "domain mutation failed: {error}"),
        }
    }
}

impl std::error::Error for VendorCompliancePersistenceError {}

impl From<sqlx::Error> for VendorCompliancePersistenceError {
    fn from(value: sqlx::Error) -> Self {
        Self::Sqlx(value)
    }
}

impl From<serde_json::Error> for VendorCompliancePersistenceError {
    fn from(value: serde_json::Error) -> Self {
        Self::Serialize(value)
    }
}

#[derive(Debug, Clone)]
pub struct VendorComplianceSqlRepository {
    pool: PgPool,
}

impl VendorComplianceSqlRepository {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }

    pub fn pool(&self) -> &PgPool {
        &self.pool
    }

    pub async fn load_lifecycle(
        &self,
        retention_policy: HistoryRetentionPolicy,
        audit_trail: ImmutableAuditTrail,
    ) -> Result<Option<VendorComplianceLifecycle>, VendorCompliancePersistenceError> {
        let row = sqlx::query!(
            r#"
SELECT payload
FROM vendor_compliance_state
WHERE state_key = $1
            "#,
            VENDOR_COMPLIANCE_STATE_KEY
        )
        .fetch_optional(&self.pool)
        .await?;

        row.map(|row| lifecycle_from_payload(row.payload, retention_policy, audit_trail))
            .transpose()
    }

    pub async fn save_lifecycle(
        &self,
        lifecycle: &VendorComplianceLifecycle,
    ) -> Result<(), VendorCompliancePersistenceError> {
        let payload = serde_json::to_value(lifecycle.snapshot())?;
        sqlx::query!(
            r#"
INSERT INTO vendor_compliance_state (state_key, payload)
VALUES ($1, $2)
ON CONFLICT (state_key)
DO UPDATE
SET payload = EXCLUDED.payload,
    updated_at_utc = CURRENT_TIMESTAMP
            "#,
            VENDOR_COMPLIANCE_STATE_KEY,
            payload
        )
        .execute(&self.pool)
        .await?;
        Ok(())
    }

    pub async fn mutate_lifecycle<T, F>(
        &self,
        retention_policy: HistoryRetentionPolicy,
        audit_trail: ImmutableAuditTrail,
        mutator: F,
    ) -> Result<(VendorComplianceLifecycle, T), VendorCompliancePersistenceError>
    where
        F: FnOnce(&mut VendorComplianceLifecycle) -> Result<T, VendorComplianceError>,
    {
        let mut transaction = self.pool.begin().await?;
        let mut lifecycle = match self
            .load_lifecycle_for_update(
                &mut transaction,
                retention_policy.clone(),
                audit_trail.clone(),
            )
            .await?
        {
            Some(lifecycle) => lifecycle,
            None => VendorComplianceLifecycle::with_audit_trail(retention_policy, audit_trail),
        };

        let value = mutator(&mut lifecycle).map_err(VendorCompliancePersistenceError::Domain)?;
        self.save_lifecycle_in_transaction(&mut transaction, &lifecycle)
            .await?;
        transaction.commit().await?;
        Ok((lifecycle, value))
    }

    async fn load_lifecycle_for_update(
        &self,
        transaction: &mut Transaction<'_, Postgres>,
        retention_policy: HistoryRetentionPolicy,
        audit_trail: ImmutableAuditTrail,
    ) -> Result<Option<VendorComplianceLifecycle>, VendorCompliancePersistenceError> {
        let row = sqlx::query!(
            r#"
SELECT payload
FROM vendor_compliance_state
WHERE state_key = $1
FOR UPDATE
            "#,
            VENDOR_COMPLIANCE_STATE_KEY
        )
        .fetch_optional(&mut **transaction)
        .await?;

        row.map(|row| lifecycle_from_payload(row.payload, retention_policy, audit_trail))
            .transpose()
    }

    async fn save_lifecycle_in_transaction(
        &self,
        transaction: &mut Transaction<'_, Postgres>,
        lifecycle: &VendorComplianceLifecycle,
    ) -> Result<(), VendorCompliancePersistenceError> {
        let payload = serde_json::to_value(lifecycle.snapshot())?;
        sqlx::query!(
            r#"
INSERT INTO vendor_compliance_state (state_key, payload)
VALUES ($1, $2)
ON CONFLICT (state_key)
DO UPDATE
SET payload = EXCLUDED.payload,
    updated_at_utc = CURRENT_TIMESTAMP
            "#,
            VENDOR_COMPLIANCE_STATE_KEY,
            payload
        )
        .execute(&mut **transaction)
        .await?;
        Ok(())
    }
}

fn lifecycle_from_payload(
    payload: serde_json::Value,
    retention_policy: HistoryRetentionPolicy,
    audit_trail: ImmutableAuditTrail,
) -> Result<VendorComplianceLifecycle, VendorCompliancePersistenceError> {
    let snapshot: VendorComplianceLifecycleSnapshot = serde_json::from_value(payload)?;
    VendorComplianceLifecycle::from_snapshot(snapshot, retention_policy, audit_trail)
        .map_err(VendorCompliancePersistenceError::Domain)
}
