use std::cmp::Ordering as CmpOrdering;
use std::collections::BTreeMap;
use std::net::SocketAddr;
use std::sync::atomic::{AtomicU64, Ordering as AtomicOrdering};
use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};

use axum::extract::{Path, Query, State};
use axum::http::StatusCode;
use axum::routing::{get, patch, post};
use axum::{Json, Router};
use corporate_catering_system::health::{evaluate_probe, HealthProbeKind, HealthState};
use corporate_catering_system::identity::{
    ActorId, AuthenticatedActorContext, AuthenticationSource, PlantId, PlantScope, Role,
};
use corporate_catering_system::menu_supply_window::{
    EmployeeMenuDiscoveryEntry, MenuHealthTag, MenuImageUrl, MenuItemId, MenuSupplyPolicy, Money,
    OrderId, OrderLineItemRequest, OrderMutation, SpecialRequest, VendorMenuItem,
    VendorMenuItemDraft,
};
use corporate_catering_system::observability::{
    initialize_telemetry_runtime_from_env, TelemetryService,
};
use corporate_catering_system::transport::http::{
    HttpEmployeeDiscoveryExecutionGateway, HttpOrderExecutionError, HttpOrderingExecutionGateway,
    HttpVendorMenuExecutionGateway,
};
use corporate_catering_system::vendor_compliance::{
    ComplianceDate, ComplianceDocumentTemplate, DocumentTemplateId, HistoryRetentionPolicy,
    VendorCategory, VendorComplianceLifecycle, VendorDocumentSubmission, VendorId,
    VendorReviewDecision,
};
use corporate_catering_system::vendor_delivery_mapping::{
    DeliveryMappingId, DeliveryRuleEffect, ServiceWindow, TaipeiBusinessMoment,
    VendorPlantDeliveryMapping, VendorPlantDeliveryPolicy,
};
use serde::{Deserialize, Serialize};

const DEFAULT_VENDOR_ID: &str = "ven-load-gate-a";
const DEFAULT_PLANT_ID: &str = "fab-a";
const DEFAULT_MENU_VARIANT_COUNT: u16 = 64;
const DEFAULT_DELIVERY_DAY_OFFSET: i32 = 2;

