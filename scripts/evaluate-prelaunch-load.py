#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import math
import re
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple

import yaml


@dataclass
class IndexedMetric:
    base: str
    tags: Dict[str, str]
    payload: Dict[str, Any]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Evaluate hard-SLO + staged capacity load-gate evidence from k6 summary"
    )
    parser.add_argument("--summary", required=True)
    parser.add_argument("--hard-slo-policy", required=True)
    parser.add_argument("--thresholds", required=True)
    parser.add_argument("--staged-policy", required=True)
    parser.add_argument("--k6-script", required=True)
    parser.add_argument("--autoscaling-manifest", required=True)
    parser.add_argument("--slo-report", required=True)
    parser.add_argument("--staged-report", required=True)
    parser.add_argument("--retained-summary-path", required=True)
    return parser.parse_args()


def load_json(path: str) -> Any:
    return json.loads(Path(path).read_text(encoding="utf-8"))


def load_yaml(path: str) -> Any:
    return yaml.safe_load(Path(path).read_text(encoding="utf-8"))


def load_yaml_documents(path: str) -> List[Any]:
    return list(yaml.safe_load_all(Path(path).read_text(encoding="utf-8")))


def utc_now_iso() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def parse_metric_name(name: str) -> Tuple[str, Dict[str, str]]:
    match = re.match(r"^([^{}]+)(?:\{([^}]*)\})?$", name)
    if not match:
        return name, {}

    base = match.group(1)
    tags_raw = (match.group(2) or "").strip()
    tags: Dict[str, str] = {}
    if not tags_raw:
        return base, tags

    for chunk in tags_raw.split(","):
        if ":" not in chunk:
            continue
        key, value = chunk.split(":", 1)
        tags[key.strip()] = value.strip()
    return base, tags


def index_metrics(summary_metrics: Dict[str, Any]) -> List[IndexedMetric]:
    indexed: List[IndexedMetric] = []
    for metric_name, payload in summary_metrics.items():
        base, tags = parse_metric_name(metric_name)
        if isinstance(payload, dict):
            indexed.append(IndexedMetric(base=base, tags=tags, payload=payload))
    return indexed


def tags_match(superset: Dict[str, str], required: Dict[str, str]) -> bool:
    for key, value in required.items():
        if superset.get(key) != value:
            return False
    return True


def pick_metrics(indexed: List[IndexedMetric], base: str, required_tags: Dict[str, str]) -> List[IndexedMetric]:
    exact = [metric for metric in indexed if metric.base == base and metric.tags == required_tags]
    if exact:
        return exact
    return [
        metric
        for metric in indexed
        if metric.base == base and tags_match(metric.tags, required_tags)
    ]


def metric_number(payload: Dict[str, Any], *keys: str) -> Optional[float]:
    for key in keys:
        if key in payload and payload[key] is not None:
            try:
                return float(payload[key])
            except (TypeError, ValueError):
                pass
    values = payload.get("values")
    if isinstance(values, dict):
        for key in keys:
            if key in values and values[key] is not None:
                try:
                    return float(values[key])
                except (TypeError, ValueError):
                    pass
    return None


def aggregate_metric_payload(metrics: List[IndexedMetric]) -> Dict[str, Any]:
    if not metrics:
        return {}

    if len(metrics) == 1:
        return metrics[0].payload

    payloads = [metric.payload for metric in metrics]

    count = sum(int(metric_number(payload, "count") or 0) for payload in payloads)
    passes = sum(int(metric_number(payload, "passes") or 0) for payload in payloads)
    fails = sum(int(metric_number(payload, "fails") or 0) for payload in payloads)
    rate = sum(float(metric_number(payload, "rate") or 0.0) for payload in payloads)

    quantiles: Dict[str, float] = {}
    for quantile_key in ["p(95)", "p(99)"]:
        available = [
            value
            for payload in payloads
            if (value := metric_number(payload, quantile_key)) is not None
        ]
        if available:
            quantiles[quantile_key] = max(available)

    aggregated: Dict[str, Any] = {
        "count": count,
        "passes": passes,
        "fails": fails,
        "rate": rate,
    }
    aggregated.update(quantiles)

    value = metric_number(payloads[0], "value")
    if value is not None:
        aggregated["value"] = value

    return aggregated


