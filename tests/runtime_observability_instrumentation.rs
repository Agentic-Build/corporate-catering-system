use std::collections::BTreeSet;

use corporate_catering_system::health::{
    evaluate_probe, runtime_health_routes, HealthProbeKind, HealthState,
};
use corporate_catering_system::observability::{TelemetryOutcome, TelemetryService};

#[test]
fn correlated_operation_generates_trace_span_and_request_ids() {
    let operation =
        TelemetryService::HttpApi.begin_operation("createEmployeeOrder", Some("emp-1"), Some("fab-a"));
    let correlation = operation.correlation_context().clone();

    assert_eq!(correlation.service_name(), "catering-http-api");
    assert_eq!(correlation.operation_id(), "createEmployeeOrder");
    assert!(correlation.request_id().starts_with("http-"));
    assert_eq!(correlation.trace_id().len(), 32);
    assert_eq!(correlation.span_id().len(), 16);

    operation.finish(TelemetryOutcome::Success);
}

#[test]
fn runtime_health_routes_cover_kubernetes_probe_paths() {
    let routes = runtime_health_routes();
    assert_eq!(routes.len(), 3);

    let route_paths = routes
        .iter()
        .map(|route| route.path())
        .collect::<BTreeSet<_>>();
    assert_eq!(
        route_paths,
        BTreeSet::from(["/health/live", "/health/ready", "/health/startup"])
    );
}

#[test]
fn health_probe_evaluation_enforces_dependency_readiness() {
    let readiness_unhealthy = evaluate_probe(
        HealthProbeKind::Readiness,
        false,
        "database dependency unavailable",
    );
    assert_eq!(readiness_unhealthy.state(), HealthState::Unhealthy);

    let startup_healthy = evaluate_probe(HealthProbeKind::Startup, true, "startup complete");
    assert_eq!(startup_healthy.state(), HealthState::Healthy);

    let liveness = evaluate_probe(HealthProbeKind::Liveness, false, "process heartbeat active");
    assert_eq!(liveness.state(), HealthState::Healthy);
}
