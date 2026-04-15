use std::collections::{BTreeMap, BTreeSet};
use std::fmt;
use std::sync::{Arc, Mutex};

use crate::identity::{AuthenticatedActorContext, Role};
use crate::vendor_compliance::VendorId;
use crate::vendor_delivery_mapping::TaipeiBusinessMoment;

const MAX_MENU_NAME_LENGTH: usize = 80;
const MAX_MENU_DESCRIPTION_LENGTH: usize = 280;
const MAX_MENU_IMAGE_URL_LENGTH: usize = 512;
const MAX_DAILY_QUANTITY: u16 = 2000;
const MIN_ORDER_LINE_ITEM_QUANTITY: u16 = 1;
const MAX_ORDER_LINE_ITEM_QUANTITY: u16 = 20;
const MAX_SPECIAL_REQUEST_COUNT: usize = 3;
const MINUTES_PER_DAY: u16 = 24 * 60;
const MIN_PREORDER_OPEN_DAYS_AHEAD: u16 = 1;
const MAX_ALLOWED_PREORDER_OPEN_DAYS_AHEAD: u16 = 7;
const DEFAULT_PREORDER_OPEN_DAYS_AHEAD: u16 = 7;
const DEFAULT_MODIFY_CANCEL_CUTOFF_MINUTE_OF_DAY: u16 = 17 * 60;
const MIN_VENDOR_OVERRIDE_CUTOFF_MINUTE_OF_DAY: u16 = 15 * 60;
const MAX_VENDOR_OVERRIDE_CUTOFF_MINUTE_OF_DAY: u16 = 20 * 60;

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct MenuItemId(String);

