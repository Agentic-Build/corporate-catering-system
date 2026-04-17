use std::collections::{HashMap, HashSet};
use std::fmt;
use std::sync::{Arc, Mutex};

use crate::identity::ActorId;
use crate::menu_supply_window::{EmployeeMenuDiscoveryEntry, MenuItemId};
use crate::vendor_compliance::VendorId;
use crate::vendor_delivery_mapping::TaipeiBusinessMoment;

const MINUTES_PER_DAY: i64 = 24 * 60;

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum RushReminderScenario {
    PreorderOpen,
    DemandSpike,
}

impl RushReminderScenario {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::PreorderOpen => "PREORDER_OPEN",
            Self::DemandSpike => "DEMAND_SPIKE",
        }
    }
}

impl fmt::Display for RushReminderScenario {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

const RUSH_REMINDER_CHANNELS: [RushReminderChannel; 3] = [
    RushReminderChannel::InApp,
    RushReminderChannel::Email,
    RushReminderChannel::WebPush,
];

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum RushReminderChannel {
    InApp,
    Email,
    WebPush,
}

impl RushReminderChannel {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::InApp => "IN_APP",
            Self::Email => "EMAIL",
            Self::WebPush => "WEB_PUSH",
        }
    }
}

impl fmt::Display for RushReminderChannel {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct RushReminderChannelPreferences {
    in_app_enabled: bool,
    email_enabled: bool,
    web_push_enabled: bool,
}

impl RushReminderChannelPreferences {
    pub const fn new(in_app_enabled: bool, email_enabled: bool, web_push_enabled: bool) -> Self {
        Self {
            in_app_enabled,
            email_enabled,
            web_push_enabled,
        }
    }

    pub const fn in_app_enabled(self) -> bool {
        self.in_app_enabled
    }

    pub const fn email_enabled(self) -> bool {
        self.email_enabled
    }

    pub const fn web_push_enabled(self) -> bool {
        self.web_push_enabled
    }