#[derive(Debug, Clone)]
struct AppState {
    next_order_sequence: Arc<AtomicU64>,
    vendor_id: VendorId,
    plant_id: PlantId,
    compliance_lifecycle: Arc<VendorComplianceLifecycle>,
    delivery_policy: Arc<VendorPlantDeliveryPolicy>,
    menu_supply_policy: MenuSupplyPolicy,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct CreateOrderRequest {
    vendor_id: String,
    delivery_epoch_day: i32,
    line_items: Vec<OrderLineItemRequestPayload>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct OrderLineItemRequestPayload {
    menu_item_id: String,
    quantity: u16,
    #[serde(default)]
    special_requests: Vec<SpecialRequestOption>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
enum SpecialRequestOption {
    LessRice,
    NoGreenOnion,
    SauceOnSide,
    NoUtensils,
    ExtraSpicy,
}

impl SpecialRequestOption {
    const fn into_domain(self) -> SpecialRequest {
        match self {
            Self::LessRice => SpecialRequest::LessRice,
            Self::NoGreenOnion => SpecialRequest::NoGreenOnion,
            Self::SauceOnSide => SpecialRequest::SauceOnSide,
            Self::NoUtensils => SpecialRequest::NoUtensils,
            Self::ExtraSpicy => SpecialRequest::ExtraSpicy,
        }
    }
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct CreateOrderResponse {
    order_id: String,
    accepted: bool,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct UpdateOrderRequest {
    operation: String,
    line_items: Option<Vec<OrderLineItemRequestPayload>>,
    cancel_reason: Option<String>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct UpdateOrderResponse {
    order_id: String,
    accepted: bool,
    operation: String,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct PickupVerificationRequest {
    verification_code: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct PickupVerificationResponse {
    order_id: String,
    verified: bool,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct MenuPricePayload {
    currency: String,
    amount_minor: u32,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct MenuDiscoveryItem {
    menu_item_id: String,
    vendor_id: String,
    name: String,
    description: String,
    image_url: Option<String>,
    menu_type: String,
    health_tags: Vec<String>,
    price: MenuPricePayload,
    remaining_quantity: u16,
    preorder_open: bool,
    preorder_open_days_ahead: u16,
    modify_cancel_cutoff_minute_of_day: u16,
    delivery_date: String,
    earliest_delivery_date: String,
    latest_delivery_date: String,
    cutoff_date: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct MenuDiscoveryDay {
    delivery_date: String,
    items: Vec<MenuDiscoveryItem>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct MenuDiscoveryPageMeta {
    page: usize,
    page_size: usize,
    total_items: usize,
    total_pages: usize,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct MenuDiscoveryResponse {
    timezone: &'static str,
    view: &'static str,
    recommendation_requested: bool,
    recommendation_applied: bool,
    from_date: String,
    to_date: String,
    days: Vec<MenuDiscoveryDay>,
    items: Vec<MenuDiscoveryItem>,
    page: MenuDiscoveryPageMeta,
}

#[derive(Debug, Deserialize, Clone, Copy)]
#[serde(rename_all = "lowercase")]
enum MenuDiscoveryViewQuery {
    Calendar,
    Week,
}

impl MenuDiscoveryViewQuery {
    const fn as_str(self) -> &'static str {
        match self {
            Self::Calendar => "calendar",
            Self::Week => "week",
        }
    }
}

#[derive(Debug, Deserialize, Clone, Copy)]
#[serde(rename_all = "camelCase")]
enum MenuSortFieldQuery {
    Name,
    PriceMinor,
    RemainingQuantity,
    DeliveryDate,
}

#[derive(Debug, Deserialize, Clone, Copy)]
#[serde(rename_all = "lowercase")]
enum SortOrderQuery {
    Asc,
    Desc,
}

#[derive(Debug, Deserialize, Default)]
#[serde(rename_all = "camelCase")]
struct EmployeeMenuDiscoveryQuery {
    plant_id: Option<String>,
    menu_date: Option<String>,
    from_date: Option<String>,
    to_date: Option<String>,
    view: Option<MenuDiscoveryViewQuery>,
    page: Option<usize>,
    page_size: Option<usize>,
    sort_by: Option<MenuSortFieldQuery>,
    sort_order: Option<SortOrderQuery>,
    search: Option<String>,
    menu_type: Option<String>,
    health_tag: Option<String>,
    price_min_minor: Option<u32>,
    price_max_minor: Option<u32>,
    remaining_quantity: Option<u16>,
    recommendation_enabled: Option<bool>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct HealthPayload {
    status: &'static str,
    probe: &'static str,
    detail: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct ErrorPayload {
    code: &'static str,
    message: String,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error + Send + Sync + 'static>> {
    initialize_telemetry_runtime_from_env(TelemetryService::HttpApi.service_name())?;

    let bind_addr =
        std::env::var("PRELAUNCH_BIND_ADDR").unwrap_or_else(|_| "127.0.0.1:18080".to_owned());
    let socket_addr: SocketAddr = bind_addr.parse()?;

    let vendor_id = VendorId::parse(
        std::env::var("PRELAUNCH_VENDOR_ID").unwrap_or_else(|_| DEFAULT_VENDOR_ID.to_owned()),
    )
    .map_err(|error| format!("PRELAUNCH_VENDOR_ID is invalid: {error}"))?;

    let plant_id = PlantId::parse(
        std::env::var("PRELAUNCH_PLANT_ID").unwrap_or_else(|_| DEFAULT_PLANT_ID.to_owned()),
    )
    .map_err(|error| format!("PRELAUNCH_PLANT_ID is invalid: {error}"))?;

    let menu_variant_count =
        parse_positive_u16_env("PRELAUNCH_MENU_VARIANT_COUNT", DEFAULT_MENU_VARIANT_COUNT)?;

    let delivery_epoch_day = resolve_delivery_epoch_day()?;

    let state =
        bootstrap_runtime_state(vendor_id, plant_id, delivery_epoch_day, menu_variant_count)
            .map_err(|error| format!("failed to bootstrap runtime state: {error}"))?;

    let app = Router::new()
        .route("/health/ready", get(ready_probe))
        .route("/health/live", get(live_probe))
        .route("/health/startup", get(startup_probe))
        .route("/api/v1/employee/menus", get(list_employee_menus))
        .route("/api/v1/employee/orders", post(create_employee_order))
        .route(
            "/api/v1/employee/orders/:orderId",
            patch(update_employee_order),
        )
        .route(
            "/api/v1/employee/orders/:orderId/pickup-verifications",
            post(verify_order_pickup),
        )
        .with_state(state);

    let listener = tokio::net::TcpListener::bind(socket_addr).await?;
    tracing::info!(bind_addr = %socket_addr, "observability runtime service listening");
    axum::serve(listener, app).await?;
    Ok(())
}

fn parse_positive_u16_env(key: &str, default_value: u16) -> Result<u16, String> {
    let raw = match std::env::var(key) {
        Ok(value) => value,
        Err(_) => return Ok(default_value),
    };
    let parsed = raw
        .parse::<u16>()
        .map_err(|error| format!("{key} must be a positive integer: {error}"))?;
    if parsed == 0 {
        return Err(format!("{key} must be greater than zero"));
    }
    Ok(parsed)
}

fn resolve_delivery_epoch_day() -> Result<i32, String> {
    if let Ok(raw) = std::env::var("PRELAUNCH_DELIVERY_EPOCH_DAY") {
        let parsed = raw
            .parse::<i32>()
            .map_err(|error| format!("PRELAUNCH_DELIVERY_EPOCH_DAY must be integer: {error}"))?;
        if parsed <= 0 {
            return Err("PRELAUNCH_DELIVERY_EPOCH_DAY must be greater than zero".to_owned());
        }
        return Ok(parsed);
    }

    let now = current_taipei_business_moment()?;
    Ok(now.epoch_day().saturating_add(DEFAULT_DELIVERY_DAY_OFFSET))
}

fn current_taipei_business_moment() -> Result<TaipeiBusinessMoment, String> {
    let unix_seconds = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|error| format!("failed to read system clock: {error}"))?
        .as_secs();
    let unix_seconds_i64 = i64::try_from(unix_seconds)
        .map_err(|_| "system clock overflowed i64 seconds".to_owned())?;
    TaipeiBusinessMoment::from_utc_unix_seconds(unix_seconds_i64).map_err(|error| {
        format!("failed to convert system time to Taipei business moment: {error}")
    })
}

fn seeded_menu_type(index: u16) -> &'static str {
    const MENU_TYPES: [&str; 7] = [
        "BENTO", "NOODLE", "SALAD", "BOWL", "SOUP", "SNACK", "BEVERAGE",
    ];
    MENU_TYPES[usize::from((index - 1) % (MENU_TYPES.len() as u16))]
}

fn seeded_menu_health_tags(index: u16) -> Vec<MenuHealthTag> {
    match index % 5 {
        0 => vec![MenuHealthTag::LowCalorie, MenuHealthTag::HighProtein],
        1 => vec![MenuHealthTag::HighProtein],
        2 => vec![MenuHealthTag::Vegetarian],
        3 => vec![MenuHealthTag::Vegan],
        _ => vec![MenuHealthTag::GlutenFree],
    }
}

fn bootstrap_runtime_state(
    vendor_id: VendorId,
    plant_id: PlantId,
    delivery_epoch_day: i32,
    menu_variant_count: u16,
) -> Result<AppState, String> {
    let committee_actor = AuthenticatedActorContext::new(
        ActorId::parse("committee-load-gate").map_err(|error| error.to_string())?,
        Role::CommitteeAdmin,
        PlantScope::all(),
        AuthenticationSource::CorporateSso,
    )
    .map_err(|error| error.to_string())?;

    let vendor_actor = AuthenticatedActorContext::new(
        ActorId::parse("vendor-load-gate").map_err(|error| error.to_string())?,
        Role::VendorOperator,
        PlantScope::restricted(vec![plant_id.clone()]).map_err(|error| error.to_string())?,
        AuthenticationSource::VendorAccountMfa,
    )
    .map_err(|error| error.to_string())?;

    let mut compliance_lifecycle =
        VendorComplianceLifecycle::new(HistoryRetentionPolicy::default());
    let vendor_category = VendorCategory::parse("RESTAURANT").map_err(|error| error.to_string())?;
    let template_id =
        DocumentTemplateId::parse("tmpl-load-gate-license").map_err(|error| error.to_string())?;

    compliance_lifecycle
        .upsert_document_template(
            &committee_actor,
            ComplianceDocumentTemplate::new(
                template_id.clone(),
                vendor_category.clone(),
                "Business License",
                true,
                365,
                vec![30, 7],
                0,
            )
            .map_err(|error| error.to_string())?,
        )
        .map_err(|error| error.to_string())?;

    let submitted_on = ComplianceDate::from_epoch_day(delivery_epoch_day.saturating_sub(30));
    let approved_on = ComplianceDate::from_epoch_day(delivery_epoch_day.saturating_sub(29));

    compliance_lifecycle
        .register_vendor_application(
            &vendor_actor,
            vendor_id.clone(),
            "Load Gate Vendor",
            vendor_category,
            submitted_on,
        )
        .map_err(|error| error.to_string())?;

    compliance_lifecycle
        .submit_document(
            &vendor_actor,
            &vendor_id,
            &template_id,
            VendorDocumentSubmission::new(
                "s3://evidence/docs/load-gate-license.pdf",
                submitted_on,
                ComplianceDate::from_epoch_day(delivery_epoch_day.saturating_add(300)),
            )
            .map_err(|error| error.to_string())?,
        )
        .map_err(|error| error.to_string())?;

    compliance_lifecycle
        .review_application(
            &committee_actor,
            &vendor_id,
            VendorReviewDecision::Approved,
            "Prelaunch load-gate vendor is approved.",
            approved_on,
        )
        .map_err(|error| error.to_string())?;

    let mut delivery_policy = VendorPlantDeliveryPolicy::new();
    let mapping_window_start = TaipeiBusinessMoment::new(delivery_epoch_day.saturating_sub(30), 0)
        .map_err(|error| format!("failed to create delivery window start: {error}"))?;
    let mapping_window_end =
        TaipeiBusinessMoment::new(delivery_epoch_day.saturating_add(30), 23 * 60 + 59)
            .map_err(|error| format!("failed to create delivery window end: {error}"))?;

    delivery_policy
        .upsert_mapping(
            &committee_actor,
            TaipeiBusinessMoment::new(delivery_epoch_day.saturating_sub(30), 1)
                .map_err(|error| error.to_string())?,
            VendorPlantDeliveryMapping::new(
                DeliveryMappingId::parse("map-load-gate-allow")
                    .map_err(|error| error.to_string())?,
                vendor_id.clone(),
                plant_id.clone(),
                ServiceWindow::new(mapping_window_start, mapping_window_end)
                    .map_err(|error| error.to_string())?,
                DeliveryRuleEffect::Allow,
                100,
            ),
        )
        .map_err(|error| error.to_string())?;

    let menu_supply_policy = MenuSupplyPolicy::default();
    let vendor_menu_gateway = HttpVendorMenuExecutionGateway::new(&menu_supply_policy);

    for index in 1..=menu_variant_count {
        let menu_item_id =
            MenuItemId::parse(format!("menu-{index}")).map_err(|error| error.to_string())?;
        let delivery_epoch_day = delivery_epoch_day.saturating_add(i32::from((index - 1) % 7));
        let image_url = MenuImageUrl::parse(format!(
            "https://cdn.example.com/menu/load-gate-{index}.jpg"
        ))
        .map_err(|error| error.to_string())?;
        let menu_item = VendorMenuItem::new(
            menu_item_id.clone(),
            vendor_id.clone(),
            VendorMenuItemDraft::new(
                format!("Load Gate Meal {index}"),
                "Seeded menu item for hard-SLO prelaunch verification",
                seeded_menu_type(index).to_owned(),
                seeded_menu_health_tags(index),
                Some(image_url),
                Money::new("TWD", 12000).map_err(|error| error.to_string())?,
                2000,
                delivery_epoch_day,
            )
            .map_err(|error| error.to_string())?,
        );

        vendor_menu_gateway
            .execute_upsert_vendor_menu_item(&vendor_actor, menu_item)
            .map_err(|error| error.to_string())?;
    }

    Ok(AppState {
        next_order_sequence: Arc::new(AtomicU64::new(1)),
        vendor_id,
        plant_id,
        compliance_lifecycle: Arc::new(compliance_lifecycle),
        delivery_policy: Arc::new(delivery_policy),
        menu_supply_policy,
    })
}

async fn ready_probe() -> (StatusCode, Json<HealthPayload>) {
    health_probe_response(HealthProbeKind::Readiness, true, "dependencies ready")
}

async fn live_probe() -> (StatusCode, Json<HealthPayload>) {
    health_probe_response(HealthProbeKind::Liveness, true, "heartbeat healthy")
}

async fn startup_probe() -> (StatusCode, Json<HealthPayload>) {
    health_probe_response(HealthProbeKind::Startup, true, "startup complete")
}

fn health_probe_response(
    probe_kind: HealthProbeKind,
    dependencies_ready: bool,
    detail: &str,
) -> (StatusCode, Json<HealthPayload>) {
    let operation_id = match probe_kind {
        HealthProbeKind::Readiness => "healthReadyProbe",
        HealthProbeKind::Liveness => "healthLiveProbe",
        HealthProbeKind::Startup => "healthStartupProbe",
    };
    let telemetry = TelemetryService::HttpApi.begin_operation(operation_id, None, None);

    let report = evaluate_probe(probe_kind, dependencies_ready, detail);
    let (status_code, status_text) = match report.state() {
        HealthState::Healthy => (StatusCode::OK, "ok"),
        HealthState::Unhealthy => (StatusCode::SERVICE_UNAVAILABLE, "degraded"),
    };
    telemetry.finish_with_http_status(status_code.as_u16());

    (
        status_code,
        Json(HealthPayload {
            status: status_text,
            probe: report.probe_kind().path(),
            detail: report.detail().to_owned(),
        }),
    )
}

async fn list_employee_menus(
    State(state): State<AppState>,
    Query(query): Query<EmployeeMenuDiscoveryQuery>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "listEmployeeMenus",
        Some("load-gate"),
        Some(state.plant_id.as_str()),
    );

    let response = match handle_list_employee_menus(&state, query) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("menu discovery payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error)
                        .expect("menu discovery error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_list_employee_menus(
    state: &AppState,
    query: EmployeeMenuDiscoveryQuery,
) -> Result<MenuDiscoveryResponse, (StatusCode, ErrorPayload)> {
    let request_plant_id = query.plant_id.as_deref().ok_or_else(|| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_MENU_DISCOVERY_QUERY",
            "plantId query parameter is required".to_owned(),
        )
    })?;
    if request_plant_id != state.plant_id.as_str() {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "UNSUPPORTED_PLANT_ID",
            format!(
                "plantId `{request_plant_id}` is unsupported by this runtime, expected `{}`",
                state.plant_id.as_str()
            ),
        ));
    }

    let moment = current_taipei_business_moment().map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "TIME_RESOLUTION_FAILED",
            error,
        )
    })?;
    handle_list_employee_menus_at(state, query, moment)
}

fn handle_list_employee_menus_at(
    state: &AppState,
    query: EmployeeMenuDiscoveryQuery,
    moment: TaipeiBusinessMoment,
) -> Result<MenuDiscoveryResponse, (StatusCode, ErrorPayload)> {
    let view = query.view.unwrap_or(MenuDiscoveryViewQuery::Week);
    let (from_epoch_day, to_epoch_day) = resolve_discovery_window(view, &query, moment.epoch_day())
        .map_err(|message| {
            domain_error(
                StatusCode::BAD_REQUEST,
                "INVALID_MENU_DISCOVERY_QUERY",
                message,
            )
        })?;
    let health_tag_filter = query
        .health_tag
        .as_deref()
        .map(MenuHealthTag::parse)
        .transpose()
        .map_err(|error| {
            domain_error(
                StatusCode::BAD_REQUEST,
                "INVALID_MENU_DISCOVERY_QUERY",
                format!("healthTag is invalid: {error}"),
            )
        })?;
    let menu_type_filter = query
        .menu_type
        .as_deref()
        .map(normalize_menu_type_filter)
        .transpose()
        .map_err(|message| {
            domain_error(
                StatusCode::BAD_REQUEST,
                "INVALID_MENU_DISCOVERY_QUERY",
                message,
            )
        })?;
    if let (Some(price_min_minor), Some(price_max_minor)) =
        (query.price_min_minor, query.price_max_minor)
    {
        if price_min_minor > price_max_minor {
            return Err(domain_error(
                StatusCode::BAD_REQUEST,
                "INVALID_MENU_DISCOVERY_QUERY",
                "priceMinMinor must be less than or equal to priceMaxMinor".to_owned(),
            ));
        }
    }

    let page = query.page.unwrap_or(1);
    if page == 0 {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_MENU_DISCOVERY_QUERY",
            "page must be greater than or equal to 1".to_owned(),
        ));
    }
    let page_size = query.page_size.unwrap_or(20);
    if page_size == 0 || page_size > 200 {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_MENU_DISCOVERY_QUERY",
            "pageSize must be between 1 and 200".to_owned(),
        ));
    }

    let discovery_gateway = HttpEmployeeDiscoveryExecutionGateway::new(
        state.compliance_lifecycle.as_ref(),
        state.delivery_policy.as_ref(),
        &state.menu_supply_policy,
    );
    let for_search = query_has_search_filters(&query);
    let mut entries = discovery_gateway
        .execute_discovery_snapshot(&state.plant_id, moment, for_search)
        .map_err(|error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "MENU_DISCOVERY_INTERNAL_ERROR",
                error.to_string(),
            )
        })?;

