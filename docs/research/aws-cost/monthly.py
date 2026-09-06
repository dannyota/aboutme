#!/usr/bin/env python3
"""Generate the unit-based monthly AWS operating model."""

from __future__ import annotations

import csv
import io
import json
import sys
from collections import defaultdict
from decimal import Decimal
from pathlib import Path

ROOT = Path(__file__).resolve().parent
OUTPUT = ROOT / "monthly-results.csv"
SIX = Decimal("0.000001")

PRICE_IDS = {
    "CloudFront Asia viewer data": "cloudfront_asia_data",
    "CloudFront HTTPS requests": "cloudfront_asia_https",
    "CloudFront viewer function": "cloudfront_function",
    "CloudFront invalidation paths": "cloudfront_invalidation",
    "Private media S3 storage": "s3_standard_storage",
    "S3 read requests": "s3_get",
    "S3 write and list requests": "s3_put",
    "CloudWatch custom metrics": "cloudwatch_custom_metric",
    "CloudWatch standard alarms": "cloudwatch_alarm",
    "CloudWatch dashboard": "cloudwatch_dashboard",
    "CloudWatch log ingestion": "cloudwatch_log_ingest",
    "CloudWatch API requests": "cloudwatch_api_request",
    "CloudWatch Logs Insights scans": "cloudwatch_logs_insights",
    "Shared SES custom metrics": "cloudwatch_custom_metric",
    "Shared SES standard alarms": "cloudwatch_alarm",
    "CloudWatch 180-day log storage": "cloudwatch_log_storage",
    "Seven EventBridge Scheduler schedules": "eventbridge_scheduler",
    "EC2-to-RDS cross-AZ transfer": "regional_cross_az",
    "Non-CloudFront internet transfer out": "ec2_internet_data_out",
    "SES recipients": "ses_recipient",
    "SNS alarm and feedback requests": "sns_request",
    "SNS alarm email deliveries": "sns_email_delivery",
    "SNS feedback delivery to SQS": "sns_sqs_delivery",
    "SQS feedback queue requests": "sqs_standard_request",
    "SQS empty feedback polls": "sqs_standard_request",
    "Customer-managed KMS keys during active period": "kms_key",
    "KMS requests": "kms_request",
    "Shared ECR image storage": "ecr_storage",
    "OpenTofu state bucket storage": "s3_standard_storage",
    "ECS EC2 Graviton host": "ec2_t4g_medium",
    "EC2 root gp3 volume": "ebs_gp3_storage",
    "Elastic IP public IPv4": "public_ipv4",
    "RDS PostgreSQL primary instance": "rds_pg_t4g_small",
    "RDS PostgreSQL gp3 storage": "rds_pg_gp3_storage",
    "Nightly RDS restore instance": "rds_pg_t4g_small",
    "Nightly RDS restore storage": "rds_pg_gp3_storage",
    "RDS excess backup storage": "rds_backup_extra",
}

SHARED_ITEMS = {
    "Shared SES custom metrics",
    "Shared SES standard alarms",
    "Customer-managed KMS keys during active period",
    "Shared ECR image storage",
    "OpenTofu state bucket storage",
}

ALLOWANCE_GROUPS = {
    "Shared SES custom metrics": "CloudWatch custom metrics",
    "Shared SES standard alarms": "CloudWatch standard alarms",
}

UAT_PRICE_IDS = {
    "UAT ECS EC2 Graviton host": "ec2_t4g_small",
    "UAT EC2 root gp3 cost reserve": "ebs_gp3_storage",
    "UAT public IPv4 cost reserve": "public_ipv4",
    "UAT RDS primary instance": "rds_pg_t4g_micro",
    "UAT RDS gp3 storage": "rds_pg_gp3_storage",
    "UAT RDS excess backup storage": "rds_backup_extra",
    "UAT restore instance": "rds_pg_t4g_micro",
    "UAT restore storage": "rds_pg_gp3_storage",
    "Production-shape medium host": "ec2_t4g_medium",
    "Production-shape replaced small host": "ec2_t4g_small",
    "Production-shape RDS instance": "rds_pg_t4g_small",
    "Production-shape RDS storage": "rds_pg_gp3_storage",
    "Temporary ALB": "alb_hour",
    "Temporary ALB LCU": "alb_lcu_hour",
    "Temporary ALB public IPv4": "public_ipv4",
    "Temporary second production node": "ec2_t4g_medium",
    "Temporary second-node gp3": "ebs_gp3_storage",
    "Temporary second-node public IPv4": "public_ipv4",
}

