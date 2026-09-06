#!/usr/bin/env python3
"""Reproduce the proposed UAT lifecycle envelope without account allowances."""

from __future__ import annotations

import csv
import io
import sys
from decimal import Decimal, ROUND_CEILING

import monthly

OUTPUT = monthly.ROOT / "uat-lifecycle-results.csv"
D = monthly.dec
SIX = Decimal("0.000001")


def render() -> bytes:
    inputs = monthly.load_json("monthly-inputs.json")
    lifecycle = monthly.load_json("uat-lifecycle-inputs.json")
    prices = monthly.load_prices()
    month = D(inputs["month_hours"])
    test = D(lifecycle["test_hours"])
    campaign = D(lifecycle["campaign_days"]) * 24 + D(lifecycle["writer_tail_hours"])
    if not 0 <= test <= campaign <= month:
        raise ValueError("test, campaign and month hours must be ordered")
    wakes = ((month - campaign) / 24).to_integral_value(rounding=ROUND_CEILING)
    node_hours = (
        test
        + (campaign - test) * D(lifecycle["hourly_maintenance_node_minutes"]) / 60
        + wakes * D(lifecycle["future_daily_node_minutes"]) / 60
        + D(lifecycle["recovery_node_hours"])
    )
    rds_hours = campaign + wakes * D(lifecycle["future_daily_rds_hours"]) + D(lifecycle["recovery_rds_hours"])
    schedule = inputs["uat"]["schedules"][lifecycle["base_schedule"]]
    usage = monthly.uat_usage(inputs, schedule)
    usage.update({
        "UAT ECS EC2 Graviton host": node_hours,
        "UAT EC2 root gp3 cost reserve": D(inputs["topology"]["root_gb"]) * node_hours / month,
        "UAT public IPv4 cost reserve": node_hours,
        "UAT RDS primary instance": rds_hours,
        "Temporary ALB": D(inputs["topology"]["alb_count"]) * test,
        "Temporary ALB LCU": D(inputs["topology"]["alb_lcu"]) * test,
        "Temporary ALB public IPv4": D(inputs["topology"]["alb_public_ipv4"]) * test,
        "CloudWatch custom metrics": D(inputs["uat"]["hourly_custom_metrics"]) * node_hours / month,
        "Seven EventBridge Scheduler schedules": D(lifecycle["scheduler_invocations"]),
    })
    items = [("base", item, monthly.price_id(item), quantity) for item, quantity in sorted(usage.items())]
    for name, task in sorted(lifecycle["fargate"].items()):
        hours = D(task["invocations"]) * max(D(task["minutes"]), D(1)) / 60
        items.extend([
            ("controller", name + " CPU", "fargate_arm_vcpu", hours * D(task["vcpu"])),
            ("controller", name + " memory", "fargate_arm_memory", hours * D(task["memory_gib"])),
            ("controller", name + " public IPv4", "public_ipv4", hours),
        ])
    items.extend([
        ("controller", "Standard state transitions", "step_functions_standard", D(lifecycle["step_functions_transitions"])),
        ("controller", "S3 conditional-write request reserve", "s3_put", D(lifecycle["controller_s3_put_equivalent_requests"])),
        ("controller", "Additional KMS requests", "kms_request", D(lifecycle["controller_kms_requests"])),
    ])
    output = io.StringIO(newline="")
    writer = csv.writer(output, lineterminator="\n")
    writer.writerow(["scope", "item", "price_id", "quantity", "usd_per_unit", "cost_usd"])
    totals = {"base": D(0), "controller": D(0)}
    for scope, item, price_id, quantity in items:
        rate = prices[price_id]
        cost = quantity * rate
        totals[scope] += cost
        writer.writerow([scope, item, price_id, quantity.quantize(SIX), rate.quantize(SIX), cost.quantize(SIX)])
    total = sum(totals.values(), D(0))
    for name, value in [*totals.items(), ("gross_total", total), ("ceiling_margin", D(lifecycle["uat_ceiling_usd"]) - total)]:
        writer.writerow(["summary", name, "", "", "", value.quantize(SIX)])
    return output.getvalue().encode("utf-8")


def main() -> None:
    generated = render()
    if sys.argv[1:] == ["--write"]:
        OUTPUT.write_bytes(generated)
    elif sys.argv[1:] == ["--check"]:
        if not OUTPUT.exists() or OUTPUT.read_bytes() != generated:
            raise SystemExit("uat-lifecycle-results.csv is stale; run uat-lifecycle.py --write")
        print("UAT lifecycle cost model is reproducible")
    else:
        raise SystemExit("use --write to regenerate or --check to verify")


if __name__ == "__main__":
    main()