    let search_query = query.search.map(|value| value.trim().to_ascii_lowercase());
    entries.retain(|entry| {
        if !entry.preorder_open() {
            return false;
        }
        if entry.menu_item().delivery_epoch_day() < from_epoch_day
            || entry.menu_item().delivery_epoch_day() > to_epoch_day
        {
            return false;
        }
        if let Some(search_query) = search_query.as_deref() {
            let name = entry.menu_item().name().to_ascii_lowercase();
            let description = entry.menu_item().description().to_ascii_lowercase();
            if !name.contains(search_query) && !description.contains(search_query) {
                return false;
            }
        }
        if let Some(menu_type_filter) = menu_type_filter.as_deref() {
            if entry.menu_item().menu_type() != menu_type_filter {
                return false;
            }
        }
        if let Some(health_tag_filter) = health_tag_filter {
            if !entry.menu_item().health_tags().contains(&health_tag_filter) {
                return false;
            }
        }
        if let Some(price_min_minor) = query.price_min_minor {
            if entry.menu_item().price().amount_minor() < price_min_minor {
                return false;
            }
        }
        if let Some(price_max_minor) = query.price_max_minor {
            if entry.menu_item().price().amount_minor() > price_max_minor {
                return false;
            }
        }
        if let Some(remaining_quantity) = query.remaining_quantity {
            if entry.remaining_quantity() != remaining_quantity {
                return false;
            }
        }
        true
    });

