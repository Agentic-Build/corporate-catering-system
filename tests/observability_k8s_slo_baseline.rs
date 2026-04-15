use std::collections::BTreeSet;
use std::fs;
use std::path::{Path, PathBuf};

use corporate_catering_system::health::runtime_health_routes;
use serde_json::Value as JsonValue;
use serde_yaml::Value as YamlValue;

fn repo_path(relative: &str) -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join(relative)
}

fn read_text(relative: &str) -> String {
    let path = repo_path(relative);
    fs::read_to_string(&path)
        .unwrap_or_else(|error| panic!("failed to read {}: {error}", path.display()))
}

fn read_yaml(relative: &str) -> YamlValue {
    let raw = read_text(relative);
    serde_yaml::from_str(&raw)
        .unwrap_or_else(|error| panic!("failed to parse YAML {relative}: {error}"))
}

fn read_json(relative: &str) -> JsonValue {
    let raw = read_text(relative);
    serde_json::from_str(&raw)
        .unwrap_or_else(|error| panic!("failed to parse JSON {relative}: {error}"))
}

fn yaml_get<'a>(value: &'a YamlValue, key: &str) -> &'a YamlValue {
    value
        .as_mapping()
        .and_then(|mapping| mapping.get(&YamlValue::String(key.to_owned())))
        .unwrap_or_else(|| panic!("missing YAML key `{key}`"))
}

fn yaml_sequence_strings(value: &YamlValue) -> Vec<String> {
    value
        .as_sequence()
        .unwrap_or_else(|| panic!("expected YAML sequence, got {value:?}"))
        .iter()
        .map(|entry| {
            entry
                .as_str()
                .unwrap_or_else(|| panic!("expected YAML string entry, got {entry:?}"))
                .to_owned()
        })
        .collect()
}

fn yaml_mapping_keys(value: &YamlValue) -> BTreeSet<String> {
    value
        .as_mapping()
        .unwrap_or_else(|| panic!("expected YAML mapping, got {value:?}"))
        .keys()
        .map(|key| {
            key.as_str()
                .unwrap_or_else(|| panic!("expected YAML string key, got {key:?}"))
                .to_owned()
        })
        .collect()
}

#[test]
fn otel_collector_exports_all_signals_to_victoria_stack() {
    let collector = read_yaml("ops/observability/otel/collector.yaml");

    let exporters = yaml_mapping_keys(yaml_get(&collector, "exporters"));
    assert!(exporters.contains("otlphttp/victoria_traces"));
    assert!(exporters.contains("prometheusremotewrite/victoria_metrics"));
    assert!(exporters.contains("otlphttp/victoria_logs"));

    let processors = yaml_mapping_keys(yaml_get(&collector, "processors"));
    assert!(processors.contains("resource/correlation"));
    assert!(processors.contains("attributes/correlation"));

    let pipelines = yaml_get(yaml_get(&collector, "service"), "pipelines")
        .as_mapping()
        .expect("pipelines must be a mapping");

    let expected = [
        ("traces", "otlphttp/victoria_traces"),
        ("metrics", "prometheusremotewrite/victoria_metrics"),
        ("logs", "otlphttp/victoria_logs"),
    ];

    for (pipeline_name, exporter_name) in expected {
        let pipeline = pipelines
            .get(&YamlValue::String(pipeline_name.to_owned()))
            .unwrap_or_else(|| panic!("missing `{pipeline_name}` pipeline"));
        let receivers = yaml_sequence_strings(yaml_get(pipeline, "receivers"));
        assert!(receivers.contains(&"otlp".to_owned()));

        let exporters = yaml_sequence_strings(yaml_get(pipeline, "exporters"));
        assert!(
            exporters.contains(&exporter_name.to_owned()),
            "pipeline `{pipeline_name}` must export to `{exporter_name}`"
        );
    }
}

