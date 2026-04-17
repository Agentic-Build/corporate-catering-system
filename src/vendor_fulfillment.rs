use std::collections::{BTreeMap, BTreeSet};
use std::fmt;
use std::sync::{Arc, Mutex};
use std::time::SystemTime;

use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

use crate::audit::{
    AuditAction, AuditCorrelationId, AuditEntityRef, AuditEntityType, AuditEvidenceWrite,
    AuditIdentityLink, AuditTimestamp, AuditTrailError, ImmutableAuditTrail,
};
use crate::identity::{AuthenticatedActorContext, PlantId, Role};
use crate::menu_supply_window::{
    MenuItemId, MenuSupplyPolicy, MenuSupplyWindowError, OrderId, OrderLifecycleState,
    OrderSnapshot, SpecialRequest,
};
use crate::object_storage::{
    ObjectStorageReference, ObjectStorageUploadPipeline, ObjectUploadIntent, StorageArtifactClass,
};
use crate::vendor_compliance::VendorId;
use crate::vendor_delivery_mapping::TaipeiBusinessMoment;

const ADVANCE_DELIVERY_STATUS_OPERATION_ID: &str = "advanceVendorFulfillmentDeliveryStatus";
const CREATE_EXPORT_BATCH_OPERATION_ID: &str = "createVendorFulfillmentExportBatch";
const BASKET_CAPACITY_PORTIONS: u16 = 12;

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
pub enum FulfillmentDeliveryStatus {
    PendingPrep,
    Preparing,
    Packed,
    OutForDelivery,
    Delivered,
    Cancelled,
}

impl FulfillmentDeliveryStatus {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::PendingPrep => "PENDING_PREP",
            Self::Preparing => "PREPARING",
            Self::Packed => "PACKED",
            Self::OutForDelivery => "OUT_FOR_DELIVERY",
            Self::Delivered => "DELIVERED",
            Self::Cancelled => "CANCELLED",
        }
    }
}

impl fmt::Display for FulfillmentDeliveryStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
pub struct FulfillmentBatchId(String);