    let sort_by = query.sort_by.unwrap_or(MenuSortFieldQuery::DeliveryDate);
    let sort_order = query.sort_order.unwrap_or(SortOrderQuery::Asc);
    entries.sort_by(|left, right| compare_menu_discovery_entry(left, right, sort_by, sort_order));

    let total_items = entries.len();
    let total_pages = if total_items == 0 {
        0
    } else {
        (total_items - 1) / page_size + 1
    };
    let start = page.saturating_sub(1).saturating_mul(page_size);
    let end = start.saturating_add(page_size).min(total_items);
    let paged_entries = if start >= total_items {
        Vec::new()
    } else {
        entries[start..end].to_vec()
    };
    let items = paged_entries
        .iter()
        .map(to_menu_discovery_item_payload)
        .collect::<Vec<_>>();

    let mut items_by_delivery_date = BTreeMap::<i32, Vec<MenuDiscoveryItem>>::new();
    for day in from_epoch_day..=to_epoch_day {
        items_by_delivery_date.insert(day, Vec::new());
    }
    for item in &items {
        if let Some(items_for_day) = items_by_delivery_date.get_mut(
            &parse_iso_date_to_epoch_day(&item.delivery_date)
                .expect("response item deliveryDate should always parse"),
        ) {
            items_for_day.push(item.clone());
        }
    }
    let days = (from_epoch_day..=to_epoch_day)
        .map(|epoch_day| MenuDiscoveryDay {
            delivery_date: epoch_day_to_iso_date(epoch_day),
            items: items_by_delivery_date
                .remove(&epoch_day)
                .unwrap_or_default(),
        })
        .collect::<Vec<_>>();

    Ok(MenuDiscoveryResponse {
        timezone: "Asia/Taipei",
        view: view.as_str(),
        recommendation_requested: query.recommendation_enabled.unwrap_or(false),
        recommendation_applied: false,
        from_date: epoch_day_to_iso_date(from_epoch_day),
        to_date: epoch_day_to_iso_date(to_epoch_day),
        days,
        items,
        page: MenuDiscoveryPageMeta {
            page,
            page_size,
            total_items,
            total_pages,
        },
    })
}

fn query_has_search_filters(query: &EmployeeMenuDiscoveryQuery) -> bool {
    query.search.is_some()
        || query.menu_type.is_some()
        || query.health_tag.is_some()
        || query.price_min_minor.is_some()
        || query.price_max_minor.is_some()
        || query.remaining_quantity.is_some()
}

fn resolve_discovery_window(
    view: MenuDiscoveryViewQuery,
    query: &EmployeeMenuDiscoveryQuery,
    now_epoch_day: i32,
) -> Result<(i32, i32), String> {
    let menu_date = query
        .menu_date
        .as_deref()
        .map(parse_iso_date_to_epoch_day)
        .transpose()?;
    let from_date = query
        .from_date
        .as_deref()
        .map(parse_iso_date_to_epoch_day)
        .transpose()?;
    let to_date = query
        .to_date
        .as_deref()
        .map(parse_iso_date_to_epoch_day)
        .transpose()?;

    let (from_epoch_day, to_epoch_day) = match view {
        MenuDiscoveryViewQuery::Week => {
            if to_date.is_some() {
                return Err("toDate is not allowed when view=week".to_owned());
            }
            let from_epoch_day = from_date.or(menu_date).unwrap_or(now_epoch_day);
            (from_epoch_day, from_epoch_day.saturating_add(6))
        }
        MenuDiscoveryViewQuery::Calendar => {
            let from_epoch_day = from_date.or(menu_date).unwrap_or(now_epoch_day);
            let to_epoch_day = to_date.unwrap_or(from_epoch_day.saturating_add(13));
            (from_epoch_day, to_epoch_day)
        }
    };

    if to_epoch_day < from_epoch_day {
        return Err("toDate must be greater than or equal to fromDate".to_owned());
    }
    if to_epoch_day.saturating_sub(from_epoch_day) > 30 {
        return Err("discovery date range must be at most 31 days".to_owned());
    }
    Ok((from_epoch_day, to_epoch_day))
}