TOPOLOGY_PRICE_IDS = {
    "Production ALB": "alb_hour",
    "Production ALB LCU": "alb_lcu_hour",
    "Production ALB public IPv4": "public_ipv4",
    "Production extra node": "ec2_t4g_medium",
    "Production extra-node gp3": "ebs_gp3_storage",
    "Production extra-node public IPv4": "public_ipv4",
}


def dec(value: object) -> Decimal:
    return Decimal(str(value))


def load_json(name: str) -> dict[str, object]:
    return json.loads((ROOT / name).read_text(encoding="utf-8"), parse_float=Decimal, parse_int=Decimal)


def load_prices() -> dict[str, Decimal]:
    with (ROOT / "pricing.csv").open(newline="", encoding="utf-8") as handle:
        return {row["id"]: dec(row["usd_per_unit"]) for row in csv.DictReader(handle)}


def production_usage(scenario: str) -> dict[str, Decimal]:
    usage: dict[str, Decimal] = defaultdict(Decimal)
    with (ROOT / "results.csv").open(newline="", encoding="utf-8") as handle:
        for row in csv.DictReader(handle):
            if row["scenario"] != scenario or row["option"] != "ec2_rds":
                continue
            if row["provider"] != "AWS" or row["included_in_gross_total"] != "yes":
                continue
            if row["item"] not in PRICE_IDS:
                if dec(row["cost_usd"]) != 0:
                    raise ValueError(f"unmapped nonzero production item: {row['item']}")
                continue
            usage[row["item"]] += dec(row["quantity"])
    return usage


def uat_usage(inputs: dict[str, object], schedule: dict[str, object]) -> dict[str, Decimal]:
    hours = dec(schedule["active_hours"])
    days = dec(schedule["test_days"])
    month = dec(inputs["month_hours"])
    uat = inputs["uat"]
    topology = inputs["topology"]
    proof = dec(uat["production_topology_proof_hours"])
    drill = dec(uat["production_shape_drill_hours"])
    usage: dict[str, Decimal] = defaultdict(Decimal)
    usage.update({
        "CloudFront Asia viewer data": dec(uat["cloudfront_gb"]),
        "CloudFront HTTPS requests": dec(uat["requests"]),
        "CloudFront viewer function": dec(uat["requests"]),
        "CloudFront invalidation paths": dec(uat["cloudfront_invalidation_paths"]),
        "Private media S3 storage": dec(uat["media_gb_month"]),
        "S3 read requests": dec(uat["s3_get_requests"]),
        "S3 write and list requests": dec(uat["s3_put_requests"]),
        "CloudWatch custom metrics": dec(uat["hourly_custom_metrics"]) * hours / month,
        "CloudWatch standard alarms": dec(uat["application_standard_alarms"]),
        "CloudWatch dashboard": dec(uat["persistent_dashboards"]),
        "CloudWatch log ingestion": dec(uat["log_ingest_gb"]),
        "CloudWatch API requests": dec(uat["cloudwatch_api_requests"]),
        "CloudWatch Logs Insights scans": dec(uat["logs_insights_scan_gb"]),
        "Shared SES custom metrics": dec(uat["shared_mail_custom_metrics"]),
        "Shared SES standard alarms": dec(uat["shared_mail_standard_alarms"]),
        "CloudWatch 180-day log storage": dec(uat["log_archive_gb_month"]),
        "Seven EventBridge Scheduler schedules": dec(uat["scheduler_invocations_per_active_hour"]) * hours,
        "EC2-to-RDS cross-AZ transfer": dec(uat["cross_az_db_gb"]),
        "Non-CloudFront internet transfer out": dec(uat["non_cloudfront_egress_gb"]),
        "SES recipients": dec(uat["emails"]),
        "SNS alarm and feedback requests": dec(uat["alarm_notifications"]) + dec(uat["email_feedback_events"]),
        "SNS alarm email deliveries": dec(uat["alarm_notifications"]),
        "SNS feedback delivery to SQS": dec(uat["email_feedback_events"]),
        "SQS feedback queue requests": Decimal(3) * dec(uat["email_feedback_events"]),
        "Customer-managed KMS keys during active period": dec(uat["kms_keys"]),
        "KMS requests": hours * dec(uat["kms_requests_per_active_hour"]),
        "Shared ECR image storage": dec(uat["ecr_gb_month"]),
        "OpenTofu state bucket storage": dec(uat["state_gb_month"]),
        "UAT ECS EC2 Graviton host": hours,
        "UAT EC2 root gp3 cost reserve": dec(inputs["topology"]["root_gb"]),
        "UAT public IPv4 cost reserve": month,
        "UAT RDS primary instance": hours,
        "UAT RDS gp3 storage": Decimal(20),
        "UAT RDS excess backup storage": dec(uat["rds_backup_extra_gb_month"]),
        "UAT restore instance": days * dec(uat["restore_hours_per_test_day"]),
        "UAT restore storage": Decimal(20) * days * dec(uat["restore_hours_per_test_day"]) / month,
        "Production-shape medium host": drill,
        "Production-shape replaced small host": -drill,
        "Production-shape RDS instance": drill,
        "Production-shape RDS storage": Decimal(50) * drill / month,
        "Temporary ALB": dec(topology["alb_count"]) * hours,
        "Temporary ALB LCU": dec(topology["alb_lcu"]) * hours,
        "Temporary ALB public IPv4": dec(topology["alb_public_ipv4"]) * hours,
        "Temporary second production node": proof,
        "Temporary second-node gp3": dec(inputs["topology"]["root_gb"]) * proof / month,
        "Temporary second-node public IPv4": proof,
    })
    return usage