def metric_count(payload: Dict[str, Any]) -> int:
    direct = metric_number(payload, "count")
    if direct is not None:
        return int(round(direct))
    passes = metric_number(payload, "passes") or 0
    fails = metric_number(payload, "fails") or 0
    return int(round(passes + fails))


def metric_rate(payload: Dict[str, Any]) -> float:
    rate = metric_number(payload, "rate")
    if rate is not None:
        return float(rate)
    value = metric_number(payload, "value")
    if value is not None:
        return float(value)
    passes = metric_number(payload, "passes")
    fails = metric_number(payload, "fails")
    if passes is not None and fails is not None:
        total = passes + fails
        if total > 0:
            return float(passes / total)
    return 0.0


def metric_quantile(payload: Dict[str, Any], quantile_key: str) -> Optional[float]:
    value = metric_number(payload, quantile_key)
    if value is not None:
        return value

    alias = {
        "p(95)": "p95",
        "p(99)": "p99",
    }.get(quantile_key)
    if alias:
        value = metric_number(payload, alias)
        if value is not None:
            return value

    return None


def to_float(value: Any) -> float:
    if isinstance(value, (float, int)):
        return float(value)
    if isinstance(value, str):
        cleaned = value.strip().strip('"')
        if not cleaned:
            raise ValueError("empty numeric string")
        return float(cleaned)
    raise ValueError(f"unsupported numeric value: {value!r}")