fn compare_menu_discovery_entry(
    left: &EmployeeMenuDiscoveryEntry,
    right: &EmployeeMenuDiscoveryEntry,
    sort_by: MenuSortFieldQuery,
    sort_order: SortOrderQuery,
) -> CmpOrdering {
    let ordering = match sort_by {
        MenuSortFieldQuery::Name => left.menu_item().name().cmp(right.menu_item().name()),
        MenuSortFieldQuery::PriceMinor => left
            .menu_item()
            .price()
            .amount_minor()
            .cmp(&right.menu_item().price().amount_minor()),
        MenuSortFieldQuery::RemainingQuantity => {
            left.remaining_quantity().cmp(&right.remaining_quantity())
        }
        MenuSortFieldQuery::DeliveryDate => left
            .menu_item()
            .delivery_epoch_day()
            .cmp(&right.menu_item().delivery_epoch_day()),
    }
    .then_with(|| {
        left.menu_item()
            .delivery_epoch_day()
            .cmp(&right.menu_item().delivery_epoch_day())
    })
    .then_with(|| left.menu_item().name().cmp(right.menu_item().name()))
    .then_with(|| {
        left.menu_item()
            .vendor_id()
            .cmp(right.menu_item().vendor_id())
    })
    .then_with(|| {
        left.menu_item()
            .menu_item_id()
            .cmp(right.menu_item().menu_item_id())
    });
    match sort_order {
        SortOrderQuery::Asc => ordering,
        SortOrderQuery::Desc => ordering.reverse(),
    }
}

fn to_menu_discovery_item_payload(entry: &EmployeeMenuDiscoveryEntry) -> MenuDiscoveryItem {
    let menu_item = entry.menu_item();
    MenuDiscoveryItem {
        menu_item_id: menu_item.menu_item_id().as_str().to_owned(),
        vendor_id: menu_item.vendor_id().as_str().to_owned(),
        name: menu_item.name().to_owned(),
        description: menu_item.description().to_owned(),
        image_url: menu_item.image_url().map(|value| value.as_str().to_owned()),
        menu_type: menu_item.menu_type().to_owned(),
        health_tags: menu_item
            .health_tags()
            .iter()
            .map(|tag| tag.as_str().to_owned())
            .collect(),
        price: MenuPricePayload {
            currency: menu_item.price().currency().to_owned(),
            amount_minor: menu_item.price().amount_minor(),
        },
        remaining_quantity: entry.remaining_quantity(),
        preorder_open: entry.preorder_open(),
        preorder_open_days_ahead: entry.preorder_open_days_ahead(),
        modify_cancel_cutoff_minute_of_day: entry.modify_cancel_cutoff_minute_of_day(),
        delivery_date: epoch_day_to_iso_date(menu_item.delivery_epoch_day()),
        earliest_delivery_date: epoch_day_to_iso_date(entry.earliest_delivery_epoch_day()),
        latest_delivery_date: epoch_day_to_iso_date(entry.latest_delivery_epoch_day()),
        cutoff_date: epoch_day_to_iso_date(entry.cutoff_epoch_day()),
    }
}

fn normalize_menu_type_filter(value: &str) -> Result<String, String> {
    let normalized = value.trim().to_ascii_uppercase();
    if normalized.is_empty() {
        return Err("menuType must be non-empty when provided".to_owned());
    }
    if normalized.len() > 32 {
        return Err("menuType must be at most 32 characters".to_owned());
    }
    if !normalized
        .chars()
        .all(|ch| ch.is_ascii_uppercase() || ch.is_ascii_digit() || ch == '_')
    {
        return Err("menuType must be uppercase snake case".to_owned());
    }
    Ok(normalized)
}

fn parse_iso_date_to_epoch_day(value: &str) -> Result<i32, String> {
    let trimmed = value.trim();
    let mut parts = trimmed.split('-');
    let year = parts
        .next()
        .ok_or_else(|| "date must use YYYY-MM-DD format".to_owned())?
        .parse::<i32>()
        .map_err(|_| "date year is invalid".to_owned())?;
    let month = parts
        .next()
        .ok_or_else(|| "date must use YYYY-MM-DD format".to_owned())?
        .parse::<u32>()
        .map_err(|_| "date month is invalid".to_owned())?;
    let day = parts
        .next()
        .ok_or_else(|| "date must use YYYY-MM-DD format".to_owned())?
        .parse::<u32>()
        .map_err(|_| "date day is invalid".to_owned())?;
    if parts.next().is_some() {
        return Err("date must use YYYY-MM-DD format".to_owned());
    }
    if !(1..=12).contains(&month) {
        return Err("date month must be between 1 and 12".to_owned());
    }
    let max_day = days_in_month(year, month);
    if day == 0 || day > max_day {
        return Err(format!(
            "date day must be between 1 and {max_day} for month {month:02}"
        ));
    }

    i32::try_from(days_from_civil(year, month, day))
        .map_err(|_| "date is out of supported epoch-day range".to_owned())
}

fn epoch_day_to_iso_date(epoch_day: i32) -> String {
    let (year, month, day) = civil_from_days(i64::from(epoch_day));
    format!("{year:04}-{month:02}-{day:02}")
}

fn days_in_month(year: i32, month: u32) -> u32 {
    match month {
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        4 | 6 | 9 | 11 => 30,
        2 if is_leap_year(year) => 29,
        2 => 28,
        _ => 0,
    }
}

fn is_leap_year(year: i32) -> bool {
    (year % 4 == 0 && year % 100 != 0) || (year % 400 == 0)
}

fn days_from_civil(year: i32, month: u32, day: u32) -> i64 {
    let year = i64::from(year) - if month <= 2 { 1 } else { 0 };
    let era = if year >= 0 { year } else { year - 399 } / 400;
    let year_of_era = year - era * 400;
    let month = i64::from(month);
    let day = i64::from(day);
    let day_of_year = (153 * (month + if month > 2 { -3 } else { 9 }) + 2) / 5 + day - 1;
    let day_of_era = year_of_era * 365 + year_of_era / 4 - year_of_era / 100 + day_of_year;
    era * 146_097 + day_of_era - 719_468
}

fn civil_from_days(days_since_epoch: i64) -> (i32, u32, u32) {
    let shifted_days = days_since_epoch + 719_468;
    let era = if shifted_days >= 0 {
        shifted_days
    } else {
        shifted_days - 146_096
    } / 146_097;
    let day_of_era = shifted_days - era * 146_097;
    let year_of_era =
        (day_of_era - day_of_era / 1_460 + day_of_era / 36_524 - day_of_era / 146_096) / 365;
    let year = year_of_era + era * 400;
    let day_of_year = day_of_era - (365 * year_of_era + year_of_era / 4 - year_of_era / 100);
    let month_piece = (5 * day_of_year + 2) / 153;
    let day = day_of_year - (153 * month_piece + 2) / 5 + 1;
    let month = month_piece + if month_piece < 10 { 3 } else { -9 };
    let year = year + if month <= 2 { 1 } else { 0 };

    (
        i32::try_from(year).expect("civil year should fit in i32"),
        u32::try_from(month).expect("civil month should fit in u32"),
        u32::try_from(day).expect("civil day should fit in u32"),
    )
}

