use std::collections::BTreeSet;
use std::fs;
use std::path::{Path, PathBuf};

use serde::Deserialize;
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

fn read_yaml_documents(relative: &str) -> Vec<YamlValue> {
    let raw = read_text(relative);
    serde_yaml::Deserializer::from_str(&raw)
        .map(|document| {
            YamlValue::deserialize(document).unwrap_or_else(|error| {
                panic!("failed to parse YAML document in {relative}: {error}")
            })
        })
        .collect()
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

fn assert_pod_least_privilege(pod_spec: &YamlValue, resource: &str) {
    assert_eq!(
        yaml_get(pod_spec, "automountServiceAccountToken").as_bool(),
        Some(false),
        "{resource} must disable service account token automount"
    );

    let pod_security_context = yaml_get(pod_spec, "securityContext");
    assert_eq!(
        yaml_get(pod_security_context, "runAsNonRoot").as_bool(),
        Some(true),
        "{resource} must enforce runAsNonRoot"
    );
    let run_as_user = yaml_get(pod_security_context, "runAsUser")
        .as_i64()
        .expect("runAsUser must be an integer");
    let run_as_group = yaml_get(pod_security_context, "runAsGroup")
        .as_i64()
        .expect("runAsGroup must be an integer");
    assert!(run_as_user > 0, "{resource} must use a non-root runAsUser");
    assert!(
        run_as_group > 0,
        "{resource} must use a non-root runAsGroup"
    );
    assert_eq!(
        yaml_get(yaml_get(pod_security_context, "seccompProfile"), "type").as_str(),
        Some("RuntimeDefault"),
        "{resource} must enforce RuntimeDefault seccomp profile"
    );
}

fn assert_container_least_privilege(container: &YamlValue, resource: &str) {
    let container_security_context = yaml_get(container, "securityContext");
    assert_eq!(
        yaml_get(container_security_context, "allowPrivilegeEscalation").as_bool(),
        Some(false),
        "{resource} must disable privilege escalation"
    );

    let dropped_caps = yaml_get(yaml_get(container_security_context, "capabilities"), "drop")
        .as_sequence()
        .expect("capabilities.drop must be a sequence")
        .iter()
        .map(|capability| {
            capability
                .as_str()
                .expect("capability entry must be a string")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert!(
        dropped_caps.contains("ALL"),
        "{resource} must drop all Linux capabilities"
    );
}

#[test]
fn kustomization_includes_runtime_infrastructure_security_resources() {
    let kustomization = read_yaml("ops/kubernetes/base/kustomization.yaml");
    let resources = yaml_sequence_strings(yaml_get(&kustomization, "resources"))
        .into_iter()
        .collect::<BTreeSet<_>>();

    for required in [
        "external-secrets.yaml",
        "postgres-topology.yaml",
        "pgbouncer.yaml",
        "deployment-web.yaml",
        "service-web.yaml",
        "gateway.yaml",
        "networkpolicy-default-deny.yaml",
        "networkpolicy-runtime-allow.yaml",
        "hpa-web.yaml",
    ] {
        assert!(
            resources.contains(required),
            "kustomization must include `{required}`"
        );
    }
}

#[test]
fn runtime_deployments_require_rw_ro_pooled_database_endpoints() {
    for deployment_file in [
        "ops/kubernetes/base/deployment.yaml",
        "ops/kubernetes/base/deployment-mcp.yaml",
        "ops/kubernetes/base/deployment-compliance-worker.yaml",
    ] {
        let deployment = read_yaml(deployment_file);
        let container = yaml_get(
            yaml_get(yaml_get(yaml_get(&deployment, "spec"), "template"), "spec"),
            "containers",
        )
        .as_sequence()
        .expect("containers must be sequence")
        .first()
        .expect("at least one container is required");

        let env_names = yaml_get(container, "env")
            .as_sequence()
            .expect("env must be sequence")
            .iter()
            .map(|entry| {
                yaml_get(entry, "name")
                    .as_str()
                    .expect("env name must be string")
                    .to_owned()
            })
            .collect::<BTreeSet<_>>();

        assert!(
            env_names.contains("DATABASE_RW_URL"),
            "{deployment_file} must declare DATABASE_RW_URL"
        );
        assert!(
            env_names.contains("DATABASE_RO_URL"),
            "{deployment_file} must declare DATABASE_RO_URL"
        );
        assert!(
            !env_names.contains("DATABASE_URL"),
            "{deployment_file} must not declare legacy DATABASE_URL"
        );
    }
}

#[test]
fn postgres_topology_and_pgbouncer_transaction_pools_are_declared() {
    let postgres_topology = read_text("ops/kubernetes/base/postgres-topology.yaml");
    assert!(postgres_topology.contains("kind: Cluster"));
    assert!(postgres_topology.contains("corporate-catering-postgres-rw"));
    assert!(postgres_topology.contains("corporate-catering-postgres-ro"));

    let pgbouncer_docs = read_yaml_documents("ops/kubernetes/base/pgbouncer.yaml");
    let mut deployment_names = BTreeSet::new();
    let mut service_names = BTreeSet::new();
    let mut transaction_mode_count = 0;

    for doc in pgbouncer_docs {
        let kind = yaml_get(&doc, "kind")
            .as_str()
            .expect("kind must be string");
        let name = yaml_get(yaml_get(&doc, "metadata"), "name")
            .as_str()
            .expect("metadata.name must be string")
            .to_owned();
        match kind {
            "Deployment" => {
                deployment_names.insert(name);
                let container = yaml_get(
                    yaml_get(yaml_get(yaml_get(&doc, "spec"), "template"), "spec"),
                    "containers",
                )
                .as_sequence()
                .expect("containers must be sequence")
                .first()
                .expect("container must exist");
                let env = yaml_get(container, "env")
                    .as_sequence()
                    .expect("env must be sequence");
                for entry in env {
                    let env_name = yaml_get(entry, "name")
                        .as_str()
                        .expect("env name must be string");
                    if env_name == "PGBOUNCER_POOL_MODE"
                        && yaml_get(entry, "value").as_str() == Some("transaction")
                    {
                        transaction_mode_count += 1;
                    }
                }
            }
            "Service" => {
                service_names.insert(name);
            }
            _ => {}
        }
    }

    assert!(deployment_names.contains("corporate-catering-pgbouncer-rw"));
    assert!(deployment_names.contains("corporate-catering-pgbouncer-ro"));
    assert!(service_names.contains("corporate-catering-pgbouncer-rw"));
    assert!(service_names.contains("corporate-catering-pgbouncer-ro"));
    assert_eq!(
        transaction_mode_count, 2,
        "both PgBouncer deployments must enforce transaction mode"
    );
}

#[test]
fn gateway_external_secret_and_default_deny_controls_are_active() {
    let gateway = read_text("ops/kubernetes/base/gateway.yaml");
    assert!(gateway.contains("kind: Gateway"));
    let gateway_docs = read_yaml_documents("ops/kubernetes/base/gateway.yaml");
    let gateway_spec = gateway_docs
        .iter()
        .find(|doc| yaml_get(doc, "kind").as_str() == Some("Gateway"))
        .map(|doc| yaml_get(doc, "spec"))
        .expect("gateway.yaml must define a Gateway resource");
    let listener_protocols = yaml_get(gateway_spec, "listeners")
        .as_sequence()
        .expect("Gateway listeners must be a sequence")
        .iter()
        .map(|listener| {
            yaml_get(listener, "protocol")
                .as_str()
                .expect("listener protocol must be string")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert!(
        listener_protocols.len() == 1 && listener_protocols.contains("HTTPS"),
        "gateway must enforce TLS-only ingress listeners"
    );
    let edge_security_policy_spec = gateway_docs
        .iter()
        .find(|doc| yaml_get(doc, "kind").as_str() == Some("SecurityPolicy"))
        .map(|doc| yaml_get(doc, "spec"))
        .expect("gateway.yaml must define a SecurityPolicy resource");
    let cors_allow_methods = yaml_get(yaml_get(edge_security_policy_spec, "cors"), "allowMethods")
        .as_sequence()
        .expect("cors.allowMethods must be a sequence")
        .iter()
        .map(|method| {
            method
                .as_str()
                .expect("cors.allowMethods entries must be strings")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert!(
        cors_allow_methods.contains("DELETE"),
        "gateway CORS allowMethods must include DELETE for contract-backed admin APIs"
    );
    assert!(gateway.contains("kind: SecurityPolicy"));
    assert!(gateway.contains("allowOrigins"));
    assert!(gateway.contains("kind: RateLimitPolicy"));
    assert!(gateway.contains("maxRequestBodyBytes"));
    assert!(gateway.contains("certificateRefs"));

    let external_secrets = read_yaml_documents("ops/kubernetes/base/external-secrets.yaml");
    let mut has_runtime_secret = false;
    let mut has_dual_rotation_keys = false;
    for doc in external_secrets {
        if yaml_get(&doc, "kind").as_str() != Some("ExternalSecret") {
            continue;
        }
        let target_name = yaml_get(yaml_get(yaml_get(&doc, "spec"), "target"), "name")
            .as_str()
            .expect("target.name must be string");
        if target_name != "corporate-catering-secrets" {
            continue;
        }
        has_runtime_secret = true;

        let secret_keys = yaml_get(yaml_get(&doc, "spec"), "data")
            .as_sequence()
            .expect("spec.data must be sequence")
            .iter()
            .map(|entry| {
                yaml_get(entry, "secretKey")
                    .as_str()
                    .expect("secretKey must be string")
                    .to_owned()
            })
            .collect::<BTreeSet<_>>();

        has_dual_rotation_keys = secret_keys
            .contains("corporate_sso_jwt_hs256_secret_base64_current")
            && secret_keys.contains("corporate_sso_jwt_hs256_secret_base64_next")
            && secret_keys.contains("vendor_mfa_jwt_hs256_secret_base64_current")
            && secret_keys.contains("vendor_mfa_jwt_hs256_secret_base64_next")
            && secret_keys.contains("mcp_oauth_service_account_hs256_secret_base64_current")
            && secret_keys.contains("mcp_oauth_service_account_hs256_secret_base64_next");
    }
    assert!(
        has_runtime_secret,
        "runtime ExternalSecret target corporate-catering-secrets must exist"
    );
    assert!(
        has_dual_rotation_keys,
        "runtime ExternalSecret must include current+next dual-key rotation wiring"
    );

    let default_deny = read_yaml("ops/kubernetes/base/networkpolicy-default-deny.yaml");
    assert_eq!(
        yaml_get(&default_deny, "kind").as_str(),
        Some("NetworkPolicy")
    );
    assert_eq!(
        yaml_get(yaml_get(&default_deny, "spec"), "podSelector")
            .as_mapping()
            .map(|mapping| mapping.len())
            .unwrap_or_default(),
        0,
        "default deny network policy must select all pods"
    );
    let policy_types =
        yaml_sequence_strings(yaml_get(yaml_get(&default_deny, "spec"), "policyTypes"))
            .into_iter()
            .collect::<BTreeSet<_>>();
    assert!(policy_types.contains("Ingress"));
    assert!(policy_types.contains("Egress"));

    let allow_network = read_text("ops/kubernetes/base/networkpolicy-runtime-allow.yaml");
    assert!(allow_network.contains("corporate-catering-runtime-allow-egress-db-pool"));
    assert!(allow_network.contains("corporate-catering-pgbouncer-allow-runtime-and-postgres"));
    assert!(allow_network.contains("corporate-catering-postgres-allow-pgbouncer-and-cluster"));
    assert!(allow_network.contains("corporate-catering-runtime-allow-egress-object-storage"));
    assert!(allow_network.contains("corporate-catering-object-storage-provision"));
    assert!(!allow_network.contains("0.0.0.0/0"));
}

#[test]
fn frontend_sveltekit_runtime_deployment_and_cache_control_contract_are_declared() {
    let web_deployment = read_yaml("ops/kubernetes/base/deployment-web.yaml");
    let container = yaml_get(
        yaml_get(
            yaml_get(yaml_get(&web_deployment, "spec"), "template"),
            "spec",
        ),
        "containers",
    )
    .as_sequence()
    .expect("containers must be sequence")
    .first()
    .expect("container must exist");

    let env = yaml_get(container, "env")
        .as_sequence()
        .expect("env must be sequence");
    let env_pairs = env
        .iter()
        .map(|entry| {
            let value = entry
                .as_mapping()
                .and_then(|mapping| mapping.get(&YamlValue::String("value".to_owned())))
                .and_then(YamlValue::as_str)
                .map(str::to_owned);
            (
                yaml_get(entry, "name")
                    .as_str()
                    .expect("env name must be string")
                    .to_owned(),
                value,
            )
        })
        .collect::<Vec<_>>();

    let runtime_kind = env_pairs
        .iter()
        .find(|(name, _)| name == "FRONTEND_RUNTIME_KIND")
        .and_then(|(_, value)| value.clone());
    assert_eq!(runtime_kind.as_deref(), Some("sveltekit-adapter-node"));

    let env_names = env_pairs
        .iter()
        .map(|(name, _)| name.clone())
        .collect::<BTreeSet<_>>();
    for required in [
        "FRONTEND_CACHE_CONTROL_DYNAMIC",
        "FRONTEND_CACHE_CONTROL_ASSET",
        "FRONTEND_CACHE_CONTROL_ASSET_IMMUTABLE",
    ] {
        assert!(
            env_names.contains(required),
            "frontend deployment must declare {required}"
        );
    }

    let web_service = read_yaml("ops/kubernetes/base/service-web.yaml");
    assert_eq!(yaml_get(&web_service, "kind").as_str(), Some("Service"));
    assert_eq!(
        yaml_get(yaml_get(&web_service, "metadata"), "name").as_str(),
        Some("corporate-catering-web")
    );

    let hooks = read_text("apps/web/src/hooks.server.ts");
    assert!(hooks.contains("cache-control"));
    assert!(hooks.contains("/_app/immutable/"));
    assert!(hooks.contains("FRONTEND_CACHE_CONTROL_ASSET_IMMUTABLE"));
}

#[test]
fn kubernetes_overlays_encode_runtime_strategy_and_environment_promotion_values() {
    for (overlay_path, expected_namespace, expected_environment, expected_secret_prefix) in [
        (
            "ops/kubernetes/overlays/dev/kustomization.yaml",
            "catering-dev",
            "development",
            "secretPathPrefix=dev",
        ),
        (
            "ops/kubernetes/overlays/staging/kustomization.yaml",
            "catering-staging",
            "staging",
            "secretPathPrefix=staging",
        ),
        (
            "ops/kubernetes/overlays/production/kustomization.yaml",
            "catering-prod",
            "production",
            "secretPathPrefix=prod",
        ),
    ] {
        let overlay_raw = read_text(overlay_path);
        assert!(
            overlay_raw.contains("resources:\n  - ../../base"),
            "{overlay_path} must consume base manifests"
        );
        assert!(
            overlay_raw.contains("../../components/topology-multi-az"),
            "{overlay_path} must include multi-AZ topology component"
        );
        assert!(
            overlay_raw.contains("../../components/autoscaling-keda-worker"),
            "{overlay_path} must include KEDA autoscaling component"
        );
        assert!(
            overlay_raw
                .contains("runtime.corporate-catering.io/scaling-strategy: hpa-plus-keda-worker"),
            "{overlay_path} must encode selected runtime scaling strategy"
        );
        assert!(
            overlay_raw.contains(expected_secret_prefix),
            "{overlay_path} must encode environment-specific secret prefix"
        );

        let overlay = read_yaml(overlay_path);
        assert_eq!(
            yaml_get(&overlay, "namespace").as_str(),
            Some(expected_namespace),
            "{overlay_path} must set expected namespace"
        );

        let annotations = yaml_get(&overlay, "commonAnnotations");
        assert_eq!(
            yaml_get(annotations, "runtime.corporate-catering.io/environment").as_str(),
            Some(expected_environment),
            "{overlay_path} must label environment"
        );
        assert_eq!(
            yaml_get(
                annotations,
                "runtime.corporate-catering.io/topology-strategy"
            )
            .as_str(),
            Some("multi-az"),
            "{overlay_path} must encode topology strategy"
        );
    }
}

#[test]
fn multi_az_topology_component_targets_required_runtime_deployments() {
    let component_kustomization =
        read_text("ops/kubernetes/components/topology-multi-az/kustomization.yaml");
    for required_deployment in [
        "corporate-catering-api",
        "corporate-catering-mcp",
        "corporate-catering-compliance-worker",
        "corporate-catering-web",
        "corporate-catering-pgbouncer-rw",
        "corporate-catering-pgbouncer-ro",
    ] {
        assert!(
            component_kustomization.contains(required_deployment),
            "topology component must patch `{required_deployment}`"
        );
    }

    for patch_file in [
        "ops/kubernetes/components/topology-multi-az/patch-deployment-api.yaml",
        "ops/kubernetes/components/topology-multi-az/patch-deployment-mcp.yaml",
        "ops/kubernetes/components/topology-multi-az/patch-deployment-compliance-worker.yaml",
        "ops/kubernetes/components/topology-multi-az/patch-deployment-web.yaml",
        "ops/kubernetes/components/topology-multi-az/patch-pgbouncer-rw.yaml",
        "ops/kubernetes/components/topology-multi-az/patch-pgbouncer-ro.yaml",
    ] {
        let patch = read_text(patch_file);
        assert!(patch.contains("topologySpreadConstraints"));
        assert!(patch.contains("topology.kubernetes.io/zone"));
        assert!(patch.contains("kubernetes.io/hostname"));
    }
}

#[test]
fn keda_component_defines_worker_scaledobject_and_removes_worker_hpa() {
    let component =
        read_text("ops/kubernetes/components/autoscaling-keda-worker/kustomization.yaml");
    assert!(component.contains("scaledobject-compliance-worker.yaml"));
    assert!(component.contains("delete-hpa-compliance-worker.yaml"));

    let scaledobject = read_text(
        "ops/kubernetes/components/autoscaling-keda-worker/scaledobject-compliance-worker.yaml",
    );
    assert!(scaledobject.contains("kind: ScaledObject"));
    assert!(scaledobject.contains("name: corporate-catering-compliance-worker"));
    assert!(scaledobject.contains("type: prometheus"));
    assert!(scaledobject.contains("type: cpu"));

    let delete_patch = read_text(
        "ops/kubernetes/components/autoscaling-keda-worker/delete-hpa-compliance-worker.yaml",
    );
    assert!(delete_patch.contains("$patch: delete"));
    assert!(delete_patch.contains("kind: HorizontalPodAutoscaler"));
    assert!(delete_patch.contains("name: corporate-catering-compliance-worker"));
}

#[test]
fn staging_overlay_declares_tuned_autoscaling_for_staged_capacity_gate() {
    let docs = read_yaml_documents("ops/kubernetes/overlays/staging/patch-autoscaling.yaml");

    let find_doc = |kind: &str, name: &str| {
        docs.iter()
            .find(|doc| {
                yaml_get(doc, "kind").as_str() == Some(kind)
                    && yaml_get(yaml_get(doc, "metadata"), "name").as_str() == Some(name)
            })
            .unwrap_or_else(|| panic!("missing {kind} `{name}` in staging autoscaling patch"))
    };

    let api_hpa = find_doc("HorizontalPodAutoscaler", "corporate-catering-api");
    let api_spec = yaml_get(api_hpa, "spec");
    assert_eq!(yaml_get(api_spec, "minReplicas").as_i64(), Some(4));
    assert_eq!(yaml_get(api_spec, "maxReplicas").as_i64(), Some(16));
    let api_behavior = yaml_get(api_spec, "behavior");
    assert_eq!(
        yaml_get(
            yaml_get(api_behavior, "scaleUp"),
            "stabilizationWindowSeconds"
        )
        .as_i64(),
        Some(15)
    );
    assert_eq!(
        yaml_get(
            yaml_get(api_behavior, "scaleDown"),
            "stabilizationWindowSeconds"
        )
        .as_i64(),
        Some(240)
    );

    let api_metrics = yaml_get(api_spec, "metrics")
        .as_sequence()
        .expect("api metrics must be sequence");
    let api_rps_target = api_metrics
        .iter()
        .find(|metric| {
            yaml_get(metric, "type").as_str() == Some("Pods")
                && yaml_get(yaml_get(yaml_get(metric, "pods"), "metric"), "name").as_str()
                    == Some("http_server_requests_per_second")
        })
        .expect("api HPA must include http_server_requests_per_second pod metric");
    assert_eq!(
        yaml_get(
            yaml_get(yaml_get(api_rps_target, "pods"), "target"),
            "averageValue"
        )
        .as_str(),
        Some("85")
    );

    let mcp_hpa = find_doc("HorizontalPodAutoscaler", "corporate-catering-mcp");
    let mcp_spec = yaml_get(mcp_hpa, "spec");
    assert_eq!(yaml_get(mcp_spec, "minReplicas").as_i64(), Some(3));
    assert_eq!(yaml_get(mcp_spec, "maxReplicas").as_i64(), Some(12));

    let scaledobject = find_doc("ScaledObject", "corporate-catering-compliance-worker");
    let scaledobject_spec = yaml_get(scaledobject, "spec");
    assert_eq!(
        yaml_get(scaledobject_spec, "pollingInterval").as_i64(),
        Some(15)
    );
    assert_eq!(
        yaml_get(scaledobject_spec, "cooldownPeriod").as_i64(),
        Some(240)
    );
    assert_eq!(
        yaml_get(scaledobject_spec, "minReplicaCount").as_i64(),
        Some(2)
    );
    assert_eq!(
        yaml_get(scaledobject_spec, "maxReplicaCount").as_i64(),
        Some(12)
    );

    let triggers = yaml_get(scaledobject_spec, "triggers")
        .as_sequence()
        .expect("scaledobject triggers must be sequence");
    let prometheus_trigger = triggers
        .iter()
        .find(|trigger| yaml_get(trigger, "type").as_str() == Some("prometheus"))
        .expect("scaledobject must include prometheus trigger");
    assert_eq!(
        yaml_get(yaml_get(prometheus_trigger, "metadata"), "threshold").as_str(),
        Some("14")
    );
}

#[test]
fn runtime_workloads_enforce_least_privilege_security_contexts() {
    for deployment_file in [
        "ops/kubernetes/base/deployment.yaml",
        "ops/kubernetes/base/deployment-mcp.yaml",
        "ops/kubernetes/base/deployment-compliance-worker.yaml",
        "ops/kubernetes/base/deployment-web.yaml",
    ] {
        let deployment = read_yaml(deployment_file);
        let pod_spec = yaml_get(yaml_get(yaml_get(&deployment, "spec"), "template"), "spec");
        assert_pod_least_privilege(pod_spec, deployment_file);

        let first_container = yaml_get(pod_spec, "containers")
            .as_sequence()
            .expect("containers must be a sequence")
            .first()
            .expect("deployment must define at least one container");
        assert_container_least_privilege(first_container, deployment_file);
    }

    let pgbouncer_docs = read_yaml_documents("ops/kubernetes/base/pgbouncer.yaml");
    let mut hardened_pgbouncer_deployments = 0;
    for doc in pgbouncer_docs {
        if yaml_get(&doc, "kind").as_str() != Some("Deployment") {
            continue;
        }
        let deployment_name = yaml_get(yaml_get(&doc, "metadata"), "name")
            .as_str()
            .expect("metadata.name must be a string");
        let resource_label = format!("ops/kubernetes/base/pgbouncer.yaml::{deployment_name}");
        let pod_spec = yaml_get(yaml_get(yaml_get(&doc, "spec"), "template"), "spec");
        assert_pod_least_privilege(pod_spec, &resource_label);

        let first_container = yaml_get(pod_spec, "containers")
            .as_sequence()
            .expect("containers must be a sequence")
            .first()
            .expect("pgbouncer deployment must define at least one container");
        assert_container_least_privilege(first_container, &resource_label);
        hardened_pgbouncer_deployments += 1;
    }
    assert_eq!(
        hardened_pgbouncer_deployments, 2,
        "both PgBouncer deployments must enforce least privilege"
    );

    let job = read_yaml("ops/kubernetes/base/job-object-storage-provision.yaml");
    let job_pod_spec = yaml_get(yaml_get(yaml_get(&job, "spec"), "template"), "spec");
    assert_pod_least_privilege(
        job_pod_spec,
        "ops/kubernetes/base/job-object-storage-provision.yaml",
    );
    let job_container = yaml_get(job_pod_spec, "containers")
        .as_sequence()
        .expect("job containers must be a sequence")
        .first()
        .expect("job must define one container");
    assert_container_least_privilege(
        job_container,
        "ops/kubernetes/base/job-object-storage-provision.yaml",
    );
}