impl MenuItemId {
    pub fn parse(value: impl Into<String>) -> Result<Self, MenuSupplyWindowError> {
        let value = value.into();
        if value.trim().is_empty() {
            return Err(MenuSupplyWindowError::InvalidMenuItemId);
        }
        Ok(Self(value))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl fmt::Display for MenuItemId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct OrderId(String);

impl OrderId {
    pub fn parse(value: impl Into<String>) -> Result<Self, MenuSupplyWindowError> {
        let value = value.into();
        if value.trim().is_empty() {
            return Err(MenuSupplyWindowError::InvalidOrderId);
        }
        Ok(Self(value))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl fmt::Display for OrderId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Money {
    currency: String,
    amount_minor: u32,
}

impl Money {
    pub fn new(
        currency: impl Into<String>,
        amount_minor: u32,
    ) -> Result<Self, MenuSupplyWindowError> {
        let currency = currency.into();
        if !is_valid_iso_currency(&currency) {
            return Err(MenuSupplyWindowError::InvalidCurrencyCode);
        }
        Ok(Self {
            currency,
            amount_minor,
        })
    }

    pub fn currency(&self) -> &str {
        &self.currency
    }

    pub fn amount_minor(&self) -> u32 {
        self.amount_minor
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MenuImageUrl(String);

impl MenuImageUrl {
    pub fn parse(value: impl Into<String>) -> Result<Self, MenuSupplyWindowError> {
        let value = value.into();
        let trimmed = value.trim();
        if trimmed.is_empty() {
            return Err(MenuSupplyWindowError::InvalidMenuImageUrl(
                "image URL must not be empty".to_owned(),
            ));
        }
        if trimmed.len() > MAX_MENU_IMAGE_URL_LENGTH {
            return Err(MenuSupplyWindowError::InvalidMenuImageUrl(format!(
                "image URL must be at most {MAX_MENU_IMAGE_URL_LENGTH} characters"
            )));
        }
        if !trimmed.starts_with("https://") {
            return Err(MenuSupplyWindowError::InvalidMenuImageUrl(
                "image URL must use https:// scheme".to_owned(),
            ));
        }

        Ok(Self(trimmed.to_owned()))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl fmt::Display for MenuImageUrl {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum SpecialRequest {
    LessRice,
    NoGreenOnion,
    SauceOnSide,
    NoUtensils,
    ExtraSpicy,
}

impl SpecialRequest {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::LessRice => "LESS_RICE",
            Self::NoGreenOnion => "NO_GREEN_ONION",
            Self::SauceOnSide => "SAUCE_ON_SIDE",
            Self::NoUtensils => "NO_UTENSILS",
            Self::ExtraSpicy => "EXTRA_SPICY",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct OrderLineItemRequest {
    menu_item_id: MenuItemId,
    quantity: u16,
    special_requests: BTreeSet<SpecialRequest>,
}

impl OrderLineItemRequest {
    pub fn new(
        menu_item_id: MenuItemId,
        quantity: u16,
        special_requests: Vec<SpecialRequest>,
    ) -> Result<Self, MenuSupplyWindowError> {
        if !(MIN_ORDER_LINE_ITEM_QUANTITY..=MAX_ORDER_LINE_ITEM_QUANTITY).contains(&quantity) {
            return Err(MenuSupplyWindowError::InvalidOrderLineItemQuantity {
                quantity,
                minimum: MIN_ORDER_LINE_ITEM_QUANTITY,
                maximum: MAX_ORDER_LINE_ITEM_QUANTITY,
            });
        }

        if special_requests.len() > MAX_SPECIAL_REQUEST_COUNT {
            return Err(MenuSupplyWindowError::TooManySpecialRequests {
                maximum: MAX_SPECIAL_REQUEST_COUNT,
            });
        }

        let mut deduped = BTreeSet::new();
        for special_request in special_requests {
            if !deduped.insert(special_request) {
                return Err(MenuSupplyWindowError::DuplicateSpecialRequest(
                    special_request,
                ));
            }
        }

        Ok(Self {
            menu_item_id,
            quantity,
            special_requests: deduped,
        })
    }

    pub fn menu_item_id(&self) -> &MenuItemId {
        &self.menu_item_id
    }

    pub fn quantity(&self) -> u16 {
        self.quantity
    }

    pub fn special_requests(&self) -> &BTreeSet<SpecialRequest> {
        &self.special_requests
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VendorMenuItemDraft {
    name: String,
    description: String,
    image_url: Option<MenuImageUrl>,
    price: Money,
    max_daily_quantity: u16,
    delivery_epoch_day: i32,
    preorder_open_days_ahead_override: Option<u16>,
    modify_cancel_cutoff_minute_of_day_override: Option<u16>,
}

impl VendorMenuItemDraft {
    pub fn new(
        name: impl Into<String>,
        description: impl Into<String>,
        image_url: Option<MenuImageUrl>,
        price: Money,
        max_daily_quantity: u16,
        delivery_epoch_day: i32,
    ) -> Result<Self, MenuSupplyWindowError> {
        let name = normalize_non_empty_text(name.into(), MAX_MENU_NAME_LENGTH, "menu name")?;
        let description = normalize_non_empty_text(
            description.into(),
            MAX_MENU_DESCRIPTION_LENGTH,
            "menu description",
        )?;
        if !(1..=MAX_DAILY_QUANTITY).contains(&max_daily_quantity) {
            return Err(MenuSupplyWindowError::InvalidMaxDailyQuantity {
                quantity: max_daily_quantity,
                minimum: 1,
                maximum: MAX_DAILY_QUANTITY,
            });
        }

        Ok(Self {
            name,
            description,
            image_url,
            price,
            max_daily_quantity,
            delivery_epoch_day,
            preorder_open_days_ahead_override: None,
            modify_cancel_cutoff_minute_of_day_override: None,
        })
    }

    pub fn with_ordering_policy_overrides(
        mut self,
        policy_override: VendorOrderingPolicyOverride,
    ) -> Self {
        self.preorder_open_days_ahead_override = policy_override.preorder_open_days_ahead;
        self.modify_cancel_cutoff_minute_of_day_override =
            policy_override.modify_cancel_cutoff_minute_of_day;
        self
    }

    pub fn name(&self) -> &str {
        &self.name
    }

    pub fn description(&self) -> &str {
        &self.description
    }

    pub fn image_url(&self) -> Option<&MenuImageUrl> {
        self.image_url.as_ref()
    }

    pub fn price(&self) -> &Money {
        &self.price
    }

    pub fn max_daily_quantity(&self) -> u16 {
        self.max_daily_quantity
    }

    pub fn delivery_epoch_day(&self) -> i32 {
        self.delivery_epoch_day
    }

    pub fn preorder_open_days_ahead_override(&self) -> Option<u16> {
        self.preorder_open_days_ahead_override
    }

    pub fn modify_cancel_cutoff_minute_of_day_override(&self) -> Option<u16> {
        self.modify_cancel_cutoff_minute_of_day_override
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VendorMenuItem {
    menu_item_id: MenuItemId,
    vendor_id: VendorId,
    name: String,
    description: String,
    image_url: Option<MenuImageUrl>,
    price: Money,
    max_daily_quantity: u16,
    delivery_epoch_day: i32,
    preorder_open_days_ahead_override: Option<u16>,
    modify_cancel_cutoff_minute_of_day_override: Option<u16>,
}

impl VendorMenuItem {
    pub fn new(menu_item_id: MenuItemId, vendor_id: VendorId, draft: VendorMenuItemDraft) -> Self {
        Self {
            menu_item_id,
            vendor_id,
            name: draft.name,
            description: draft.description,
            image_url: draft.image_url,
            price: draft.price,
            max_daily_quantity: draft.max_daily_quantity,
            delivery_epoch_day: draft.delivery_epoch_day,
            preorder_open_days_ahead_override: draft.preorder_open_days_ahead_override,
            modify_cancel_cutoff_minute_of_day_override: draft
                .modify_cancel_cutoff_minute_of_day_override,
        }
    }

    pub fn menu_item_id(&self) -> &MenuItemId {
        &self.menu_item_id
    }

    pub fn vendor_id(&self) -> &VendorId {
        &self.vendor_id
    }

    pub fn name(&self) -> &str {
        &self.name
    }

    pub fn description(&self) -> &str {
        &self.description
    }

    pub fn image_url(&self) -> Option<&MenuImageUrl> {
        self.image_url.as_ref()
    }

    pub fn price(&self) -> &Money {
        &self.price
    }

    pub fn max_daily_quantity(&self) -> u16 {
        self.max_daily_quantity
    }

    pub fn delivery_epoch_day(&self) -> i32 {
        self.delivery_epoch_day
    }

    pub fn preorder_open_days_ahead_override(&self) -> Option<u16> {
        self.preorder_open_days_ahead_override
    }

    pub fn modify_cancel_cutoff_minute_of_day_override(&self) -> Option<u16> {
        self.modify_cancel_cutoff_minute_of_day_override
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VendorMenuItemState {
    menu_item: VendorMenuItem,
    remaining_quantity: u16,
    preorder_open_days_ahead: u16,
    modify_cancel_cutoff_minute_of_day: u16,
}

impl VendorMenuItemState {
    pub fn menu_item(&self) -> &VendorMenuItem {
        &self.menu_item
    }

    pub fn remaining_quantity(&self) -> u16 {
        self.remaining_quantity
    }

    pub fn preorder_open_days_ahead(&self) -> u16 {
        self.preorder_open_days_ahead
    }

    pub fn modify_cancel_cutoff_minute_of_day(&self) -> u16 {
        self.modify_cancel_cutoff_minute_of_day
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct VendorOrderingPolicy {
    preorder_open_days_ahead: u16,
    modify_cancel_cutoff_minute_of_day: u16,
}

impl VendorOrderingPolicy {
    pub const fn preorder_open_days_ahead(self) -> u16 {
        self.preorder_open_days_ahead
    }

    pub const fn modify_cancel_cutoff_minute_of_day(self) -> u16 {
        self.modify_cancel_cutoff_minute_of_day
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct VendorOrderingPolicyOverride {
    pub preorder_open_days_ahead: Option<u16>,
    pub modify_cancel_cutoff_minute_of_day: Option<u16>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct OrderingGovernancePolicy {
    max_preorder_open_days_ahead: u16,
    default_preorder_open_days_ahead: u16,
    default_modify_cancel_cutoff_minute_of_day: u16,
    min_vendor_override_cutoff_minute_of_day: u16,
    max_vendor_override_cutoff_minute_of_day: u16,
}

impl OrderingGovernancePolicy {
    pub fn new(
        max_preorder_open_days_ahead: u16,
        default_preorder_open_days_ahead: u16,
        default_modify_cancel_cutoff_minute_of_day: u16,
        min_vendor_override_cutoff_minute_of_day: u16,
        max_vendor_override_cutoff_minute_of_day: u16,
    ) -> Result<Self, MenuSupplyWindowError> {
        if !(MIN_PREORDER_OPEN_DAYS_AHEAD..=MAX_ALLOWED_PREORDER_OPEN_DAYS_AHEAD)
            .contains(&max_preorder_open_days_ahead)
        {
            return Err(MenuSupplyWindowError::InvalidGovernanceConfiguration(
                format!(
                    "max preorder open days must be between {MIN_PREORDER_OPEN_DAYS_AHEAD} and {MAX_ALLOWED_PREORDER_OPEN_DAYS_AHEAD}"
                ),
            ));
        }

        if !(MIN_PREORDER_OPEN_DAYS_AHEAD..=max_preorder_open_days_ahead)
            .contains(&default_preorder_open_days_ahead)
        {
            return Err(MenuSupplyWindowError::InvalidGovernanceConfiguration(
                "default preorder open days must be within configured override bounds".to_owned(),
            ));
        }

        if min_vendor_override_cutoff_minute_of_day >= MINUTES_PER_DAY
            || max_vendor_override_cutoff_minute_of_day >= MINUTES_PER_DAY
            || min_vendor_override_cutoff_minute_of_day > max_vendor_override_cutoff_minute_of_day
        {
            return Err(MenuSupplyWindowError::InvalidGovernanceConfiguration(
                "vendor cutoff override bounds are invalid".to_owned(),
            ));
        }

        if default_modify_cancel_cutoff_minute_of_day < min_vendor_override_cutoff_minute_of_day
            || default_modify_cancel_cutoff_minute_of_day > max_vendor_override_cutoff_minute_of_day
        {
            return Err(MenuSupplyWindowError::InvalidGovernanceConfiguration(
                "default cutoff must be inside configured vendor override bounds".to_owned(),
            ));
        }

        Ok(Self {
            max_preorder_open_days_ahead,
            default_preorder_open_days_ahead,
            default_modify_cancel_cutoff_minute_of_day,
            min_vendor_override_cutoff_minute_of_day,
            max_vendor_override_cutoff_minute_of_day,
        })
    }

    pub const fn max_preorder_open_days_ahead(self) -> u16 {
        self.max_preorder_open_days_ahead
    }

    pub const fn default_preorder_open_days_ahead(self) -> u16 {
        self.default_preorder_open_days_ahead
    }

    pub const fn default_modify_cancel_cutoff_minute_of_day(self) -> u16 {
        self.default_modify_cancel_cutoff_minute_of_day
    }

    pub const fn min_vendor_override_cutoff_minute_of_day(self) -> u16 {
        self.min_vendor_override_cutoff_minute_of_day
    }

    pub const fn max_vendor_override_cutoff_minute_of_day(self) -> u16 {
        self.max_vendor_override_cutoff_minute_of_day
    }

    fn default_vendor_policy(self) -> VendorOrderingPolicy {
        VendorOrderingPolicy {
            preorder_open_days_ahead: self.default_preorder_open_days_ahead,
            modify_cancel_cutoff_minute_of_day: self.default_modify_cancel_cutoff_minute_of_day,
        }
    }

    fn resolve_vendor_policy(
        self,
        policy_override: VendorOrderingPolicyOverride,
    ) -> Result<VendorOrderingPolicy, MenuSupplyWindowError> {
        let preorder_open_days_ahead = policy_override
            .preorder_open_days_ahead
            .unwrap_or(self.default_preorder_open_days_ahead);
        if !(MIN_PREORDER_OPEN_DAYS_AHEAD..=self.max_preorder_open_days_ahead)
            .contains(&preorder_open_days_ahead)
        {
            return Err(MenuSupplyWindowError::VendorOverrideOutOfBounds {
                field: "preorderOpenDaysAhead",
                minimum: MIN_PREORDER_OPEN_DAYS_AHEAD,
                maximum: self.max_preorder_open_days_ahead,
                actual: preorder_open_days_ahead,
            });
        }

        let modify_cancel_cutoff_minute_of_day = policy_override
            .modify_cancel_cutoff_minute_of_day
            .unwrap_or(self.default_modify_cancel_cutoff_minute_of_day);
        if modify_cancel_cutoff_minute_of_day < self.min_vendor_override_cutoff_minute_of_day
            || modify_cancel_cutoff_minute_of_day > self.max_vendor_override_cutoff_minute_of_day
        {
            return Err(MenuSupplyWindowError::VendorOverrideOutOfBounds {
                field: "modifyCancelCutoffMinuteOfDay",
                minimum: self.min_vendor_override_cutoff_minute_of_day,
                maximum: self.max_vendor_override_cutoff_minute_of_day,
                actual: modify_cancel_cutoff_minute_of_day,
            });
        }

        Ok(VendorOrderingPolicy {
            preorder_open_days_ahead,
            modify_cancel_cutoff_minute_of_day,
        })
    }
}

impl Default for OrderingGovernancePolicy {
    fn default() -> Self {
        Self {
            max_preorder_open_days_ahead: MAX_ALLOWED_PREORDER_OPEN_DAYS_AHEAD,
            default_preorder_open_days_ahead: DEFAULT_PREORDER_OPEN_DAYS_AHEAD,
            default_modify_cancel_cutoff_minute_of_day: DEFAULT_MODIFY_CANCEL_CUTOFF_MINUTE_OF_DAY,
            min_vendor_override_cutoff_minute_of_day: MIN_VENDOR_OVERRIDE_CUTOFF_MINUTE_OF_DAY,
            max_vendor_override_cutoff_minute_of_day: MAX_VENDOR_OVERRIDE_CUTOFF_MINUTE_OF_DAY,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum OrderMutation {
    ReplaceLineItems {
        line_items: Vec<OrderLineItemRequest>,
    },
    Cancel,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct StoredOrder {
    vendor_id: VendorId,
    delivery_epoch_day: i32,
    line_items: BTreeMap<MenuItemId, u16>,
}

#[derive(Debug, Clone, Default)]
struct MenuSupplyState {
    menu_items: BTreeMap<MenuItemId, VendorMenuItem>,
    allocated_quantity_by_menu_item: BTreeMap<MenuItemId, u16>,
    orders: BTreeMap<OrderId, StoredOrder>,
    vendor_ordering_policies: BTreeMap<VendorId, VendorOrderingPolicy>,
}

#[derive(Debug, Clone)]
pub struct MenuSupplyPolicy {
    governance: OrderingGovernancePolicy,
    state: Arc<Mutex<MenuSupplyState>>,
}

impl Default for MenuSupplyPolicy {
    fn default() -> Self {
        Self::new(OrderingGovernancePolicy::default())
    }
}

impl MenuSupplyPolicy {
    pub fn new(governance: OrderingGovernancePolicy) -> Self {
        Self {
            governance,
            state: Arc::new(Mutex::new(MenuSupplyState::default())),
        }
    }

    pub fn governance(&self) -> OrderingGovernancePolicy {
        self.governance
    }

    pub fn upsert_vendor_ordering_policy(
        &self,
        actor: &AuthenticatedActorContext,
        vendor_id: &VendorId,
        policy_override: VendorOrderingPolicyOverride,
    ) -> Result<VendorOrderingPolicy, MenuSupplyWindowError> {
        ensure_role(actor, Role::VendorOperator)?;

        let resolved = self.governance.resolve_vendor_policy(policy_override)?;
        let mut state = lock_state(&self.state)?;
        state
            .vendor_ordering_policies
            .insert(vendor_id.clone(), resolved);
        Ok(resolved)
    }

    pub fn effective_vendor_ordering_policy(
        &self,
        vendor_id: &VendorId,
    ) -> Result<VendorOrderingPolicy, MenuSupplyWindowError> {
        let state = lock_state(&self.state)?;
        Ok(self.effective_vendor_policy_locked(&state, vendor_id))
    }

    pub fn upsert_menu_item(
        &self,
        actor: &AuthenticatedActorContext,
        menu_item: VendorMenuItem,
    ) -> Result<(), MenuSupplyWindowError> {
        ensure_role(actor, Role::VendorOperator)?;

        let mut state = lock_state(&self.state)?;
        let currently_allocated = state
            .allocated_quantity_by_menu_item
            .get(menu_item.menu_item_id())
            .copied()
            .unwrap_or(0);
        if currently_allocated > menu_item.max_daily_quantity() {
            return Err(MenuSupplyWindowError::QuotaReductionBelowAllocated {
                menu_item_id: menu_item.menu_item_id().clone(),
                allocated_quantity: currently_allocated,
                attempted_max_daily_quantity: menu_item.max_daily_quantity(),
            });
        }

        if menu_item.preorder_open_days_ahead_override().is_some()
            || menu_item
                .modify_cancel_cutoff_minute_of_day_override()
                .is_some()
        {
            let resolved_policy =
                self.governance
                    .resolve_vendor_policy(VendorOrderingPolicyOverride {
                        preorder_open_days_ahead: menu_item.preorder_open_days_ahead_override(),
                        modify_cancel_cutoff_minute_of_day: menu_item
                            .modify_cancel_cutoff_minute_of_day_override(),
                    })?;
            state
                .vendor_ordering_policies
                .insert(menu_item.vendor_id().clone(), resolved_policy);
        }

        state
            .menu_items
            .insert(menu_item.menu_item_id().clone(), menu_item);
        Ok(())
    }

    pub fn menu_item(
        &self,
        menu_item_id: &MenuItemId,
    ) -> Result<Option<VendorMenuItem>, MenuSupplyWindowError> {
        let state = lock_state(&self.state)?;
        Ok(state.menu_items.get(menu_item_id).cloned())
    }

    pub fn menu_item_state(
        &self,
        menu_item_id: &MenuItemId,
    ) -> Result<Option<VendorMenuItemState>, MenuSupplyWindowError> {
        let state = lock_state(&self.state)?;
        let Some(menu_item) = state.menu_items.get(menu_item_id).cloned() else {
            return Ok(None);
        };
        let allocated = state
            .allocated_quantity_by_menu_item
            .get(menu_item_id)
            .copied()
            .unwrap_or(0);
        let policy = self.effective_vendor_policy_locked(&state, menu_item.vendor_id());
        Ok(Some(VendorMenuItemState {
            remaining_quantity: menu_item.max_daily_quantity().saturating_sub(allocated),
            menu_item,
            preorder_open_days_ahead: policy.preorder_open_days_ahead(),
            modify_cancel_cutoff_minute_of_day: policy.modify_cancel_cutoff_minute_of_day(),
        }))
    }

    pub fn remaining_quantity(
        &self,
        menu_item_id: &MenuItemId,
    ) -> Result<Option<u16>, MenuSupplyWindowError> {
        let state = lock_state(&self.state)?;
        let menu_item = match state.menu_items.get(menu_item_id) {
            Some(menu_item) => menu_item,
            None => return Ok(None),
        };
        let allocated = state
            .allocated_quantity_by_menu_item
            .get(menu_item_id)
            .copied()
            .unwrap_or(0);
        Ok(Some(
            menu_item.max_daily_quantity().saturating_sub(allocated),
        ))
    }

    pub fn create_order(
        &self,
        order_id: OrderId,
        vendor_id: &VendorId,
        delivery_epoch_day: i32,
        line_items: Vec<OrderLineItemRequest>,
        placed_at: TaipeiBusinessMoment,
    ) -> Result<(), MenuSupplyWindowError> {
        let mut state = lock_state(&self.state)?;
        if state.orders.contains_key(&order_id) {
            return Err(MenuSupplyWindowError::OrderAlreadyExists(order_id));
        }

        self.enforce_create_window_locked(&state, vendor_id, delivery_epoch_day, placed_at)?;
        let aggregated_line_items = self.validate_and_aggregate_line_items_locked(
            &state,
            vendor_id,
            delivery_epoch_day,
            &line_items,
        )?;
        self.ensure_quota_capacity_locked(&state, &aggregated_line_items)?;

        for (menu_item_id, quantity) in &aggregated_line_items {
            let allocated = state
                .allocated_quantity_by_menu_item
                .entry(menu_item_id.clone())
                .or_insert(0);
            *allocated = allocated.saturating_add(*quantity);
        }

        state.orders.insert(
            order_id,
            StoredOrder {
                vendor_id: vendor_id.clone(),
                delivery_epoch_day,
                line_items: aggregated_line_items,
            },
        );

        Ok(())
    }

    pub fn update_order(
        &self,
        order_id: &OrderId,
        mutation: OrderMutation,
        requested_at: TaipeiBusinessMoment,
    ) -> Result<(), MenuSupplyWindowError> {
        let mut state = lock_state(&self.state)?;
        let stored_order = state
            .orders
            .get(order_id)
            .cloned()
            .ok_or_else(|| MenuSupplyWindowError::OrderNotFound(order_id.clone()))?;

        self.enforce_modify_cancel_cutoff_locked(
            &state,
            &stored_order.vendor_id,
            stored_order.delivery_epoch_day,
            requested_at,
        )?;

        match mutation {
            OrderMutation::Cancel => {
                self.release_allocations_locked(&mut state, &stored_order.line_items);
                state.orders.remove(order_id);
                Ok(())
            }
            OrderMutation::ReplaceLineItems { line_items } => {
                let next_line_items = self.validate_and_aggregate_line_items_locked(
                    &state,
                    &stored_order.vendor_id,
                    stored_order.delivery_epoch_day,
                    &line_items,
                )?;
                self.ensure_quota_capacity_for_update_locked(
                    &state,
                    &stored_order.line_items,
                    &next_line_items,
                )?;

                self.release_allocations_locked(&mut state, &stored_order.line_items);
                for (menu_item_id, quantity) in &next_line_items {
                    let allocated = state
                        .allocated_quantity_by_menu_item
                        .entry(menu_item_id.clone())
                        .or_insert(0);
                    *allocated = allocated.saturating_add(*quantity);
                }

                state.orders.insert(
                    order_id.clone(),
                    StoredOrder {
                        vendor_id: stored_order.vendor_id,
                        delivery_epoch_day: stored_order.delivery_epoch_day,
                        line_items: next_line_items,
                    },
                );
                Ok(())
            }
        }
    }

    fn effective_vendor_policy_locked(
        &self,
        state: &MenuSupplyState,
        vendor_id: &VendorId,
    ) -> VendorOrderingPolicy {
        state
            .vendor_ordering_policies
            .get(vendor_id)
            .copied()
            .unwrap_or_else(|| self.governance.default_vendor_policy())
    }

    fn enforce_create_window_locked(
        &self,
        state: &MenuSupplyState,
        vendor_id: &VendorId,
        delivery_epoch_day: i32,
        placed_at: TaipeiBusinessMoment,
    ) -> Result<(), MenuSupplyWindowError> {
        let policy = self.effective_vendor_policy_locked(state, vendor_id);
        let earliest_delivery_epoch_day = placed_at.epoch_day();
        let latest_delivery_epoch_day = placed_at
            .epoch_day()
            .saturating_add(i32::from(policy.preorder_open_days_ahead()));

        if delivery_epoch_day < earliest_delivery_epoch_day
            || delivery_epoch_day > latest_delivery_epoch_day
        {
            return Err(MenuSupplyWindowError::PreorderWindowClosed {
                vendor_id: vendor_id.clone(),
                earliest_delivery_epoch_day,
                latest_delivery_epoch_day,
                requested_delivery_epoch_day: delivery_epoch_day,
            });
        }

        self.enforce_modify_cancel_cutoff_locked(state, vendor_id, delivery_epoch_day, placed_at)
    }

    fn enforce_modify_cancel_cutoff_locked(
        &self,
        state: &MenuSupplyState,
        vendor_id: &VendorId,
        delivery_epoch_day: i32,
        requested_at: TaipeiBusinessMoment,
    ) -> Result<(), MenuSupplyWindowError> {
        let policy = self.effective_vendor_policy_locked(state, vendor_id);
        let cutoff_epoch_day = delivery_epoch_day.saturating_sub(1);
        let cutoff =
            TaipeiBusinessMoment::new(cutoff_epoch_day, policy.modify_cancel_cutoff_minute_of_day)
                .map_err(|error| {
                    MenuSupplyWindowError::InvalidGovernanceConfiguration(error.to_string())
                })?;

        if requested_at >= cutoff {
            return Err(MenuSupplyWindowError::ModifyCancelCutoffPassed {
                delivery_epoch_day,
                cutoff_epoch_day,
                cutoff_minute_of_day: policy.modify_cancel_cutoff_minute_of_day,
                requested_epoch_day: requested_at.epoch_day(),
                requested_minute_of_day: requested_at.minute_of_day(),
            });
        }

        Ok(())
    }

    fn validate_and_aggregate_line_items_locked(
        &self,
        state: &MenuSupplyState,
        vendor_id: &VendorId,
        delivery_epoch_day: i32,
        line_items: &[OrderLineItemRequest],
    ) -> Result<BTreeMap<MenuItemId, u16>, MenuSupplyWindowError> {
        if line_items.is_empty() {
            return Err(MenuSupplyWindowError::EmptyOrderLineItems);
        }

        let mut aggregated_line_items = BTreeMap::<MenuItemId, u16>::new();
        for line_item in line_items {
            let menu_item = state
                .menu_items
                .get(line_item.menu_item_id())
                .ok_or_else(|| MenuSupplyWindowError::MenuItemNotFound {
                    menu_item_id: line_item.menu_item_id().clone(),
                })?;
            if menu_item.vendor_id() != vendor_id {
                return Err(MenuSupplyWindowError::MenuItemVendorMismatch {
                    menu_item_id: menu_item.menu_item_id().clone(),
                    expected_vendor_id: vendor_id.clone(),
                    actual_vendor_id: menu_item.vendor_id().clone(),
                });
            }
            if menu_item.delivery_epoch_day() != delivery_epoch_day {
                return Err(MenuSupplyWindowError::MenuItemDeliveryDateMismatch {
                    menu_item_id: menu_item.menu_item_id().clone(),
                    expected_delivery_epoch_day: delivery_epoch_day,
                    actual_delivery_epoch_day: menu_item.delivery_epoch_day(),
                });
            }

            if aggregated_line_items
                .insert(line_item.menu_item_id().clone(), line_item.quantity())
                .is_some()
            {
                return Err(MenuSupplyWindowError::DuplicateMenuItemInOrder {
                    menu_item_id: line_item.menu_item_id().clone(),
                });
            }
        }

        Ok(aggregated_line_items)
    }

    fn ensure_quota_capacity_locked(
        &self,
        state: &MenuSupplyState,
        requested_line_items: &BTreeMap<MenuItemId, u16>,
    ) -> Result<(), MenuSupplyWindowError> {
        for (menu_item_id, requested_quantity) in requested_line_items {
            let menu_item = state.menu_items.get(menu_item_id).ok_or_else(|| {
                MenuSupplyWindowError::MenuItemNotFound {
                    menu_item_id: menu_item_id.clone(),
                }
            })?;
            let allocated_quantity = state
                .allocated_quantity_by_menu_item
                .get(menu_item_id)
                .copied()
                .unwrap_or(0);
            let remaining_quantity = menu_item
                .max_daily_quantity()
                .saturating_sub(allocated_quantity);
            if *requested_quantity > remaining_quantity {
                return Err(MenuSupplyWindowError::QuotaExceeded {
                    menu_item_id: menu_item_id.clone(),
                    requested_quantity: *requested_quantity,
                    remaining_quantity,
                });
            }
        }

        Ok(())
    }

    fn ensure_quota_capacity_for_update_locked(
        &self,
        state: &MenuSupplyState,
        current_line_items: &BTreeMap<MenuItemId, u16>,
        next_line_items: &BTreeMap<MenuItemId, u16>,
    ) -> Result<(), MenuSupplyWindowError> {
        let affected_menu_items = current_line_items
            .keys()
            .chain(next_line_items.keys())
            .cloned()
            .collect::<BTreeSet<_>>();

        for menu_item_id in affected_menu_items {
            let menu_item = state.menu_items.get(&menu_item_id).ok_or_else(|| {
                MenuSupplyWindowError::MenuItemNotFound {
                    menu_item_id: menu_item_id.clone(),
                }
            })?;
            let currently_allocated = state
                .allocated_quantity_by_menu_item
                .get(&menu_item_id)
                .copied()
                .unwrap_or(0);
            let current_order_quantity =
                current_line_items.get(&menu_item_id).copied().unwrap_or(0);
            let next_order_quantity = next_line_items.get(&menu_item_id).copied().unwrap_or(0);

            let projected_allocated = currently_allocated
                .saturating_sub(current_order_quantity)
                .saturating_add(next_order_quantity);

            if projected_allocated > menu_item.max_daily_quantity() {
                let remaining_quantity = menu_item
                    .max_daily_quantity()
                    .saturating_sub(currently_allocated.saturating_sub(current_order_quantity));
                return Err(MenuSupplyWindowError::QuotaExceeded {
                    menu_item_id,
                    requested_quantity: next_order_quantity,
                    remaining_quantity,
                });
            }
        }

        Ok(())
    }

    fn release_allocations_locked(
        &self,
        state: &mut MenuSupplyState,
        line_items: &BTreeMap<MenuItemId, u16>,
    ) {
        for (menu_item_id, quantity) in line_items {
            if let Some(allocated) = state.allocated_quantity_by_menu_item.get_mut(menu_item_id) {
                *allocated = allocated.saturating_sub(*quantity);
                if *allocated == 0 {
                    state.allocated_quantity_by_menu_item.remove(menu_item_id);
                }
            }
        }
    }
}

fn normalize_non_empty_text(
    value: String,
    max_length: usize,
    field_name: &'static str,
) -> Result<String, MenuSupplyWindowError> {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return Err(MenuSupplyWindowError::InvalidTextField {
            field: field_name,
            reason: "must not be empty",
        });
    }
    if trimmed.len() > max_length {
        return Err(MenuSupplyWindowError::InvalidTextField {
            field: field_name,
            reason: "exceeds maximum length",
        });
    }

    Ok(trimmed.to_owned())
}

fn ensure_role(actor: &AuthenticatedActorContext, role: Role) -> Result<(), MenuSupplyWindowError> {
    if actor.role() != role {
        return Err(MenuSupplyWindowError::UnauthorizedRole {
            expected: role,
            actual: actor.role(),
        });
    }
    Ok(())
}

fn lock_state(
    state: &Arc<Mutex<MenuSupplyState>>,
) -> Result<std::sync::MutexGuard<'_, MenuSupplyState>, MenuSupplyWindowError> {
    state
        .lock()
        .map_err(|_| MenuSupplyWindowError::StatePoisoned)
}

fn is_valid_iso_currency(value: &str) -> bool {
    value.len() == 3 && value.chars().all(|ch| ch.is_ascii_uppercase())
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum MenuSupplyWindowError {
    InvalidMenuItemId,
    InvalidOrderId,
    InvalidCurrencyCode,
    InvalidMenuImageUrl(String),
    InvalidTextField {
        field: &'static str,
        reason: &'static str,
    },
    InvalidMaxDailyQuantity {
        quantity: u16,
        minimum: u16,
        maximum: u16,
    },
    InvalidOrderLineItemQuantity {
        quantity: u16,
        minimum: u16,
        maximum: u16,
    },
    DuplicateSpecialRequest(SpecialRequest),
    TooManySpecialRequests {
        maximum: usize,
    },
    InvalidGovernanceConfiguration(String),
    VendorOverrideOutOfBounds {
        field: &'static str,
        minimum: u16,
        maximum: u16,
        actual: u16,
    },
    UnauthorizedRole {
        expected: Role,
        actual: Role,
    },
    StatePoisoned,
    EmptyOrderLineItems,
    DuplicateMenuItemInOrder {
        menu_item_id: MenuItemId,
    },
    MenuItemNotFound {
        menu_item_id: MenuItemId,
    },
    MenuItemVendorMismatch {
        menu_item_id: MenuItemId,
        expected_vendor_id: VendorId,
        actual_vendor_id: VendorId,
    },
    MenuItemDeliveryDateMismatch {
        menu_item_id: MenuItemId,
        expected_delivery_epoch_day: i32,
        actual_delivery_epoch_day: i32,
    },
    QuotaExceeded {
        menu_item_id: MenuItemId,
        requested_quantity: u16,
        remaining_quantity: u16,
    },
    QuotaReductionBelowAllocated {
        menu_item_id: MenuItemId,
        allocated_quantity: u16,
        attempted_max_daily_quantity: u16,
    },
    PreorderWindowClosed {
        vendor_id: VendorId,
        earliest_delivery_epoch_day: i32,
        latest_delivery_epoch_day: i32,
        requested_delivery_epoch_day: i32,
    },
    ModifyCancelCutoffPassed {
        delivery_epoch_day: i32,
        cutoff_epoch_day: i32,
        cutoff_minute_of_day: u16,
        requested_epoch_day: i32,
        requested_minute_of_day: u16,
    },
    OrderAlreadyExists(OrderId),
    OrderNotFound(OrderId),
}

impl fmt::Display for MenuSupplyWindowError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::InvalidMenuItemId => f.write_str("menu item id must not be empty"),
            Self::InvalidOrderId => f.write_str("order id must not be empty"),
            Self::InvalidCurrencyCode => {
                f.write_str("currency must be a 3-letter uppercase ISO code")
            }
            Self::InvalidMenuImageUrl(message) => write!(f, "invalid menu image URL: {message}"),
            Self::InvalidTextField { field, reason } => write!(f, "invalid {field}: {reason}"),
            Self::InvalidMaxDailyQuantity {
                quantity,
                minimum,
                maximum,
            } => write!(
                f,
                "max daily quantity must be between {minimum} and {maximum}, got {quantity}"
            ),
            Self::InvalidOrderLineItemQuantity {
                quantity,
                minimum,
                maximum,
            } => write!(
                f,
                "order line-item quantity must be between {minimum} and {maximum}, got {quantity}"
            ),
            Self::DuplicateSpecialRequest(special_request) => write!(
                f,
                "duplicate special request {} is not allowed",
                special_request.as_str()
            ),
            Self::TooManySpecialRequests { maximum } => write!(
                f,
                "special requests are limited to {maximum} controlled options per line item"
            ),
            Self::InvalidGovernanceConfiguration(message) => {
                write!(f, "invalid ordering governance configuration: {message}")
            }
            Self::VendorOverrideOutOfBounds {
                field,
                minimum,
                maximum,
                actual,
            } => write!(
                f,
                "vendor override `{field}` must be between {minimum} and {maximum}, got {actual}"
            ),
            Self::UnauthorizedRole { expected, actual } => write!(
                f,
                "operation requires role {expected:?}, but actor has role {actual:?}"
            ),
            Self::StatePoisoned => {
                f.write_str("menu supply state is poisoned due to a previous panic")
            }
            Self::EmptyOrderLineItems => f.write_str("order must include at least one line item"),
            Self::DuplicateMenuItemInOrder { menu_item_id } => {
                write!(f, "order contains duplicate menu item {menu_item_id}")
            }
            Self::MenuItemNotFound { menu_item_id } => {
                write!(f, "menu item {menu_item_id} does not exist")
            }
            Self::MenuItemVendorMismatch {
                menu_item_id,
                expected_vendor_id,
                actual_vendor_id,
            } => write!(
                f,
                "menu item {menu_item_id} belongs to vendor {actual_vendor_id}, expected {expected_vendor_id}"
            ),
            Self::MenuItemDeliveryDateMismatch {
                menu_item_id,
                expected_delivery_epoch_day,
                actual_delivery_epoch_day,
            } => write!(
                f,
                "menu item {menu_item_id} targets delivery day {actual_delivery_epoch_day}, expected {expected_delivery_epoch_day}"
            ),
            Self::QuotaExceeded {
                menu_item_id,
                requested_quantity,
                remaining_quantity,
            } => write!(
                f,
                "menu item {menu_item_id} has only {remaining_quantity} portions remaining, requested {requested_quantity}"
            ),
            Self::QuotaReductionBelowAllocated {
                menu_item_id,
                allocated_quantity,
                attempted_max_daily_quantity,
            } => write!(
                f,
                "menu item {menu_item_id} already has {allocated_quantity} allocated portions, cannot reduce max daily quantity to {attempted_max_daily_quantity}"
            ),
            Self::PreorderWindowClosed {
                vendor_id,
                earliest_delivery_epoch_day,
                latest_delivery_epoch_day,
                requested_delivery_epoch_day,
            } => write!(
                f,
                "vendor {vendor_id} accepts delivery days between {earliest_delivery_epoch_day} and {latest_delivery_epoch_day}, requested {requested_delivery_epoch_day}"
            ),
            Self::ModifyCancelCutoffPassed {
                delivery_epoch_day,
                cutoff_epoch_day,
                cutoff_minute_of_day,
                requested_epoch_day,
                requested_minute_of_day,
            } => write!(
                f,
                "delivery day {delivery_epoch_day} is past modify/cancel cutoff ({cutoff_epoch_day} minute {cutoff_minute_of_day}); current Taipei business moment is day {requested_epoch_day} minute {requested_minute_of_day}"
            ),
            Self::OrderAlreadyExists(order_id) => {
                write!(f, "order {order_id} already exists in quota ledger")
            }
            Self::OrderNotFound(order_id) => {
                write!(f, "order {order_id} does not exist in quota ledger")
            }
        }
    }
}

impl std::error::Error for MenuSupplyWindowError {}
