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