async fn create_employee_order(
    State(state): State<AppState>,
    Json(request): Json<CreateOrderRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "createEmployeeOrder",
        Some("load-gate"),
        Some(state.plant_id.as_str()),
    );

    let response = match handle_create_employee_order(&state, request) {
        Ok(response) => {
            telemetry.finish_with_http_status(StatusCode::CREATED.as_u16());
            (
                StatusCode::CREATED,
                Json(
                    serde_json::to_value(response)
                        .expect("create order payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error)
                        .expect("error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_create_employee_order(
    state: &AppState,
    request: CreateOrderRequest,
) -> Result<CreateOrderResponse, (StatusCode, ErrorPayload)> {
    let request_vendor_id = VendorId::parse(request.vendor_id).map_err(|error| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_ORDER_REQUEST",
            format!("vendorId is invalid: {error}"),
        )
    })?;
    if request_vendor_id != state.vendor_id {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "UNSUPPORTED_VENDOR_ID",
            "order request targets a vendor that is not configured in prelaunch runtime".to_owned(),
        ));
    }

    let line_items = parse_domain_line_items(request.line_items)?;
    let requested_at = current_taipei_business_moment().map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "TIME_RESOLUTION_FAILED",
            error,
        )
    })?;

    let order_id = OrderId::parse(format!(
        "ord-load-gate-{}",
        state
            .next_order_sequence
            .fetch_add(1, AtomicOrdering::Relaxed)
    ))
    .map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "ORDER_ID_GENERATION_FAILED",
            format!("generated order id is invalid: {error}"),
        )
    })?;

    let ordering_gateway = HttpOrderingExecutionGateway::new(
        state.compliance_lifecycle.as_ref(),
        state.delivery_policy.as_ref(),
        &state.menu_supply_policy,
    );

    ordering_gateway
        .execute_create_employee_order(
            order_id.clone(),
            &request_vendor_id,
            &state.plant_id,
            request.delivery_epoch_day,
            line_items,
            requested_at,
        )
        .map_err(map_http_order_execution_error)?;

    Ok(CreateOrderResponse {
        order_id: order_id.as_str().to_owned(),
        accepted: true,
    })
}

async fn update_employee_order(
    State(state): State<AppState>,
    Path(order_id): Path<String>,
    Json(request): Json<UpdateOrderRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "updateEmployeeOrder",
        Some("load-gate"),
        Some(state.plant_id.as_str()),
    );

    let response = match handle_update_employee_order(&state, order_id, request) {
        Ok(response) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(response)
                        .expect("update order payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error)
                        .expect("error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_update_employee_order(
    state: &AppState,
    order_id_raw: String,
    request: UpdateOrderRequest,
) -> Result<UpdateOrderResponse, (StatusCode, ErrorPayload)> {
    let order_id = OrderId::parse(order_id_raw).map_err(|error| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_ORDER_UPDATE_REQUEST",
            format!("orderId path parameter is invalid: {error}"),
        )
    })?;

    let mutation = parse_order_mutation(request)?;
    let requested_at = current_taipei_business_moment().map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "TIME_RESOLUTION_FAILED",
            error,
        )
    })?;

    let ordering_gateway = HttpOrderingExecutionGateway::new(
        state.compliance_lifecycle.as_ref(),
        state.delivery_policy.as_ref(),
        &state.menu_supply_policy,
    );

    let operation = mutation.operation_name().to_owned();
    ordering_gateway
        .execute_update_employee_order(
            &order_id,
            &state.vendor_id,
            &state.plant_id,
            mutation,
            requested_at,
        )
        .map_err(map_http_order_execution_error)?;

    Ok(UpdateOrderResponse {
        order_id: order_id.as_str().to_owned(),
        accepted: true,
        operation,
    })
}

fn parse_domain_line_items(
    payloads: Vec<OrderLineItemRequestPayload>,
) -> Result<Vec<OrderLineItemRequest>, (StatusCode, ErrorPayload)> {
    payloads
        .into_iter()
        .map(|payload| {
            let menu_item_id = MenuItemId::parse(payload.menu_item_id).map_err(|error| {
                domain_error(
                    StatusCode::BAD_REQUEST,
                    "INVALID_ORDER_REQUEST",
                    format!("menuItemId is invalid: {error}"),
                )
            })?;
            let special_requests = payload
                .special_requests
                .into_iter()
                .map(SpecialRequestOption::into_domain)
                .collect::<Vec<_>>();
            OrderLineItemRequest::new(menu_item_id, payload.quantity, special_requests).map_err(
                |error| {
                    domain_error(
                        StatusCode::BAD_REQUEST,
                        "INVALID_ORDER_REQUEST",
                        format!("line item is invalid: {error}"),
                    )
                },
            )
        })
        .collect::<Result<Vec<_>, _>>()
}

fn parse_order_mutation(
    request: UpdateOrderRequest,
) -> Result<OrderMutation, (StatusCode, ErrorPayload)> {
    match request.operation.as_str() {
        "REPLACE_LINE_ITEMS" => {
            let line_items = request.line_items.ok_or_else(|| {
                domain_error(
                    StatusCode::BAD_REQUEST,
                    "INVALID_ORDER_UPDATE_REQUEST",
                    "lineItems is required for REPLACE_LINE_ITEMS".to_owned(),
                )
            })?;
            let parsed_line_items = parse_domain_line_items(line_items).map_err(|(_, error)| {
                domain_error(
                    StatusCode::BAD_REQUEST,
                    "INVALID_ORDER_UPDATE_REQUEST",
                    error.message,
                )
            })?;
            Ok(OrderMutation::ReplaceLineItems {
                line_items: parsed_line_items,
            })
        }
        "CANCEL" => {
            let cancel_reason = request.cancel_reason.unwrap_or_default();
            if cancel_reason.trim().is_empty() {
                return Err(domain_error(
                    StatusCode::BAD_REQUEST,
                    "INVALID_ORDER_UPDATE_REQUEST",
                    "cancelReason must be non-empty for CANCEL".to_owned(),
                ));
            }
            Ok(OrderMutation::Cancel)
        }
        other => Err(domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_ORDER_UPDATE_REQUEST",
            format!("unsupported update operation `{other}`"),
        )),
    }
}

fn map_http_order_execution_error(error: HttpOrderExecutionError) -> (StatusCode, ErrorPayload) {
    match error {
        HttpOrderExecutionError::Deliverability(error) => domain_error(
            StatusCode::FORBIDDEN,
            "ORDER_VENDOR_DELIVERY_REJECTED",
            error.to_string(),
        ),
        HttpOrderExecutionError::MenuSupply(error) => domain_error(
            StatusCode::CONFLICT,
            "ORDER_POLICY_VIOLATION",
            error.to_string(),
        ),
        HttpOrderExecutionError::UnsupportedEmployeeMutation { operation } => domain_error(
            StatusCode::BAD_REQUEST,
            "ORDER_MUTATION_NOT_ALLOWED",
            format!("unsupported employee order mutation `{operation}`"),
        ),
    }
}