#[test]
fn instrumentation_baseline_requires_cross_signal_correlation_fields() {
    let baseline = read_yaml("ops/observability/otel/instrumentation-baseline.yaml");

    assert_eq!(
        yaml_get(&baseline, "decisionIssueId").as_str(),
        Some("issue-pickup-throughput-slo")
    );
    assert_eq!(
        yaml_get(&baseline, "defaultBehavior").as_str(),
        Some("hard-enforced")
    );

    let required_context: BTreeSet<String> =
        yaml_sequence_strings(yaml_get(&baseline, "requiredCorrelationContext"))
            .into_iter()
            .collect();
    for key in [
        "trace_id",
        "span_id",
        "request_id",
        "operation_id",
        "actor_id",
        "plant_id",
        "service.name",
        "service.namespace",
        "deployment.environment",
    ] {
        assert!(
            required_context.contains(key),
            "missing context key `{key}`"
        );
    }

    let services = yaml_get(&baseline, "services")
        .as_sequence()
        .expect("services must be a sequence");
    assert!(
        services.len() >= 3,
        "at least three services must be instrumented"
    );

    for service in services {
        let signals = yaml_get(service, "signals");
        assert_eq!(yaml_get(signals, "traces").as_str(), Some("required"));
        assert_eq!(yaml_get(signals, "metrics").as_str(), Some("required"));
        assert_eq!(yaml_get(signals, "logs").as_str(), Some("required"));

        let attributes = yaml_sequence_strings(yaml_get(service, "requiredAttributes"));
        assert!(
            attributes.contains(&"operation_id".to_owned()),
            "service instrumentation must include operation_id attribute"
        );
    }
}

#[test]
fn hard_slo_policy_blocks_release_without_dashboard_alerts_and_load_thresholds() {
    let policy = read_yaml("ops/observability/slo/hard-slo-policy.yaml");
    let spec = yaml_get(&policy, "spec");
    let gate = yaml_get(spec, "releaseGate");

    assert_eq!(yaml_get(gate, "mode").as_str(), Some("blocking"));
    assert_eq!(
        yaml_get(gate, "dashboardRef").as_str(),
        Some("ops/observability/slo/grafana-dashboard-hard-slo.json")
    );
    assert_eq!(
        yaml_get(gate, "alertsRef").as_str(),
        Some("ops/observability/slo/alerts.yaml")
    );
    assert_eq!(
        yaml_get(gate, "loadThresholdsRef").as_str(),
        Some("ops/observability/load/prelaunch-thresholds.yaml")
    );

    let objective_ids: BTreeSet<String> = yaml_get(spec, "objectives")
        .as_sequence()
        .expect("objectives must be a sequence")
        .iter()
        .map(|objective| {
            yaml_get(objective, "id")
                .as_str()
                .expect("objective id must be a string")
                .to_owned()
        })
        .collect();

    assert_eq!(
        objective_ids,
        BTreeSet::from([
            "order-api-availability".to_owned(),
            "order-api-error-budget-burn".to_owned(),
            "order-api-latency-p95".to_owned(),
        ])
    );

    let scenarios = yaml_get(
        yaml_get(spec, "preLaunchLoadAcceptance"),
        "requiredScenarios",
    )
    .as_sequence()
    .expect("requiredScenarios must be a sequence");
    assert!(scenarios.iter().any(|scenario| {
        yaml_get(scenario, "name").as_str() == Some("peak-order-placement")
            && yaml_get(scenario, "p95LatencyMsMax").as_i64() == Some(350)
    }));
    assert!(scenarios.iter().any(|scenario| {
        yaml_get(scenario, "name").as_str() == Some("mixed-order-and-menu-reads")
            && yaml_get(scenario, "p95LatencyMsMax").as_i64() == Some(250)
    }));
}