def evaluate_hard_slo(
    *,
    summary_metrics: Dict[str, Any],
    hard_slo_policy: Dict[str, Any],
    thresholds: Dict[str, Any],
    k6_script_raw: str,
    retained_summary_path: str,
) -> Tuple[Dict[str, Any], List[str]]:
    violations: List[str] = []
    indexed = index_metrics(summary_metrics)

    policy_scenarios = (
        hard_slo_policy.get("spec", {})
        .get("preLaunchLoadAcceptance", {})
        .get("requiredScenarios", [])
    )
    threshold_scenarios = thresholds.get("scenarios", {})

    if not policy_scenarios:
        violations.append(
            "failed to parse preLaunchLoadAcceptance.requiredScenarios from hard-slo-policy.yaml"
        )
    if not threshold_scenarios:
        violations.append("failed to parse scenarios from prelaunch-thresholds.yaml")

    report: Dict[str, Any] = {
        "generatedAt": None,
        "summaryPath": retained_summary_path,
        "scenarios": [],
        "readiness": None,
        "violations": [],
        "status": "fail",
    }

    for scenario in policy_scenarios:
        name = scenario.get("name")
        if not name:
            violations.append("hard-slo policy contains a scenario without name")
            continue

        threshold_spec = threshold_scenarios.get(name)
        if threshold_spec is None:
            violations.append(
                f"policy scenario {name} is missing from prelaunch-thresholds.yaml"
            )
            continue

        for key in [
            "minRps",
            "p95LatencyMsMax",
            "p99LatencyMsMax",
            "errorRateMax",
            "readinessSuccessRateMin",
        ]:
            policy_value = to_float(scenario.get(key))
            threshold_value = to_float(
                threshold_spec.get(key)
                if key == "minRps"
                else threshold_spec.get("thresholds", {}).get(key)
            )
            if abs(policy_value - threshold_value) > 1e-9:
                violations.append(
                    f"policy/threshold mismatch for scenario {name} on {key}"
                )

        if f'"{name}"' not in k6_script_raw and f"'{name}'" not in k6_script_raw:
            violations.append(f"k6 scenario key {name} is missing in k6-prelaunch.js")

        request_metric = aggregate_metric_payload(
            pick_metrics(indexed, "http_reqs", {"scenario": name})
        )
        duration_metric = aggregate_metric_payload(
            pick_metrics(indexed, "http_req_duration", {"scenario": name})
        )
        failed_metric = aggregate_metric_payload(
            pick_metrics(indexed, "http_req_failed", {"scenario": name})
        )

        if not request_metric:
            violations.append(f"missing request metric for scenario {name}")
            continue
        if not duration_metric:
            violations.append(f"missing duration metric for scenario {name}")
            continue
        if not failed_metric:
            violations.append(f"missing error-rate metric for scenario {name}")
            continue

        observed_rate = metric_rate(request_metric)
        observed_p95 = metric_quantile(duration_metric, "p(95)")
        observed_p99 = metric_quantile(duration_metric, "p(99)")
        observed_error_rate = metric_rate(failed_metric)

        if observed_p95 is None:
            violations.append(f"missing p95 latency quantile in summary metrics for scenario {name}")
            continue
        if observed_p99 is None:
            violations.append(f"missing p99 latency quantile in summary metrics for scenario {name}")
            continue

        report["scenarios"].append(
            {
                "name": name,
                "observed": {
                    "requestRate": observed_rate,
                    "p95LatencyMs": observed_p95,
                    "p99LatencyMs": observed_p99,
                    "errorRate": observed_error_rate,
                },
                "thresholds": {
                    "name": name,
                    "minRps": to_float(scenario.get("minRps")),
                    "p95LatencyMsMax": to_float(scenario.get("p95LatencyMsMax")),
                    "p99LatencyMsMax": to_float(scenario.get("p99LatencyMsMax")),
                    "errorRateMax": to_float(scenario.get("errorRateMax")),
                    "readinessSuccessRateMin": to_float(
                        scenario.get("readinessSuccessRateMin")
                    ),
                },
            }
        )

        if observed_rate < to_float(scenario.get("minRps")) * 0.95:
            violations.append(
                f"scenario {name} observed rate {observed_rate:.2f} rps is below required floor {to_float(scenario.get('minRps'))}"
            )
        if observed_p95 > to_float(scenario.get("p95LatencyMsMax")):
            violations.append(
                f"scenario {name} observed p95 latency {observed_p95:.2f}ms exceeds max {to_float(scenario.get('p95LatencyMsMax'))}ms"
            )
        if observed_p99 > to_float(scenario.get("p99LatencyMsMax")):
            violations.append(
                f"scenario {name} observed p99 latency {observed_p99:.2f}ms exceeds max {to_float(scenario.get('p99LatencyMsMax'))}ms"
            )
        if observed_error_rate > to_float(scenario.get("errorRateMax")):
            violations.append(
                f"scenario {name} observed error rate {observed_error_rate:.6f} exceeds max {to_float(scenario.get('errorRateMax'))}"
            )

    readiness_metric = aggregate_metric_payload(
        pick_metrics(indexed, "checks", {"check_type": "readiness"})
    )
    if not readiness_metric:
        violations.append("missing readiness check metric output")
    else:
        readiness_rate = metric_rate(readiness_metric)
        readiness_mins = [
            to_float(scenario.get("readinessSuccessRateMin"))
            for scenario in policy_scenarios
            if scenario.get("readinessSuccessRateMin") is not None
        ]
        readiness_min = min(readiness_mins) if readiness_mins else 0.999
        report["readiness"] = {
            "observedRate": readiness_rate,
            "minimumRequired": readiness_min,
        }
        if readiness_rate < readiness_min:
            violations.append(
                f"readiness success rate {readiness_rate:.5f} is below {readiness_min}"
            )

    report["generatedAt"] = utc_now_iso()
    report["violations"] = list(violations)
    report["status"] = "pass" if not violations else "fail"

    return report, violations