def add_production_topology(usage: dict[str, Decimal], inputs: dict[str, object], extra_hours: Decimal) -> None:
    month = dec(inputs["month_hours"])
    topology = inputs["topology"]
    root_gb = dec(topology["root_gb"])
    usage.update({
        "Production ALB": dec(topology["alb_count"]) * month,
        "Production ALB LCU": dec(topology["alb_lcu"]) * month,
        "Production ALB public IPv4": dec(topology["alb_public_ipv4"]) * month,
        "Production extra node": extra_hours,
        "Production extra-node gp3": root_gb * extra_hours / month,
        "Production extra-node public IPv4": extra_hours,
    })


def price_id(item: str) -> str:
    return PRICE_IDS.get(item) or UAT_PRICE_IDS.get(item) or TOPOLOGY_PRICE_IDS[item]


def total(usage: dict[str, Decimal], rates: dict[str, Decimal], allowances: dict[str, object] | None) -> tuple[Decimal, Decimal]:
    gross = sum((quantity * rates[price_id(item)] for item, quantity in usage.items()), Decimal(0))
    if allowances is None:
        return gross, gross
    billable: dict[str, Decimal] = defaultdict(Decimal)
    for item, quantity in usage.items():
        billable[ALLOWANCE_GROUPS.get(item, item)] += quantity
    for item, allowance in allowances.items():
        billable[item] = max(Decimal(0), billable.get(item, Decimal(0)) - dec(allowance))
    net = sum((quantity * rates[price_id(item)] for item, quantity in billable.items()), Decimal(0))
    return gross, net


def merged(production: dict[str, Decimal], uat: dict[str, Decimal]) -> dict[str, Decimal]:
    result = defaultdict(Decimal, production)
    for item, quantity in uat.items():
        if item not in SHARED_ITEMS:
            result[item] += quantity
    return result


def record(scenario: str, gross: Decimal, net: Decimal, incremental: Decimal | None = None) -> dict[str, str]:
    return {
        "scenario": scenario,
        "gross_usd": f"{gross.quantize(SIX):f}",
        "allowance_savings_usd": f"{(gross - net).quantize(SIX):f}",
        "after_allowance_usd": f"{net.quantize(SIX):f}",
        "incremental_uat_usd": "" if incremental is None else f"{incremental.quantize(SIX):f}",
    }