#[test]
fn hard_slo_dashboard_contains_required_release_gating_panels() {
    let dashboard = read_json("ops/observability/slo/grafana-dashboard-hard-slo.json");

    assert_eq!(
        dashboard["title"].as_str(),
        Some("Corporate Catering Hard SLO Gate")
    );

    let tags = dashboard["tags"]
        .as_array()
        .expect("dashboard tags must be an array")
        .iter()
        .map(|tag| tag.as_str().expect("tag must be string").to_owned())
        .collect::<BTreeSet<_>>();
    assert!(tags.contains("hard-slo"));
    assert!(tags.contains("release-gate"));

    let panel_titles = dashboard["panels"]
        .as_array()
        .expect("dashboard panels must be an array")
        .iter()
        .map(|panel| {
            panel["title"]
                .as_str()
                .expect("panel title must be string")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();

    for required in [
        "Hard SLO: Order API Availability",
        "Hard SLO: Order API P95 Latency",
        "Hard SLO: Error Budget Burn Ratio",
        "Kubernetes Readiness Success Ratio",
    ] {
        assert!(
            panel_titles.contains(required),
            "missing required dashboard panel `{required}`"
        );
    }
}

#[test]
fn hard_slo_alert_rules_cover_release_blocking_and_kubernetes_peak_signals() {
    let alerts = read_yaml("ops/observability/slo/alerts.yaml");

    let groups = yaml_get(&alerts, "groups")
        .as_sequence()
        .expect("groups must be sequence");
    assert!(!groups.is_empty(), "at least one rule group must exist");

    let mut alert_names = BTreeSet::new();
    for group in groups {
        for rule in yaml_get(group, "rules")
            .as_sequence()
            .expect("rules must be sequence")
        {
            let alert_name = yaml_get(rule, "alert")
                .as_str()
                .expect("alert name must be string");
            alert_names.insert(alert_name.to_owned());

            let labels = yaml_get(rule, "labels");
            assert_eq!(yaml_get(labels, "gate").as_str(), Some("hard-slo"));
        }
    }

    for required in [
        "OrderApiAvailabilityBurnRateFast",
        "OrderApiLatencyP95Breach",
        "OrderApiErrorBudgetBurnTooFast",
        "KubernetesReadinessDrop",
        "KubernetesHpaSaturation",
    ] {
        assert!(
            alert_names.contains(required),
            "missing required hard-SLO alert `{required}`"
        );
    }
}

#[test]
fn kubernetes_manifests_define_health_checks_and_load_scaling_signals() {
    let kustomization = read_yaml("ops/kubernetes/base/kustomization.yaml");
    let resources = yaml_sequence_strings(yaml_get(&kustomization, "resources"))
        .into_iter()
        .collect::<BTreeSet<_>>();
    for required in [
        "deployment.yaml",
        "deployment-mcp.yaml",
        "deployment-compliance-worker.yaml",
        "hpa.yaml",
        "hpa-mcp.yaml",
        "hpa-compliance-worker.yaml",
    ] {
        assert!(
            resources.contains(required),
            "kustomization must include `{required}`"
        );
    }

    let deployment_files = [
        "ops/kubernetes/base/deployment.yaml",
        "ops/kubernetes/base/deployment-mcp.yaml",
        "ops/kubernetes/base/deployment-compliance-worker.yaml",
    ];
    let runtime_health_paths = runtime_health_routes()
        .iter()
        .map(|route| route.path())
        .collect::<BTreeSet<_>>();
    assert_eq!(
        runtime_health_paths,
        BTreeSet::from(["/health/live", "/health/ready", "/health/startup"])
    );
    let mut deployed_service_names = BTreeSet::new();
    for deployment_file in deployment_files {
        let deployment = read_yaml(deployment_file);
        assert_eq!(yaml_get(&deployment, "kind").as_str(), Some("Deployment"));

        let container = yaml_get(
            yaml_get(yaml_get(yaml_get(&deployment, "spec"), "template"), "spec"),
            "containers",
        )
        .as_sequence()
        .expect("containers must be sequence")
        .first()
        .expect("at least one container is required");

        assert_eq!(
            yaml_get(yaml_get(container, "readinessProbe"), "httpGet")
                .as_mapping()
                .and_then(|http_get| http_get.get(&YamlValue::String("path".to_owned())))
                .and_then(YamlValue::as_str),
            Some("/health/ready")
        );
        assert_eq!(
            yaml_get(yaml_get(container, "livenessProbe"), "httpGet")
                .as_mapping()
                .and_then(|http_get| http_get.get(&YamlValue::String("path".to_owned())))
                .and_then(YamlValue::as_str),
            Some("/health/live")
        );
        assert_eq!(
            yaml_get(yaml_get(container, "startupProbe"), "httpGet")
                .as_mapping()
                .and_then(|http_get| http_get.get(&YamlValue::String("path".to_owned())))
                .and_then(YamlValue::as_str),
            Some("/health/startup")
        );

        let env = yaml_get(container, "env")
            .as_sequence()
            .expect("env must be sequence");
        let env_names = env
            .iter()
            .map(|entry| {
                yaml_get(entry, "name")
                    .as_str()
                    .expect("env name must be string")
                    .to_owned()
            })
            .collect::<BTreeSet<_>>();
        for required in [
            "OTEL_SERVICE_NAME",
            "OTEL_EXPORTER_OTLP_ENDPOINT",
            "OTEL_TRACES_EXPORTER",
            "OTEL_METRICS_EXPORTER",
            "OTEL_LOGS_EXPORTER",
        ] {
            assert!(
                env_names.contains(required),
                "missing required OTEL env variable `{required}` in {deployment_file}"
            );
        }

        let otel_service_name = env
            .iter()
            .find(|entry| yaml_get(entry, "name").as_str() == Some("OTEL_SERVICE_NAME"))
            .and_then(|entry| yaml_get(entry, "value").as_str())
            .expect("OTEL_SERVICE_NAME value must exist")
            .to_owned();
        deployed_service_names.insert(otel_service_name);
    }

    let baseline = read_yaml("ops/observability/otel/instrumentation-baseline.yaml");
    let baseline_service_names = yaml_get(&baseline, "services")
        .as_sequence()
        .expect("services must be sequence")
        .iter()
        .map(|service| {
            yaml_get(service, "otelServiceName")
                .as_str()
                .expect("otelServiceName must be string")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert_eq!(deployed_service_names, baseline_service_names);

    let hpa_expectations = [
        (
            "ops/kubernetes/base/hpa.yaml",
            BTreeSet::from([
                "http_server_requests_per_second".to_owned(),
                "in_flight_requests".to_owned(),
            ]),
        ),
        (
            "ops/kubernetes/base/hpa-mcp.yaml",
            BTreeSet::from([
                "mcp_tool_requests_per_second".to_owned(),
                "mcp_tool_in_flight_requests".to_owned(),
            ]),
        ),
        (
            "ops/kubernetes/base/hpa-compliance-worker.yaml",
            BTreeSet::from(["compliance_lifecycle_jobs_in_flight".to_owned()]),
        ),
    ];

    for (hpa_file, required_pod_metrics) in hpa_expectations {
        let hpa = read_yaml(hpa_file);
        assert_eq!(
            yaml_get(&hpa, "kind").as_str(),
            Some("HorizontalPodAutoscaler")
        );
        assert!(
            yaml_get(yaml_get(&hpa, "spec"), "minReplicas")
                .as_i64()
                .unwrap_or_default()
                >= 3
        );

        let metrics = yaml_get(yaml_get(&hpa, "spec"), "metrics")
            .as_sequence()
            .expect("HPA metrics must be sequence");

        let has_cpu_metric = metrics.iter().any(|metric| {
            yaml_get(metric, "type").as_str() == Some("Resource")
                && yaml_get(yaml_get(metric, "resource"), "name").as_str() == Some("cpu")
        });
        assert!(has_cpu_metric, "HPA must include CPU utilization metric");

        let pod_metric_names = metrics
            .iter()
            .filter(|metric| yaml_get(metric, "type").as_str() == Some("Pods"))
            .map(|metric| {
                yaml_get(yaml_get(yaml_get(metric, "pods"), "metric"), "name")
                    .as_str()
                    .expect("pods metric name must be string")
                    .to_owned()
            })
            .collect::<BTreeSet<_>>();

        for required in required_pod_metrics {
            assert!(
                pod_metric_names.contains(&required),
                "missing pod metric `{required}` in `{hpa_file}`"
            );
        }
    }
}

#[test]
fn prelaunch_load_assets_are_aligned_with_hard_slo_policy() {
    let policy = read_yaml("ops/observability/slo/hard-slo-policy.yaml");
    let policy_scenarios = yaml_get(
        yaml_get(yaml_get(&policy, "spec"), "preLaunchLoadAcceptance"),
        "requiredScenarios",
    )
    .as_sequence()
    .expect("policy requiredScenarios must be sequence")
    .iter()
    .map(|scenario| {
        yaml_get(scenario, "name")
            .as_str()
            .expect("scenario name must be string")
            .to_owned()
    })
    .collect::<BTreeSet<_>>();
    assert_eq!(
        policy_scenarios,
        BTreeSet::from([
            "mixed-order-and-menu-reads".to_owned(),
            "peak-order-placement".to_owned(),
        ])
    );

    let thresholds = read_yaml("ops/observability/load/prelaunch-thresholds.yaml");
    let threshold_scenarios = yaml_get(&thresholds, "scenarios")
        .as_mapping()
        .expect("threshold scenarios must be mapping")
        .keys()
        .map(|key| {
            key.as_str()
                .expect("threshold scenario key must be string")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert_eq!(threshold_scenarios, policy_scenarios);

    let k6_script = read_text("ops/observability/load/k6-prelaunch.js");
    for required in [
        "peak_order_placement",
        "mixed_order_and_menu_reads",
        "/api/v1/employee/orders",
        "/api/v1/employee/menus",
    ] {
        assert!(
            k6_script.contains(required),
            "k6 prelaunch script must include `{required}`"
        );
    }
}

#[test]
fn ci_workflow_enforces_observability_hard_slo_gate() {
    let workflow = read_yaml(".github/workflows/observability-slo-gate.yml");

    let jobs = yaml_get(&workflow, "jobs")
        .as_mapping()
        .expect("workflow jobs must be mapping");
    let job = jobs
        .get(&YamlValue::String("hard-slo-gate".to_owned()))
        .expect("hard-slo-gate job must exist");

    let steps = yaml_get(job, "steps")
        .as_sequence()
        .expect("workflow steps must be sequence");

    let runs_gate_script = steps.iter().any(|step| {
        step.as_mapping()
            .and_then(|mapping| mapping.get(&YamlValue::String("run".to_owned())))
            .and_then(YamlValue::as_str)
            == Some("./scripts/check-observability-slo-baseline.sh")
    });

    assert!(
        runs_gate_script,
        "workflow must run observability hard-SLO baseline script"
    );

    let gate_script = read_text("scripts/check-observability-slo-baseline.sh");
    assert!(
        gate_script.contains("cargo run --quiet --bin observability_runtime_service"),
        "hard-SLO gate must execute the Rust runtime service, not a synthetic mock"
    );
    assert!(
        !gate_script.contains("mock-prelaunch-server.js"),
        "hard-SLO gate must not target a mock prelaunch server"
    );
}

#[test]
fn runtime_observability_bootstrap_and_metric_contracts_are_wired() {
    let source = read_text("src/observability.rs");

    for required in [
        "global::set_tracer_provider",
        "global::set_meter_provider",
        "opentelemetry_otlp::new_pipeline()",
        "http_server_requests_total",
        "http_server_request_duration_ms",
        "http_server_requests_per_second",
        "mcp_tool_requests_per_second",
        "in_flight_requests",
        "mcp_tool_in_flight_requests",
        "compliance_lifecycle_jobs_in_flight",
    ] {
        assert!(
            source.contains(required),
            "runtime observability source must contain `{required}`"
        );
    }
}
