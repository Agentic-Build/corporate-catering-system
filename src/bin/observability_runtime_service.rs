use std::net::SocketAddr;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;

use axum::extract::{Path, State};
use axum::http::StatusCode;
use axum::routing::{get, patch, post};
use axum::{Json, Router};
use corporate_catering_system::health::{evaluate_probe, HealthProbeKind, HealthState};
use corporate_catering_system::observability::{
    initialize_telemetry_runtime_from_env, TelemetryService,
};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone)]
struct AppState {
    next_order_sequence: Arc<AtomicU64>,
}

impl Default for AppState {
    fn default() -> Self {
        Self {
            next_order_sequence: Arc::new(AtomicU64::new(1)),
        }
    }
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct CreateOrderRequest {
    vendor_id: String,
    delivery_epoch_day: i32,
    line_items: Vec<OrderLineItemRequest>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct OrderLineItemRequest {
    menu_item_id: String,
    quantity: u32,
    special_request_option: Option<String>,
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
    line_items: Option<Vec<OrderLineItemRequest>>,
    cancel_reason: Option<String>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct UpdateOrderResponse {
    order_id: String,
    accepted: bool,
    operation: String,
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

    let state = AppState::default();
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
        .with_state(state);

    let listener = tokio::net::TcpListener::bind(socket_addr).await?;
    tracing::info!(bind_addr = %socket_addr, "observability runtime service listening");
    axum::serve(listener, app).await?;
    Ok(())
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

async fn list_employee_menus() -> (StatusCode, Json<Vec<MenuSummary>>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "listEmployeeMenus",
        Some("load-gate"),
        Some("plant-a"),
    );
    let payload = vec![
        MenuSummary {
            menu_item_id: "menu-1".to_owned(),
            vendor_id: "vendor-1".to_owned(),
            display_name: "Roasted Chicken Bento".to_owned(),
        },
        MenuSummary {
            menu_item_id: "menu-2".to_owned(),
            vendor_id: "vendor-2".to_owned(),
            display_name: "Mushroom Rice Bowl".to_owned(),
        },
    ];
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
        Some("plant-a"),
    );

    let request_valid = !request.vendor_id.trim().is_empty()
        && request.delivery_epoch_day > 0
        && !request.line_items.is_empty()
        && request.line_items.iter().all(|line| {
            !line.menu_item_id.trim().is_empty()
                && line.quantity > 0
                && line
                    .special_request_option
                    .as_ref()
                    .map(|value| !value.trim().is_empty())
                    .unwrap_or(true)
        });

    if !request_valid {
        telemetry.finish_with_http_status(StatusCode::BAD_REQUEST.as_u16());
        return (
            StatusCode::BAD_REQUEST,
            Json(
                serde_json::to_value(ErrorPayload {
                    code: "INVALID_ORDER_REQUEST",
                    message: "order payload is invalid".to_owned(),
                })
                .expect("error payload serialization should succeed"),
            ),
        );
    }

    let next_order_id = state.next_order_sequence.fetch_add(1, Ordering::Relaxed);
    telemetry.finish_with_http_status(StatusCode::CREATED.as_u16());

    (
        StatusCode::CREATED,
        Json(
            serde_json::to_value(CreateOrderResponse {
                order_id: format!("order-{next_order_id}"),
                accepted: true,
            })
            .expect("create order payload serialization should succeed"),
        ),
    )
}

async fn update_employee_order(
    Path(order_id): Path<String>,
    Json(request): Json<UpdateOrderRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "updateEmployeeOrder",
        Some("load-gate"),
        Some("plant-a"),
    );

    let valid_operation = match request.operation.as_str() {
        "REPLACE_LINE_ITEMS" => request
            .line_items
            .as_ref()
            .map(|line_items| {
                !line_items.is_empty()
                    && line_items
                        .iter()
                        .all(|line| !line.menu_item_id.trim().is_empty() && line.quantity > 0)
            })
            .unwrap_or(false),
        "CANCEL" => request
            .cancel_reason
            .as_ref()
            .map(|value| !value.trim().is_empty())
            .unwrap_or(false),
        _ => false,
    };

    if order_id.trim().is_empty() || !valid_operation {
        telemetry.finish_with_http_status(StatusCode::BAD_REQUEST.as_u16());
        return (
            StatusCode::BAD_REQUEST,
            Json(
                serde_json::to_value(ErrorPayload {
                    code: "INVALID_ORDER_UPDATE_REQUEST",
                    message: "order update payload is invalid".to_owned(),
                })
                .expect("error payload serialization should succeed"),
            ),
        );
    }

    telemetry.finish_with_http_status(StatusCode::OK.as_u16());
    (
        StatusCode::OK,
        Json(
            serde_json::to_value(UpdateOrderResponse {
                order_id,
                accepted: true,
                operation: request.operation,
            })
            .expect("update order payload serialization should succeed"),
        ),
    )
}
