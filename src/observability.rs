use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::OnceLock;
use std::time::Instant;

use opentelemetry::global;
use opentelemetry::global::BoxedSpan;
use opentelemetry::metrics::{Counter, Histogram, UpDownCounter};
use opentelemetry::trace::{Span, Tracer, TracerProvider as _};
use opentelemetry::KeyValue;
use opentelemetry_otlp::WithExportConfig;
use opentelemetry_sdk::Resource;
use tracing_subscriber::prelude::*;

static REQUEST_SEQUENCE: AtomicU64 = AtomicU64::new(1);
static TELEMETRY_BOOTSTRAP: OnceLock<TelemetryBootstrap> = OnceLock::new();

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TelemetryBootstrapConfig {
    exporter_endpoint: String,
    service_name: String,
    service_namespace: String,
    deployment_environment: String,
}

impl TelemetryBootstrapConfig {
    pub fn from_env(default_service_name: &str) -> Self {
        let exporter_endpoint = std::env::var("OTEL_EXPORTER_OTLP_ENDPOINT")
            .unwrap_or_else(|_| "http://127.0.0.1:4317".to_owned());
        let service_name = std::env::var("OTEL_SERVICE_NAME")
            .ok()
            .filter(|value| !value.trim().is_empty())
            .unwrap_or_else(|| default_service_name.to_owned());
        let service_namespace = std::env::var("OTEL_SERVICE_NAMESPACE")
            .ok()
            .filter(|value| !value.trim().is_empty())
            .unwrap_or_else(|| "corporate-catering".to_owned());
        let deployment_environment = std::env::var("DEPLOYMENT_ENVIRONMENT")
            .ok()
            .filter(|value| !value.trim().is_empty())
            .unwrap_or_else(|| "production".to_owned());

        Self {
            exporter_endpoint,
            service_name,
            service_namespace,
            deployment_environment,
        }
    }
}

#[derive(Debug)]
struct TelemetryBootstrap {
    _tracer_provider: opentelemetry_sdk::trace::TracerProvider,
    _meter_provider: opentelemetry_sdk::metrics::SdkMeterProvider,
}

pub fn initialize_telemetry_runtime_from_env(
    default_service_name: &str,
) -> Result<(), Box<dyn std::error::Error + Send + Sync + 'static>> {
    initialize_telemetry_runtime(TelemetryBootstrapConfig::from_env(default_service_name))
}

pub fn initialize_telemetry_runtime(
    config: TelemetryBootstrapConfig,
) -> Result<(), Box<dyn std::error::Error + Send + Sync + 'static>> {
    if TELEMETRY_BOOTSTRAP.get().is_some() {
        return Ok(());
    }

    let bootstrap = TelemetryBootstrap::build(config)?;
    if TELEMETRY_BOOTSTRAP.set(bootstrap).is_err() {
        return Ok(());
    }

    Ok(())
}

impl TelemetryBootstrap {
    fn build(
        config: TelemetryBootstrapConfig,
    ) -> Result<Self, Box<dyn std::error::Error + Send + Sync + 'static>> {
        let mut resource_attributes = vec![
            KeyValue::new("service.name", config.service_name.clone()),
            KeyValue::new("service.namespace", config.service_namespace),
            KeyValue::new("deployment.environment", config.deployment_environment),
        ];
        resource_attributes.extend(parse_resource_attributes_from_env());
        let resource = Resource::new(resource_attributes);

        let tracer_provider = opentelemetry_otlp::new_pipeline()
            .tracing()
            .with_exporter(
                opentelemetry_otlp::new_exporter()
                    .tonic()
                    .with_endpoint(config.exporter_endpoint.clone()),
            )
            .with_trace_config(
                opentelemetry_sdk::trace::Config::default().with_resource(resource.clone()),
            )
            .install_batch(opentelemetry_sdk::runtime::Tokio)?;

        let meter_provider = opentelemetry_otlp::new_pipeline()
            .metrics(opentelemetry_sdk::runtime::Tokio)
            .with_exporter(
                opentelemetry_otlp::new_exporter()
                    .tonic()
                    .with_endpoint(config.exporter_endpoint.clone()),
            )
            .with_resource(resource.clone())
            .with_period(std::time::Duration::from_secs(5))
            .build()?;

        global::set_tracer_provider(tracer_provider.clone());
        global::set_meter_provider(meter_provider.clone());

        let tracing_layer =
            tracing_opentelemetry::layer().with_tracer(tracer_provider.tracer(config.service_name));
        let formatting_layer = tracing_subscriber::fmt::layer()
            .json()
            .with_target(false)
            .with_level(true)
            .flatten_event(true);
        let env_filter = tracing_subscriber::EnvFilter::try_from_default_env()
            .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info"));

        let _ = tracing_subscriber::registry()
            .with(env_filter)
            .with(formatting_layer)
            .with(tracing_layer)
            .try_init();

        Ok(Self {
            _tracer_provider: tracer_provider,
            _meter_provider: meter_provider,
        })
    }
}

