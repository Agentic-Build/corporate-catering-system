use serde::{Deserialize, Serialize};
use crate::audit::AuditTimestamp;
use crate::identity::ActorId;
use crate::menu_supply_window::{Money, OrderId};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct EmployeeQuotaProfile {
    employee_actor_id: ActorId,
    weekly_quota_minor: Money,
}

impl EmployeeQuotaProfile {
    pub fn new(employee_actor_id: ActorId, weekly_quota_minor: Money) -> Self {
        Self {
            employee_actor_id,
            weekly_quota_minor,
        }
    }

    pub fn employee_actor_id(&self) -> &ActorId {
        &self.employee_actor_id
    }

    pub fn weekly_quota_minor(&self) -> &Money {
        &self.weekly_quota_minor
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct QuotaLedgerEntry {
    entry_id: u64,
    order_id: Option<OrderId>,
    employee_actor_id: ActorId,
    cycle_id: String,
    amount: Money,
    occurred_at: AuditTimestamp,
}

impl QuotaLedgerEntry {
    pub fn entry_id(&self) -> u64 {
        self.entry_id
    }

    pub fn order_id(&self) -> Option<&OrderId> {
        self.order_id.as_ref()
    }

    pub fn employee_actor_id(&self) -> &ActorId {
        &self.employee_actor_id
    }

    pub fn cycle_id(&self) -> &str {
        &self.cycle_id
    }

    pub fn amount(&self) -> &Money {
        &self.amount
    }

    pub fn occurred_at(&self) -> AuditTimestamp {
        self.occurred_at
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct QuotaUsageSummary {
    cycle_id: String,
    total_used_minor: Money,
    limit_minor: Money,
    remaining_minor: Money,
}

impl QuotaUsageSummary {
    pub fn new(cycle_id: String, total_used_minor: Money, limit_minor: Money) -> Self {
        let remaining = limit_minor.amount_minor().saturating_sub(total_used_minor.amount_minor());
        let remaining_minor = Money::new(limit_minor.currency(), remaining).expect("remaining amount must be valid");
        Self {
            cycle_id,
            total_used_minor,
            limit_minor,
            remaining_minor,
        }
    }

    pub fn cycle_id(&self) -> &str {
        &self.cycle_id
    }

    pub fn total_used_minor(&self) -> &Money {
        &self.total_used_minor
    }

    pub fn limit_minor(&self) -> &Money {
        &self.limit_minor
    }

    pub fn remaining_minor(&self) -> &Money {
        &self.remaining_minor
    }
}