def find_phase_for_time(phases: List[Dict[str, Any]], elapsed_seconds: float) -> Dict[str, Any]:
    for phase in phases:
        if elapsed_seconds >= phase["startSeconds"] and elapsed_seconds < phase["endSeconds"]:
            return phase
    return phases[-1]


def find_resource_document(documents: List[Any], kind: str, name: str) -> Optional[Dict[str, Any]]:
    for document in documents:
        if not isinstance(document, dict):
            continue
        if document.get("kind") != kind:
            continue
        metadata = document.get("metadata", {})
        if metadata.get("name") == name:
            return document
    return None


def parse_scale_behavior(raw_behavior: Dict[str, Any], default_scale_up_select: str = "Max") -> Dict[str, Any]:
    def parse_direction(raw_direction: Dict[str, Any], default_select: str) -> Dict[str, Any]:
        policies = []
        for policy in raw_direction.get("policies", []) or []:
            if not isinstance(policy, dict):
                continue
            policy_type = str(policy.get("type", "")).strip()
            if policy_type not in {"Pods", "Percent"}:
                continue
            policies.append(
                {
                    "type": policy_type,
                    "value": to_float(policy.get("value", 0)),
                    "periodSeconds": max(1.0, to_float(policy.get("periodSeconds", 60))),
                }
            )

        return {
            "stabilizationWindowSeconds": int(to_float(raw_direction.get("stabilizationWindowSeconds", 0))),
            "selectPolicy": str(raw_direction.get("selectPolicy", default_select)),
            "policies": policies,
        }

    return {
        "scaleUp": parse_direction(raw_behavior.get("scaleUp", {}), default_scale_up_select),
        "scaleDown": parse_direction(raw_behavior.get("scaleDown", {}), "Min"),
    }


def compute_allowed_delta(current: int, direction_behavior: Dict[str, Any], time_step_seconds: int) -> float:
    policies = direction_behavior.get("policies", [])
    if not policies:
        return float("inf")

    deltas = []
    for policy in policies:
        period = max(1.0, float(policy["periodSeconds"]))
        if policy["type"] == "Pods":
            deltas.append(float(policy["value"]) * float(time_step_seconds) / period)
        elif policy["type"] == "Percent":
            deltas.append(
                (float(current) * float(policy["value"]) / 100.0) * float(time_step_seconds) / period
            )

    if not deltas:
        return float("inf")

    select_policy = str(direction_behavior.get("selectPolicy", "Max"))
    if select_policy == "Min":
        return min(deltas)
    return max(deltas)