fn parse_resource_attributes_from_env() -> Vec<KeyValue> {
    std::env::var("OTEL_RESOURCE_ATTRIBUTES")
        .ok()
        .map(|raw| {
            raw.split(',')
                .filter_map(|entry| {
                    let trimmed = entry.trim();
                    if trimmed.is_empty() {
                        return None;
                    }

                    let (key, value) = trimmed.split_once('=')?;
                    let key = key.trim();
                    let value = value.trim();
                    if key.is_empty() || value.is_empty() {
                        return None;
                    }

                    Some(KeyValue::new(key.to_owned(), value.to_owned()))
                })
                .collect()
        })
        .unwrap_or_default()
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum TelemetryService {
    HttpApi,
    McpGateway,
    ComplianceWorker,
}

impl TelemetryService {
    pub const fn service_name(self) -> &'static str {
        match self {
            Self::HttpApi => "catering-http-api",
            Self::McpGateway => "catering-mcp-gateway",
            Self::ComplianceWorker => "catering-compliance-worker",
        }
    }

    const fn request_prefix(self) -> &'static str {
        match self {
            Self::HttpApi => "http",
            Self::McpGateway => "mcp",
            Self::ComplianceWorker => "compliance",
        }
    }

    fn instruments(self) -> &'static TelemetryInstruments {
        static HTTP: OnceLock<TelemetryInstruments> = OnceLock::new();
        static MCP: OnceLock<TelemetryInstruments> = OnceLock::new();
        static COMPLIANCE: OnceLock<TelemetryInstruments> = OnceLock::new();

        match self {
            Self::HttpApi => {
                HTTP.get_or_init(|| TelemetryInstruments::new(self, self.service_name()))
            }
            Self::McpGateway => {
                MCP.get_or_init(|| TelemetryInstruments::new(self, self.service_name()))
            }
            Self::ComplianceWorker => {
                COMPLIANCE.get_or_init(|| TelemetryInstruments::new(self, self.service_name()))
            }
        }
    }

    pub fn begin_operation(
        self,
        operation_id: impl Into<String>,
        actor_id: Option<&str>,
        plant_id: Option<&str>,
    ) -> CorrelatedOperation {
        let operation_id = operation_id.into();
        let request_id = format!(
            "{}-{}",
            self.request_prefix(),
            REQUEST_SEQUENCE.fetch_add(1, Ordering::Relaxed)
        );

        let mut attributes = vec![
            KeyValue::new("service.name", self.service_name()),
            KeyValue::new("service_name", self.service_name()),
            KeyValue::new("operation_id", operation_id.clone()),
            KeyValue::new("request_id", request_id.clone()),
        ];
        if let Some(actor_id) = actor_id {
            attributes.push(KeyValue::new("actor_id", actor_id.to_owned()));
        }
        if let Some(plant_id) = plant_id {
            attributes.push(KeyValue::new("plant_id", plant_id.to_owned()));
        }

        let tracer = global::tracer(self.service_name());
        let mut span = tracer.start(format!("{}.{}", self.service_name(), operation_id));
        for attribute in &attributes {
            span.set_attribute(attribute.clone());
        }

        let span_context = span.span_context().clone();
        let correlation = CorrelationContext {
            service_name: self.service_name(),
            operation_id: operation_id.clone(),
            request_id,
            trace_id: span_context.trace_id().to_string(),
            span_id: span_context.span_id().to_string(),
        };

        tracing::info!(
            service_name = %correlation.service_name,
            operation_id = %correlation.operation_id,
            request_id = %correlation.request_id,
            trace_id = %correlation.trace_id,
            span_id = %correlation.span_id,
            "observability operation span started"
        );

        self.instruments().on_start(&attributes);

        CorrelatedOperation {
            service: self,
            started_at: Instant::now(),
            attributes,
            span,
            correlation,
        }
    }
}

struct TelemetryInstruments {
    operation_calls: Counter<u64>,
    operation_duration_ms: Histogram<f64>,
    http_server_requests_total: Option<Counter<u64>>,
    http_server_request_duration_ms: Option<Histogram<f64>>,
    hpa_requests_per_second: Option<Counter<u64>>,
    in_flight_work: Option<UpDownCounter<i64>>,
}

