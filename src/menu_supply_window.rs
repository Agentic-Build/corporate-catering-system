use std::collections::{BTreeMap, BTreeSet};
use std::fmt;
use std::sync::{Arc, Mutex};

use serde::{Deserialize, Serialize};

use crate::audit::{
    AuditAction, AuditCorrelationId, AuditEntityRef, AuditEntityType, AuditEvidenceWrite,
    AuditIdentityLink, AuditTimestamp, AuditTrailError, ImmutableAuditTrail,
};
use crate::identity::{ActorId, AuthenticatedActorContext, PlantId, Role};
use crate::object_storage::ObjectStorageReference;
use crate::vendor_compliance::VendorId;
use crate::vendor_delivery_mapping::TaipeiBusinessMoment;

const MAX_MENU_NAME_LENGTH: usize = 80;
const MAX_MENU_DESCRIPTION_LENGTH: usize = 280;
const MAX_MENU_TYPE_LENGTH: usize = 32;
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
const MINIO_BUCKET_MENU_IMAGES_ENV: &str = "MINIO_BUCKET_MENU_IMAGES";
const DEFAULT_MENU_IMAGE_BUCKET: &str = "menu-assets";
const MENU_IMAGE_OBJECT_KEY_PREFIX: &str = "menu-images";
const MENU_IMAGE_MAX_SIZE_BYTES: u64 = 10 * 1024 * 1024;
const MENU_IMAGE_ALLOWED_EXTENSIONS: [&str; 4] = ["jpg", "jpeg", "png", "webp"];
const DEFAULT_ORDER_RETENTION_DAYS: u16 = 365;
const UPSERT_VENDOR_MENU_ITEM_OPERATION_ID: &str = "upsertVendorMenuItem";
const UPSERT_VENDOR_ORDERING_POLICY_OPERATION_ID: &str = "upsertVendorOrderingPolicy";
const CREATE_EMPLOYEE_ORDER_OPERATION_ID: &str = "createEmployeeOrder";
const UPDATE_EMPLOYEE_ORDER_OPERATION_ID: &str = "updateEmployeeOrder";
const VERIFY_PICKUP_ORDER_OPERATION_ID: &str = "verifyPickupOrder";
const MARK_ORDER_SOLD_OUT_OPERATION_ID: &str = "markOrderSoldOut";
const MARK_ORDER_REFUND_PENDING_OPERATION_ID: &str = "markOrderRefundPending";
const MARK_ORDER_REFUNDED_OPERATION_ID: &str = "markOrderRefunded";
const PURGE_ORDER_DATA_OPERATION_ID: &str = "purgeOrderData";
const ORDER_RETENTION_CORRELATION_PREFIX: &str = "order-retention";

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
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

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
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

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
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

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
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
        let object_ref = ObjectStorageReference::parse(trimmed)
            .map_err(|error| MenuSupplyWindowError::InvalidMenuImageUrl(error.to_string()))?;
        let expected_bucket = configured_menu_image_bucket();
        let (bucket, key) = object_ref.split_parts();
        if bucket != expected_bucket.as_str() {
            return Err(MenuSupplyWindowError::InvalidMenuImageUrl(format!(
                "image object bucket must be `{expected_bucket}`"
            )));
        }
        if !object_key_matches_expected_prefix(key, MENU_IMAGE_OBJECT_KEY_PREFIX) {
            return Err(MenuSupplyWindowError::InvalidMenuImageUrl(
                "image object key must use the managed menu-images prefix".to_owned(),
            ));
        }
        if !object_key_matches_artifact_metadata(
            key,
            MENU_IMAGE_MAX_SIZE_BYTES,
            &MENU_IMAGE_ALLOWED_EXTENSIONS,
        ) {
            return Err(MenuSupplyWindowError::InvalidMenuImageUrl(
                "image object key must include managed size and extension metadata".to_owned(),
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

impl<'de> Deserialize<'de> for MenuImageUrl {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: serde::Deserializer<'de>,
    {
        let value = String::deserialize(deserializer)?;
        Self::parse(value).map_err(serde::de::Error::custom)
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
pub enum MenuHealthTag {
    LowCalorie,
    HighProtein,
    Vegetarian,
    Vegan,
    GlutenFree,
}

impl MenuHealthTag {
    pub fn parse(value: impl AsRef<str>) -> Result<Self, MenuSupplyWindowError> {
        match value.as_ref() {
            "LOW_CALORIE" => Ok(Self::LowCalorie),
            "HIGH_PROTEIN" => Ok(Self::HighProtein),
            "VEGETARIAN" => Ok(Self::Vegetarian),
            "VEGAN" => Ok(Self::Vegan),
            "GLUTEN_FREE" => Ok(Self::GlutenFree),
            _ => Err(MenuSupplyWindowError::InvalidMenuHealthTag),
        }
    }

    pub const fn as_str(self) -> &'static str {
        match self {
            Self::LowCalorie => "LOW_CALORIE",
            Self::HighProtein => "HIGH_PROTEIN",
            Self::Vegetarian => "VEGETARIAN",
            Self::Vegan => "VEGAN",
            Self::GlutenFree => "GLUTEN_FREE",
        }
    }
}

impl fmt::Display for MenuHealthTag {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum VendorMenuItemStatus {
    Listed,
    Paused,
    Delisted,
}

impl VendorMenuItemStatus {
    pub fn parse(value: impl AsRef<str>) -> Result<Self, MenuSupplyWindowError> {
        match value.as_ref() {
            "LISTED" => Ok(Self::Listed),
            "PAUSED" => Ok(Self::Paused),
            "DELISTED" => Ok(Self::Delisted),
            _ => Err(MenuSupplyWindowError::InvalidVendorMenuStatus),
        }
    }

    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Listed => "LISTED",
            Self::Paused => "PAUSED",
            Self::Delisted => "DELISTED",
        }
    }

    pub const fn is_orderable(self) -> bool {
        matches!(self, Self::Listed)
    }
}

impl Default for VendorMenuItemStatus {
    fn default() -> Self {
        Self::Listed
    }
}

impl fmt::Display for VendorMenuItemStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
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
    menu_type: String,
    status: VendorMenuItemStatus,
    health_tags: BTreeSet<MenuHealthTag>,
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
        menu_type: impl Into<String>,
        health_tags: Vec<MenuHealthTag>,
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
        let menu_type = normalize_menu_type(menu_type.into())?;
        if !(1..=MAX_DAILY_QUANTITY).contains(&max_daily_quantity) {
            return Err(MenuSupplyWindowError::InvalidMaxDailyQuantity {
                quantity: max_daily_quantity,
                minimum: 1,
                maximum: MAX_DAILY_QUANTITY,
            });
        }

        let mut deduped_health_tags = BTreeSet::new();
        for health_tag in health_tags {
            if !deduped_health_tags.insert(health_tag) {
                return Err(MenuSupplyWindowError::DuplicateMenuHealthTag(health_tag));
            }
        }

        Ok(Self {
            name,
            description,
            menu_type,
            status: VendorMenuItemStatus::Listed,
            health_tags: deduped_health_tags,
            image_url,
            price,
            max_daily_quantity,
            delivery_epoch_day,
            preorder_open_days_ahead_override: None,
            modify_cancel_cutoff_minute_of_day_override: None,
        })
    }

    pub fn with_status(mut self, status: VendorMenuItemStatus) -> Self {
        self.status = status;
        self
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

    pub fn menu_type(&self) -> &str {
        &self.menu_type
    }

    pub fn status(&self) -> VendorMenuItemStatus {
        self.status
    }

    pub fn health_tags(&self) -> &BTreeSet<MenuHealthTag> {
        &self.health_tags
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

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct VendorMenuItem {
    menu_item_id: MenuItemId,
    vendor_id: VendorId,
    name: String,
    description: String,
    menu_type: String,
    #[serde(default)]
    status: VendorMenuItemStatus,
    health_tags: BTreeSet<MenuHealthTag>,
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
            menu_type: draft.menu_type,
            status: draft.status,
            health_tags: draft.health_tags,
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

    pub fn menu_type(&self) -> &str {
        &self.menu_type
    }

    pub fn status(&self) -> VendorMenuItemStatus {
        self.status
    }

    pub fn health_tags(&self) -> &BTreeSet<MenuHealthTag> {
        &self.health_tags
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

    pub fn with_status(mut self, status: VendorMenuItemStatus) -> Self {
        self.status = status;
        self
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

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct EmployeeMenuDiscoveryEntry {
    menu_item: VendorMenuItem,
    remaining_quantity: u16,
    preorder_open_days_ahead: u16,
    modify_cancel_cutoff_minute_of_day: u16,
    earliest_delivery_epoch_day: i32,
    latest_delivery_epoch_day: i32,
    cutoff_epoch_day: i32,
    preorder_open: bool,
}

impl EmployeeMenuDiscoveryEntry {
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

    pub fn earliest_delivery_epoch_day(&self) -> i32 {
        self.earliest_delivery_epoch_day
    }

    pub fn latest_delivery_epoch_day(&self) -> i32 {
        self.latest_delivery_epoch_day
    }

    pub fn cutoff_epoch_day(&self) -> i32 {
        self.cutoff_epoch_day
    }

    pub fn preorder_open(&self) -> bool {
        self.preorder_open
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
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

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
pub struct VendorOrderingPolicyOverride {
    pub preorder_open_days_ahead: Option<u16>,
    pub modify_cancel_cutoff_minute_of_day: Option<u16>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
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

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub struct OrderRetentionPolicy {
    retention_days: u16,
}

impl OrderRetentionPolicy {
    pub fn new(retention_days: u16) -> Result<Self, MenuSupplyWindowError> {
        if retention_days == 0 {
            return Err(MenuSupplyWindowError::InvalidOrderRetentionPolicy);
        }
        Ok(Self { retention_days })
    }

    pub const fn retention_days(self) -> u16 {
        self.retention_days
    }
}

impl Default for OrderRetentionPolicy {
    fn default() -> Self {
        Self {
            retention_days: DEFAULT_ORDER_RETENTION_DAYS,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct OrderPurgeReport {
    pub purged_orders: usize,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum OrderMutation {
    ReplaceLineItems {
        line_items: Vec<OrderLineItemRequest>,
    },
    Cancel,
    MarkSoldOut,
    MarkRefundPending,
    MarkRefunded,
    MarkFulfilled,
}

impl OrderMutation {
    pub const fn operation_name(&self) -> &'static str {
        match self {
            Self::ReplaceLineItems { .. } => "REPLACE_LINE_ITEMS",
            Self::Cancel => "CANCEL",
            Self::MarkSoldOut => "MARK_SOLD_OUT",
            Self::MarkRefundPending => "MARK_REFUND_PENDING",
            Self::MarkRefunded => "MARK_REFUNDED",
            Self::MarkFulfilled => "MARK_FULFILLED",
        }
    }

    pub const fn is_employee_patch_operation(&self) -> bool {
        matches!(self, Self::ReplaceLineItems { .. } | Self::Cancel)
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum OrderLifecycleState {
    Pending,
    Modified,
    Cancelled,
    SoldOut,
    RefundPending,
    Refunded,
    Fulfilled,
}

impl OrderLifecycleState {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Pending => "PENDING",
            Self::Modified => "MODIFIED",
            Self::Cancelled => "CANCELLED",
            Self::SoldOut => "SOLD_OUT",
            Self::RefundPending => "REFUND_PENDING",
            Self::Refunded => "REFUNDED",
            Self::Fulfilled => "FULFILLED",
        }
    }
}

impl fmt::Display for OrderLifecycleState {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum OrderTimelineEventType {
    Created,
    Modified,
    Cancelled,
    SoldOut,
    RefundPending,
    Refunded,
    Fulfilled,
}

impl OrderTimelineEventType {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Created => "CREATED",
            Self::Modified => "MODIFIED",
            Self::Cancelled => "CANCELLED",
            Self::SoldOut => "SOLD_OUT",
            Self::RefundPending => "REFUND_PENDING",
            Self::Refunded => "REFUNDED",
            Self::Fulfilled => "FULFILLED",
        }
    }
}

impl fmt::Display for OrderTimelineEventType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct OrderTimelineEvent {
    occurred_at: TaipeiBusinessMoment,
    event_type: OrderTimelineEventType,
    state: OrderLifecycleState,
}

impl OrderTimelineEvent {
    fn new(
        occurred_at: TaipeiBusinessMoment,
        event_type: OrderTimelineEventType,
        state: OrderLifecycleState,
    ) -> Self {
        Self {
            occurred_at,
            event_type,
            state,
        }
    }

    pub fn occurred_at(&self) -> TaipeiBusinessMoment {
        self.occurred_at
    }

    pub fn event_type(&self) -> OrderTimelineEventType {
        self.event_type
    }

    pub fn state(&self) -> OrderLifecycleState {
        self.state
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct OrderSnapshot {
    order_id: OrderId,
    employee_actor_id: ActorId,
    vendor_id: VendorId,
    plant_id: PlantId,
    delivery_epoch_day: i32,
    state: OrderLifecycleState,
    line_items: BTreeMap<MenuItemId, u16>,
    special_requests_by_menu_item: BTreeMap<MenuItemId, BTreeSet<SpecialRequest>>,
    timeline: Vec<OrderTimelineEvent>,
    inventory_reserved: bool,
}

impl OrderSnapshot {
    pub fn order_id(&self) -> &OrderId {
        &self.order_id
    }

    pub fn employee_actor_id(&self) -> &ActorId {
        &self.employee_actor_id
    }

    pub fn vendor_id(&self) -> &VendorId {
        &self.vendor_id
    }

    pub fn plant_id(&self) -> &PlantId {
        &self.plant_id
    }

    pub fn delivery_epoch_day(&self) -> i32 {
        self.delivery_epoch_day
    }

    pub fn state(&self) -> OrderLifecycleState {
        self.state
    }

    pub fn line_items(&self) -> &BTreeMap<MenuItemId, u16> {
        &self.line_items
    }

    pub fn special_requests_by_menu_item(&self) -> &BTreeMap<MenuItemId, BTreeSet<SpecialRequest>> {
        &self.special_requests_by_menu_item
    }

    pub fn timeline(&self) -> &[OrderTimelineEvent] {
        &self.timeline
    }

    pub fn inventory_reserved(&self) -> bool {
        self.inventory_reserved
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
struct StoredOrder {
    employee_actor_id: ActorId,
    vendor_id: VendorId,
    plant_id: PlantId,
    delivery_epoch_day: i32,
    state: OrderLifecycleState,
    line_items: BTreeMap<MenuItemId, u16>,
    special_requests_by_menu_item: BTreeMap<MenuItemId, BTreeSet<SpecialRequest>>,
    timeline: Vec<OrderTimelineEvent>,
    inventory_reserved: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct AggregatedOrderLineItems {
    quantities: BTreeMap<MenuItemId, u16>,
    special_requests_by_menu_item: BTreeMap<MenuItemId, BTreeSet<SpecialRequest>>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
struct MenuSupplyState {
    menu_items: BTreeMap<MenuItemId, VendorMenuItem>,
    allocated_quantity_by_menu_item: BTreeMap<MenuItemId, u16>,
    orders: BTreeMap<OrderId, StoredOrder>,
    vendor_ordering_policies: BTreeMap<VendorId, VendorOrderingPolicy>,
}

#[derive(Debug, Clone)]
pub struct MenuSupplyPolicy {
    governance: OrderingGovernancePolicy,
    retention_policy: OrderRetentionPolicy,
    state: Arc<Mutex<MenuSupplyState>>,
    audit_trail: ImmutableAuditTrail,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MenuSupplyPolicySnapshot {
    governance: OrderingGovernancePolicy,
    retention_policy: OrderRetentionPolicy,
    state: MenuSupplyState,
}

impl Default for MenuSupplyPolicy {
    fn default() -> Self {
        Self::new(OrderingGovernancePolicy::default())
    }
}

impl MenuSupplyPolicy {
    pub fn new(governance: OrderingGovernancePolicy) -> Self {
        Self::with_audit_trail_and_retention(
            governance,
            ImmutableAuditTrail::default(),
            OrderRetentionPolicy::default(),
        )
    }

    pub fn with_audit_trail(
        governance: OrderingGovernancePolicy,
        audit_trail: ImmutableAuditTrail,
    ) -> Self {
        Self::with_audit_trail_and_retention(
            governance,
            audit_trail,
            OrderRetentionPolicy::default(),
        )
    }

    pub fn with_audit_trail_and_retention(
        governance: OrderingGovernancePolicy,
        audit_trail: ImmutableAuditTrail,
        retention_policy: OrderRetentionPolicy,
    ) -> Self {
        Self {
            governance,
            retention_policy,
            state: Arc::new(Mutex::new(MenuSupplyState::default())),
            audit_trail,
        }
    }

    pub fn governance(&self) -> OrderingGovernancePolicy {
        self.governance
    }

    pub fn retention_policy(&self) -> OrderRetentionPolicy {
        self.retention_policy
    }

    pub fn audit_trail(&self) -> ImmutableAuditTrail {
        self.audit_trail.clone()
    }

    pub fn snapshot(&self) -> Result<MenuSupplyPolicySnapshot, MenuSupplyWindowError> {
        let state = lock_state(&self.state)?;
        Ok(MenuSupplyPolicySnapshot {
            governance: self.governance,
            retention_policy: self.retention_policy,
            state: state.clone(),
        })
    }

    pub fn from_snapshot(
        snapshot: MenuSupplyPolicySnapshot,
        audit_trail: ImmutableAuditTrail,
    ) -> Result<Self, MenuSupplyWindowError> {
        Self::validate_snapshot_state(&snapshot.state)?;
        Ok(Self {
            governance: snapshot.governance,
            retention_policy: snapshot.retention_policy,
            state: Arc::new(Mutex::new(snapshot.state)),
            audit_trail,
        })
    }

    fn validate_snapshot_state(state: &MenuSupplyState) -> Result<(), MenuSupplyWindowError> {
        for menu_item in state.menu_items.values() {
            if let Some(image_url) = menu_item.image_url() {
                let object_ref =
                    ObjectStorageReference::parse(image_url.as_str()).map_err(|error| {
                        MenuSupplyWindowError::InvalidMenuImageUrl(error.to_string())
                    })?;
                let (_, key) = object_ref.split_parts();
                if !object_key_matches_vendor_owner_scope(
                    key,
                    MENU_IMAGE_OBJECT_KEY_PREFIX,
                    menu_item.vendor_id().as_str(),
                ) {
                    return Err(MenuSupplyWindowError::InvalidMenuImageUrl(
                        "menu image reference is not owned by the vendor scope".to_owned(),
                    ));
                }
            }
        }
        Ok(())
    }

    pub fn upsert_vendor_ordering_policy(
        &self,
        actor: &AuthenticatedActorContext,
        vendor_id: &VendorId,
        policy_override: VendorOrderingPolicyOverride,
    ) -> Result<VendorOrderingPolicy, MenuSupplyWindowError> {
        ensure_role(actor, Role::VendorOperator)?;

        let audit_event = AuditEvidenceWrite::new(
            AuditTimestamp::now_taipei().map_err(MenuSupplyWindowError::AuditTrail)?,
            AuditIdentityLink::from_actor(actor, UPSERT_VENDOR_ORDERING_POLICY_OPERATION_ID),
            AuditAction::UpsertVendorOrderingPolicy,
            AuditEntityRef::new(AuditEntityType::VendorOrderingPolicy, vendor_id.as_str())
                .map_err(MenuSupplyWindowError::AuditTrail)?,
            AuditCorrelationId::for_vendor(vendor_id.as_str())
                .map_err(MenuSupplyWindowError::AuditTrail)?,
        );

        let resolved = self.governance.resolve_vendor_policy(policy_override)?;
        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();
        state
            .vendor_ordering_policies
            .insert(vendor_id.clone(), resolved);
        if let Err(error) = self.audit_trail.append(audit_event) {
            *state = previous_state;
            return Err(MenuSupplyWindowError::AuditTrail(error));
        }
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
        if let Some(image_url) = menu_item.image_url() {
            let object_ref = ObjectStorageReference::parse(image_url.as_str())
                .map_err(|error| MenuSupplyWindowError::InvalidMenuImageUrl(error.to_string()))?;
            let (_, key) = object_ref.split_parts();
            if !object_key_matches_vendor_owner_scope(
                key,
                MENU_IMAGE_OBJECT_KEY_PREFIX,
                menu_item.vendor_id().as_str(),
            ) {
                return Err(MenuSupplyWindowError::InvalidMenuImageUrl(
                    "menu image reference is not owned by the vendor scope".to_owned(),
                ));
            }
        }
        let audit_event = AuditEvidenceWrite::new(
            AuditTimestamp::now_taipei().map_err(MenuSupplyWindowError::AuditTrail)?,
            AuditIdentityLink::from_actor(actor, UPSERT_VENDOR_MENU_ITEM_OPERATION_ID),
            AuditAction::UpsertVendorMenuItem,
            AuditEntityRef::new(AuditEntityType::MenuItem, menu_item.menu_item_id().as_str())
                .map_err(MenuSupplyWindowError::AuditTrail)?,
            AuditCorrelationId::for_vendor(menu_item.vendor_id().as_str())
                .map_err(MenuSupplyWindowError::AuditTrail)?,
        );

        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();
        if let Some(existing_menu_item) = state.menu_items.get(menu_item.menu_item_id()) {
            if existing_menu_item.vendor_id() != menu_item.vendor_id() {
                return Err(MenuSupplyWindowError::MenuItemVendorMismatch {
                    menu_item_id: menu_item.menu_item_id().clone(),
                    expected_vendor_id: menu_item.vendor_id().clone(),
                    actual_vendor_id: existing_menu_item.vendor_id().clone(),
                });
            }
        }
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
        if let Err(error) = self.audit_trail.append(audit_event) {
            *state = previous_state;
            return Err(MenuSupplyWindowError::AuditTrail(error));
        }
        Ok(())
    }

    pub fn menu_item(
        &self,
        menu_item_id: &MenuItemId,
    ) -> Result<Option<VendorMenuItem>, MenuSupplyWindowError> {
        let state = lock_state(&self.state)?;
        Ok(state.menu_items.get(menu_item_id).cloned())
    }

    pub fn vendor_has_menu_image_reference(
        &self,
        vendor_id: &VendorId,
        object_ref: &ObjectStorageReference,
    ) -> Result<bool, MenuSupplyWindowError> {
        let state = lock_state(&self.state)?;
        Ok(state.menu_items.values().any(|menu_item| {
            menu_item.vendor_id() == vendor_id
                && menu_item
                    .image_url()
                    .is_some_and(|image_url| image_url.as_str() == object_ref.as_str())
        }))
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

    pub fn vendor_menu_item_states(
        &self,
        vendor_id: &VendorId,
    ) -> Result<Vec<VendorMenuItemState>, MenuSupplyWindowError> {
        let state = lock_state(&self.state)?;
        let mut states = Vec::new();
        for menu_item in state.menu_items.values() {
            if menu_item.vendor_id() != vendor_id {
                continue;
            }
            let allocated = state
                .allocated_quantity_by_menu_item
                .get(menu_item.menu_item_id())
                .copied()
                .unwrap_or(0);
            let policy = self.effective_vendor_policy_locked(&state, menu_item.vendor_id());
            states.push(VendorMenuItemState {
                menu_item: menu_item.clone(),
                remaining_quantity: menu_item.max_daily_quantity().saturating_sub(allocated),
                preorder_open_days_ahead: policy.preorder_open_days_ahead(),
                modify_cancel_cutoff_minute_of_day: policy.modify_cancel_cutoff_minute_of_day(),
            });
        }
        states.sort_by(|left, right| {
            left.menu_item()
                .delivery_epoch_day()
                .cmp(&right.menu_item().delivery_epoch_day())
                .then_with(|| left.menu_item().name().cmp(right.menu_item().name()))
                .then_with(|| {
                    left.menu_item()
                        .menu_item_id()
                        .cmp(right.menu_item().menu_item_id())
                })
        });
        Ok(states)
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

    pub fn employee_discovery_snapshot(
        &self,
        deliverable_vendor_ids: &BTreeSet<VendorId>,
        at: TaipeiBusinessMoment,
    ) -> Result<Vec<EmployeeMenuDiscoveryEntry>, MenuSupplyWindowError> {
        let state = lock_state(&self.state)?;
        let mut entries = Vec::new();
        for menu_item in state.menu_items.values() {
            if !deliverable_vendor_ids.contains(menu_item.vendor_id()) {
                continue;
            }
            if !menu_item.status().is_orderable() {
                continue;
            }

            let allocated = state
                .allocated_quantity_by_menu_item
                .get(menu_item.menu_item_id())
                .copied()
                .unwrap_or(0);
            let remaining_quantity = menu_item.max_daily_quantity().saturating_sub(allocated);
            let policy = self.effective_vendor_policy_locked(&state, menu_item.vendor_id());
            let earliest_delivery_epoch_day = at.epoch_day();
            let latest_delivery_epoch_day = at
                .epoch_day()
                .saturating_add(i32::from(policy.preorder_open_days_ahead()));
            let cutoff_epoch_day = menu_item.delivery_epoch_day().saturating_sub(1);
            let cutoff = TaipeiBusinessMoment::new(
                cutoff_epoch_day,
                policy.modify_cancel_cutoff_minute_of_day(),
            )
            .map_err(|error| {
                MenuSupplyWindowError::InvalidGovernanceConfiguration(error.to_string())
            })?;
            let preorder_open = menu_item.delivery_epoch_day() >= earliest_delivery_epoch_day
                && menu_item.delivery_epoch_day() <= latest_delivery_epoch_day
                && at < cutoff;

            entries.push(EmployeeMenuDiscoveryEntry {
                menu_item: menu_item.clone(),
                remaining_quantity,
                preorder_open_days_ahead: policy.preorder_open_days_ahead(),
                modify_cancel_cutoff_minute_of_day: policy.modify_cancel_cutoff_minute_of_day(),
                earliest_delivery_epoch_day,
                latest_delivery_epoch_day,
                cutoff_epoch_day,
                preorder_open,
            });
        }

        entries.sort_by(|left, right| {
            left.menu_item()
                .delivery_epoch_day()
                .cmp(&right.menu_item().delivery_epoch_day())
                .then_with(|| {
                    left.menu_item()
                        .vendor_id()
                        .cmp(right.menu_item().vendor_id())
                })
                .then_with(|| left.menu_item().name().cmp(right.menu_item().name()))
                .then_with(|| {
                    left.menu_item()
                        .menu_item_id()
                        .cmp(right.menu_item().menu_item_id())
                })
        });
        Ok(entries)
    }

    pub fn order_snapshot(
        &self,
        order_id: &OrderId,
    ) -> Result<Option<OrderSnapshot>, MenuSupplyWindowError> {
        let state = lock_state(&self.state)?;
        Ok(state
            .orders
            .get(order_id)
            .map(|stored_order| order_snapshot_from_stored(order_id, stored_order)))
    }

    pub fn order_timeline(
        &self,
        order_id: &OrderId,
    ) -> Result<Option<Vec<OrderTimelineEvent>>, MenuSupplyWindowError> {
        Ok(self
            .order_snapshot(order_id)?
            .map(|snapshot| snapshot.timeline))
    }

    pub fn order_snapshots_for_employee(
        &self,
        employee_actor_id: &ActorId,
    ) -> Result<Vec<OrderSnapshot>, MenuSupplyWindowError> {
        let state = lock_state(&self.state)?;
        let mut snapshots = state
            .orders
            .iter()
            .filter_map(|(order_id, stored_order)| {
                if &stored_order.employee_actor_id != employee_actor_id {
                    return None;
                }
                Some(order_snapshot_from_stored(order_id, stored_order))
            })
            .collect::<Vec<_>>();
        snapshots.sort_by(|left, right| left.order_id().cmp(right.order_id()));
        Ok(snapshots)
    }

    pub fn purge_expired_orders(
        &self,
        actor: &AuthenticatedActorContext,
        as_of: AuditTimestamp,
    ) -> Result<OrderPurgeReport, MenuSupplyWindowError> {
        ensure_role(actor, Role::CommitteeAdmin)?;

        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();
        let retention_days = i64::from(self.retention_policy.retention_days());
        let purge_candidates = state
            .orders
            .iter()
            .filter_map(|(order_id, stored_order)| {
                let order_age_days =
                    i64::from(as_of.epoch_day()) - i64::from(stored_order.delivery_epoch_day);
                if order_age_days > retention_days {
                    Some(order_id.clone())
                } else {
                    None
                }
            })
            .collect::<Vec<_>>();

        let mut purged_orders = 0usize;
        for order_id in purge_candidates {
            let Some(stored_order) = state.orders.remove(&order_id) else {
                continue;
            };
            if stored_order.inventory_reserved {
                if let Err(error) = release_order_allocation_locked(&mut state, &stored_order) {
                    *state = previous_state;
                    return Err(error);
                }
            }
            purged_orders = purged_orders.saturating_add(1);
        }

        let entity_id = format!("{ORDER_RETENTION_CORRELATION_PREFIX}:{}", as_of.epoch_day());
        let entity = match AuditEntityRef::new(AuditEntityType::Order, entity_id.clone()) {
            Ok(value) => value,
            Err(error) => {
                *state = previous_state;
                return Err(MenuSupplyWindowError::AuditTrail(error));
            }
        };
        let correlation_id = match AuditCorrelationId::parse(entity_id) {
            Ok(value) => value,
            Err(error) => {
                *state = previous_state;
                return Err(MenuSupplyWindowError::AuditTrail(error));
            }
        };
        let audit_event = match AuditEvidenceWrite::new_with_reason(
            as_of,
            AuditIdentityLink::from_actor(actor, PURGE_ORDER_DATA_OPERATION_ID),
            AuditAction::PurgeOrderData,
            entity,
            format!(
                "purge order data asOfEpochDay={} retentionDays={} purgedOrders={}",
                as_of.epoch_day(),
                self.retention_policy.retention_days(),
                purged_orders
            ),
            correlation_id,
        ) {
            Ok(write) => write,
            Err(error) => {
                *state = previous_state;
                return Err(MenuSupplyWindowError::AuditTrail(error));
            }
        };

        if let Err(error) = self.audit_trail.append(audit_event) {
            *state = previous_state;
            return Err(MenuSupplyWindowError::AuditTrail(error));
        }

        Ok(OrderPurgeReport { purged_orders })
    }

    pub fn order_snapshots_for_vendor_delivery_day(
        &self,
        vendor_id: &VendorId,
        delivery_epoch_day: i32,
    ) -> Result<Vec<OrderSnapshot>, MenuSupplyWindowError> {
        let state = lock_state(&self.state)?;
        let mut snapshots = state
            .orders
            .iter()
            .filter_map(|(order_id, stored_order)| {
                if stored_order.vendor_id != *vendor_id
                    || stored_order.delivery_epoch_day != delivery_epoch_day
                {
                    return None;
                }
                Some(order_snapshot_from_stored(order_id, stored_order))
            })
            .collect::<Vec<_>>();
        snapshots.sort_by(|left, right| left.order_id().cmp(right.order_id()));
        Ok(snapshots)
    }

    pub fn order_snapshots_for_vendor_date_range(
        &self,
        vendor_id: &VendorId,
        from_epoch_day: i32,
        to_epoch_day: i32,
    ) -> Result<Vec<OrderSnapshot>, MenuSupplyWindowError> {
        let state = lock_state(&self.state)?;
        let mut snapshots = state
            .orders
            .iter()
            .filter_map(|(order_id, stored_order)| {
                if stored_order.vendor_id != *vendor_id {
                    return None;
                }
                if stored_order.delivery_epoch_day < from_epoch_day
                    || stored_order.delivery_epoch_day > to_epoch_day
                {
                    return None;
                }
                Some(order_snapshot_from_stored(order_id, stored_order))
            })
            .collect::<Vec<_>>();
        snapshots.sort_by(|left, right| left.order_id().cmp(right.order_id()));
        Ok(snapshots)
    }

    pub fn create_order(
        &self,
        actor: &AuthenticatedActorContext,
        order_id: OrderId,
        vendor_id: &VendorId,
        plant_id: &PlantId,
        delivery_epoch_day: i32,
        line_items: Vec<OrderLineItemRequest>,
        placed_at: TaipeiBusinessMoment,
    ) -> Result<(), MenuSupplyWindowError> {
        ensure_role(actor, Role::Employee)?;
        ensure_target_plant_in_scope(actor, plant_id)?;
        let audit_event = AuditEvidenceWrite::new(
            AuditTimestamp::from_taipei_business_moment(
                placed_at.epoch_day(),
                placed_at.minute_of_day(),
            )
            .map_err(MenuSupplyWindowError::AuditTrail)?,
            AuditIdentityLink::from_actor(actor, CREATE_EMPLOYEE_ORDER_OPERATION_ID),
            AuditAction::CreateEmployeeOrder,
            AuditEntityRef::new(AuditEntityType::Order, order_id.as_str())
                .map_err(MenuSupplyWindowError::AuditTrail)?,
            AuditCorrelationId::for_vendor(vendor_id.as_str())
                .map_err(MenuSupplyWindowError::AuditTrail)?,
        );
        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();
        let aggregated_line_items = self.validate_and_aggregate_line_items_locked(
            &state,
            vendor_id,
            delivery_epoch_day,
            &line_items,
        )?;
        if let Some(existing_order) = state.orders.get(&order_id) {
            if existing_order.vendor_id == *vendor_id
                && existing_order.employee_actor_id == *actor.actor_id()
                && existing_order.plant_id == *plant_id
                && existing_order.delivery_epoch_day == delivery_epoch_day
                && existing_order.line_items == aggregated_line_items.quantities
                && existing_order.special_requests_by_menu_item
                    == aggregated_line_items.special_requests_by_menu_item
                && existing_order.state == OrderLifecycleState::Pending
                && existing_order.inventory_reserved
            {
                self.audit_trail
                    .append(audit_event)
                    .map_err(MenuSupplyWindowError::AuditTrail)?;
                return Ok(());
            }
            return Err(MenuSupplyWindowError::OrderAlreadyExists(order_id));
        }

        self.enforce_create_window_locked(&state, vendor_id, delivery_epoch_day, placed_at)?;
        let current_allocations = BTreeMap::new();
        self.ensure_quota_capacity_for_transition_locked(
            &state,
            &current_allocations,
            &aggregated_line_items.quantities,
        )?;
        self.apply_allocation_transition_locked(
            &mut state,
            &current_allocations,
            &aggregated_line_items.quantities,
        )?;

        state.orders.insert(
            order_id,
            StoredOrder {
                employee_actor_id: actor.actor_id().clone(),
                vendor_id: vendor_id.clone(),
                plant_id: plant_id.clone(),
                delivery_epoch_day,
                state: OrderLifecycleState::Pending,
                line_items: aggregated_line_items.quantities,
                special_requests_by_menu_item: aggregated_line_items.special_requests_by_menu_item,
                timeline: vec![OrderTimelineEvent::new(
                    placed_at,
                    OrderTimelineEventType::Created,
                    OrderLifecycleState::Pending,
                )],
                inventory_reserved: true,
            },
        );
        if let Err(error) = self.audit_trail.append(audit_event) {
            *state = previous_state;
            return Err(MenuSupplyWindowError::AuditTrail(error));
        }

        Ok(())
    }

    pub fn update_order(
        &self,
        actor: &AuthenticatedActorContext,
        order_id: &OrderId,
        mutation: OrderMutation,
        requested_at: TaipeiBusinessMoment,
    ) -> Result<(), MenuSupplyWindowError> {
        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();
        let stored_order = state
            .orders
            .get(order_id)
            .cloned()
            .ok_or_else(|| MenuSupplyWindowError::OrderNotFound(order_id.clone()))?;
        ensure_order_mutation_authorized(actor, order_id, &stored_order, &mutation)?;
        let (operation_id, action) = audit_identity_for_order_mutation(&mutation);
        let audit_event = AuditEvidenceWrite::new(
            AuditTimestamp::from_taipei_business_moment(
                requested_at.epoch_day(),
                requested_at.minute_of_day(),
            )
            .map_err(MenuSupplyWindowError::AuditTrail)?,
            AuditIdentityLink::from_actor(actor, operation_id),
            action,
            AuditEntityRef::new(AuditEntityType::Order, order_id.as_str())
                .map_err(MenuSupplyWindowError::AuditTrail)?,
            AuditCorrelationId::for_vendor(stored_order.vendor_id.as_str())
                .map_err(MenuSupplyWindowError::AuditTrail)?,
        );

        match mutation {
            OrderMutation::Cancel => {
                if stored_order.state == OrderLifecycleState::Cancelled {
                    self.audit_trail
                        .append(audit_event)
                        .map_err(MenuSupplyWindowError::AuditTrail)?;
                    return Ok(());
                }
                if !matches!(
                    stored_order.state,
                    OrderLifecycleState::Pending | OrderLifecycleState::Modified
                ) {
                    return Err(MenuSupplyWindowError::InvalidOrderLifecycleTransition {
                        order_id: order_id.clone(),
                        current_state: stored_order.state,
                        operation: "CANCEL",
                    });
                }

                self.enforce_modify_cancel_cutoff_locked(
                    &state,
                    &stored_order.vendor_id,
                    stored_order.delivery_epoch_day,
                    requested_at,
                )?;
                if !stored_order.inventory_reserved {
                    return Err(MenuSupplyWindowError::InventoryLedgerCorrupted {
                        menu_item_id: None,
                        reason: "cancel transition expected reserved inventory",
                    });
                }

                let mut next_order = stored_order.clone();
                self.ensure_quota_capacity_for_transition_locked(
                    &state,
                    &next_order.line_items,
                    &BTreeMap::new(),
                )?;
                self.apply_allocation_transition_locked(
                    &mut state,
                    &next_order.line_items,
                    &BTreeMap::new(),
                )?;
                next_order.inventory_reserved = false;
                next_order.state = OrderLifecycleState::Cancelled;
                next_order.timeline.push(OrderTimelineEvent::new(
                    requested_at,
                    OrderTimelineEventType::Cancelled,
                    OrderLifecycleState::Cancelled,
                ));
                state.orders.insert(order_id.clone(), next_order);
                if let Err(error) = self.audit_trail.append(audit_event) {
                    *state = previous_state;
                    return Err(MenuSupplyWindowError::AuditTrail(error));
                }
                Ok(())
            }
            OrderMutation::ReplaceLineItems { line_items } => {
                if !matches!(
                    stored_order.state,
                    OrderLifecycleState::Pending | OrderLifecycleState::Modified
                ) {
                    return Err(MenuSupplyWindowError::InvalidOrderLifecycleTransition {
                        order_id: order_id.clone(),
                        current_state: stored_order.state,
                        operation: "REPLACE_LINE_ITEMS",
                    });
                }
                let next_line_items = self.validate_and_aggregate_line_items_locked(
                    &state,
                    &stored_order.vendor_id,
                    stored_order.delivery_epoch_day,
                    &line_items,
                )?;

                if stored_order.line_items == next_line_items.quantities
                    && stored_order.special_requests_by_menu_item
                        == next_line_items.special_requests_by_menu_item
                {
                    self.audit_trail
                        .append(audit_event)
                        .map_err(MenuSupplyWindowError::AuditTrail)?;
                    return Ok(());
                }
                if !stored_order.inventory_reserved {
                    return Err(MenuSupplyWindowError::InventoryLedgerCorrupted {
                        menu_item_id: None,
                        reason: "line-item replacement expected reserved inventory",
                    });
                }

                self.enforce_modify_cancel_cutoff_locked(
                    &state,
                    &stored_order.vendor_id,
                    stored_order.delivery_epoch_day,
                    requested_at,
                )?;
                self.ensure_quota_capacity_for_transition_locked(
                    &state,
                    &stored_order.line_items,
                    &next_line_items.quantities,
                )?;
                self.apply_allocation_transition_locked(
                    &mut state,
                    &stored_order.line_items,
                    &next_line_items.quantities,
                )?;

                let mut next_order = stored_order.clone();
                next_order.line_items = next_line_items.quantities;
                next_order.special_requests_by_menu_item =
                    next_line_items.special_requests_by_menu_item;
                next_order.state = OrderLifecycleState::Modified;
                next_order.timeline.push(OrderTimelineEvent::new(
                    requested_at,
                    OrderTimelineEventType::Modified,
                    OrderLifecycleState::Modified,
                ));
                state.orders.insert(order_id.clone(), next_order);
                if let Err(error) = self.audit_trail.append(audit_event) {
                    *state = previous_state;
                    return Err(MenuSupplyWindowError::AuditTrail(error));
                }
                Ok(())
            }
            OrderMutation::MarkSoldOut => {
                if stored_order.state == OrderLifecycleState::SoldOut {
                    self.audit_trail
                        .append(audit_event)
                        .map_err(MenuSupplyWindowError::AuditTrail)?;
                    return Ok(());
                }
                if !matches!(
                    stored_order.state,
                    OrderLifecycleState::Pending | OrderLifecycleState::Modified
                ) {
                    return Err(MenuSupplyWindowError::InvalidOrderLifecycleTransition {
                        order_id: order_id.clone(),
                        current_state: stored_order.state,
                        operation: "MARK_SOLD_OUT",
                    });
                }

                let mut next_order = stored_order.clone();
                if next_order.inventory_reserved {
                    self.ensure_quota_capacity_for_transition_locked(
                        &state,
                        &next_order.line_items,
                        &BTreeMap::new(),
                    )?;
                    self.apply_allocation_transition_locked(
                        &mut state,
                        &next_order.line_items,
                        &BTreeMap::new(),
                    )?;
                    next_order.inventory_reserved = false;
                }
                next_order.state = OrderLifecycleState::SoldOut;
                next_order.timeline.push(OrderTimelineEvent::new(
                    requested_at,
                    OrderTimelineEventType::SoldOut,
                    OrderLifecycleState::SoldOut,
                ));
                state.orders.insert(order_id.clone(), next_order);
                if let Err(error) = self.audit_trail.append(audit_event) {
                    *state = previous_state;
                    return Err(MenuSupplyWindowError::AuditTrail(error));
                }
                Ok(())
            }
            OrderMutation::MarkRefundPending => {
                if stored_order.state == OrderLifecycleState::RefundPending {
                    self.audit_trail
                        .append(audit_event)
                        .map_err(MenuSupplyWindowError::AuditTrail)?;
                    return Ok(());
                }
                if !matches!(
                    stored_order.state,
                    OrderLifecycleState::Cancelled
                        | OrderLifecycleState::SoldOut
                        | OrderLifecycleState::Fulfilled
                ) {
                    return Err(MenuSupplyWindowError::InvalidOrderLifecycleTransition {
                        order_id: order_id.clone(),
                        current_state: stored_order.state,
                        operation: "MARK_REFUND_PENDING",
                    });
                }

                let mut next_order = stored_order.clone();
                next_order.state = OrderLifecycleState::RefundPending;
                next_order.timeline.push(OrderTimelineEvent::new(
                    requested_at,
                    OrderTimelineEventType::RefundPending,
                    OrderLifecycleState::RefundPending,
                ));
                state.orders.insert(order_id.clone(), next_order);
                if let Err(error) = self.audit_trail.append(audit_event) {
                    *state = previous_state;
                    return Err(MenuSupplyWindowError::AuditTrail(error));
                }
                Ok(())
            }
            OrderMutation::MarkRefunded => {
                if stored_order.state == OrderLifecycleState::Refunded {
                    self.audit_trail
                        .append(audit_event)
                        .map_err(MenuSupplyWindowError::AuditTrail)?;
                    return Ok(());
                }
                if stored_order.state != OrderLifecycleState::RefundPending {
                    return Err(MenuSupplyWindowError::InvalidOrderLifecycleTransition {
                        order_id: order_id.clone(),
                        current_state: stored_order.state,
                        operation: "MARK_REFUNDED",
                    });
                }

                let mut next_order = stored_order.clone();
                next_order.state = OrderLifecycleState::Refunded;
                next_order.timeline.push(OrderTimelineEvent::new(
                    requested_at,
                    OrderTimelineEventType::Refunded,
                    OrderLifecycleState::Refunded,
                ));
                state.orders.insert(order_id.clone(), next_order);
                if let Err(error) = self.audit_trail.append(audit_event) {
                    *state = previous_state;
                    return Err(MenuSupplyWindowError::AuditTrail(error));
                }
                Ok(())
            }
            OrderMutation::MarkFulfilled => {
                if stored_order.state == OrderLifecycleState::Fulfilled {
                    return Err(MenuSupplyWindowError::InvalidOrderLifecycleTransition {
                        order_id: order_id.clone(),
                        current_state: stored_order.state,
                        operation: "MARK_FULFILLED",
                    });
                }
                if !matches!(
                    stored_order.state,
                    OrderLifecycleState::Pending | OrderLifecycleState::Modified
                ) {
                    return Err(MenuSupplyWindowError::InvalidOrderLifecycleTransition {
                        order_id: order_id.clone(),
                        current_state: stored_order.state,
                        operation: "MARK_FULFILLED",
                    });
                }
                if !stored_order.inventory_reserved {
                    return Err(MenuSupplyWindowError::InventoryLedgerCorrupted {
                        menu_item_id: None,
                        reason: "fulfillment transition expected reserved inventory",
                    });
                }

                let mut next_order = stored_order.clone();
                next_order.state = OrderLifecycleState::Fulfilled;
                next_order.timeline.push(OrderTimelineEvent::new(
                    requested_at,
                    OrderTimelineEventType::Fulfilled,
                    OrderLifecycleState::Fulfilled,
                ));
                state.orders.insert(order_id.clone(), next_order);
                if let Err(error) = self.audit_trail.append(audit_event) {
                    *state = previous_state;
                    return Err(MenuSupplyWindowError::AuditTrail(error));
                }
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
    ) -> Result<AggregatedOrderLineItems, MenuSupplyWindowError> {
        if line_items.is_empty() {
            return Err(MenuSupplyWindowError::EmptyOrderLineItems);
        }

        let mut quantities = BTreeMap::<MenuItemId, u16>::new();
        let mut special_requests_by_menu_item =
            BTreeMap::<MenuItemId, BTreeSet<SpecialRequest>>::new();
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
            if !menu_item.status().is_orderable() {
                return Err(MenuSupplyWindowError::MenuItemUnavailableStatus {
                    menu_item_id: menu_item.menu_item_id().clone(),
                    status: menu_item.status(),
                });
            }

            if quantities
                .insert(line_item.menu_item_id().clone(), line_item.quantity())
                .is_some()
            {
                return Err(MenuSupplyWindowError::DuplicateMenuItemInOrder {
                    menu_item_id: line_item.menu_item_id().clone(),
                });
            }
            special_requests_by_menu_item.insert(
                line_item.menu_item_id().clone(),
                line_item.special_requests().clone(),
            );
        }

        Ok(AggregatedOrderLineItems {
            quantities,
            special_requests_by_menu_item,
        })
    }

    fn ensure_quota_capacity_for_transition_locked(
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
                .checked_sub(current_order_quantity)
                .ok_or_else(|| MenuSupplyWindowError::InventoryLedgerCorrupted {
                    menu_item_id: Some(menu_item_id.clone()),
                    reason: "allocated quantity is smaller than currently reserved quantity",
                })?
                .checked_add(next_order_quantity)
                .ok_or_else(|| MenuSupplyWindowError::InventoryLedgerCorrupted {
                    menu_item_id: Some(menu_item_id.clone()),
                    reason: "allocated quantity overflow while projecting transition",
                })?;

            if projected_allocated > menu_item.max_daily_quantity() {
                let remaining_quantity = menu_item.max_daily_quantity().saturating_sub(
                    currently_allocated
                        .checked_sub(current_order_quantity)
                        .ok_or_else(|| MenuSupplyWindowError::InventoryLedgerCorrupted {
                            menu_item_id: Some(menu_item_id.clone()),
                            reason:
                                "allocated quantity is smaller than currently reserved quantity",
                        })?,
                );
                return Err(MenuSupplyWindowError::QuotaExceeded {
                    menu_item_id,
                    requested_quantity: next_order_quantity,
                    remaining_quantity,
                });
            }
        }

        Ok(())
    }

    fn apply_allocation_transition_locked(
        &self,
        state: &mut MenuSupplyState,
        current_line_items: &BTreeMap<MenuItemId, u16>,
        next_line_items: &BTreeMap<MenuItemId, u16>,
    ) -> Result<(), MenuSupplyWindowError> {
        let affected_menu_items = current_line_items
            .keys()
            .chain(next_line_items.keys())
            .cloned()
            .collect::<BTreeSet<_>>();

        for menu_item_id in affected_menu_items {
            let current_quantity = current_line_items.get(&menu_item_id).copied().unwrap_or(0);
            let next_quantity = next_line_items.get(&menu_item_id).copied().unwrap_or(0);
            if current_quantity == next_quantity {
                continue;
            }

            if next_quantity > current_quantity {
                let add = next_quantity - current_quantity;
                let allocated = state
                    .allocated_quantity_by_menu_item
                    .entry(menu_item_id.clone())
                    .or_insert(0);
                *allocated = allocated.checked_add(add).ok_or_else(|| {
                    MenuSupplyWindowError::InventoryLedgerCorrupted {
                        menu_item_id: Some(menu_item_id.clone()),
                        reason: "allocated quantity overflow while reserving inventory",
                    }
                })?;
            } else {
                let remove = current_quantity - next_quantity;
                let allocated = state
                    .allocated_quantity_by_menu_item
                    .get_mut(&menu_item_id)
                    .ok_or_else(|| MenuSupplyWindowError::InventoryLedgerCorrupted {
                        menu_item_id: Some(menu_item_id.clone()),
                        reason: "missing allocated quantity while releasing inventory",
                    })?;
                *allocated = allocated.checked_sub(remove).ok_or_else(|| {
                    MenuSupplyWindowError::InventoryLedgerCorrupted {
                        menu_item_id: Some(menu_item_id.clone()),
                        reason: "allocated quantity underflow while releasing inventory",
                    }
                })?;
                if *allocated == 0 {
                    state.allocated_quantity_by_menu_item.remove(&menu_item_id);
                }
            }
        }

        Ok(())
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

fn normalize_menu_type(value: String) -> Result<String, MenuSupplyWindowError> {
    let normalized = normalize_non_empty_text(value, MAX_MENU_TYPE_LENGTH, "menu type")?;
    if !normalized
        .chars()
        .all(|ch| ch.is_ascii_uppercase() || ch.is_ascii_digit() || ch == '_')
    {
        return Err(MenuSupplyWindowError::InvalidTextField {
            field: "menu type",
            reason: "must be uppercase snake case",
        });
    }
    Ok(normalized)
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

fn ensure_target_plant_in_scope(
    actor: &AuthenticatedActorContext,
    target_plant: &PlantId,
) -> Result<(), MenuSupplyWindowError> {
    if !actor.plant_scope().contains(target_plant) {
        return Err(MenuSupplyWindowError::TargetPlantOutOfScope {
            actor_id: actor.actor_id().clone(),
            target_plant: target_plant.clone(),
        });
    }
    Ok(())
}

fn ensure_order_mutation_authorized(
    actor: &AuthenticatedActorContext,
    order_id: &OrderId,
    stored_order: &StoredOrder,
    mutation: &OrderMutation,
) -> Result<(), MenuSupplyWindowError> {
    let required_role = match mutation {
        OrderMutation::ReplaceLineItems { .. } | OrderMutation::Cancel => Role::Employee,
        OrderMutation::MarkSoldOut => Role::VendorOperator,
        OrderMutation::MarkRefundPending | OrderMutation::MarkRefunded => Role::PayrollOperator,
        OrderMutation::MarkFulfilled => Role::Employee,
    };
    ensure_role(actor, required_role)?;
    ensure_target_plant_in_scope(actor, &stored_order.plant_id)?;
    match mutation {
        OrderMutation::ReplaceLineItems { .. }
        | OrderMutation::Cancel
        | OrderMutation::MarkFulfilled => {
            ensure_employee_order_owner(actor, order_id, stored_order)
        }
        OrderMutation::MarkSoldOut
        | OrderMutation::MarkRefundPending
        | OrderMutation::MarkRefunded => Ok(()),
    }
}

fn ensure_employee_order_owner(
    actor: &AuthenticatedActorContext,
    order_id: &OrderId,
    stored_order: &StoredOrder,
) -> Result<(), MenuSupplyWindowError> {
    if actor.actor_id() != &stored_order.employee_actor_id {
        return Err(MenuSupplyWindowError::OrderMutationActorMismatch {
            actor_id: actor.actor_id().clone(),
            order_id: order_id.clone(),
            owner_actor_id: stored_order.employee_actor_id.clone(),
        });
    }
    Ok(())
}

fn audit_identity_for_order_mutation(mutation: &OrderMutation) -> (&'static str, AuditAction) {
    match mutation {
        OrderMutation::ReplaceLineItems { .. } | OrderMutation::Cancel => (
            UPDATE_EMPLOYEE_ORDER_OPERATION_ID,
            AuditAction::UpdateEmployeeOrder,
        ),
        OrderMutation::MarkSoldOut => (
            MARK_ORDER_SOLD_OUT_OPERATION_ID,
            AuditAction::MarkOrderSoldOut,
        ),
        OrderMutation::MarkRefundPending => (
            MARK_ORDER_REFUND_PENDING_OPERATION_ID,
            AuditAction::MarkOrderRefundPending,
        ),
        OrderMutation::MarkRefunded => (
            MARK_ORDER_REFUNDED_OPERATION_ID,
            AuditAction::MarkOrderRefunded,
        ),
        OrderMutation::MarkFulfilled => (
            VERIFY_PICKUP_ORDER_OPERATION_ID,
            AuditAction::VerifyPickupOrder,
        ),
    }
}

fn lock_state(
    state: &Arc<Mutex<MenuSupplyState>>,
) -> Result<std::sync::MutexGuard<'_, MenuSupplyState>, MenuSupplyWindowError> {
    state
        .lock()
        .map_err(|_| MenuSupplyWindowError::StatePoisoned)
}

fn configured_menu_image_bucket() -> String {
    std::env::var(MINIO_BUCKET_MENU_IMAGES_ENV)
        .ok()
        .map(|bucket| bucket.trim().to_owned())
        .filter(|bucket| !bucket.is_empty())
        .unwrap_or_else(|| DEFAULT_MENU_IMAGE_BUCKET.to_owned())
}

fn object_key_matches_expected_prefix(object_key: &str, artifact_prefix: &str) -> bool {
    let segments = object_key
        .split('/')
        .filter(|segment| !segment.is_empty())
        .collect::<Vec<_>>();
    segments
        .iter()
        .position(|segment| *segment == artifact_prefix)
        .is_some_and(|index| segments.get(index + 1).is_some())
}

fn object_key_matches_vendor_owner_scope(
    object_key: &str,
    artifact_prefix: &str,
    vendor_scope: &str,
) -> bool {
    let owner_scope = normalize_owner_scope_segment(vendor_scope);
    if owner_scope.is_empty() {
        return false;
    }
    let segments = object_key
        .split('/')
        .filter(|segment| !segment.is_empty())
        .collect::<Vec<_>>();
    segments
        .iter()
        .position(|segment| *segment == artifact_prefix)
        .and_then(|index| segments.get(index + 1))
        .is_some_and(|candidate| *candidate == owner_scope)
}

fn object_key_matches_artifact_metadata(
    object_key: &str,
    max_size_bytes: u64,
    allowed_extensions: &[&str],
) -> bool {
    let Some(object_file_name) = object_key.rsplit('/').next() else {
        return false;
    };
    let mut parts = object_file_name.splitn(3, '-');
    let Some(size_bytes_segment) = parts.next() else {
        return false;
    };
    let Some(digest_segment) = parts.next() else {
        return false;
    };
    let Some(file_name_segment) = parts.next() else {
        return false;
    };
    let Ok(size_bytes) = size_bytes_segment.parse::<u64>() else {
        return false;
    };
    if size_bytes == 0 || size_bytes > max_size_bytes {
        return false;
    }
    if digest_segment.is_empty()
        || !digest_segment
            .chars()
            .all(|character| character.is_ascii_hexdigit())
    {
        return false;
    }
    let Some(extension) = file_name_segment.rsplit('.').next() else {
        return false;
    };
    let normalized_extension = extension.to_ascii_lowercase();
    allowed_extensions.contains(&normalized_extension.as_str())
}

fn normalize_owner_scope_segment(value: &str) -> String {
    let mut normalized = String::with_capacity(value.len());
    for character in value.trim().chars() {
        if character.is_ascii_alphanumeric() || matches!(character, '-' | '_' | '.') {
            normalized.push(character.to_ascii_lowercase());
        } else {
            normalized.push('-');
        }
    }
    normalized.trim_matches('-').to_owned()
}

fn release_order_allocation_locked(
    state: &mut MenuSupplyState,
    stored_order: &StoredOrder,
) -> Result<(), MenuSupplyWindowError> {
    let mut remove_menu_item_ids = Vec::new();
    for (menu_item_id, quantity) in &stored_order.line_items {
        let allocated = state
            .allocated_quantity_by_menu_item
            .get_mut(menu_item_id)
            .ok_or_else(|| MenuSupplyWindowError::InventoryLedgerCorrupted {
                menu_item_id: Some(menu_item_id.clone()),
                reason: "missing allocated quantity while purging reserved order",
            })?;
        *allocated = allocated.checked_sub(*quantity).ok_or_else(|| {
            MenuSupplyWindowError::InventoryLedgerCorrupted {
                menu_item_id: Some(menu_item_id.clone()),
                reason: "allocated quantity underflow while purging reserved order",
            }
        })?;
        if *allocated == 0 {
            remove_menu_item_ids.push(menu_item_id.clone());
        }
    }
    for menu_item_id in remove_menu_item_ids {
        state.allocated_quantity_by_menu_item.remove(&menu_item_id);
    }
    Ok(())
}

fn order_snapshot_from_stored(order_id: &OrderId, stored_order: &StoredOrder) -> OrderSnapshot {
    OrderSnapshot {
        order_id: order_id.clone(),
        employee_actor_id: stored_order.employee_actor_id.clone(),
        vendor_id: stored_order.vendor_id.clone(),
        plant_id: stored_order.plant_id.clone(),
        delivery_epoch_day: stored_order.delivery_epoch_day,
        state: stored_order.state,
        line_items: stored_order.line_items.clone(),
        special_requests_by_menu_item: stored_order.special_requests_by_menu_item.clone(),
        timeline: stored_order.timeline.clone(),
        inventory_reserved: stored_order.inventory_reserved,
    }
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
    InvalidVendorMenuStatus,
    InvalidMenuHealthTag,
    DuplicateMenuHealthTag(MenuHealthTag),
    DuplicateSpecialRequest(SpecialRequest),
    TooManySpecialRequests {
        maximum: usize,
    },
    InvalidGovernanceConfiguration(String),
    InvalidOrderRetentionPolicy,
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
    TargetPlantOutOfScope {
        actor_id: crate::identity::ActorId,
        target_plant: PlantId,
    },
    OrderMutationActorMismatch {
        actor_id: crate::identity::ActorId,
        order_id: OrderId,
        owner_actor_id: crate::identity::ActorId,
    },
    AuditTrail(AuditTrailError),
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
    MenuItemUnavailableStatus {
        menu_item_id: MenuItemId,
        status: VendorMenuItemStatus,
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
    InvalidOrderLifecycleTransition {
        order_id: OrderId,
        current_state: OrderLifecycleState,
        operation: &'static str,
    },
    InventoryLedgerCorrupted {
        menu_item_id: Option<MenuItemId>,
        reason: &'static str,
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
            Self::InvalidMenuImageUrl(message) => {
                write!(f, "invalid menu image object reference: {message}")
            }
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
            Self::InvalidVendorMenuStatus => {
                f.write_str("vendor menu status must be one of LISTED, PAUSED, DELISTED")
            }
            Self::InvalidMenuHealthTag => f.write_str(
                "menu health tag must be one of LOW_CALORIE, HIGH_PROTEIN, VEGETARIAN, VEGAN, GLUTEN_FREE",
            ),
            Self::DuplicateMenuHealthTag(health_tag) => write!(
                f,
                "duplicate menu health tag {} is not allowed",
                health_tag.as_str()
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
            Self::InvalidOrderRetentionPolicy => {
                f.write_str("order retention policy requires retention_days > 0")
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
            Self::TargetPlantOutOfScope {
                actor_id,
                target_plant,
            } => write!(
                f,
                "actor {actor_id} is not authorized for plant {}",
                target_plant.as_str()
            ),
            Self::OrderMutationActorMismatch {
                actor_id,
                order_id,
                owner_actor_id,
            } => write!(
                f,
                "actor {actor_id} is not the owner of order {order_id}; expected owner {owner_actor_id}"
            ),
            Self::AuditTrail(error) => write!(f, "audit trail write failed: {error}"),
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
            Self::MenuItemUnavailableStatus {
                menu_item_id,
                status,
            } => write!(
                f,
                "menu item {menu_item_id} is currently in {status} status and cannot be ordered"
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
            Self::InvalidOrderLifecycleTransition {
                order_id,
                current_state,
                operation,
            } => write!(
                f,
                "order {order_id} cannot perform operation {operation} from lifecycle state {current_state}"
            ),
            Self::InventoryLedgerCorrupted {
                menu_item_id,
                reason,
            } => {
                if let Some(menu_item_id) = menu_item_id {
                    write!(
                        f,
                        "inventory ledger for menu item {menu_item_id} is corrupted: {reason}"
                    )
                } else {
                    write!(f, "inventory ledger is corrupted: {reason}")
                }
            }
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
