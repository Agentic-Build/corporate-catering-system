use std::collections::{BTreeMap, BTreeSet};

use serde::{Deserialize, Serialize};

pub const OPERATIONS_ANALYTICS_METRIC_SCHEMA_VERSION_V1: &str = "operations-v1";

pub const METRIC_KEY_ANOMALY_TRIGGERED_TOTAL: &str = "anomaly_triggered_total";
pub const METRIC_KEY_ANOMALY_CLOSED_TOTAL: &str = "anomaly_closed_total";
pub const METRIC_KEY_PAYROLL_SETTLEMENT_RECORDS_TOTAL: &str = "payroll_settlement_records_total";
pub const METRIC_KEY_PAYROLL_DISPUTED_RECORDS_TOTAL: &str = "payroll_disputed_records_total";
pub const METRIC_KEY_PAYROLL_DEDUCTION_FAILED_RECORDS_TOTAL: &str =
    "payroll_deduction_failed_records_total";
pub const METRIC_KEY_PAYROLL_HR_SYNC_FAILED_TOTAL: &str = "payroll_hr_sync_failed_total";

#[derive(Debug, Clone, PartialEq)]
pub struct OperationsAnalyticsMetricDefinition {
    pub key: &'static str,
    pub display_name: &'static str,
    pub unit: &'static str,
    pub formula: &'static str,
    pub source: &'static str,
    pub version: &'static str,
}

#[derive(Debug, Clone, PartialEq)]
pub struct OperationsAnalyticsMetricValue {
    pub key: &'static str,
    pub value: f64,
}

