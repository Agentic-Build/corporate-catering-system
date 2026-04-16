use std::fmt;
use std::time::Duration;

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
