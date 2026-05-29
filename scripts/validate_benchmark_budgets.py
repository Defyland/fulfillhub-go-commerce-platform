#!/usr/bin/env python3

import json
import pathlib
import sys


BUDGETS = {
    "smoke": {"p95_ms": 250.0, "error_rate": 0.01},
    "load": {"p95_ms": 500.0, "error_rate": 0.01},
    "stress": {"p95_ms": 1000.0, "error_rate": 0.02},
    "spike": {"p95_ms": 1000.0, "error_rate": 0.02},
}


def scenario_for(path: pathlib.Path) -> str | None:
    name = path.name
    for scenario in BUDGETS:
        if f"k6-{scenario}-summary.json" in name:
            return scenario
    return None


def metric_value(summary: dict, metric: str, key: str) -> float:
    try:
        return float(summary["metrics"][metric][key])
    except KeyError as exc:
        raise ValueError(f"missing metrics.{metric}.{key}") from exc


def validate(path: pathlib.Path) -> list[str]:
    scenario = scenario_for(path)
    if scenario is None:
        return []
    summary = json.loads(path.read_text())
    budget = BUDGETS[scenario]
    p95 = metric_value(summary, "http_req_duration", "p(95)")
    error_rate = metric_value(summary, "http_req_failed", "value")
    failures = []
    if p95 > budget["p95_ms"]:
        failures.append(f"{path}: p95 {p95:.3f}ms exceeds {budget['p95_ms']:.3f}ms for {scenario}")
    if error_rate >= budget["error_rate"]:
        failures.append(f"{path}: error rate {error_rate:.6f} exceeds {budget['error_rate']:.6f} for {scenario}")
    return failures


def main() -> int:
    root = pathlib.Path("benchmarks/results")
    summaries = sorted(root.rglob("k6-*-summary.json")) + sorted(root.glob("*-k6-*-summary.json"))
    seen = set()
    failures: list[str] = []
    checked = 0
    for path in summaries:
        if path in seen:
            continue
        seen.add(path)
        scenario = scenario_for(path)
        if scenario is None:
            continue
        checked += 1
        failures.extend(validate(path))
    if checked == 0:
        print("no k6 summary files found under benchmarks/results", file=sys.stderr)
        return 1
    if failures:
        print("\n".join(failures), file=sys.stderr)
        return 1
    print(f"validated {checked} k6 summary files against benchmark budgets")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