impl FulfillmentBatchId {
    pub fn parse(value: impl Into<String>) -> Result<Self, VendorFulfillmentError> {
        let value = value.into();
        if value.trim().is_empty() {
            return Err(VendorFulfillmentError::InvalidBatchId);
        }
        Ok(Self(value))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl fmt::Display for FulfillmentBatchId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct FulfillmentOrderLineItem {
    menu_item_id: MenuItemId,
    quantity: u16,
    special_requests: BTreeSet<SpecialRequest>,
}

impl FulfillmentOrderLineItem {
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

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct VendorFulfillmentOrderEntry {
    order_id: OrderId,
    plant_id: PlantId,
    order_state: OrderLifecycleState,
    delivery_status: FulfillmentDeliveryStatus,
    line_items: Vec<FulfillmentOrderLineItem>,
}

impl VendorFulfillmentOrderEntry {
    pub fn order_id(&self) -> &OrderId {
        &self.order_id
    }

    pub fn plant_id(&self) -> &PlantId {
        &self.plant_id
    }

    pub fn order_state(&self) -> OrderLifecycleState {
        self.order_state
    }

    pub fn delivery_status(&self) -> FulfillmentDeliveryStatus {
        self.delivery_status
    }

    pub fn line_items(&self) -> &[FulfillmentOrderLineItem] {
        &self.line_items
    }

    pub fn total_portions(&self) -> u32 {
        self.line_items
            .iter()
            .map(|line_item| u32::from(line_item.quantity()))
            .sum()
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct VendorFulfillmentPlantEntry {
    plant_id: PlantId,
    order_count: u32,
    portion_count: u32,
    delivery_status_counts: BTreeMap<FulfillmentDeliveryStatus, u32>,
    special_request_counts: BTreeMap<SpecialRequest, u32>,
}

impl VendorFulfillmentPlantEntry {
    pub fn plant_id(&self) -> &PlantId {
        &self.plant_id
    }

    pub fn order_count(&self) -> u32 {
        self.order_count
    }

    pub fn portion_count(&self) -> u32 {
        self.portion_count
    }

    pub fn delivery_status_counts(&self) -> &BTreeMap<FulfillmentDeliveryStatus, u32> {
        &self.delivery_status_counts
    }

    pub fn special_request_counts(&self) -> &BTreeMap<SpecialRequest, u32> {
        &self.special_request_counts
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct FulfillmentStatusAuditEntry {
    order_id: OrderId,
    vendor_id: VendorId,
    delivery_epoch_day: i32,
    occurred_at: TaipeiBusinessMoment,
    from_status: FulfillmentDeliveryStatus,
    to_status: FulfillmentDeliveryStatus,
    audit_identity: AuditIdentityLink,
}

impl FulfillmentStatusAuditEntry {
    pub fn order_id(&self) -> &OrderId {
        &self.order_id
    }

    pub fn vendor_id(&self) -> &VendorId {
        &self.vendor_id
    }

    pub fn delivery_epoch_day(&self) -> i32 {
        self.delivery_epoch_day
    }

    pub fn occurred_at(&self) -> TaipeiBusinessMoment {
        self.occurred_at
    }

    pub fn from_status(&self) -> FulfillmentDeliveryStatus {
        self.from_status
    }

    pub fn to_status(&self) -> FulfillmentDeliveryStatus {
        self.to_status
    }

    pub fn audit_identity(&self) -> &AuditIdentityLink {
        &self.audit_identity
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct VendorFulfillmentBoardSnapshot {
    vendor_id: VendorId,
    delivery_epoch_day: i32,
    generated_at: TaipeiBusinessMoment,
    plant_entries: Vec<VendorFulfillmentPlantEntry>,
    order_entries: Vec<VendorFulfillmentOrderEntry>,
    status_transitions: Vec<FulfillmentStatusAuditEntry>,
}

impl VendorFulfillmentBoardSnapshot {
    pub fn vendor_id(&self) -> &VendorId {
        &self.vendor_id
    }

    pub fn delivery_epoch_day(&self) -> i32 {
        self.delivery_epoch_day
    }

    pub fn generated_at(&self) -> TaipeiBusinessMoment {
        self.generated_at
    }

    pub fn plant_entries(&self) -> &[VendorFulfillmentPlantEntry] {
        &self.plant_entries
    }

    pub fn order_entries(&self) -> &[VendorFulfillmentOrderEntry] {
        &self.order_entries
    }

    pub fn status_transitions(&self) -> &[FulfillmentStatusAuditEntry] {
        &self.status_transitions
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct FulfillmentDailySummaryPlantRow {
    plant_id: PlantId,
    order_count: u32,
    portion_count: u32,
}

impl FulfillmentDailySummaryPlantRow {
    pub fn plant_id(&self) -> &PlantId {
        &self.plant_id
    }

    pub fn order_count(&self) -> u32 {
        self.order_count
    }

    pub fn portion_count(&self) -> u32 {
        self.portion_count
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct FulfillmentDailySummaryExport {
    vendor_id: VendorId,
    delivery_epoch_day: i32,
    total_orders: u32,
    total_portions: u32,
    total_special_requests: u32,
    per_plant: Vec<FulfillmentDailySummaryPlantRow>,
}

impl FulfillmentDailySummaryExport {
    pub fn vendor_id(&self) -> &VendorId {
        &self.vendor_id
    }

    pub fn delivery_epoch_day(&self) -> i32 {
        self.delivery_epoch_day
    }

    pub fn total_orders(&self) -> u32 {
        self.total_orders
    }

    pub fn total_portions(&self) -> u32 {
        self.total_portions
    }

    pub fn total_special_requests(&self) -> u32 {
        self.total_special_requests
    }

    pub fn per_plant(&self) -> &[FulfillmentDailySummaryPlantRow] {
        &self.per_plant
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct FulfillmentPlantPartitionOrderRow {
    order_id: OrderId,
    delivery_status: FulfillmentDeliveryStatus,
    portion_count: u32,
    special_requests: BTreeSet<SpecialRequest>,
}

impl FulfillmentPlantPartitionOrderRow {
    pub fn order_id(&self) -> &OrderId {
        &self.order_id
    }

    pub fn delivery_status(&self) -> FulfillmentDeliveryStatus {
        self.delivery_status
    }

    pub fn portion_count(&self) -> u32 {
        self.portion_count
    }

    pub fn special_requests(&self) -> &BTreeSet<SpecialRequest> {
        &self.special_requests
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct FulfillmentPlantPartitionSheetRow {
    plant_id: PlantId,
    total_orders: u32,
    total_portions: u32,
    special_request_counts: BTreeMap<SpecialRequest, u32>,
    orders: Vec<FulfillmentPlantPartitionOrderRow>,
}

impl FulfillmentPlantPartitionSheetRow {
    pub fn plant_id(&self) -> &PlantId {
        &self.plant_id
    }

    pub fn total_orders(&self) -> u32 {
        self.total_orders
    }

    pub fn total_portions(&self) -> u32 {
        self.total_portions
    }

    pub fn special_request_counts(&self) -> &BTreeMap<SpecialRequest, u32> {
        &self.special_request_counts
    }

    pub fn orders(&self) -> &[FulfillmentPlantPartitionOrderRow] {
        &self.orders
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct FulfillmentPlantPartitionSheetExport {
    rows: Vec<FulfillmentPlantPartitionSheetRow>,
}

impl FulfillmentPlantPartitionSheetExport {
    pub fn rows(&self) -> &[FulfillmentPlantPartitionSheetRow] {
        &self.rows
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct FulfillmentLabelEntry {
    order_id: OrderId,
    plant_id: PlantId,
    delivery_status: FulfillmentDeliveryStatus,
    menu_item_id: MenuItemId,
    quantity: u16,
    special_requests: BTreeSet<SpecialRequest>,
}

impl FulfillmentLabelEntry {
    pub fn order_id(&self) -> &OrderId {
        &self.order_id
    }

    pub fn plant_id(&self) -> &PlantId {
        &self.plant_id
    }

    pub fn delivery_status(&self) -> FulfillmentDeliveryStatus {
        self.delivery_status
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

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct FulfillmentLabelSheetExport {
    labels: Vec<FulfillmentLabelEntry>,
}

impl FulfillmentLabelSheetExport {
    pub fn labels(&self) -> &[FulfillmentLabelEntry] {
        &self.labels
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct FulfillmentBasketEntry {
    basket_code: String,
    plant_id: PlantId,
    order_ids: Vec<OrderId>,
    portion_count: u32,
}

impl FulfillmentBasketEntry {
    pub fn basket_code(&self) -> &str {
        &self.basket_code
    }

    pub fn plant_id(&self) -> &PlantId {
        &self.plant_id
    }

    pub fn order_ids(&self) -> &[OrderId] {
        &self.order_ids
    }

    pub fn portion_count(&self) -> u32 {
        self.portion_count
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct FulfillmentBasketListExport {
    basket_capacity_portions: u16,
    baskets: Vec<FulfillmentBasketEntry>,
}

impl FulfillmentBasketListExport {
    pub fn basket_capacity_portions(&self) -> u16 {
        self.basket_capacity_portions
    }

    pub fn baskets(&self) -> &[FulfillmentBasketEntry] {
        &self.baskets
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
pub enum FulfillmentArtifactType {
    DailySummary,
    PlantPartitionSheet,
    Labels,
    BasketList,
}

impl FulfillmentArtifactType {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::DailySummary => "DAILY_SUMMARY",
            Self::PlantPartitionSheet => "PLANT_PARTITION_SHEET",
            Self::Labels => "LABELS",
            Self::BasketList => "BASKET_LIST",
        }
    }

    const fn file_stem(self) -> &'static str {
        match self {
            Self::DailySummary => "daily-summary",
            Self::PlantPartitionSheet => "plant-partition-sheet",
            Self::Labels => "labels",
            Self::BasketList => "basket-list",
        }
    }

    const fn storage_artifact_class(self) -> StorageArtifactClass {
        match self {
            Self::DailySummary => StorageArtifactClass::FulfillmentDailySummary,
            Self::PlantPartitionSheet => StorageArtifactClass::FulfillmentPlantPartitionSheet,
            Self::Labels => StorageArtifactClass::FulfillmentLabels,
            Self::BasketList => StorageArtifactClass::FulfillmentBasketList,
        }
    }
}

impl fmt::Display for FulfillmentArtifactType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct FulfillmentArtifactReference {
    artifact_type: FulfillmentArtifactType,
    object_ref: String,
    mime_type: String,
    size_bytes: u64,
    sha256: String,
}

impl FulfillmentArtifactReference {
    pub fn new(
        artifact_type: FulfillmentArtifactType,
        object_ref: impl Into<String>,
        mime_type: impl Into<String>,
        size_bytes: u64,
        sha256: impl Into<String>,
    ) -> Result<Self, VendorFulfillmentError> {
        let object_ref = object_ref.into();
        ObjectStorageReference::parse(object_ref.as_str()).map_err(|error| {
            VendorFulfillmentError::InvalidArtifactObjectReference {
                artifact_type,
                reason: error.to_string(),
            }
        })?;
        if size_bytes == 0 {
            return Err(VendorFulfillmentError::ArtifactStorageConfiguration(
                "artifact payload size must be greater than zero".to_owned(),
            ));
        }
        let mime_type = mime_type.into().trim().to_owned();
        if mime_type.is_empty() {
            return Err(VendorFulfillmentError::ArtifactStorageConfiguration(
                "artifact mime type must not be empty".to_owned(),
            ));
        }
        let sha256 = sha256.into().trim().to_ascii_lowercase();
        if sha256.len() != 64
            || !sha256
                .chars()
                .all(|character| character.is_ascii_hexdigit())
        {
            return Err(VendorFulfillmentError::ArtifactStorageConfiguration(
                "artifact sha256 must be a 64-character lowercase hex string".to_owned(),
            ));
        }
        Ok(Self {
            artifact_type,
            object_ref,
            mime_type,
            size_bytes,
            sha256,
        })
    }

    pub fn artifact_type(&self) -> FulfillmentArtifactType {
        self.artifact_type
    }

    pub fn object_ref(&self) -> &str {
        &self.object_ref
    }

    pub fn mime_type(&self) -> &str {
        &self.mime_type
    }

    pub fn size_bytes(&self) -> u64 {
        self.size_bytes
    }

    pub fn sha256(&self) -> &str {
        &self.sha256
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct FulfillmentBatchArtifacts {
    artifacts: Vec<FulfillmentArtifactReference>,
}

impl FulfillmentBatchArtifacts {
    pub fn artifacts(&self) -> &[FulfillmentArtifactReference] {
        &self.artifacts
    }

    pub fn artifact(
        &self,
        artifact_type: FulfillmentArtifactType,
    ) -> Option<&FulfillmentArtifactReference> {
        self.artifacts
            .iter()
            .find(|artifact| artifact.artifact_type == artifact_type)
    }
}

pub trait FulfillmentArtifactStore: Send + Sync {
    fn store_json_artifact(
        &self,
        vendor_id: &VendorId,
        batch_id: &FulfillmentBatchId,
        delivery_epoch_day: i32,
        artifact_type: FulfillmentArtifactType,
        payload: &[u8],
    ) -> Result<FulfillmentArtifactReference, VendorFulfillmentError>;
}

pub struct ObjectStorageFulfillmentArtifactStore {
    object_storage_upload_pipeline: Arc<ObjectStorageUploadPipeline>,
    client: reqwest::blocking::Client,
}

impl ObjectStorageFulfillmentArtifactStore {
    pub fn new(
        object_storage_upload_pipeline: Arc<ObjectStorageUploadPipeline>,
    ) -> Result<Self, VendorFulfillmentError> {
        let client = std::thread::spawn(|| reqwest::blocking::Client::builder().build())
            .join()
            .map_err(|_| {
                VendorFulfillmentError::ArtifactStorageConfiguration(
                    "blocking client builder thread panicked".to_owned(),
                )
            })?
            .map_err(|error| {
                VendorFulfillmentError::ArtifactStorageConfiguration(error.to_string())
            })?;
        Ok(Self {
            object_storage_upload_pipeline,
            client,
        })
    }
}

impl FulfillmentArtifactStore for ObjectStorageFulfillmentArtifactStore {
    fn store_json_artifact(
        &self,
        vendor_id: &VendorId,
        batch_id: &FulfillmentBatchId,
        delivery_epoch_day: i32,
        artifact_type: FulfillmentArtifactType,
        payload: &[u8],
    ) -> Result<FulfillmentArtifactReference, VendorFulfillmentError> {
        let artifact_class = artifact_type.storage_artifact_class();
        let upload_plan = self
            .object_storage_upload_pipeline
            .create_upload_plan(
                ObjectUploadIntent {
                    artifact_class,
                    owner_scope: Some(vendor_id.as_str().to_owned()),
                    file_name: format!(
                        "{}-{}-{}.json",
                        batch_id.as_str(),
                        delivery_epoch_day,
                        artifact_type.file_stem()
                    ),
                    mime_type: "application/json".to_owned(),
                    size_bytes: u64::try_from(payload.len())
                        .expect("artifact payload length should fit"),
                    thumbnail_size_bytes: None,
                },
                SystemTime::now(),
            )
            .map_err(|error| VendorFulfillmentError::ArtifactStorageWrite {
                artifact_type,
                reason: error.to_string(),
            })?;

        let mut request = self.client.put(upload_plan.primary.upload_url.as_str());
        for (name, value) in &upload_plan.primary.required_headers {
            request = request.header(name.as_str(), value.as_str());
        }
        let response = request.body(payload.to_vec()).send().map_err(|error| {
            VendorFulfillmentError::ArtifactStorageWrite {
                artifact_type,
                reason: format!("failed to upload artifact payload: {error}"),
            }
        })?;
        if !response.status().is_success() {
            return Err(VendorFulfillmentError::ArtifactStorageWrite {
                artifact_type,
                reason: format!(
                    "object storage rejected artifact upload with status {}",
                    response.status()
                ),
            });
        }

        FulfillmentArtifactReference::new(
            artifact_type,
            upload_plan.primary.object_ref.as_str().to_owned(),
            upload_plan.metadata.mime_type,
            u64::try_from(payload.len()).expect("artifact payload length should fit"),
            sha256_hex(payload),
        )
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct VendorFulfillmentBatchSnapshot {
    batch_id: FulfillmentBatchId,
    vendor_id: VendorId,
    delivery_epoch_day: i32,
    captured_at: TaipeiBusinessMoment,
    board: VendorFulfillmentBoardSnapshot,
    artifacts: FulfillmentBatchArtifacts,
    audit_identity: AuditIdentityLink,
}

impl VendorFulfillmentBatchSnapshot {
    pub fn batch_id(&self) -> &FulfillmentBatchId {
        &self.batch_id
    }

    pub fn vendor_id(&self) -> &VendorId {
        &self.vendor_id
    }

    pub fn delivery_epoch_day(&self) -> i32 {
        self.delivery_epoch_day
    }

    pub fn captured_at(&self) -> TaipeiBusinessMoment {
        self.captured_at
    }

    pub fn board(&self) -> &VendorFulfillmentBoardSnapshot {
        &self.board
    }

    pub fn artifacts(&self) -> &FulfillmentBatchArtifacts {
        &self.artifacts
    }

    pub fn audit_identity(&self) -> &AuditIdentityLink {
        &self.audit_identity
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct DeliveryStatusRecord {
    status: FulfillmentDeliveryStatus,
    vendor_id: VendorId,
    delivery_epoch_day: i32,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
struct VendorFulfillmentState {
    order_delivery_statuses: BTreeMap<OrderId, DeliveryStatusRecord>,
    status_audit_log: Vec<FulfillmentStatusAuditEntry>,
    batches: BTreeMap<FulfillmentBatchId, VendorFulfillmentBatchSnapshot>,
    next_batch_sequence: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct VendorFulfillmentPolicySnapshot {
    state: VendorFulfillmentState,
}

#[derive(Clone)]
pub struct VendorFulfillmentPolicy {
    state: Arc<Mutex<VendorFulfillmentState>>,
    audit_trail: ImmutableAuditTrail,
    artifact_store: Arc<dyn FulfillmentArtifactStore>,
}

impl VendorFulfillmentPolicy {
    pub fn new(artifact_store: Arc<dyn FulfillmentArtifactStore>) -> Self {
        Self::with_audit_trail(ImmutableAuditTrail::default(), artifact_store)
    }

    pub fn with_audit_trail(
        audit_trail: ImmutableAuditTrail,
        artifact_store: Arc<dyn FulfillmentArtifactStore>,
    ) -> Self {
        Self {
            state: Arc::new(Mutex::new(VendorFulfillmentState::default())),
            audit_trail,
            artifact_store,
        }
    }

    pub fn from_snapshot(
        snapshot: VendorFulfillmentPolicySnapshot,
        audit_trail: ImmutableAuditTrail,
        artifact_store: Arc<dyn FulfillmentArtifactStore>,
    ) -> Result<Self, VendorFulfillmentError> {
        let mut state = snapshot.state;
        validate_snapshot_state(&mut state)?;
        Ok(Self {
            state: Arc::new(Mutex::new(state)),
            audit_trail,
            artifact_store,
        })
    }

    pub fn snapshot(&self) -> Result<VendorFulfillmentPolicySnapshot, VendorFulfillmentError> {
        let state = lock_state(&self.state)?;
        Ok(VendorFulfillmentPolicySnapshot {
            state: state.clone(),
        })
    }

    pub fn audit_trail(&self) -> ImmutableAuditTrail {
        self.audit_trail.clone()
    }

    pub fn vendor_operations_board(
        &self,
        menu_supply_policy: &MenuSupplyPolicy,
        vendor_id: &VendorId,
        delivery_epoch_day: i32,
        generated_at: TaipeiBusinessMoment,
    ) -> Result<VendorFulfillmentBoardSnapshot, VendorFulfillmentError> {
        let orders = menu_supply_policy
            .order_snapshots_for_vendor_delivery_day(vendor_id, delivery_epoch_day)?;
        let state = lock_state(&self.state)?;
        build_board_snapshot(
            vendor_id,
            delivery_epoch_day,
            generated_at,
            &orders,
            &state.order_delivery_statuses,
            &state.status_audit_log,
        )
    }

    pub fn transition_delivery_status(
        &self,
        actor: &AuthenticatedActorContext,
        menu_supply_policy: &MenuSupplyPolicy,
        expected_vendor_id: &VendorId,
        order_id: &OrderId,
        to_status: FulfillmentDeliveryStatus,
        occurred_at: TaipeiBusinessMoment,
    ) -> Result<FulfillmentStatusAuditEntry, VendorFulfillmentError> {
        ensure_role(actor, Role::VendorOperator)?;

        let order_snapshot = menu_supply_policy
            .order_snapshot(order_id)?
            .ok_or_else(|| VendorFulfillmentError::OrderNotFound(order_id.clone()))?;
        if order_snapshot.vendor_id() != expected_vendor_id {
            return Err(VendorFulfillmentError::OrderVendorScopeMismatch {
                order_id: order_id.clone(),
                expected_vendor_id: expected_vendor_id.clone(),
                actual_vendor_id: order_snapshot.vendor_id().clone(),
            });
        }
        if !actor.plant_scope().contains(order_snapshot.plant_id()) {
            return Err(VendorFulfillmentError::TargetPlantOutOfScope {
                actor_id: actor.actor_id().clone(),
                target_plant: order_snapshot.plant_id().clone(),
            });
        }

        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();
        let from_status = match state.order_delivery_statuses.get(order_id) {
            Some(record) => {
                if record.vendor_id != *expected_vendor_id {
                    return Err(VendorFulfillmentError::OrderVendorScopeMismatch {
                        order_id: order_id.clone(),
                        expected_vendor_id: expected_vendor_id.clone(),
                        actual_vendor_id: record.vendor_id.clone(),
                    });
                }
                record.status
            }
            None => default_delivery_status(order_snapshot.state()),
        };
        if from_status == to_status {
            return Err(VendorFulfillmentError::DeliveryStatusUnchanged {
                order_id: order_id.clone(),
                status: to_status,
            });
        }
        if !delivery_transition_allowed(from_status, to_status) {
            return Err(VendorFulfillmentError::InvalidDeliveryStatusTransition {
                order_id: order_id.clone(),
                from_status,
                to_status,
            });
        }

        state.order_delivery_statuses.insert(
            order_id.clone(),
            DeliveryStatusRecord {
                status: to_status,
                vendor_id: order_snapshot.vendor_id().clone(),
                delivery_epoch_day: order_snapshot.delivery_epoch_day(),
            },
        );

        let audit_entry = FulfillmentStatusAuditEntry {
            order_id: order_id.clone(),
            vendor_id: order_snapshot.vendor_id().clone(),
            delivery_epoch_day: order_snapshot.delivery_epoch_day(),
            occurred_at,
            from_status,
            to_status,
            audit_identity: AuditIdentityLink::from_actor(
                actor,
                ADVANCE_DELIVERY_STATUS_OPERATION_ID,
            ),
        };
        state.status_audit_log.push(audit_entry.clone());
        self.audit_trail
            .append(AuditEvidenceWrite::new(
                AuditTimestamp::from_taipei_business_moment(
                    occurred_at.epoch_day(),
                    occurred_at.minute_of_day(),
                )
                .map_err(VendorFulfillmentError::AuditTrail)?,
                AuditIdentityLink::from_actor(actor, ADVANCE_DELIVERY_STATUS_OPERATION_ID),
                AuditAction::AdvanceVendorFulfillmentDeliveryStatus,
                AuditEntityRef::new(AuditEntityType::Order, order_id.as_str())
                    .map_err(VendorFulfillmentError::AuditTrail)?,
                AuditCorrelationId::for_vendor(order_snapshot.vendor_id().as_str())
                    .map_err(VendorFulfillmentError::AuditTrail)?,
            ))
            .map_err(|error| {
                *state = previous_state;
                VendorFulfillmentError::AuditTrail(error)
            })?;
        Ok(audit_entry)
    }

    pub fn create_export_batch(
        &self,
        actor: &AuthenticatedActorContext,
        menu_supply_policy: &MenuSupplyPolicy,
        vendor_id: &VendorId,
        delivery_epoch_day: i32,
        captured_at: TaipeiBusinessMoment,
    ) -> Result<VendorFulfillmentBatchSnapshot, VendorFulfillmentError> {
        ensure_role(actor, Role::VendorOperator)?;

        let board = self.vendor_operations_board(
            menu_supply_policy,
            vendor_id,
            delivery_epoch_day,
            captured_at,
        )?;
        for plant_entry in board.plant_entries() {
            if !actor.plant_scope().contains(plant_entry.plant_id()) {
                return Err(VendorFulfillmentError::TargetPlantOutOfScope {
                    actor_id: actor.actor_id().clone(),
                    target_plant: plant_entry.plant_id().clone(),
                });
            }
        }
        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();
        state.next_batch_sequence = state
            .next_batch_sequence
            .checked_add(1)
            .ok_or(VendorFulfillmentError::BatchSequenceOverflow)?;
        let batch_id = FulfillmentBatchId::parse(format!(
            "fbatch-{delivery_epoch_day}-{:06}",
            state.next_batch_sequence
        ))?;
        let artifacts = build_batch_artifacts(
            vendor_id,
            &batch_id,
            delivery_epoch_day,
            board.order_entries(),
            self.artifact_store.as_ref(),
        )?;

        let snapshot = VendorFulfillmentBatchSnapshot {
            batch_id: batch_id.clone(),
            vendor_id: vendor_id.clone(),
            delivery_epoch_day,
            captured_at,
            board,
            artifacts,
            audit_identity: AuditIdentityLink::from_actor(actor, CREATE_EXPORT_BATCH_OPERATION_ID),
        };
        state.batches.insert(batch_id, snapshot.clone());
        self.audit_trail
            .append(AuditEvidenceWrite::new(
                AuditTimestamp::from_taipei_business_moment(
                    captured_at.epoch_day(),
                    captured_at.minute_of_day(),
                )
                .map_err(VendorFulfillmentError::AuditTrail)?,
                AuditIdentityLink::from_actor(actor, CREATE_EXPORT_BATCH_OPERATION_ID),
                AuditAction::CreateVendorFulfillmentExportBatch,
                AuditEntityRef::new(
                    AuditEntityType::FulfillmentBatch,
                    snapshot.batch_id().as_str(),
                )
                .map_err(VendorFulfillmentError::AuditTrail)?,
                AuditCorrelationId::for_vendor(vendor_id.as_str())
                    .map_err(VendorFulfillmentError::AuditTrail)?,
            ))
            .map_err(|error| {
                *state = previous_state;
                VendorFulfillmentError::AuditTrail(error)
            })?;
        Ok(snapshot)
    }

    pub fn batch_snapshot(
        &self,
        batch_id: &FulfillmentBatchId,
    ) -> Result<VendorFulfillmentBatchSnapshot, VendorFulfillmentError> {
        let state = lock_state(&self.state)?;
        state
            .batches
            .get(batch_id)
            .cloned()
            .ok_or_else(|| VendorFulfillmentError::BatchNotFound(batch_id.clone()))
    }

    pub fn status_audit_log(
        &self,
    ) -> Result<Vec<FulfillmentStatusAuditEntry>, VendorFulfillmentError> {
        let state = lock_state(&self.state)?;
        Ok(state.status_audit_log.clone())
    }
}

#[derive(Debug, Clone, Default)]
struct PlantAggregate {
    order_count: u32,
    portion_count: u32,
    delivery_status_counts: BTreeMap<FulfillmentDeliveryStatus, u32>,
    special_request_counts: BTreeMap<SpecialRequest, u32>,
}

fn build_board_snapshot(
    vendor_id: &VendorId,
    delivery_epoch_day: i32,
    generated_at: TaipeiBusinessMoment,
    orders: &[OrderSnapshot],
    status_overrides: &BTreeMap<OrderId, DeliveryStatusRecord>,
    status_audit_log: &[FulfillmentStatusAuditEntry],
) -> Result<VendorFulfillmentBoardSnapshot, VendorFulfillmentError> {
    let mut order_entries = Vec::with_capacity(orders.len());
    let mut plant_aggregates = BTreeMap::<PlantId, PlantAggregate>::new();
    let mut order_ids = BTreeSet::new();

    for order in orders {
        order_ids.insert(order.order_id().clone());
        let delivery_status = status_overrides
            .get(order.order_id())
            .filter(|record| {
                record.vendor_id == *vendor_id && record.delivery_epoch_day == delivery_epoch_day
            })
            .map(|record| record.status)
            .unwrap_or_else(|| default_delivery_status(order.state()));

        let line_items = build_order_line_items(order)?;
        let order_entry = VendorFulfillmentOrderEntry {
            order_id: order.order_id().clone(),
            plant_id: order.plant_id().clone(),
            order_state: order.state(),
            delivery_status,
            line_items,
        };

        let plant_aggregate = plant_aggregates
            .entry(order_entry.plant_id().clone())
            .or_default();
        plant_aggregate.order_count = plant_aggregate.order_count.saturating_add(1);
        plant_aggregate.portion_count = plant_aggregate
            .portion_count
            .saturating_add(order_entry.total_portions());
        *plant_aggregate
            .delivery_status_counts
            .entry(delivery_status)
            .or_insert(0) += 1;

        for line_item in order_entry.line_items() {
            let quantity = u32::from(line_item.quantity());
            for special_request in line_item.special_requests() {
                *plant_aggregate
                    .special_request_counts
                    .entry(*special_request)
                    .or_insert(0) += quantity;
            }
        }

        order_entries.push(order_entry);
    }

    let mut plant_entries = Vec::with_capacity(plant_aggregates.len());
    for (plant_id, aggregate) in plant_aggregates {
        plant_entries.push(VendorFulfillmentPlantEntry {
            plant_id,
            order_count: aggregate.order_count,
            portion_count: aggregate.portion_count,
            delivery_status_counts: aggregate.delivery_status_counts,
            special_request_counts: aggregate.special_request_counts,
        });
    }

    let mut status_transitions = status_audit_log
        .iter()
        .filter(|entry| {
            entry.vendor_id() == vendor_id
                && entry.delivery_epoch_day() == delivery_epoch_day
                && order_ids.contains(entry.order_id())
        })
        .cloned()
        .collect::<Vec<_>>();
    status_transitions.sort_by(|left, right| {
        left.occurred_at()
            .cmp(&right.occurred_at())
            .then_with(|| left.order_id().cmp(right.order_id()))
    });

    Ok(VendorFulfillmentBoardSnapshot {
        vendor_id: vendor_id.clone(),
        delivery_epoch_day,
        generated_at,
        plant_entries,
        order_entries,
        status_transitions,
    })
}

fn build_order_line_items(
    order: &OrderSnapshot,
) -> Result<Vec<FulfillmentOrderLineItem>, VendorFulfillmentError> {
    let mut line_items = Vec::with_capacity(order.line_items().len());
    for (menu_item_id, quantity) in order.line_items() {
        let special_requests = order
            .special_requests_by_menu_item()
            .get(menu_item_id)
            .cloned()
            .ok_or_else(|| VendorFulfillmentError::MissingSpecialRequestSnapshot {
                order_id: order.order_id().clone(),
                menu_item_id: menu_item_id.clone(),
            })?;
        line_items.push(FulfillmentOrderLineItem {
            menu_item_id: menu_item_id.clone(),
            quantity: *quantity,
            special_requests,
        });
    }
    Ok(line_items)
}

fn build_batch_artifacts(
    vendor_id: &VendorId,
    batch_id: &FulfillmentBatchId,
    delivery_epoch_day: i32,
    order_entries: &[VendorFulfillmentOrderEntry],
    artifact_store: &dyn FulfillmentArtifactStore,
) -> Result<FulfillmentBatchArtifacts, VendorFulfillmentError> {
    let mut per_plant_summary = BTreeMap::<PlantId, FulfillmentDailySummaryPlantRow>::new();
    let mut plant_partition = BTreeMap::<PlantId, FulfillmentPlantPartitionSheetRow>::new();
    let mut labels = Vec::new();
    let mut total_orders = 0_u32;
    let mut total_portions = 0_u32;
    let mut total_special_requests = 0_u32;

    for order_entry in order_entries {
        total_orders = total_orders.saturating_add(1);
        total_portions = total_portions.saturating_add(order_entry.total_portions());

        let summary_row = per_plant_summary
            .entry(order_entry.plant_id().clone())
            .or_insert(FulfillmentDailySummaryPlantRow {
                plant_id: order_entry.plant_id().clone(),
                order_count: 0,
                portion_count: 0,
            });
        summary_row.order_count = summary_row.order_count.saturating_add(1);
        summary_row.portion_count = summary_row
            .portion_count
            .saturating_add(order_entry.total_portions());

        let partition_row = plant_partition
            .entry(order_entry.plant_id().clone())
            .or_insert(FulfillmentPlantPartitionSheetRow {
                plant_id: order_entry.plant_id().clone(),
                total_orders: 0,
                total_portions: 0,
                special_request_counts: BTreeMap::new(),
                orders: Vec::new(),
            });
        partition_row.total_orders = partition_row.total_orders.saturating_add(1);
        partition_row.total_portions = partition_row
            .total_portions
            .saturating_add(order_entry.total_portions());

        let mut order_special_requests = BTreeSet::new();
        for line_item in order_entry.line_items() {
            labels.push(FulfillmentLabelEntry {
                order_id: order_entry.order_id().clone(),
                plant_id: order_entry.plant_id().clone(),
                delivery_status: order_entry.delivery_status(),
                menu_item_id: line_item.menu_item_id().clone(),
                quantity: line_item.quantity(),
                special_requests: line_item.special_requests().clone(),
            });

            let quantity = u32::from(line_item.quantity());
            for special_request in line_item.special_requests() {
                order_special_requests.insert(*special_request);
                *partition_row
                    .special_request_counts
                    .entry(*special_request)
                    .or_insert(0) += quantity;
                total_special_requests = total_special_requests.saturating_add(quantity);
            }
        }
        partition_row
            .orders
            .push(FulfillmentPlantPartitionOrderRow {
                order_id: order_entry.order_id().clone(),
                delivery_status: order_entry.delivery_status(),
                portion_count: order_entry.total_portions(),
                special_requests: order_special_requests,
            });
    }

    labels.sort_by(|left, right| {
        left.plant_id()
            .cmp(right.plant_id())
            .then_with(|| left.order_id().cmp(right.order_id()))
            .then_with(|| left.menu_item_id().cmp(right.menu_item_id()))
    });

    let daily_summary = FulfillmentDailySummaryExport {
        vendor_id: vendor_id.clone(),
        delivery_epoch_day,
        total_orders,
        total_portions,
        total_special_requests,
        per_plant: per_plant_summary.into_values().collect::<Vec<_>>(),
    };
    let plant_partition_sheet = FulfillmentPlantPartitionSheetExport {
        rows: plant_partition.into_values().collect::<Vec<_>>(),
    };
    let label_sheet = FulfillmentLabelSheetExport { labels };
    let basket_list = build_basket_list(order_entries);
    let artifacts = vec![
        build_artifact_reference(
            vendor_id,
            batch_id,
            delivery_epoch_day,
            FulfillmentArtifactType::DailySummary,
            &daily_summary,
            artifact_store,
        )?,
        build_artifact_reference(
            vendor_id,
            batch_id,
            delivery_epoch_day,
            FulfillmentArtifactType::PlantPartitionSheet,
            &plant_partition_sheet,
            artifact_store,
        )?,
        build_artifact_reference(
            vendor_id,
            batch_id,
            delivery_epoch_day,
            FulfillmentArtifactType::Labels,
            &label_sheet,
            artifact_store,
        )?,
        build_artifact_reference(
            vendor_id,
            batch_id,
            delivery_epoch_day,
            FulfillmentArtifactType::BasketList,
            &basket_list,
            artifact_store,
        )?,
    ];
    Ok(FulfillmentBatchArtifacts { artifacts })
}

fn build_artifact_reference<T: Serialize>(
    vendor_id: &VendorId,
    batch_id: &FulfillmentBatchId,
    delivery_epoch_day: i32,
    artifact_type: FulfillmentArtifactType,
    payload: &T,
    artifact_store: &dyn FulfillmentArtifactStore,
) -> Result<FulfillmentArtifactReference, VendorFulfillmentError> {
    let serialized = serde_json::to_vec(payload).map_err(|error| {
        VendorFulfillmentError::ArtifactSerialization {
            artifact_type,
            reason: error.to_string(),
        }
    })?;
    artifact_store.store_json_artifact(
        vendor_id,
        batch_id,
        delivery_epoch_day,
        artifact_type,
        serialized.as_slice(),
    )
}

fn sha256_hex(payload: &[u8]) -> String {
    let mut digest = Sha256::new();
    digest.update(payload);
    let digest = digest.finalize();
    let mut output = String::with_capacity(digest.len() * 2);
    for byte in digest {
        output.push_str(format!("{byte:02x}").as_str());
    }
    output
}

fn build_basket_list(order_entries: &[VendorFulfillmentOrderEntry]) -> FulfillmentBasketListExport {
    let mut per_plant_orders = BTreeMap::<PlantId, Vec<&VendorFulfillmentOrderEntry>>::new();
    for order_entry in order_entries {
        per_plant_orders
            .entry(order_entry.plant_id().clone())
            .or_default()
            .push(order_entry);
    }

    let mut baskets = Vec::new();
    for (plant_id, mut plant_orders) in per_plant_orders {
        plant_orders.sort_by(|left, right| left.order_id().cmp(right.order_id()));

        let mut basket_index = 1_u32;
        let mut current_order_ids = Vec::new();
        let mut current_portions = 0_u32;

        for order in plant_orders {
            let order_portions = order.total_portions();
            let next_portions = current_portions.saturating_add(order_portions);
            if !current_order_ids.is_empty() && next_portions > u32::from(BASKET_CAPACITY_PORTIONS)
            {
                baskets.push(FulfillmentBasketEntry {
                    basket_code: format!("{}-{:02}", plant_id.as_str(), basket_index),
                    plant_id: plant_id.clone(),
                    order_ids: current_order_ids,
                    portion_count: current_portions,
                });
                basket_index = basket_index.saturating_add(1);
                current_order_ids = Vec::new();
                current_portions = 0;
            }

            current_order_ids.push(order.order_id().clone());
            current_portions = current_portions.saturating_add(order_portions);
        }

        if !current_order_ids.is_empty() {
            baskets.push(FulfillmentBasketEntry {
                basket_code: format!("{}-{:02}", plant_id.as_str(), basket_index),
                plant_id,
                order_ids: current_order_ids,
                portion_count: current_portions,
            });
        }
    }

    FulfillmentBasketListExport {
        basket_capacity_portions: BASKET_CAPACITY_PORTIONS,
        baskets,
    }
}

fn validate_snapshot_state(
    state: &mut VendorFulfillmentState,
) -> Result<(), VendorFulfillmentError> {
    let mut max_sequence = 0_u64;
    let required_artifact_types = [
        FulfillmentArtifactType::DailySummary,
        FulfillmentArtifactType::PlantPartitionSheet,
        FulfillmentArtifactType::Labels,
        FulfillmentArtifactType::BasketList,
    ];

    for (batch_key, batch) in &state.batches {
        if batch.batch_id() != batch_key {
            return Err(VendorFulfillmentError::SnapshotInvariantViolation(
                "batch key does not match snapshot batch_id".to_owned(),
            ));
        }

        let mut artifact_types = BTreeSet::new();
        for artifact in batch.artifacts().artifacts() {
            if !artifact_types.insert(artifact.artifact_type()) {
                return Err(VendorFulfillmentError::SnapshotInvariantViolation(format!(
                    "batch {} contains duplicate artifact type {}",
                    batch.batch_id().as_str(),
                    artifact.artifact_type()
                )));
            }

            let _validated = FulfillmentArtifactReference::new(
                artifact.artifact_type(),
                artifact.object_ref().to_owned(),
                artifact.mime_type().to_owned(),
                artifact.size_bytes(),
                artifact.sha256().to_owned(),
            )?;
        }

        for artifact_type in required_artifact_types {
            if !artifact_types.contains(&artifact_type) {
                return Err(VendorFulfillmentError::SnapshotInvariantViolation(format!(
                    "batch {} is missing artifact type {artifact_type}",
                    batch.batch_id().as_str()
                )));
            }
        }

        let sequence = parse_batch_sequence(batch.batch_id())?;
        max_sequence = max_sequence.max(sequence);
    }

    if state.next_batch_sequence < max_sequence {
        state.next_batch_sequence = max_sequence;
    }

    Ok(())
}

fn parse_batch_sequence(batch_id: &FulfillmentBatchId) -> Result<u64, VendorFulfillmentError> {
    let Some((_, raw_sequence)) = batch_id.as_str().rsplit_once('-') else {
        return Err(VendorFulfillmentError::SnapshotInvariantViolation(format!(
            "batch id {} is missing numeric sequence suffix",
            batch_id.as_str()
        )));
    };
    raw_sequence.parse::<u64>().map_err(|error| {
        VendorFulfillmentError::SnapshotInvariantViolation(format!(
            "batch id {} has invalid sequence suffix: {error}",
            batch_id.as_str()
        ))
    })
}

fn default_delivery_status(order_state: OrderLifecycleState) -> FulfillmentDeliveryStatus {
    match order_state {
        OrderLifecycleState::Pending | OrderLifecycleState::Modified => {
            FulfillmentDeliveryStatus::PendingPrep
        }
        OrderLifecycleState::Fulfilled => FulfillmentDeliveryStatus::Delivered,
        OrderLifecycleState::Cancelled
        | OrderLifecycleState::SoldOut
        | OrderLifecycleState::RefundPending
        | OrderLifecycleState::Refunded => FulfillmentDeliveryStatus::Cancelled,
    }
}

fn delivery_transition_allowed(
    from_status: FulfillmentDeliveryStatus,
    to_status: FulfillmentDeliveryStatus,
) -> bool {
    matches!(
        (from_status, to_status),
        (
            FulfillmentDeliveryStatus::PendingPrep,
            FulfillmentDeliveryStatus::Preparing
        ) | (
            FulfillmentDeliveryStatus::PendingPrep,
            FulfillmentDeliveryStatus::Packed
        ) | (
            FulfillmentDeliveryStatus::PendingPrep,
            FulfillmentDeliveryStatus::Cancelled
        ) | (
            FulfillmentDeliveryStatus::Preparing,
            FulfillmentDeliveryStatus::Packed
        ) | (
            FulfillmentDeliveryStatus::Preparing,
            FulfillmentDeliveryStatus::Cancelled
        ) | (
            FulfillmentDeliveryStatus::Packed,
            FulfillmentDeliveryStatus::OutForDelivery
        ) | (
            FulfillmentDeliveryStatus::Packed,
            FulfillmentDeliveryStatus::Cancelled
        ) | (
            FulfillmentDeliveryStatus::OutForDelivery,
            FulfillmentDeliveryStatus::Delivered
        ) | (
            FulfillmentDeliveryStatus::OutForDelivery,
            FulfillmentDeliveryStatus::Cancelled
        )
    )
}

fn ensure_role(
    actor: &AuthenticatedActorContext,
    role: Role,
) -> Result<(), VendorFulfillmentError> {
    if actor.role() != role {
        return Err(VendorFulfillmentError::UnauthorizedRole {
            expected: role,
            actual: actor.role(),
        });
    }
    Ok(())
}

fn lock_state(
    state: &Arc<Mutex<VendorFulfillmentState>>,
) -> Result<std::sync::MutexGuard<'_, VendorFulfillmentState>, VendorFulfillmentError> {
    state
        .lock()
        .map_err(|_| VendorFulfillmentError::StatePoisoned)
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum VendorFulfillmentError {
    InvalidBatchId,
    BatchNotFound(FulfillmentBatchId),
    BatchSequenceOverflow,
    UnauthorizedRole {
        expected: Role,
        actual: Role,
    },
    TargetPlantOutOfScope {
        actor_id: crate::identity::ActorId,
        target_plant: PlantId,
    },
    OrderNotFound(OrderId),
    OrderVendorScopeMismatch {
        order_id: OrderId,
        expected_vendor_id: VendorId,
        actual_vendor_id: VendorId,
    },
    MissingSpecialRequestSnapshot {
        order_id: OrderId,
        menu_item_id: MenuItemId,
    },
    InvalidDeliveryStatusTransition {
        order_id: OrderId,
        from_status: FulfillmentDeliveryStatus,
        to_status: FulfillmentDeliveryStatus,
    },
    DeliveryStatusUnchanged {
        order_id: OrderId,
        status: FulfillmentDeliveryStatus,
    },
    ArtifactSerialization {
        artifact_type: FulfillmentArtifactType,
        reason: String,
    },
    ArtifactStorageConfiguration(String),
    ArtifactStorageWrite {
        artifact_type: FulfillmentArtifactType,
        reason: String,
    },
    InvalidArtifactObjectReference {
        artifact_type: FulfillmentArtifactType,
        reason: String,
    },
    SnapshotInvariantViolation(String),
    AuditTrail(AuditTrailError),
    MenuSupply(MenuSupplyWindowError),
    StatePoisoned,
}

impl From<MenuSupplyWindowError> for VendorFulfillmentError {
    fn from(value: MenuSupplyWindowError) -> Self {
        Self::MenuSupply(value)
    }
}

impl fmt::Display for VendorFulfillmentError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::InvalidBatchId => f.write_str("fulfillment batch id must not be empty"),
            Self::BatchNotFound(batch_id) => write!(
                f,
                "fulfillment batch snapshot `{}` does not exist",
                batch_id.as_str()
            ),
            Self::BatchSequenceOverflow => {
                f.write_str("fulfillment batch sequence overflowed while allocating id")
            }
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
            Self::OrderNotFound(order_id) => {
                write!(f, "order {order_id} does not exist in menu supply state")
            }
            Self::OrderVendorScopeMismatch {
                order_id,
                expected_vendor_id,
                actual_vendor_id,
            } => write!(
                f,
                "order {order_id} belongs to vendor {actual_vendor_id}, expected {expected_vendor_id}"
            ),
            Self::MissingSpecialRequestSnapshot {
                order_id,
                menu_item_id,
            } => write!(
                f,
                "order {order_id} is missing controlled special-request snapshot for menu item {menu_item_id}"
            ),
            Self::InvalidDeliveryStatusTransition {
                order_id,
                from_status,
                to_status,
            } => write!(
                f,
                "order {order_id} cannot transition delivery status from {from_status} to {to_status}"
            ),
            Self::DeliveryStatusUnchanged { order_id, status } => {
                write!(f, "order {order_id} is already in delivery status {status}")
            }
            Self::ArtifactSerialization {
                artifact_type,
                reason,
            } => write!(
                f,
                "failed to serialize export artifact {artifact_type}: {reason}"
            ),
            Self::ArtifactStorageConfiguration(reason) => {
                write!(f, "artifact storage configuration is invalid: {reason}")
            }
            Self::ArtifactStorageWrite {
                artifact_type,
                reason,
            } => write!(
                f,
                "failed to persist export artifact {artifact_type} to object storage: {reason}"
            ),
            Self::InvalidArtifactObjectReference {
                artifact_type,
                reason,
            } => write!(
                f,
                "generated object reference for export artifact {artifact_type} is invalid: {reason}"
            ),
            Self::SnapshotInvariantViolation(reason) => {
                write!(f, "persisted vendor fulfillment snapshot invariant violated: {reason}")
            }
            Self::AuditTrail(error) => write!(f, "audit trail write failed: {error}"),
            Self::MenuSupply(error) => write!(f, "menu supply read failed: {error}"),
            Self::StatePoisoned => {
                f.write_str("vendor fulfillment state is poisoned due to a previous panic")
            }
        }
    }
}

impl std::error::Error for VendorFulfillmentError {}
