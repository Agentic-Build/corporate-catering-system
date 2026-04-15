use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::OnceLock;
use std::time::Instant;

use opentelemetry::global;
use opentelemetry::global::BoxedSpan;
use opentelemetry::metrics::{Counter, Histogram};
use opentelemetry::trace::{Span, Tracer};
use opentelemetry::KeyValue;

static REQUEST_SEQUENCE: AtomicU64 = AtomicU64::new(1);

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
            Self::HttpApi => HTTP.get_or_init(|| TelemetryInstruments::new(self.service_name())),
            Self::McpGateway => MCP.get_or_init(|| TelemetryInstruments::new(self.service_name())),
            Self::ComplianceWorker => {
                COMPLIANCE.get_or_init(|| TelemetryInstruments::new(self.service_name()))
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
}

impl TelemetryInstruments {
    fn new(service_name: &str) -> Self {
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

        Self {
            operation_calls,
            operation_duration_ms,
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

        let mut attributes = self.attributes.clone();
        attributes.push(KeyValue::new("outcome", outcome_value));

        let instruments = self.service.instruments();
        instruments.operation_calls.add(1, &attributes);
        instruments.operation_duration_ms.record(elapsed_ms, &attributes);

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