def render() -> bytes:
    inputs = load_json("monthly-inputs.json")
    rates = load_prices()
    allowances = inputs["monthly_allowances"]
    records: list[dict[str, str]] = []
    productions: dict[str, tuple[dict[str, Decimal], Decimal]] = {}
    uats: dict[str, dict[str, Decimal]] = {}
    for name, spec in inputs["production"].items():
        usage = production_usage(str(spec["source_scenario"]))
        add_production_topology(usage, inputs, dec(spec["extra_node_hours"]))
        gross, net = total(usage, rates, allowances)
        productions[name] = (usage, net)
        records.append(record(f"production_{name}", gross, net))
    for name, schedule in inputs["uat"]["schedules"].items():
        usage = uat_usage(inputs, schedule)
        gross, net = total(usage, rates, allowances)
        uats[name] = usage
        records.append(record(f"uat_{name}", gross, net))
    for name, prod_name, uat_name in (
        ("low", "low", "baseline"),
        ("expected", "expected", "baseline"),
        ("high", "expected", "high"),
    ):
        prod_usage, prod_net = productions[prod_name]
        gross, net = total(merged(prod_usage, uats[uat_name]), rates, allowances)
        records.append(record(f"combined_{name}", gross, net, net - prod_net))
    buffer = io.StringIO(newline="")
    writer = csv.DictWriter(buffer, fieldnames=list(records[0]), lineterminator="\n")
    writer.writeheader()
    writer.writerows(records)
    return buffer.getvalue().encode()


def check_invariants() -> None:
    inputs = load_json("monthly-inputs.json")
    rates = load_prices()
    allowances = inputs["monthly_allowances"]
    production = {"CloudFront HTTPS requests": Decimal(10_000_000)}
    uat = {"CloudFront HTTPS requests": Decimal(1_000_000)}
    assert total(production, rates, allowances)[1] == 0
    combined = merged(production, uat)
    assert total(combined, rates, allowances)[1] == Decimal(1_000_000) * rates["cloudfront_asia_https"]
    production["Shared ECR image storage"] = Decimal(12)
    uat["Shared ECR image storage"] = Decimal(6)
    assert merged(production, uat)["Shared ECR image storage"] == Decimal(12)
    assert total(combined, rates, None)[0] == total(combined, rates, None)[1]
    production = {"CloudWatch custom metrics": Decimal(10)}
    uat = {"CloudWatch custom metrics": Decimal(5)}
    assert total(production, rates, allowances)[1] == 0
    assert total(merged(production, uat), rates, allowances)[1] == Decimal(5) * rates["cloudwatch_custom_metric"]
    pooled = {
        "CloudWatch custom metrics": Decimal(3),
        "Shared SES custom metrics": Decimal(7),
    }
    assert total(pooled, rates, allowances)[1] == 0
    baseline = uat_usage(inputs, inputs["uat"]["schedules"]["baseline"])
    assert baseline["CloudWatch standard alarms"] + baseline["Shared SES standard alarms"] == Decimal(25)
    production = production_usage("production_expected")
    combined = merged(production, baseline)
    assert combined["CloudWatch standard alarms"] + combined["Shared SES standard alarms"] == Decimal(43)
    dashboard = {"CloudWatch dashboard": Decimal(1)}
    assert total(dashboard, rates, allowances)[1] == rates["cloudwatch_dashboard"]
    one_lcu = {"Production ALB LCU": Decimal(1)}
    two_lcu = {"Production ALB LCU": Decimal(2)}
    assert total(two_lcu, rates, None)[0] - total(one_lcu, rates, None)[0] == rates["alb_lcu_hour"]


def main() -> None:
    check_invariants()
    generated = render()
    if "--check" in sys.argv:
        if not OUTPUT.exists() or OUTPUT.read_bytes() != generated:
            raise SystemExit("monthly-results.csv is stale; run monthly.py --write")
        print("monthly cost model is reproducible; arithmetic invariants passed")
    elif "--write" in sys.argv:
        OUTPUT.write_bytes(generated)
    else:
        raise SystemExit("use --write to regenerate or --check to verify")


if __name__ == "__main__":
    main()
