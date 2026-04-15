use std::net::SocketAddr;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};

use axum::extract::{Path, State};
use axum::http::StatusCode;
use axum::routing::{get, patch, post};
use axum::{Json, Router};
use corporate_catering_system::health::{evaluate_probe, HealthProbeKind, HealthState};
use corporate_catering_system::identity::{
    ActorId, AuthenticatedActorContext, AuthenticationSource, PlantId, PlantScope, Role,
};
use corporate_catering_system::menu_supply_window::{
    MenuImageUrl, MenuItemId, MenuSupplyPolicy, Money, OrderId, OrderLineItemRequest,
    OrderMutation, SpecialRequest, VendorMenuItem, VendorMenuItemDraft,
};
use corporate_catering_system::observability::{
    initialize_telemetry_runtime_from_env, TelemetryService,
};
use corporate_catering_system::transport::http::{
    HttpDeliveryExecutionGateway, HttpOrderExecutionError, HttpOrderingExecutionGateway,
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
    menu_item_ids: Arc<Vec<MenuItemId>>,
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

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct MenuSummary {
    menu_item_id: String,
    vendor_id: String,
    display_name: String,
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
    let mut menu_item_ids = Vec::with_capacity(usize::from(menu_variant_count));

    for index in 1..=menu_variant_count {
        let menu_item_id =
            MenuItemId::parse(format!("menu-{index}")).map_err(|error| error.to_string())?;
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
        menu_item_ids.push(menu_item_id);
    }

    Ok(AppState {
        next_order_sequence: Arc::new(AtomicU64::new(1)),
        vendor_id,
        plant_id,
        menu_item_ids: Arc::new(menu_item_ids),
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
) -> (StatusCode, Json<Vec<MenuSummary>>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "listEmployeeMenus",
        Some("load-gate"),
        Some(state.plant_id.as_str()),
    );

    let moment = match current_taipei_business_moment() {
        Ok(value) => value,
        Err(error) => {
            tracing::error!(error = %error, "failed to resolve Taipei business time for menu listing");
            telemetry.finish_with_http_status(StatusCode::INTERNAL_SERVER_ERROR.as_u16());
            return (StatusCode::INTERNAL_SERVER_ERROR, Json(Vec::new()));
        }
    };

    let delivery_gateway = HttpDeliveryExecutionGateway::new(
        state.compliance_lifecycle.as_ref(),
        state.delivery_policy.as_ref(),
    );
    let visible_vendor_ids =
        delivery_gateway.execute_list_employee_menus_for_browse(&state.plant_id, moment);

    let payload = if visible_vendor_ids
        .iter()
        .any(|vendor_id| vendor_id == &state.vendor_id)
    {
        state
            .menu_item_ids
            .iter()
            .filter_map(|menu_item_id| {
                state
                    .menu_supply_policy
                    .menu_item(menu_item_id)
                    .ok()
                    .flatten()
                    .map(|menu_item| MenuSummary {
                        menu_item_id: menu_item.menu_item_id().as_str().to_owned(),
                        vendor_id: menu_item.vendor_id().as_str().to_owned(),
                        display_name: menu_item.name().to_owned(),
                    })
            })
            .collect::<Vec<_>>()
    } else {
        Vec::new()
    };

    telemetry.finish_with_http_status(StatusCode::OK.as_u16());
    (StatusCode::OK, Json(payload))
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
        state.next_order_sequence.fetch_add(1, Ordering::Relaxed)
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