def simulate_autoscaling(
    *,
    phases: List[Dict[str, Any]],
    workload_policy: Dict[str, Any],
    scaler_runtime: Dict[str, Any],
    max_direction_changes: int,
    max_end_of_phase_gap: int,
    time_step_seconds: int,
) -> Tuple[Dict[str, Any], List[str]]:
    violations: List[str] = []

    min_replicas = int(scaler_runtime["minReplicas"])
    max_replicas = int(scaler_runtime["maxReplicas"])
    target_per_replica = float(scaler_runtime["targetPerReplica"])
    behavior = scaler_runtime["behavior"]
    phase_demand = workload_policy.get("phaseDemand", {})

    for phase in phases:
        if phase["name"] not in phase_demand:
            violations.append(
                f"autoscaling phase demand missing for workload {workload_policy['name']} phase {phase['name']}"
            )

    current = max(min_replicas, int(workload_policy.get("initialReplicas", min_replicas)))
    current = min(max_replicas, current)
    previous_direction = 0
    direction_changes = 0

    total_duration = max(int(phase["endSeconds"]) for phase in phases)
    desired_history: List[Tuple[int, int]] = []
    timeline: List[Dict[str, Any]] = []

    for elapsed in range(0, total_duration, time_step_seconds):
        phase = find_phase_for_time(phases, float(elapsed))
        demand = float(phase_demand.get(phase["name"], 0.0))
        desired = int(math.ceil(demand / target_per_replica)) if target_per_replica > 0 else max_replicas
        desired = max(min_replicas, min(max_replicas, desired))
        desired_history.append((elapsed, desired))

        next_replicas = current
        direction = 0

        if desired > current:
            allowed_up = compute_allowed_delta(current, behavior["scaleUp"], time_step_seconds)
            if allowed_up > 0:
                step = max(1, int(math.ceil(allowed_up)))
                next_replicas = min(desired, current + step)
                direction = 1 if next_replicas > current else 0
        elif desired < current:
            effective_desired = desired
            stabilization = int(behavior["scaleDown"].get("stabilizationWindowSeconds", 0))
            if stabilization > 0:
                lower_bound = elapsed - stabilization
                within_window = [
                    history_desired
                    for ts, history_desired in desired_history
                    if ts >= lower_bound
                ]
                if within_window:
                    effective_desired = max(effective_desired, max(within_window))

            if effective_desired < current:
                allowed_down = compute_allowed_delta(
                    current, behavior["scaleDown"], time_step_seconds
                )
                if allowed_down > 0:
                    step = max(1, int(math.ceil(allowed_down)))
                    next_replicas = max(effective_desired, current - step)
                    direction = -1 if next_replicas < current else 0

        next_replicas = max(min_replicas, min(max_replicas, next_replicas))

        if direction != 0:
            if previous_direction != 0 and direction != previous_direction:
                direction_changes += 1
            previous_direction = direction

        timeline.append(
            {
                "timeSeconds": elapsed + time_step_seconds,
                "phase": phase["name"],
                "demand": demand,
                "desiredReplicas": desired,
                "actualReplicas": next_replicas,
            }
        )
        current = next_replicas

    phase_end_gap: Dict[str, int] = {}
    for phase in phases:
        phase_name = phase["name"]
        phase_end = int(phase["endSeconds"])
        entries = [entry for entry in timeline if entry["timeSeconds"] <= phase_end]
        if not entries:
            continue
        terminal = entries[-1]
        gap = abs(int(terminal["actualReplicas"]) - int(terminal["desiredReplicas"]))
        phase_end_gap[phase_name] = gap
        if gap > max_end_of_phase_gap:
            violations.append(
                f"autoscaling convergence gap for workload {workload_policy['name']} phase {phase_name} is {gap}, exceeds {max_end_of_phase_gap}"
            )

    if direction_changes > max_direction_changes:
        violations.append(
            f"autoscaling direction changes for workload {workload_policy['name']} is {direction_changes}, exceeds {max_direction_changes}"
        )

    simulation_result = {
        "name": workload_policy["name"],
        "kind": workload_policy["kind"],
        "targetMetricName": workload_policy["targetMetricName"],
        "targetPerReplica": target_per_replica,
        "minReplicas": min_replicas,
        "maxReplicas": max_replicas,
        "directionChanges": direction_changes,
        "phaseEndReplicaGap": phase_end_gap,
        "timeline": timeline,
    }

    return simulation_result, violations