fn domain_error(
    status: StatusCode,
    code: &'static str,
    message: String,
) -> (StatusCode, ErrorPayload) {
    (status, ErrorPayload { code, message })
}

async fn verify_order_pickup(
    State(state): State<AppState>,
    Path(order_id): Path<String>,
    Json(request): Json<PickupVerificationRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "verifyPickupOrder",
        Some("load-gate"),
        Some(state.plant_id.as_str()),
    );

    if request.verification_code.trim().is_empty() {
        telemetry.finish_with_http_status(StatusCode::BAD_REQUEST.as_u16());
        return (
            StatusCode::BAD_REQUEST,
            Json(
                serde_json::to_value(ErrorPayload {
                    code: "INVALID_PICKUP_VERIFICATION_REQUEST",
                    message: "verificationCode must be non-empty".to_owned(),
                })
                .expect("error payload serialization should succeed"),
            ),
        );
    }

    let order_id = match OrderId::parse(order_id) {
        Ok(value) => value,
        Err(error) => {
            telemetry.finish_with_http_status(StatusCode::BAD_REQUEST.as_u16());
            return (
                StatusCode::BAD_REQUEST,
                Json(
                    serde_json::to_value(ErrorPayload {
                        code: "INVALID_PICKUP_VERIFICATION_REQUEST",
                        message: format!("orderId path parameter is invalid: {error}"),
                    })
                    .expect("error payload serialization should succeed"),
                ),
            );
        }
    };

    let snapshot = match state.menu_supply_policy.order_snapshot(&order_id) {
        Ok(value) => value,
        Err(error) => {
            telemetry.finish_with_http_status(StatusCode::INTERNAL_SERVER_ERROR.as_u16());
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(
                    serde_json::to_value(ErrorPayload {
                        code: "PICKUP_VERIFICATION_INTERNAL_ERROR",
                        message: error.to_string(),
                    })
                    .expect("error payload serialization should succeed"),
                ),
            );
        }
    };

    if snapshot.is_none() {
        telemetry.finish_with_http_status(StatusCode::NOT_FOUND.as_u16());
        return (
            StatusCode::NOT_FOUND,
            Json(
                serde_json::to_value(ErrorPayload {
                    code: "ORDER_NOT_FOUND",
                    message: "order does not exist for pickup verification".to_owned(),
                })
                .expect("error payload serialization should succeed"),
            ),
        );
    }

    telemetry.finish_with_http_status(StatusCode::OK.as_u16());
    (
        StatusCode::OK,
        Json(
            serde_json::to_value(PickupVerificationResponse {
                order_id: order_id.as_str().to_owned(),
                verified: true,
            })
            .expect("pickup verification payload serialization should succeed"),
        ),
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    fn actor_id(value: &str) -> ActorId {
        ActorId::parse(value).expect("actor id should be valid")
    }

    fn plant_id(value: &str) -> PlantId {
        PlantId::parse(value).expect("plant id should be valid")
    }

    fn vendor_id(value: &str) -> VendorId {
        VendorId::parse(value).expect("vendor id should be valid")
    }

    fn menu_item_id(value: &str) -> MenuItemId {
        MenuItemId::parse(value).expect("menu item id should be valid")
    }

    fn order_id(value: &str) -> OrderId {
        OrderId::parse(value).expect("order id should be valid")
    }

    fn taipei_moment(epoch_day: i32, minute_of_day: u16) -> TaipeiBusinessMoment {
        TaipeiBusinessMoment::new(epoch_day, minute_of_day).expect("Taipei moment should be valid")
    }

    fn committee_admin() -> AuthenticatedActorContext {
        AuthenticatedActorContext::new(
            actor_id("committee-discovery-test"),
            Role::CommitteeAdmin,
            PlantScope::all(),
            AuthenticationSource::CorporateSso,
        )
        .expect("committee actor should be valid")
    }

    fn vendor_operator() -> AuthenticatedActorContext {
        AuthenticatedActorContext::new(
            actor_id("vendor-discovery-test"),
            Role::VendorOperator,
            PlantScope::restricted(vec![plant_id("fab-a")]).expect("scope should be valid"),
            AuthenticationSource::VendorAccountMfa,
        )
        .expect("vendor actor should be valid")
    }

    fn build_state(now_epoch_day: i32) -> AppState {
        std::env::set_var("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4317");

        let committee = committee_admin();
        let vendor_actor = vendor_operator();
        let plant = plant_id("fab-a");
        let vendor_visible = vendor_id("ven-discoverytst-a1");
        let vendor_hidden = vendor_id("ven-discoverytst-b1");

        let mut compliance_lifecycle =
            VendorComplianceLifecycle::new(HistoryRetentionPolicy::default());
        let category = VendorCategory::parse("RESTAURANT").expect("category should be valid");
        let template = DocumentTemplateId::parse("tmpl-discovery-license")
            .expect("template id should be valid");
        compliance_lifecycle
            .upsert_document_template(
                &committee,
                ComplianceDocumentTemplate::new(
                    template.clone(),
                    category.clone(),
                    "Business License",
                    true,
                    365,
                    vec![30, 7],
                    0,
                )
                .expect("template should be valid"),
            )
            .expect("template should be upserted");

        for (vendor, display_name) in [
            (&vendor_visible, "Visible Vendor"),
            (&vendor_hidden, "Hidden Vendor"),
        ] {
            compliance_lifecycle
                .register_vendor_application(
                    &vendor_actor,
                    vendor.clone(),
                    display_name,
                    category.clone(),
                    ComplianceDate::from_epoch_day(now_epoch_day.saturating_sub(5)),
                )
                .expect("vendor application should be registered");
            compliance_lifecycle
                .submit_document(
                    &vendor_actor,
                    vendor,
                    &template,
                    VendorDocumentSubmission::new(
                        "s3://evidence/docs/discovery-license.pdf",
                        ComplianceDate::from_epoch_day(now_epoch_day.saturating_sub(5)),
                        ComplianceDate::from_epoch_day(now_epoch_day.saturating_add(300)),
                    )
                    .expect("document submission should be valid"),
                )
                .expect("document should be submitted");
            compliance_lifecycle
                .review_application(
                    &committee,
                    vendor,
                    VendorReviewDecision::Approved,
                    "approved",
                    ComplianceDate::from_epoch_day(now_epoch_day.saturating_sub(4)),
                )
                .expect("vendor should be approved");
        }

        let mut delivery_policy = VendorPlantDeliveryPolicy::new();
        delivery_policy
            .upsert_mapping(
                &committee,
                taipei_moment(now_epoch_day.saturating_sub(1), 1),
                VendorPlantDeliveryMapping::new(
                    DeliveryMappingId::parse("map-discovery-allow")
                        .expect("mapping id should be valid"),
                    vendor_visible.clone(),
                    plant.clone(),
                    ServiceWindow::new(
                        taipei_moment(now_epoch_day.saturating_sub(1), 0),
                        taipei_moment(now_epoch_day.saturating_add(10), 23 * 60 + 59),
                    )
                    .expect("service window should be valid"),
                    DeliveryRuleEffect::Allow,
                    100,
                ),
            )
            .expect("allow mapping should be configured");

        let menu_supply_policy = MenuSupplyPolicy::default();
        menu_supply_policy
            .upsert_menu_item(
                &vendor_actor,
                VendorMenuItem::new(
                    menu_item_id("menu-discoverytsta1"),
                    vendor_visible.clone(),
                    VendorMenuItemDraft::new(
                        "Visible Bento",
                        "high protein bento",
                        "BENTO",
                        vec![MenuHealthTag::HighProtein],
                        Some(
                            MenuImageUrl::parse("https://cdn.example.com/menu/visible-bento.jpg")
                                .expect("image should be valid"),
                        ),
                        Money::new("TWD", 12000).expect("money should be valid"),
                        5,
                        now_epoch_day.saturating_add(1),
                    )
                    .expect("menu draft should be valid"),
                ),
            )
            .expect("visible bento menu should be upserted");
        menu_supply_policy
            .upsert_menu_item(
                &vendor_actor,
                VendorMenuItem::new(
                    menu_item_id("menu-discoverytsta2"),
                    vendor_visible.clone(),
                    VendorMenuItemDraft::new(
                        "Visible Salad",
                        "vegan salad bowl",
                        "SALAD",
                        vec![MenuHealthTag::Vegan],
                        Some(
                            MenuImageUrl::parse("https://cdn.example.com/menu/visible-salad.jpg")
                                .expect("image should be valid"),
                        ),
                        Money::new("TWD", 9000).expect("money should be valid"),
                        8,
                        now_epoch_day.saturating_add(3),
                    )
                    .expect("menu draft should be valid"),
                ),
            )
            .expect("visible salad menu should be upserted");
        menu_supply_policy
            .upsert_menu_item(
                &vendor_actor,
                VendorMenuItem::new(
                    menu_item_id("menu-discoverytstb1"),
                    vendor_hidden.clone(),
                    VendorMenuItemDraft::new(
                        "Hidden Bento",
                        "should not be discoverable",
                        "BENTO",
                        vec![MenuHealthTag::HighProtein],
                        Some(
                            MenuImageUrl::parse("https://cdn.example.com/menu/hidden-bento.jpg")
                                .expect("image should be valid"),
                        ),
                        Money::new("TWD", 11000).expect("money should be valid"),
                        9,
                        now_epoch_day.saturating_add(1),
                    )
                    .expect("menu draft should be valid"),
                ),
            )
            .expect("hidden vendor menu should be upserted");

        menu_supply_policy
            .create_order(
                order_id("ord-discovery-tst-001"),
                &vendor_visible,
                now_epoch_day.saturating_add(1),
                vec![
                    OrderLineItemRequest::new(menu_item_id("menu-discoverytsta1"), 2, vec![])
                        .expect("line item should be valid"),
                ],
                taipei_moment(now_epoch_day, 600),
            )
            .expect("order should consume inventory");

        AppState {
            next_order_sequence: Arc::new(AtomicU64::new(1)),
            vendor_id: vendor_visible,
            plant_id: plant,
            compliance_lifecycle: Arc::new(compliance_lifecycle),
            delivery_policy: Arc::new(delivery_policy),
            menu_supply_policy,
        }
    }

    #[test]
    fn discovery_filters_are_deterministic_and_use_exact_inventory() {
        let now_epoch_day = 300;
        let state = build_state(now_epoch_day);
        let query = EmployeeMenuDiscoveryQuery {
            plant_id: Some("fab-a".to_owned()),
            view: Some(MenuDiscoveryViewQuery::Week),
            menu_date: Some(epoch_day_to_iso_date(now_epoch_day)),
            menu_type: Some("bento".to_owned()),
            health_tag: Some("HIGH_PROTEIN".to_owned()),
            price_min_minor: Some(10000),
            price_max_minor: Some(13000),
            remaining_quantity: Some(3),
            recommendation_enabled: Some(false),
            ..EmployeeMenuDiscoveryQuery::default()
        };

        let response =
            handle_list_employee_menus_at(&state, query, taipei_moment(now_epoch_day, 600))
                .expect("discovery request should succeed");

        assert_eq!(response.items.len(), 1);
        assert_eq!(response.items[0].menu_item_id, "menu-discoverytsta1");
        assert_eq!(response.items[0].vendor_id, "ven-discoverytst-a1");
        assert_eq!(response.items[0].remaining_quantity, 3);
        assert_eq!(response.items[0].menu_type, "BENTO");
        assert_eq!(
            response.days.len(),
            7,
            "week view should provide seven dates"
        );
    }

    #[test]
    fn recommendation_flag_does_not_change_core_discovery_behavior() {
        let now_epoch_day = 300;
        let state = build_state(now_epoch_day);
        let query = EmployeeMenuDiscoveryQuery {
            plant_id: Some("fab-a".to_owned()),
            view: Some(MenuDiscoveryViewQuery::Week),
            menu_date: Some(epoch_day_to_iso_date(now_epoch_day)),
            recommendation_enabled: Some(true),
            ..EmployeeMenuDiscoveryQuery::default()
        };

        let response_a =
            handle_list_employee_menus_at(&state, query, taipei_moment(now_epoch_day, 600))
                .expect("discovery request should succeed");
        let query = EmployeeMenuDiscoveryQuery {
            plant_id: Some("fab-a".to_owned()),
            view: Some(MenuDiscoveryViewQuery::Week),
            menu_date: Some(epoch_day_to_iso_date(now_epoch_day)),
            recommendation_enabled: Some(true),
            ..EmployeeMenuDiscoveryQuery::default()
        };
        let response_b =
            handle_list_employee_menus_at(&state, query, taipei_moment(now_epoch_day, 600))
                .expect("discovery request should succeed");

        assert!(response_a.recommendation_requested);
        assert!(!response_a.recommendation_applied);
        assert_eq!(
            response_a
                .items
                .iter()
                .map(|item| item.menu_item_id.clone())
                .collect::<Vec<_>>(),
            response_b
                .items
                .iter()
                .map(|item| item.menu_item_id.clone())
                .collect::<Vec<_>>(),
            "deterministic ordering should remain stable"
        );
    }

    #[test]
    fn discovery_rejects_missing_plant_id_query_parameter() {
        let now_epoch_day = 300;
        let state = build_state(now_epoch_day);
        let query = EmployeeMenuDiscoveryQuery {
            view: Some(MenuDiscoveryViewQuery::Week),
            menu_date: Some(epoch_day_to_iso_date(now_epoch_day)),
            ..EmployeeMenuDiscoveryQuery::default()
        };

        let error = handle_list_employee_menus(&state, query)
            .expect_err("missing plantId must fail without legacy fallback");
        assert_eq!(error.0, StatusCode::BAD_REQUEST);
        assert_eq!(error.1.code, "INVALID_MENU_DISCOVERY_QUERY");
    }
}