impl TelemetryInstruments {
    fn new(service: TelemetryService, service_name: &str) -> Self {
        let meter = global::meter(service_name.to_owned());
        let operation_calls = meter
            .u64_counter("service.operation.calls.total")
            .with_description("Correlated operation invocations")
            .init();
        let operation_duration_ms = meter
            .f64_histogram("service.operation.duration.ms")
            .with_description("Correlated operation duration in milliseconds")
            .with_unit("ms")
            .init();
        let http_server_requests_total = matches!(service, TelemetryService::HttpApi).then(|| {
            meter
                .u64_counter("http_server_requests_total")
                .with_description("HTTP request count for release SLO gating")
                .init()
        });
        let http_server_request_duration_ms =
            matches!(service, TelemetryService::HttpApi).then(|| {
                meter
                    .f64_histogram("http_server_request_duration_ms")
                    .with_description("HTTP request duration for release SLO gating")
                    .with_unit("ms")
                    .init()
            });
        let hpa_requests_per_second = match service {
            TelemetryService::HttpApi => Some(
                meter
                    .u64_counter("http_server_requests_per_second")
                    .with_description(
                        "HTTP request counter used by autoscaling adapters to compute per-second load",
                    )
                    .with_unit("1")
                    .init(),
            ),
            TelemetryService::McpGateway => Some(
                meter
                    .u64_counter("mcp_tool_requests_per_second")
                    .with_description(
                        "MCP tool request counter used by autoscaling adapters to compute per-second load",
                    )
                    .with_unit("1")
                    .init(),
            ),
            TelemetryService::ComplianceWorker => None,
        };
        let in_flight_work = match service {
            TelemetryService::HttpApi => Some(
                meter
                    .i64_up_down_counter("in_flight_requests")
                    .with_description("In-flight HTTP requests for autoscaling")
                    .with_unit("1")
                    .init(),
            ),
            TelemetryService::McpGateway => Some(
                meter
                    .i64_up_down_counter("mcp_tool_in_flight_requests")
                    .with_description("In-flight MCP tool requests for autoscaling")
                    .with_unit("1")
                    .init(),
            ),
            TelemetryService::ComplianceWorker => Some(
                meter
                    .i64_up_down_counter("compliance_lifecycle_jobs_in_flight")
                    .with_description("In-flight compliance lifecycle jobs for autoscaling")
                    .with_unit("1")
                    .init(),
            ),
        };

        Self {
            operation_calls,
            operation_duration_ms,
            http_server_requests_total,
            http_server_request_duration_ms,
            hpa_requests_per_second,
            in_flight_work,
        }
    }

    fn on_start(&self, attributes: &[KeyValue]) {
        if let Some(in_flight) = &self.in_flight_work {
            in_flight.add(1, attributes);
        }
    }

    fn on_finish(&self, elapsed_ms: f64, outcome: TelemetryOutcome, attributes: &[KeyValue]) {
        let mut metric_attributes = attributes.to_vec();
        metric_attributes.push(KeyValue::new("outcome", outcome.as_str()));

        self.operation_calls.add(1, &metric_attributes);
        self.operation_duration_ms
            .record(elapsed_ms, &metric_attributes);

        if let Some(http_requests_total) = &self.http_server_requests_total {
            let mut http_attributes = metric_attributes.clone();
            http_attributes.push(KeyValue::new("status_code", outcome.status_code()));
            http_requests_total.add(1, &http_attributes);
        }
        if let Some(http_duration) = &self.http_server_request_duration_ms {
            let mut http_attributes = metric_attributes.clone();
            http_attributes.push(KeyValue::new("status_code", outcome.status_code()));
            http_duration.record(elapsed_ms, &http_attributes);
        }
        if let Some(requests_per_second) = &self.hpa_requests_per_second {
            requests_per_second.add(1, &metric_attributes);
        }
        if let Some(in_flight) = &self.in_flight_work {
            in_flight.add(-1, &metric_attributes);
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CorrelationContext {
    service_name: &'static str,
    operation_id: String,
    request_id: String,
    trace_id: String,
    span_id: String,
}

impl CorrelationContext {
    pub fn service_name(&self) -> &'static str {
        self.service_name
    }

    pub fn operation_id(&self) -> &str {
        &self.operation_id
    }

    pub fn request_id(&self) -> &str {
        &self.request_id
    }

    pub fn trace_id(&self) -> &str {
        &self.trace_id
    }

    pub fn span_id(&self) -> &str {
        &self.span_id
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TelemetryOutcome {
    Success,
    Error,
}

impl TelemetryOutcome {
    const fn as_str(self) -> &'static str {
        match self {
            Self::Success => "success",
            Self::Error => "error",
        }
    }

    const fn status_code(self) -> &'static str {
        match self {
            Self::Success => "200",
            Self::Error => "500",
        }
    }
}

pub struct CorrelatedOperation {
    service: TelemetryService,
    started_at: Instant,
    attributes: Vec<KeyValue>,
    span: BoxedSpan,
    correlation: CorrelationContext,
}

impl CorrelatedOperation {
    pub fn correlation_context(&self) -> &CorrelationContext {
        &self.correlation
    }

    pub fn finish(mut self, outcome: TelemetryOutcome) {
        let elapsed_ms = self.started_at.elapsed().as_secs_f64() * 1000.0;
        let outcome_value = outcome.as_str();

        let instruments = self.service.instruments();
        instruments.on_finish(elapsed_ms, outcome, &self.attributes);

        self.span
            .set_attribute(KeyValue::new("operation.outcome", outcome_value));
        self.span
            .set_attribute(KeyValue::new("operation.duration_ms", elapsed_ms));
        self.span.end();

        tracing::info!(
            service_name = %self.correlation.service_name,
            operation_id = %self.correlation.operation_id,
            request_id = %self.correlation.request_id,
            trace_id = %self.correlation.trace_id,
            span_id = %self.correlation.span_id,
            outcome = outcome_value,
            duration_ms = elapsed_ms,
            "observability operation span finished"
        );
    }
}