def evaluate_staged_capacity(
    *,
    summary_metrics: Dict[str, Any],
    staged_policy: Dict[str, Any],
    autoscaling_documents: List[Any],
    retained_summary_path: str,
) -> Tuple[Dict[str, Any], List[str]]:
    violations: List[str] = []
    indexed = index_metrics(summary_metrics)

    forbidden_gates = staged_policy.get("forbiddenMandatoryGates", [])
    for gate in forbidden_gates:
        gate_value = to_float(gate.get("value", 0))
        if gate_value <= 0:
            violations.append("forbiddenMandatoryGates entries must use a positive `value`")

    phases = staged_policy.get("stagedRamp", {}).get("phases", [])
    if not phases:
        violations.append("staged policy must define stagedRamp.phases")

    phase_results: List[Dict[str, Any]] = []
    for phase in phases:
        name = phase.get("name")
        if not name:
            violations.append("stagedRamp contains a phase without name")
            continue

        start = int(to_float(phase.get("startSeconds", 0)))
        end = int(to_float(phase.get("endSeconds", 0)))
        duration = end - start
        if duration <= 0:
            violations.append(f"phase {name} has invalid time window")
            continue

        requests_metric = aggregate_metric_payload(
            pick_metrics(indexed, "http_reqs", {"staged_phase": name})
        )
        duration_metric = aggregate_metric_payload(
            pick_metrics(indexed, "http_req_duration", {"staged_phase": name})
        )
        failed_metric = aggregate_metric_payload(
            pick_metrics(indexed, "http_req_failed", {"staged_phase": name})
        )

        if not requests_metric:
            violations.append(f"missing staged-phase request metric for {name}")
            continue
        if not duration_metric:
            violations.append(f"missing staged-phase duration metric for {name}")
            continue
        if not failed_metric:
            violations.append(f"missing staged-phase error metric for {name}")
            continue

        request_count = metric_count(requests_metric)
        observed_rps = float(request_count) / float(duration)
        observed_p95 = metric_quantile(duration_metric, "p(95)")
        observed_p99 = metric_quantile(duration_metric, "p(99)")
        observed_error_rate = metric_rate(failed_metric)

        if observed_p95 is None:
            violations.append(f"missing staged-phase p95 for {name}")
            continue
        if observed_p99 is None:
            violations.append(f"missing staged-phase p99 for {name}")
            continue

        min_rps = to_float(phase.get("minRps"))
        max_rps = to_float(phase.get("maxRps"))
        p95_max = to_float(phase.get("p95LatencyMsMax"))
        p99_max = to_float(phase.get("p99LatencyMsMax"))
        error_max = to_float(phase.get("errorRateMax"))

        if observed_rps < min_rps:
            violations.append(
                f"staged phase {name} observed rps {observed_rps:.2f} below min {min_rps:.2f}"
            )
        if observed_rps > max_rps:
            violations.append(
                f"staged phase {name} observed rps {observed_rps:.2f} above max {max_rps:.2f}"
            )
        if observed_p95 > p95_max:
            violations.append(
                f"staged phase {name} p95 {observed_p95:.2f}ms exceeds {p95_max:.2f}ms"
            )
        if observed_p99 > p99_max:
            violations.append(
                f"staged phase {name} p99 {observed_p99:.2f}ms exceeds {p99_max:.2f}ms"
            )
        if observed_error_rate > error_max:
            violations.append(
                f"staged phase {name} error rate {observed_error_rate:.6f} exceeds {error_max:.6f}"
            )

        phase_results.append(
            {
                "name": name,
                "windowSeconds": {"start": start, "end": end},
                "observed": {
                    "requestCount": request_count,
                    "requestRateRps": observed_rps,
                    "p95LatencyMs": observed_p95,
                    "p99LatencyMs": observed_p99,
                    "errorRate": observed_error_rate,
                },
                "targets": {
                    "minRps": min_rps,
                    "maxRps": max_rps,
                    "p95LatencyMsMax": p95_max,
                    "p99LatencyMsMax": p99_max,
                    "errorRateMax": error_max,
                },
            }
        )

    total_duration = max([int(to_float(phase.get("endSeconds", 0))) for phase in phases] or [1])

    split_results: List[Dict[str, Any]] = []
    for split in staged_policy.get("splitValidation", []):
        split_name = split.get("name")
        load_split = split.get("loadSplit")
        if not split_name or not load_split:
            violations.append("splitValidation entries must include name and loadSplit")
            continue

        requests_metric = aggregate_metric_payload(
            pick_metrics(indexed, "http_reqs", {"load_split": str(load_split)})
        )
        duration_metric = aggregate_metric_payload(
            pick_metrics(indexed, "http_req_duration", {"load_split": str(load_split)})
        )
        failed_metric = aggregate_metric_payload(
            pick_metrics(indexed, "http_req_failed", {"load_split": str(load_split)})
        )

        if not requests_metric:
            violations.append(f"missing split request metric for {split_name}")
            continue
        if not duration_metric:
            violations.append(f"missing split duration metric for {split_name}")
            continue
        if not failed_metric:
            violations.append(f"missing split error metric for {split_name}")
            continue

        request_count = metric_count(requests_metric)
        observed_rps = float(request_count) / float(total_duration)
        observed_p95 = metric_quantile(duration_metric, "p(95)")
        observed_p99 = metric_quantile(duration_metric, "p(99)")
        observed_error_rate = metric_rate(failed_metric)

        if observed_p95 is None or observed_p99 is None:
            violations.append(f"missing split latency quantiles for {split_name}")
            continue

        min_rps = to_float(split.get("minRps"))
        p95_max = to_float(split.get("p95LatencyMsMax"))
        p99_max = to_float(split.get("p99LatencyMsMax"))
        error_max = to_float(split.get("errorRateMax"))

        if observed_rps < min_rps:
            violations.append(
                f"split {split_name} observed rps {observed_rps:.2f} below min {min_rps:.2f}"
            )
        if observed_p95 > p95_max:
            violations.append(
                f"split {split_name} p95 {observed_p95:.2f}ms exceeds {p95_max:.2f}ms"
            )
        if observed_p99 > p99_max:
            violations.append(
                f"split {split_name} p99 {observed_p99:.2f}ms exceeds {p99_max:.2f}ms"
            )
        if observed_error_rate > error_max:
            violations.append(
                f"split {split_name} error rate {observed_error_rate:.6f} exceeds {error_max:.6f}"
            )

        split_results.append(
            {
                "name": split_name,
                "loadSplit": load_split,
                "observed": {
                    "requestCount": request_count,
                    "requestRateRps": observed_rps,
                    "p95LatencyMs": observed_p95,
                    "p99LatencyMs": observed_p99,
                    "errorRate": observed_error_rate,
                },
                "targets": {
                    "minRps": min_rps,
                    "p95LatencyMsMax": p95_max,
                    "p99LatencyMsMax": p99_max,
                    "errorRateMax": error_max,
                },
            }
        )

    autoscaling_cfg = staged_policy.get("autoscalingConvergence", {})
    max_direction_changes = int(to_float(autoscaling_cfg.get("maxDirectionChanges", 2)))
    max_end_of_phase_gap = int(to_float(autoscaling_cfg.get("maxEndOfPhaseReplicaGap", 1)))
    time_step_seconds = int(to_float(autoscaling_cfg.get("timeStepSeconds", 20)))

    autoscaling_results: List[Dict[str, Any]] = []
    for workload in autoscaling_cfg.get("workloads", []):
        workload_name = workload.get("name")
        kind = workload.get("kind")
        target_metric = workload.get("targetMetricName")
        if not workload_name or not kind or not target_metric:
            violations.append(
                "autoscaling workload entry must include name, kind, and targetMetricName"
            )
            continue

        if kind == "hpa":
            document = find_resource_document(
                autoscaling_documents, "HorizontalPodAutoscaler", workload_name
            )
            if document is None:
                violations.append(
                    f"autoscaling manifest missing HorizontalPodAutoscaler {workload_name}"
                )
                continue

            spec = document.get("spec", {})
            metrics = spec.get("metrics", [])
            target_per_replica = None
            for metric in metrics:
                if metric.get("type") != "Pods":
                    continue
                pods = metric.get("pods", {})
                metric_name = ((pods.get("metric") or {}).get("name"))
                if metric_name == target_metric:
                    target_per_replica = to_float(
                        ((pods.get("target") or {}).get("averageValue"))
                    )
                    break
            if target_per_replica is None:
                violations.append(
                    f"autoscaling metric {target_metric} missing for HPA workload {workload_name}"
                )
                continue

            behavior = parse_scale_behavior(spec.get("behavior", {}))
            runtime_cfg = {
                "minReplicas": int(to_float(spec.get("minReplicas", 1))),
                "maxReplicas": int(to_float(spec.get("maxReplicas", 1))),
                "targetPerReplica": target_per_replica,
                "behavior": behavior,
            }
        elif kind == "keda":
            document = find_resource_document(autoscaling_documents, "ScaledObject", workload_name)
            if document is None:
                violations.append(
                    f"autoscaling manifest missing ScaledObject {workload_name}"
                )
                continue

            spec = document.get("spec", {})
            target_per_replica = None
            for trigger in spec.get("triggers", []) or []:
                metadata = trigger.get("metadata", {})
                if metadata.get("metricName") == target_metric:
                    target_per_replica = to_float(metadata.get("threshold"))
                    break
            if target_per_replica is None:
                violations.append(
                    f"autoscaling trigger metric {target_metric} missing for ScaledObject workload {workload_name}"
                )
                continue

            advanced = spec.get("advanced", {})
            hpa_cfg = advanced.get("horizontalPodAutoscalerConfig", {})
            behavior = parse_scale_behavior(hpa_cfg.get("behavior", {}))
            runtime_cfg = {
                "minReplicas": int(to_float(spec.get("minReplicaCount", 1))),
                "maxReplicas": int(to_float(spec.get("maxReplicaCount", 1))),
                "targetPerReplica": target_per_replica,
                "behavior": behavior,
            }
        else:
            violations.append(f"unsupported autoscaling workload kind `{kind}`")
            continue

        simulation, simulation_violations = simulate_autoscaling(
            phases=phases,
            workload_policy=workload,
            scaler_runtime=runtime_cfg,
            max_direction_changes=max_direction_changes,
            max_end_of_phase_gap=max_end_of_phase_gap,
            time_step_seconds=time_step_seconds,
        )
        autoscaling_results.append(simulation)
        violations.extend(simulation_violations)

    report = {
        "generatedAt": utc_now_iso(),
        "decisionIssueId": staged_policy.get("decisionIssueId"),
        "clarificationIds": staged_policy.get("clarificationIds", []),
        "summaryPath": retained_summary_path,
        "status": "pass" if not violations else "fail",
        "violations": list(violations),
        "stagedRamp": {
            "phases": phase_results,
        },
        "splitValidation": split_results,
        "autoscalingConvergence": {
            "maxDirectionChanges": max_direction_changes,
            "maxEndOfPhaseReplicaGap": max_end_of_phase_gap,
            "workloads": autoscaling_results,
        },
        "tuning": staged_policy.get("tuning", {}),
    }

    return report, violations