#[derive(Debug, Clone, PartialEq)]
pub struct OperationsAnalyticsBreakdownRow {
    pub dimension_value: String,
    pub metrics: Vec<OperationsAnalyticsMetricValue>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct OperationsAnalyticsTimeBreakdownRow {
    pub epoch_day: i32,
    pub metrics: Vec<OperationsAnalyticsMetricValue>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct OperationsAnalyticsDashboardSnapshot {
    pub metric_schema_version: &'static str,
    pub metric_definitions: Vec<OperationsAnalyticsMetricDefinition>,
    pub from_epoch_day: i32,
    pub to_epoch_day: i32,
    pub vendor_breakdown: Vec<OperationsAnalyticsBreakdownRow>,
    pub plant_breakdown: Vec<OperationsAnalyticsBreakdownRow>,
    pub time_breakdown: Vec<OperationsAnalyticsTimeBreakdownRow>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct OperationsAnalyticsQuery<'a> {
    pub from_epoch_day: i32,
    pub to_epoch_day: i32,
    pub vendor_scope: Option<&'a str>,
}

#[derive(Debug, Clone, Default)]
struct MetricBucket {
    totals: BTreeMap<&'static str, f64>,
}

impl MetricBucket {
    fn add_metric(&mut self, key: &'static str, delta: f64) {
        *self.totals.entry(key).or_insert(0.0) += delta;
    }

    fn merge_into(&self, target: &mut BTreeMap<&'static str, f64>) {
        for (&key, &value) in &self.totals {
            *target.entry(key).or_insert(0.0) += value;
        }
    }
}

#[derive(Debug, Clone, Default)]
pub struct OperationsAnalyticsWarehouse {
    by_vendor_plant_day: BTreeMap<(String, String, i32), MetricBucket>,
    recorded_payroll_settlement_batches: BTreeSet<String>,
    recorded_payroll_hr_sync_batches: BTreeSet<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OperationsAnalyticsWarehouseSnapshot {
    rows: Vec<OperationsAnalyticsWarehouseRowSnapshot>,
    recorded_payroll_settlement_batches: BTreeSet<String>,
    recorded_payroll_hr_sync_batches: BTreeSet<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OperationsAnalyticsWarehouseRowSnapshot {
    vendor_id: String,
    plant_id: String,
    epoch_day: i32,
    metrics: BTreeMap<String, f64>,
}

impl OperationsAnalyticsWarehouse {
    pub fn record_anomaly_triggered(&mut self, vendor_id: &str, plant_id: &str, epoch_day: i32) {
        self.record_metric(
            vendor_id,
            plant_id,
            epoch_day,
            METRIC_KEY_ANOMALY_TRIGGERED_TOTAL,
            1.0,
        );
    }

    pub fn record_anomaly_closed(&mut self, vendor_id: &str, plant_id: &str, epoch_day: i32) {
        self.record_metric(
            vendor_id,
            plant_id,
            epoch_day,
            METRIC_KEY_ANOMALY_CLOSED_TOTAL,
            1.0,
        );
    }

    pub fn record_payroll_settlement_closed(
        &mut self,
        vendor_id: &str,
        plant_id: &str,
        epoch_day: i32,
        batch_id: &str,
        total_records: usize,
        disputed_records: usize,
        deduction_failed_records: usize,
    ) {
        if !self
            .recorded_payroll_settlement_batches
            .insert(batch_id.to_owned())
        {
            return;
        }
        self.record_metric(
            vendor_id,
            plant_id,
            epoch_day,
            METRIC_KEY_PAYROLL_SETTLEMENT_RECORDS_TOTAL,
            total_records as f64,
        );
        self.record_metric(
            vendor_id,
            plant_id,
            epoch_day,
            METRIC_KEY_PAYROLL_DISPUTED_RECORDS_TOTAL,
            disputed_records as f64,
        );
        self.record_metric(
            vendor_id,
            plant_id,
            epoch_day,
            METRIC_KEY_PAYROLL_DEDUCTION_FAILED_RECORDS_TOTAL,
            deduction_failed_records as f64,
        );
    }

    pub fn record_payroll_hr_sync_outcome(
        &mut self,
        vendor_id: &str,
        plant_id: &str,
        epoch_day: i32,
        batch_id: &str,
        succeeded: bool,
    ) {
        if !self
            .recorded_payroll_hr_sync_batches
            .insert(batch_id.to_owned())
        {
            return;
        }
        if !succeeded {
            self.record_metric(
                vendor_id,
                plant_id,
                epoch_day,
                METRIC_KEY_PAYROLL_HR_SYNC_FAILED_TOTAL,
                1.0,
            );
        }
    }

    pub fn snapshot(&self) -> OperationsAnalyticsWarehouseSnapshot {
        let rows = self
            .by_vendor_plant_day
            .iter()
            .map(|((vendor_id, plant_id, epoch_day), bucket)| {
                let metrics = bucket
                    .totals
                    .iter()
                    .map(|(key, value)| ((*key).to_owned(), *value))
                    .collect::<BTreeMap<_, _>>();
                OperationsAnalyticsWarehouseRowSnapshot {
                    vendor_id: vendor_id.clone(),
                    plant_id: plant_id.clone(),
                    epoch_day: *epoch_day,
                    metrics,
                }
            })
            .collect::<Vec<_>>();
        OperationsAnalyticsWarehouseSnapshot {
            rows,
            recorded_payroll_settlement_batches: self.recorded_payroll_settlement_batches.clone(),
            recorded_payroll_hr_sync_batches: self.recorded_payroll_hr_sync_batches.clone(),
        }
    }

    pub fn from_snapshot(snapshot: OperationsAnalyticsWarehouseSnapshot) -> Result<Self, String> {
        let mut by_vendor_plant_day = BTreeMap::new();
        for row in snapshot.rows {
            let mut totals = BTreeMap::new();
            for (key, value) in row.metrics {
                let metric_key = metric_key_from_owned(key.as_str())?;
                totals.insert(metric_key, value);
            }
            by_vendor_plant_day.insert(
                (row.vendor_id, row.plant_id, row.epoch_day),
                MetricBucket { totals },
            );
        }
        Ok(Self {
            by_vendor_plant_day,
            recorded_payroll_settlement_batches: snapshot.recorded_payroll_settlement_batches,
            recorded_payroll_hr_sync_batches: snapshot.recorded_payroll_hr_sync_batches,
        })
    }

    pub fn query(
        &self,
        query: OperationsAnalyticsQuery<'_>,
    ) -> OperationsAnalyticsDashboardSnapshot {
        if query.from_epoch_day > query.to_epoch_day {
            return OperationsAnalyticsDashboardSnapshot {
                metric_schema_version: OPERATIONS_ANALYTICS_METRIC_SCHEMA_VERSION_V1,
                metric_definitions: metric_definitions_v1().to_vec(),
                from_epoch_day: query.from_epoch_day,
                to_epoch_day: query.to_epoch_day,
                vendor_breakdown: Vec::new(),
                plant_breakdown: Vec::new(),
                time_breakdown: Vec::new(),
            };
        }

        let mut by_vendor: BTreeMap<String, BTreeMap<&'static str, f64>> = BTreeMap::new();
        let mut by_plant: BTreeMap<String, BTreeMap<&'static str, f64>> = BTreeMap::new();
        let mut by_day: BTreeMap<i32, BTreeMap<&'static str, f64>> = BTreeMap::new();

        for ((vendor_id, plant_id, epoch_day), bucket) in &self.by_vendor_plant_day {
            if *epoch_day < query.from_epoch_day || *epoch_day > query.to_epoch_day {
                continue;
            }
            if let Some(scope_vendor_id) = query.vendor_scope {
                if vendor_id != scope_vendor_id {
                    continue;
                }
            }

            bucket.merge_into(by_vendor.entry(vendor_id.clone()).or_default());
            bucket.merge_into(by_plant.entry(plant_id.clone()).or_default());
            bucket.merge_into(by_day.entry(*epoch_day).or_default());
        }

        OperationsAnalyticsDashboardSnapshot {
            metric_schema_version: OPERATIONS_ANALYTICS_METRIC_SCHEMA_VERSION_V1,
            metric_definitions: metric_definitions_v1().to_vec(),
            from_epoch_day: query.from_epoch_day,
            to_epoch_day: query.to_epoch_day,
            vendor_breakdown: by_vendor
                .into_iter()
                .map(
                    |(dimension_value, totals)| OperationsAnalyticsBreakdownRow {
                        dimension_value,
                        metrics: as_metric_values(totals),
                    },
                )
                .collect(),
            plant_breakdown: by_plant
                .into_iter()
                .map(
                    |(dimension_value, totals)| OperationsAnalyticsBreakdownRow {
                        dimension_value,
                        metrics: as_metric_values(totals),
                    },
                )
                .collect(),
            time_breakdown: by_day
                .into_iter()
                .map(|(epoch_day, totals)| OperationsAnalyticsTimeBreakdownRow {
                    epoch_day,
                    metrics: as_metric_values(totals),
                })
                .collect(),
        }
    }

    fn record_metric(
        &mut self,
        vendor_id: &str,
        plant_id: &str,
        epoch_day: i32,
        metric_key: &'static str,
        delta: f64,
    ) {
        if !delta.is_finite() || delta == 0.0 {
            return;
        }
        self.by_vendor_plant_day
            .entry((vendor_id.to_owned(), plant_id.to_owned(), epoch_day))
            .or_default()
            .add_metric(metric_key, delta);
    }
}

fn metric_key_from_owned(value: &str) -> Result<&'static str, String> {
    match value {
        METRIC_KEY_ANOMALY_TRIGGERED_TOTAL => Ok(METRIC_KEY_ANOMALY_TRIGGERED_TOTAL),
        METRIC_KEY_ANOMALY_CLOSED_TOTAL => Ok(METRIC_KEY_ANOMALY_CLOSED_TOTAL),
        METRIC_KEY_PAYROLL_SETTLEMENT_RECORDS_TOTAL => {
            Ok(METRIC_KEY_PAYROLL_SETTLEMENT_RECORDS_TOTAL)
        }
        METRIC_KEY_PAYROLL_DISPUTED_RECORDS_TOTAL => Ok(METRIC_KEY_PAYROLL_DISPUTED_RECORDS_TOTAL),
        METRIC_KEY_PAYROLL_DEDUCTION_FAILED_RECORDS_TOTAL => {
            Ok(METRIC_KEY_PAYROLL_DEDUCTION_FAILED_RECORDS_TOTAL)
        }
        METRIC_KEY_PAYROLL_HR_SYNC_FAILED_TOTAL => Ok(METRIC_KEY_PAYROLL_HR_SYNC_FAILED_TOTAL),
        _ => Err(format!(
            "unsupported operations analytics metric key `{value}`"
        )),
    }
}

fn metric_definitions_v1() -> &'static [OperationsAnalyticsMetricDefinition] {
    &[
        OperationsAnalyticsMetricDefinition {
            key: METRIC_KEY_ANOMALY_TRIGGERED_TOTAL,
            display_name: "Triggered Anomaly Alerts",
            unit: "count",
            formula: "SUM(triggered alerts emitted by anomaly.evaluate_alerts)",
            source: "ANOMALY_ALERT_WORKFLOW",
            version: OPERATIONS_ANALYTICS_METRIC_SCHEMA_VERSION_V1,
        },
        OperationsAnalyticsMetricDefinition {
            key: METRIC_KEY_ANOMALY_CLOSED_TOTAL,
            display_name: "Closed Anomaly Alerts",
            unit: "count",
            formula: "SUM(alert lifecycle transitions where status == CLOSED)",
            source: "ANOMALY_ALERT_WORKFLOW",
            version: OPERATIONS_ANALYTICS_METRIC_SCHEMA_VERSION_V1,
        },
        OperationsAnalyticsMetricDefinition {
            key: METRIC_KEY_PAYROLL_SETTLEMENT_RECORDS_TOTAL,
            display_name: "Payroll Settlement Records",
            unit: "count",
            formula: "SUM(reconciliation.totalRecords from monthly settlement snapshots)",
            source: "PAYROLL_MONTHLY_SETTLEMENT_EXPORT",
            version: OPERATIONS_ANALYTICS_METRIC_SCHEMA_VERSION_V1,
        },
        OperationsAnalyticsMetricDefinition {
            key: METRIC_KEY_PAYROLL_DISPUTED_RECORDS_TOTAL,
            display_name: "Payroll Disputed Records",
            unit: "count",
            formula: "SUM(reconciliation.disputedRecords from monthly settlement snapshots)",
            source: "PAYROLL_MONTHLY_SETTLEMENT_EXPORT",
            version: OPERATIONS_ANALYTICS_METRIC_SCHEMA_VERSION_V1,
        },
        OperationsAnalyticsMetricDefinition {
            key: METRIC_KEY_PAYROLL_DEDUCTION_FAILED_RECORDS_TOTAL,
            display_name: "Payroll Deduction Failed Records",
            unit: "count",
            formula: "SUM(reconciliation.deductionFailedRecords from monthly settlement snapshots)",
            source: "PAYROLL_MONTHLY_SETTLEMENT_EXPORT",
            version: OPERATIONS_ANALYTICS_METRIC_SCHEMA_VERSION_V1,
        },
        OperationsAnalyticsMetricDefinition {
            key: METRIC_KEY_PAYROLL_HR_SYNC_FAILED_TOTAL,
            display_name: "Payroll HR Sync Failures",
            unit: "count",
            formula: "SUM(sync events where payroll HR API adjunct outcome == FAILED)",
            source: "PAYROLL_HR_API_SYNC",
            version: OPERATIONS_ANALYTICS_METRIC_SCHEMA_VERSION_V1,
        },
    ]
}

fn as_metric_values(totals: BTreeMap<&'static str, f64>) -> Vec<OperationsAnalyticsMetricValue> {
    metric_definitions_v1()
        .iter()
        .map(|definition| OperationsAnalyticsMetricValue {
            key: definition.key,
            value: totals.get(definition.key).copied().unwrap_or(0.0),
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn metric_value(metrics: &[OperationsAnalyticsMetricValue], key: &str) -> f64 {
        metrics
            .iter()
            .find(|metric| metric.key == key)
            .map(|metric| metric.value)
            .unwrap_or(0.0)
    }

    #[test]
    fn query_returns_vendor_plant_and_time_breakdowns() {
        let mut warehouse = OperationsAnalyticsWarehouse::default();
        warehouse.record_anomaly_triggered("ven-a", "fab-a", 100);
        warehouse.record_anomaly_triggered("ven-a", "fab-a", 100);
        warehouse.record_anomaly_closed("ven-a", "fab-a", 101);
        warehouse.record_payroll_settlement_closed("ven-a", "fab-a", 101, "batch-101", 40, 3, 1);
        warehouse.record_payroll_hr_sync_outcome("ven-a", "fab-a", 101, "batch-101", false);

        let snapshot = warehouse.query(OperationsAnalyticsQuery {
            from_epoch_day: 100,
            to_epoch_day: 101,
            vendor_scope: None,
        });

        assert_eq!(
            snapshot.metric_schema_version,
            OPERATIONS_ANALYTICS_METRIC_SCHEMA_VERSION_V1
        );
        assert_eq!(snapshot.vendor_breakdown.len(), 1);
        assert_eq!(snapshot.vendor_breakdown[0].dimension_value, "ven-a");
        assert_eq!(
            metric_value(
                &snapshot.vendor_breakdown[0].metrics,
                METRIC_KEY_ANOMALY_TRIGGERED_TOTAL
            ),
            2.0
        );
        assert_eq!(
            metric_value(
                &snapshot.vendor_breakdown[0].metrics,
                METRIC_KEY_ANOMALY_CLOSED_TOTAL
            ),
            1.0
        );
        assert_eq!(
            metric_value(
                &snapshot.vendor_breakdown[0].metrics,
                METRIC_KEY_PAYROLL_SETTLEMENT_RECORDS_TOTAL
            ),
            40.0
        );
        assert_eq!(
            metric_value(
                &snapshot.vendor_breakdown[0].metrics,
                METRIC_KEY_PAYROLL_DISPUTED_RECORDS_TOTAL
            ),
            3.0
        );
        assert_eq!(
            metric_value(
                &snapshot.vendor_breakdown[0].metrics,
                METRIC_KEY_PAYROLL_DEDUCTION_FAILED_RECORDS_TOTAL
            ),
            1.0
        );
        assert_eq!(
            metric_value(
                &snapshot.vendor_breakdown[0].metrics,
                METRIC_KEY_PAYROLL_HR_SYNC_FAILED_TOTAL
            ),
            1.0
        );

        assert_eq!(snapshot.plant_breakdown.len(), 1);
        assert_eq!(snapshot.plant_breakdown[0].dimension_value, "fab-a");
        assert_eq!(snapshot.time_breakdown.len(), 2);
        assert_eq!(snapshot.time_breakdown[0].epoch_day, 100);
        assert_eq!(snapshot.time_breakdown[1].epoch_day, 101);
        assert_eq!(
            metric_value(
                &snapshot.time_breakdown[0].metrics,
                METRIC_KEY_ANOMALY_TRIGGERED_TOTAL
            ),
            2.0
        );
        assert_eq!(
            metric_value(
                &snapshot.time_breakdown[1].metrics,
                METRIC_KEY_PAYROLL_SETTLEMENT_RECORDS_TOTAL
            ),
            40.0
        );
    }

    #[test]
    fn query_honors_vendor_scope() {
        let mut warehouse = OperationsAnalyticsWarehouse::default();
        warehouse.record_anomaly_triggered("ven-a", "fab-a", 300);
        warehouse.record_anomaly_triggered("ven-b", "fab-a", 300);

        let snapshot = warehouse.query(OperationsAnalyticsQuery {
            from_epoch_day: 300,
            to_epoch_day: 300,
            vendor_scope: Some("ven-a"),
        });
        assert_eq!(snapshot.vendor_breakdown.len(), 1);
        assert_eq!(snapshot.vendor_breakdown[0].dimension_value, "ven-a");
        assert_eq!(
            metric_value(
                &snapshot.vendor_breakdown[0].metrics,
                METRIC_KEY_ANOMALY_TRIGGERED_TOTAL
            ),
            1.0
        );
        assert_eq!(snapshot.plant_breakdown.len(), 1);
        assert_eq!(snapshot.time_breakdown.len(), 1);
    }

    #[test]
    fn payroll_settlement_metrics_are_idempotent_for_replayed_batches() {
        let mut warehouse = OperationsAnalyticsWarehouse::default();
        warehouse.record_payroll_settlement_closed("ven-a", "fab-a", 300, "batch-a", 40, 3, 1);
        warehouse.record_payroll_settlement_closed("ven-a", "fab-a", 300, "batch-a", 40, 3, 1);
        warehouse.record_payroll_settlement_closed("ven-a", "fab-a", 300, "batch-b", 10, 2, 0);

        let snapshot = warehouse.query(OperationsAnalyticsQuery {
            from_epoch_day: 300,
            to_epoch_day: 300,
            vendor_scope: Some("ven-a"),
        });
        let metrics = &snapshot.vendor_breakdown[0].metrics;
        assert_eq!(
            metric_value(metrics, METRIC_KEY_PAYROLL_SETTLEMENT_RECORDS_TOTAL),
            50.0
        );
        assert_eq!(
            metric_value(metrics, METRIC_KEY_PAYROLL_DISPUTED_RECORDS_TOTAL),
            5.0
        );
        assert_eq!(
            metric_value(metrics, METRIC_KEY_PAYROLL_DEDUCTION_FAILED_RECORDS_TOTAL),
            1.0
        );
    }

    #[test]
    fn payroll_hr_sync_failure_metric_uses_first_recorded_batch_outcome() {
        let mut warehouse = OperationsAnalyticsWarehouse::default();
        warehouse.record_payroll_hr_sync_outcome("ven-a", "fab-a", 300, "batch-success", true);
        warehouse.record_payroll_hr_sync_outcome("ven-a", "fab-a", 300, "batch-success", false);
        warehouse.record_payroll_hr_sync_outcome("ven-a", "fab-a", 300, "batch-failed", false);
        warehouse.record_payroll_hr_sync_outcome("ven-a", "fab-a", 300, "batch-failed", true);

        let snapshot = warehouse.query(OperationsAnalyticsQuery {
            from_epoch_day: 300,
            to_epoch_day: 300,
            vendor_scope: Some("ven-a"),
        });
        let metrics = &snapshot.vendor_breakdown[0].metrics;
        assert_eq!(
            metric_value(metrics, METRIC_KEY_PAYROLL_HR_SYNC_FAILED_TOTAL),
            1.0
        );
    }

    #[test]
    fn query_returns_empty_breakdowns_for_invalid_range() {
        let warehouse = OperationsAnalyticsWarehouse::default();
        let snapshot = warehouse.query(OperationsAnalyticsQuery {
            from_epoch_day: 12,
            to_epoch_day: 10,
            vendor_scope: None,
        });
        assert!(snapshot.vendor_breakdown.is_empty());
        assert!(snapshot.plant_breakdown.is_empty());
        assert!(snapshot.time_breakdown.is_empty());
    }
}