    pub const fn allows(self, channel: RushReminderChannel) -> bool {
        match channel {
            RushReminderChannel::InApp => self.in_app_enabled,
            RushReminderChannel::Email => self.email_enabled,
            RushReminderChannel::WebPush => self.web_push_enabled,
        }
    }
}

impl Default for RushReminderChannelPreferences {
    fn default() -> Self {
        Self {
            in_app_enabled: true,
            email_enabled: false,
            web_push_enabled: false,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct RushReminderPreferences {
    preorder_open_enabled: bool,
    demand_spike_enabled: bool,
    channels: RushReminderChannelPreferences,
}

impl RushReminderPreferences {
    pub const fn new(preorder_open_enabled: bool, demand_spike_enabled: bool) -> Self {
        Self {
            preorder_open_enabled,
            demand_spike_enabled,
            channels: RushReminderChannelPreferences {
                in_app_enabled: true,
                email_enabled: false,
                web_push_enabled: false,
            },
        }
    }

    pub const fn with_channels(
        preorder_open_enabled: bool,
        demand_spike_enabled: bool,
        channels: RushReminderChannelPreferences,
    ) -> Self {
        Self {
            preorder_open_enabled,
            demand_spike_enabled,
            channels,
        }
    }

    pub const fn preorder_open_enabled(self) -> bool {
        self.preorder_open_enabled
    }

    pub const fn demand_spike_enabled(self) -> bool {
        self.demand_spike_enabled
    }

    pub const fn channel_preferences(self) -> RushReminderChannelPreferences {
        self.channels
    }

    pub const fn in_app_enabled(self) -> bool {
        self.channels.in_app_enabled()
    }

    pub const fn email_enabled(self) -> bool {
        self.channels.email_enabled()
    }

    pub const fn web_push_enabled(self) -> bool {
        self.channels.web_push_enabled()
    }

    pub const fn allows(self, scenario: RushReminderScenario) -> bool {
        match scenario {
            RushReminderScenario::PreorderOpen => self.preorder_open_enabled,
            RushReminderScenario::DemandSpike => self.demand_spike_enabled,
        }
    }

    pub const fn allows_channel(self, channel: RushReminderChannel) -> bool {
        self.channels.allows(channel)
    }
}

impl Default for RushReminderPreferences {
    fn default() -> Self {
        Self {
            preorder_open_enabled: true,
            demand_spike_enabled: true,
            channels: RushReminderChannelPreferences::default(),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct RushReminderChannelFeatureFlags {
    email_enabled: bool,
    web_push_enabled: bool,
}

impl RushReminderChannelFeatureFlags {
    pub const fn new(email_enabled: bool, web_push_enabled: bool) -> Self {
        Self {
            email_enabled,
            web_push_enabled,
        }
    }

    pub const fn email_enabled(self) -> bool {
        self.email_enabled
    }

    pub const fn web_push_enabled(self) -> bool {
        self.web_push_enabled
    }

    pub const fn supports(self, channel: RushReminderChannel) -> bool {
        match channel {
            RushReminderChannel::InApp => true,
            RushReminderChannel::Email => self.email_enabled,
            RushReminderChannel::WebPush => self.web_push_enabled,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct RushReminderRetryPolicy {
    in_app_max_attempts: u16,
    email_max_attempts: u16,
    web_push_max_attempts: u16,
}

impl RushReminderRetryPolicy {
    pub fn new(
        in_app_max_attempts: u16,
        email_max_attempts: u16,
        web_push_max_attempts: u16,
    ) -> Result<Self, RushReminderError> {
        if in_app_max_attempts == 0 {
            return Err(RushReminderError::InvalidPolicy(
                "in-app retry attempts must be greater than zero".to_owned(),
            ));
        }
        if email_max_attempts == 0 {
            return Err(RushReminderError::InvalidPolicy(
                "email retry attempts must be greater than zero".to_owned(),
            ));
        }
        if web_push_max_attempts == 0 {
            return Err(RushReminderError::InvalidPolicy(
                "web-push retry attempts must be greater than zero".to_owned(),
            ));
        }
        Ok(Self {
            in_app_max_attempts,
            email_max_attempts,
            web_push_max_attempts,
        })
    }

    pub const fn in_app_max_attempts(self) -> u16 {
        self.in_app_max_attempts
    }

    pub const fn email_max_attempts(self) -> u16 {
        self.email_max_attempts
    }

    pub const fn web_push_max_attempts(self) -> u16 {
        self.web_push_max_attempts
    }

    pub const fn max_attempts_for(self, channel: RushReminderChannel) -> u16 {
        match channel {
            RushReminderChannel::InApp => self.in_app_max_attempts,
            RushReminderChannel::Email => self.email_max_attempts,
            RushReminderChannel::WebPush => self.web_push_max_attempts,
        }
    }
}

impl Default for RushReminderRetryPolicy {
    fn default() -> Self {
        Self {
            in_app_max_attempts: 1,
            email_max_attempts: 3,
            web_push_max_attempts: 3,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct RushReminderPolicy {
    preorder_open_min_lead_days: u16,
    preorder_open_max_lead_days: u16,
    preorder_open_throttle_minutes: u16,
    demand_spike_remaining_quantity_threshold: u16,
    demand_spike_throttle_minutes: u16,
}

impl RushReminderPolicy {
    pub fn new(
        preorder_open_min_lead_days: u16,
        preorder_open_max_lead_days: u16,
        preorder_open_throttle_minutes: u16,
        demand_spike_remaining_quantity_threshold: u16,
        demand_spike_throttle_minutes: u16,
    ) -> Result<Self, RushReminderError> {
        if preorder_open_min_lead_days == 0 {
            return Err(RushReminderError::InvalidPolicy(
                "preorder-open minimum lead days must be greater than zero".to_owned(),
            ));
        }
        if preorder_open_max_lead_days == 0 {
            return Err(RushReminderError::InvalidPolicy(
                "preorder-open maximum lead days must be greater than zero".to_owned(),
            ));
        }
        if preorder_open_min_lead_days > preorder_open_max_lead_days {
            return Err(RushReminderError::InvalidPolicy(
                "preorder-open minimum lead days must be less than or equal to maximum lead days"
                    .to_owned(),
            ));
        }
        if preorder_open_throttle_minutes == 0 {
            return Err(RushReminderError::InvalidPolicy(
                "preorder-open throttle minutes must be greater than zero".to_owned(),
            ));
        }
        if demand_spike_remaining_quantity_threshold == 0 {
            return Err(RushReminderError::InvalidPolicy(
                "demand-spike remaining quantity threshold must be greater than zero".to_owned(),
            ));
        }
        if demand_spike_throttle_minutes == 0 {
            return Err(RushReminderError::InvalidPolicy(
                "demand-spike throttle minutes must be greater than zero".to_owned(),
            ));
        }

        Ok(Self {
            preorder_open_min_lead_days,
            preorder_open_max_lead_days,
            preorder_open_throttle_minutes,
            demand_spike_remaining_quantity_threshold,
            demand_spike_throttle_minutes,
        })
    }

    pub const fn preorder_open_min_lead_days(self) -> u16 {
        self.preorder_open_min_lead_days
    }

    pub const fn preorder_open_max_lead_days(self) -> u16 {
        self.preorder_open_max_lead_days
    }

    pub const fn preorder_open_throttle_minutes(self) -> u16 {
        self.preorder_open_throttle_minutes
    }

    pub const fn demand_spike_remaining_quantity_threshold(self) -> u16 {
        self.demand_spike_remaining_quantity_threshold
    }

    pub const fn demand_spike_throttle_minutes(self) -> u16 {
        self.demand_spike_throttle_minutes
    }

    fn throttle_minutes_for(self, scenario: RushReminderScenario) -> i64 {
        match scenario {
            RushReminderScenario::PreorderOpen => i64::from(self.preorder_open_throttle_minutes),
            RushReminderScenario::DemandSpike => i64::from(self.demand_spike_throttle_minutes),
        }
    }
}

impl Default for RushReminderPolicy {
    fn default() -> Self {
        Self {
            preorder_open_min_lead_days: 1,
            preorder_open_max_lead_days: 7,
            preorder_open_throttle_minutes: 180,
            demand_spike_remaining_quantity_threshold: 5,
            demand_spike_throttle_minutes: 30,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RushReminderNotification {
    actor_id: ActorId,
    scenario: RushReminderScenario,
    menu_item_id: MenuItemId,
    vendor_id: VendorId,
    delivery_epoch_day: i32,
    remaining_quantity: u16,
    scheduled_at: TaipeiBusinessMoment,
}

impl RushReminderNotification {
    pub fn actor_id(&self) -> &ActorId {
        &self.actor_id
    }

    pub fn scenario(&self) -> RushReminderScenario {
        self.scenario
    }

    pub fn menu_item_id(&self) -> &MenuItemId {
        &self.menu_item_id
    }

    pub fn vendor_id(&self) -> &VendorId {
        &self.vendor_id
    }

    pub fn delivery_epoch_day(&self) -> i32 {
        self.delivery_epoch_day
    }

    pub fn remaining_quantity(&self) -> u16 {
        self.remaining_quantity
    }

    pub fn scheduled_at(&self) -> TaipeiBusinessMoment {
        self.scheduled_at
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RushReminderDeliveryRecord {
    notification: RushReminderNotification,
    channel: RushReminderChannel,
    delivered_at: TaipeiBusinessMoment,
    attempts: u16,
}

impl RushReminderDeliveryRecord {
    pub fn notification(&self) -> &RushReminderNotification {
        &self.notification
    }

    pub fn channel(&self) -> RushReminderChannel {
        self.channel
    }

    pub fn delivered_at(&self) -> TaipeiBusinessMoment {
        self.delivered_at
    }

    pub fn attempts(&self) -> u16 {
        self.attempts
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RushReminderDeliveryFailure {
    notification: RushReminderNotification,
    channel: RushReminderChannel,
    attempted_at: TaipeiBusinessMoment,
    attempt: u16,
    exhausted: bool,
    message: String,
}

impl RushReminderDeliveryFailure {
    pub fn notification(&self) -> &RushReminderNotification {
        &self.notification
    }

    pub fn channel(&self) -> RushReminderChannel {
        self.channel
    }

    pub fn attempted_at(&self) -> TaipeiBusinessMoment {
        self.attempted_at
    }

    pub fn attempt(&self) -> u16 {
        self.attempt
    }

    pub fn exhausted(&self) -> bool {
        self.exhausted
    }

    pub fn message(&self) -> &str {
        &self.message
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct RushReminderScheduleReport {
    pub scheduled_count: usize,
    pub throttled_count: usize,
    pub opted_out_count: usize,
    pub skipped_count: usize,
    pub scheduled: Vec<RushReminderNotification>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RushReminderChannelDispatchStat {
    pub channel: RushReminderChannel,
    pub attempted_count: usize,
    pub delivered_count: usize,
    pub failed_count: usize,
    pub skipped_count: usize,
    pub retry_count: usize,
}

impl RushReminderChannelDispatchStat {
    fn new(channel: RushReminderChannel) -> Self {
        Self {
            channel,
            attempted_count: 0,
            delivered_count: 0,
            failed_count: 0,
            skipped_count: 0,
            retry_count: 0,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RushReminderDispatchReport {
    pub delivered_count: usize,
    pub failed_count: usize,
    pub skipped_count: usize,
    pub delivered: Vec<RushReminderDeliveryRecord>,
    pub failures: Vec<RushReminderDeliveryFailure>,
    pub channel_stats: Vec<RushReminderChannelDispatchStat>,
}

impl Default for RushReminderDispatchReport {
    fn default() -> Self {
        Self {
            delivered_count: 0,
            failed_count: 0,
            skipped_count: 0,
            delivered: Vec::new(),
            failures: Vec::new(),
            channel_stats: RUSH_REMINDER_CHANNELS
                .iter()
                .copied()
                .map(RushReminderChannelDispatchStat::new)
                .collect(),
        }
    }
}

impl RushReminderDispatchReport {
    fn channel_stat_mut(
        &mut self,
        channel: RushReminderChannel,
    ) -> &mut RushReminderChannelDispatchStat {
        let index = match channel {
            RushReminderChannel::InApp => 0,
            RushReminderChannel::Email => 1,
            RushReminderChannel::WebPush => 2,
        };
        &mut self.channel_stats[index]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RushReminderDeliveryError {
    message: String,
}

impl RushReminderDeliveryError {
    pub fn new(message: impl Into<String>) -> Self {
        Self {
            message: message.into(),
        }
    }

    pub fn message(&self) -> &str {
        &self.message
    }
}

impl fmt::Display for RushReminderDeliveryError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.message)
    }
}

impl std::error::Error for RushReminderDeliveryError {}

pub trait RushReminderDeliveryGateway: Send + Sync + fmt::Debug {
    fn deliver(
        &self,
        channel: RushReminderChannel,
        notification: &RushReminderNotification,
    ) -> Result<(), RushReminderDeliveryError>;
}

#[derive(Debug, Default)]
pub struct NoopRushReminderDeliveryGateway;

impl RushReminderDeliveryGateway for NoopRushReminderDeliveryGateway {
    fn deliver(
        &self,
        _channel: RushReminderChannel,
        _notification: &RushReminderNotification,
    ) -> Result<(), RushReminderDeliveryError> {
        Ok(())
    }
}

#[derive(Debug, Clone)]
pub struct RushReminderWorkflow {
    policy: RushReminderPolicy,
    state: Arc<Mutex<RushReminderState>>,
}

impl Default for RushReminderWorkflow {
    fn default() -> Self {
        Self::new(RushReminderPolicy::default())
    }
}

impl RushReminderWorkflow {
    pub fn new(policy: RushReminderPolicy) -> Self {
        Self {
            policy,
            state: Arc::new(Mutex::new(RushReminderState::default())),
        }
    }

    pub fn policy(&self) -> RushReminderPolicy {
        self.policy
    }

    pub fn upsert_preferences(
        &self,
        actor_id: ActorId,
        preferences: RushReminderPreferences,
    ) -> Result<(), RushReminderError> {
        let mut state = lock_state(&self.state)?;
        state.preferences_by_actor.insert(actor_id, preferences);
        Ok(())
    }

    pub fn preferences_for(
        &self,
        actor_id: &ActorId,
    ) -> Result<RushReminderPreferences, RushReminderError> {
        let state = lock_state(&self.state)?;
        Ok(state
            .preferences_by_actor
            .get(actor_id)
            .copied()
            .unwrap_or_default())
    }

    pub fn schedule_from_discovery(
        &self,
        runtime_enabled: bool,
        subscriber_actor_ids: &HashSet<ActorId>,
        entries: &[EmployeeMenuDiscoveryEntry],
        at: TaipeiBusinessMoment,
    ) -> Result<RushReminderScheduleReport, RushReminderError> {
        let mut report = RushReminderScheduleReport::default();

        if !runtime_enabled {
            report.skipped_count = subscriber_actor_ids.len().saturating_mul(entries.len());
            return Ok(report);
        }

        let mut state = lock_state(&self.state)?;

        for actor_id in subscriber_actor_ids {
            let preferences = state
                .preferences_by_actor
                .get(actor_id)
                .copied()
                .unwrap_or_default();
            for entry in entries {
                if should_schedule_preorder_open(entry, at, self.policy) {
                    schedule_if_allowed(
                        &mut state,
                        actor_id,
                        preferences,
                        entry,
                        RushReminderScenario::PreorderOpen,
                        self.policy,
                        at,
                        &mut report,
                    );
                }
                if should_schedule_demand_spike(entry, self.policy) {
                    schedule_if_allowed(
                        &mut state,
                        actor_id,
                        preferences,
                        entry,
                        RushReminderScenario::DemandSpike,
                        self.policy,
                        at,
                        &mut report,
                    );
                }
            }
        }

        Ok(report)
    }

    pub fn dispatch_pending(
        &self,
        runtime_enabled: bool,
        channel_feature_flags: RushReminderChannelFeatureFlags,
        retry_policy: RushReminderRetryPolicy,
        delivery_gateway: &(dyn RushReminderDeliveryGateway + Send + Sync),
        at: TaipeiBusinessMoment,
    ) -> Result<RushReminderDispatchReport, RushReminderError> {
        let mut report = RushReminderDispatchReport::default();

        let (pending, preferences_by_actor, mut handled_registry) = {
            let mut state = lock_state(&self.state)?;
            if !runtime_enabled {
                report.skipped_count = state.pending.len();
                return Ok(report);
            }
            (
                std::mem::take(&mut state.pending),
                state.preferences_by_actor.clone(),
                state.handled_registry.clone(),
            )
        };

        let mut delivered = Vec::new();
        let mut failures = Vec::new();
        for notification in pending {
            let preferences = preferences_by_actor
                .get(notification.actor_id())
                .copied()
                .unwrap_or_default();
            for channel in RUSH_REMINDER_CHANNELS {
                if !channel_feature_flags.supports(channel) || !preferences.allows_channel(channel)
                {
                    report.skipped_count = report.skipped_count.saturating_add(1);
                    let channel_stat = report.channel_stat_mut(channel);
                    channel_stat.skipped_count = channel_stat.skipped_count.saturating_add(1);
                    continue;
                }

                let delivery_key = reminder_delivery_key(&notification, channel);
                if handled_registry.contains(&delivery_key) {
                    report.skipped_count = report.skipped_count.saturating_add(1);
                    let channel_stat = report.channel_stat_mut(channel);
                    channel_stat.skipped_count = channel_stat.skipped_count.saturating_add(1);
                    continue;
                }

                let max_attempts = retry_policy.max_attempts_for(channel);
                let mut attempt = 0u16;
                while attempt < max_attempts {
                    attempt = attempt.saturating_add(1);
                    let channel_stat = report.channel_stat_mut(channel);
                    channel_stat.attempted_count = channel_stat.attempted_count.saturating_add(1);
                    if attempt > 1 {
                        channel_stat.retry_count = channel_stat.retry_count.saturating_add(1);
                    }

                    match delivery_gateway.deliver(channel, &notification) {
                        Ok(()) => {
                            delivered.push(RushReminderDeliveryRecord {
                                notification: notification.clone(),
                                channel,
                                delivered_at: at,
                                attempts: attempt,
                            });
                            let channel_stat = report.channel_stat_mut(channel);
                            channel_stat.delivered_count =
                                channel_stat.delivered_count.saturating_add(1);
                            handled_registry.insert(delivery_key.clone());
                            break;
                        }
                        Err(error) => {
                            let exhausted = attempt >= max_attempts;
                            failures.push(RushReminderDeliveryFailure {
                                notification: notification.clone(),
                                channel,
                                attempted_at: at,
                                attempt,
                                exhausted,
                                message: error.to_string(),
                            });
                            let channel_stat = report.channel_stat_mut(channel);
                            channel_stat.failed_count = channel_stat.failed_count.saturating_add(1);
                            if exhausted {
                                handled_registry.insert(delivery_key.clone());
                                break;
                            }
                        }
                    }
                }
            }
        }

        report.delivered_count = delivered.len();
        report.failed_count = failures.len();
        report.delivered = delivered;
        report.failures = failures;

        if report.delivered_count > 0 || report.failed_count > 0 {
            let mut state = lock_state(&self.state)?;
            state.delivered.extend(report.delivered.iter().cloned());
            state.failures.extend(report.failures.iter().cloned());
            state.handled_registry.extend(handled_registry);
        }

        Ok(report)
    }

    pub fn pending_notifications(
        &self,
    ) -> Result<Vec<RushReminderNotification>, RushReminderError> {
        let state = lock_state(&self.state)?;
        Ok(state.pending.clone())
    }

    pub fn delivered_notifications(
        &self,
    ) -> Result<Vec<RushReminderNotification>, RushReminderError> {
        let state = lock_state(&self.state)?;
        Ok(state
            .delivered
            .iter()
            .map(|delivery| delivery.notification.clone())
            .collect())
    }

    pub fn delivered_channel_records(
        &self,
    ) -> Result<Vec<RushReminderDeliveryRecord>, RushReminderError> {
        let state = lock_state(&self.state)?;
        Ok(state.delivered.clone())
    }

    pub fn delivery_failures(&self) -> Result<Vec<RushReminderDeliveryFailure>, RushReminderError> {
        let state = lock_state(&self.state)?;
        Ok(state.failures.clone())
    }
}

#[derive(Debug, Clone, Default)]
struct RushReminderState {
    preferences_by_actor: HashMap<ActorId, RushReminderPreferences>,
    throttle_registry: HashMap<ReminderThrottleKey, TaipeiBusinessMoment>,
    pending: Vec<RushReminderNotification>,
    delivered: Vec<RushReminderDeliveryRecord>,
    failures: Vec<RushReminderDeliveryFailure>,
    handled_registry: HashSet<ReminderDeliveryKey>,
}

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
struct ReminderThrottleKey {
    actor_id: ActorId,
    scenario: RushReminderScenario,
    menu_item_id: MenuItemId,
    delivery_epoch_day: i32,
}

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
struct ReminderDeliveryKey {
    actor_id: ActorId,
    scenario: RushReminderScenario,
    menu_item_id: MenuItemId,
    delivery_epoch_day: i32,
    channel: RushReminderChannel,
}

fn should_schedule_preorder_open(
    entry: &EmployeeMenuDiscoveryEntry,
    at: TaipeiBusinessMoment,
    policy: RushReminderPolicy,
) -> bool {
    if !entry.preorder_open() {
        return false;
    }
    if entry.remaining_quantity() == 0 {
        return false;
    }

    let lead_days = entry
        .menu_item()
        .delivery_epoch_day()
        .saturating_sub(at.epoch_day());
    lead_days >= i32::from(policy.preorder_open_min_lead_days())
        && lead_days <= i32::from(policy.preorder_open_max_lead_days())
}

fn should_schedule_demand_spike(
    entry: &EmployeeMenuDiscoveryEntry,
    policy: RushReminderPolicy,
) -> bool {
    entry.preorder_open()
        && entry.remaining_quantity() > 0
        && entry.remaining_quantity() <= policy.demand_spike_remaining_quantity_threshold()
}

fn schedule_if_allowed(
    state: &mut RushReminderState,
    actor_id: &ActorId,
    preferences: RushReminderPreferences,
    entry: &EmployeeMenuDiscoveryEntry,
    scenario: RushReminderScenario,
    policy: RushReminderPolicy,
    at: TaipeiBusinessMoment,
    report: &mut RushReminderScheduleReport,
) {
    if !preferences.allows(scenario) {
        report.opted_out_count = report.opted_out_count.saturating_add(1);
        return;
    }

    let throttle_key = ReminderThrottleKey {
        actor_id: actor_id.clone(),
        scenario,
        menu_item_id: entry.menu_item().menu_item_id().clone(),
        delivery_epoch_day: entry.menu_item().delivery_epoch_day(),
    };

    let throttle_minutes = policy.throttle_minutes_for(scenario);
    if let Some(previous_scheduled_at) = state.throttle_registry.get(&throttle_key) {
        let elapsed_minutes = minutes_between(*previous_scheduled_at, at);
        if elapsed_minutes < 0 || elapsed_minutes < throttle_minutes {
            report.throttled_count = report.throttled_count.saturating_add(1);
            return;
        }
    }

    let notification = RushReminderNotification {
        actor_id: actor_id.clone(),
        scenario,
        menu_item_id: entry.menu_item().menu_item_id().clone(),
        vendor_id: entry.menu_item().vendor_id().clone(),
        delivery_epoch_day: entry.menu_item().delivery_epoch_day(),
        remaining_quantity: entry.remaining_quantity(),
        scheduled_at: at,
    };

    state.pending.push(notification.clone());
    state.throttle_registry.insert(throttle_key, at);

    report.scheduled_count = report.scheduled_count.saturating_add(1);
    report.scheduled.push(notification);
}

fn reminder_delivery_key(
    notification: &RushReminderNotification,
    channel: RushReminderChannel,
) -> ReminderDeliveryKey {
    ReminderDeliveryKey {
        actor_id: notification.actor_id().clone(),
        scenario: notification.scenario(),
        menu_item_id: notification.menu_item_id().clone(),
        delivery_epoch_day: notification.delivery_epoch_day(),
        channel,
    }
}

fn minutes_between(previous: TaipeiBusinessMoment, current: TaipeiBusinessMoment) -> i64 {
    let day_delta = i64::from(current.epoch_day()) - i64::from(previous.epoch_day());
    let minute_delta = i64::from(current.minute_of_day()) - i64::from(previous.minute_of_day());
    day_delta
        .saturating_mul(MINUTES_PER_DAY)
        .saturating_add(minute_delta)
}

fn lock_state(
    state: &Arc<Mutex<RushReminderState>>,
) -> Result<std::sync::MutexGuard<'_, RushReminderState>, RushReminderError> {
    state.lock().map_err(|_| RushReminderError::StatePoisoned)
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RushReminderError {
    InvalidPolicy(String),
    StatePoisoned,
}

impl fmt::Display for RushReminderError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::InvalidPolicy(message) => write!(f, "invalid rush reminder policy: {message}"),
            Self::StatePoisoned => {
                f.write_str("rush reminder state is poisoned due to a previous panic")
            }
        }
    }
}

impl std::error::Error for RushReminderError {}