def main() -> int:
    args = parse_args()

    summary = load_json(args.summary)
    hard_slo_policy = load_yaml(args.hard_slo_policy)
    thresholds = load_yaml(args.thresholds)
    staged_policy = load_json(args.staged_policy)
    k6_script_raw = Path(args.k6_script).read_text(encoding="utf-8")
    autoscaling_documents = load_yaml_documents(args.autoscaling_manifest)

    summary_metrics = summary.get("metrics", {})
    if not isinstance(summary_metrics, dict):
        raise SystemExit("k6 summary does not contain metrics mapping")

    slo_report, slo_violations = evaluate_hard_slo(
        summary_metrics=summary_metrics,
        hard_slo_policy=hard_slo_policy,
        thresholds=thresholds,
        k6_script_raw=k6_script_raw,
        retained_summary_path=args.retained_summary_path,
    )

    staged_report, staged_violations = evaluate_staged_capacity(
        summary_metrics=summary_metrics,
        staged_policy=staged_policy,
        autoscaling_documents=autoscaling_documents,
        retained_summary_path=args.retained_summary_path,
    )

    Path(args.slo_report).write_text(
        json.dumps(slo_report, indent=2) + "\n", encoding="utf-8"
    )
    Path(args.staged_report).write_text(
        json.dumps(staged_report, indent=2) + "\n", encoding="utf-8"
    )

    violations = slo_violations + staged_violations
    return 1 if violations else 0


if __name__ == "__main__":
    raise SystemExit(main())
