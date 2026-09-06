#!/usr/bin/env python3
"""Reproduce Phase 9 gross cost estimates from saved prices and workloads."""

from __future__ import annotations

import csv
import json
from decimal import Decimal
from pathlib import Path


ROOT = Path(__file__).resolve().parent
HOURS_PER_DAY = Decimal("24")


def decimal(value: object) -> Decimal:
    return Decimal(str(value))


def ceiling(value: Decimal) -> Decimal:
    return value.to_integral_value(rounding="ROUND_CEILING")


def load_prices() -> dict[str, Decimal]:
    with (ROOT / "pricing.csv").open(newline="", encoding="utf-8") as handle:
        return {
            row["id"]: decimal(row["usd_per_unit"])
            for row in csv.DictReader(handle)
            if row["usd_per_unit"]
        }


def add(
    rows: list[dict[str, str]],
    *,
    scenario: dict[str, object],
    option: str,
    provider: str,
    lifecycle: str,
    category: str,
    item: str,
    quantity: Decimal,
    unit: str,
    rate: Decimal,
    note: str,
    included: bool = True,
) -> None:
    rows.append(
        {
            "scenario": str(scenario["id"]),
            "environment": str(scenario["environment"]),
            "option": option,
            "provider": provider,
            "lifecycle": lifecycle,
            "category": category,
            "item": item,
            "quantity": f"{quantity:f}",
            "unit": unit,
            "rate_usd": f"{rate:f}",
            "cost_usd": f"{quantity * rate:.6f}",
            "included_in_gross_total": "yes" if included else "no",
            "assumption": note,
        }
    )


def add_shared(
    rows: list[dict[str, str]],
    scenario: dict[str, object],
    option: str,
    assumptions: dict[str, object],
    prices: dict[str, Decimal],
) -> None:
    hours = decimal(scenario["active_hours"])
    month_hours = decimal(assumptions["month_hours"])
    fraction = hours / month_hours
    environment = str(scenario["environment"])
    retained_months = decimal(
        assumptions[
            "uat_retained_months_after_destroy"
            if environment == "uat"
            else "production_retained_months"
        ]
    )

    add(
        rows,
        scenario=scenario,
        option=option,
        provider="AWS",
        lifecycle="active",
        category="edge",
        item="CloudFront Asia viewer data",
        quantity=decimal(scenario["cloudfront_gb"]),
        unit="GB",
        rate=prices["cloudfront_asia_data"],
        note="All viewer bytes use the Asia first-tier gross rate.",
    )
    for item, price_id in (
        ("CloudFront HTTPS requests", "cloudfront_asia_https"),
        ("CloudFront viewer function", "cloudfront_function"),
    ):
        add(
            rows,
            scenario=scenario,
            option=option,
            provider="AWS",
            lifecycle="active",
            category="edge",
            item=item,
            quantity=decimal(scenario["requests"]),
            unit="request",
            rate=prices[price_id],
            note="Gross marginal price; no allowance deducted.",
        )
    add(
        rows,
        scenario=scenario,
        option=option,
        provider="AWS",
        lifecycle="active",
        category="edge",
        item="CloudFront invalidation paths",
        quantity=decimal(scenario["cloudfront_invalidation_paths"]),
        unit="path",
        rate=prices["cloudfront_invalidation"],
        note="Publish and revocation sensitivity; gross marginal price before the monthly 1000-path allowance.",
    )

    add(
        rows,
        scenario=scenario,
        option=option,
        provider="AWS",
        lifecycle="active",
        category="storage",
        item="Private media S3 storage",
        quantity=decimal(scenario["media_gb_month"]),
        unit="GB-month",
        rate=prices["s3_standard_storage"],
        note="S3 remains private and is reached only by Go in the same Region.",
    )
    for key, item, price_id in (
        ("s3_get_requests", "S3 read requests", "s3_get"),
        ("s3_put_requests", "S3 write and list requests", "s3_put"),
    ):
        add(
            rows,
            scenario=scenario,
            option=option,
            provider="AWS",
            lifecycle="active",
            category="storage",
            item=item,
            quantity=decimal(scenario[key]),
            unit="request",
            rate=prices[price_id],
            note="Request count is a workload input, not derived from HTTP requests.",
        )

    custom_metrics = decimal(assumptions["container_insights_metric_streams"]) + decimal(
        assumptions["application_and_job_custom_metrics"]
    )
    for item, quantity, price_id, unit in (
        ("CloudWatch custom metrics", custom_metrics * fraction, "cloudwatch_custom_metric", "metric-month"),
        ("CloudWatch standard alarms", decimal(assumptions["standard_metric_alarms"]) * fraction, "cloudwatch_alarm", "alarm-month"),
        ("CloudWatch dashboard", decimal(assumptions["dashboards"]) * fraction, "cloudwatch_dashboard", "dashboard-month"),
        ("CloudWatch log ingestion", decimal(scenario["log_ingest_gb"]), "cloudwatch_log_ingest", "GB"),
        ("CloudWatch API requests", decimal(scenario["cloudwatch_api_requests"]), "cloudwatch_api_request", "request"),
        ("CloudWatch Logs Insights scans", decimal(scenario["logs_insights_scan_gb"]), "cloudwatch_logs_insights", "GB"),
    ):
        add(
            rows,
            scenario=scenario,
            option=option,
            provider="AWS",
            lifecycle="active",
            category="observability",
            item=item,
            quantity=quantity,
            unit=unit,
            rate=prices[price_id],
            note="Counts include the Phase 10 alarm inventory and standard ECS Container Insights estimate.",
        )
    for item, quantity, price_id, unit in (
        ("Shared SES custom metrics", decimal(assumptions["shared_email_custom_metrics"]), "cloudwatch_custom_metric", "metric-month"),
        ("Shared SES standard alarms", decimal(assumptions["shared_email_standard_alarms"]), "cloudwatch_alarm", "alarm-month"),
    ):
        add(
            rows,
            scenario=scenario,
            option=option,
            provider="AWS",
            lifecycle="retained",
            category="mail",
            item=item,
            quantity=quantity,
            unit=unit,
            rate=prices[price_id],
            note="Assumed persistent shared-email inventory; verify during stack adoption.",
        )
    log_storage_months = Decimal("6")
    add(
        rows,
        scenario=scenario,
        option=option,
        provider="AWS",
        lifecycle="retained",
        category="observability",
        item="CloudWatch 180-day log storage",
        quantity=decimal(scenario["log_ingest_gb"]) * log_storage_months,
        unit="GB-month",
        rate=prices["cloudwatch_log_storage"],
        note="UAT prices the full six-month retention liability for its run logs; production is steady-state at six monthly cohorts.",
    )
    if environment == "uat":
        add(
            rows,
            scenario=scenario,
            option=option,
            provider="AWS",
            lifecycle="sensitivity",
            category="observability",
            item="UAT retained-log monthly run rate",
            quantity=decimal(scenario["log_ingest_gb"]),
            unit="GB-month",
            rate=prices["cloudwatch_log_storage"],
            note="One month of the six-month liability; excluded because the full liability is already included.",
            included=False,
        )
    add(
        rows,
        scenario=scenario,
        option=option,
        provider="AWS",
        lifecycle="active",
        category="observability",
        item="Default CloudFront metrics in us-east-1",
        quantity=Decimal("1") * fraction,
        unit="distribution-month",
        rate=prices["cloudwatch_cf_standard"],
        note="The 5xx alarm uses the standard metric; no additional CloudFront metrics are enabled.",
    )
    add(
        rows,
        scenario=scenario,
        option=option,
        provider="AWS",
        lifecycle="active",
        category="observability",
        item="Additional CloudFront metrics in us-east-1",
        quantity=decimal(assumptions["cloudfront_additional_metrics"]) * fraction,
        unit="metric-month",
        rate=prices["cloudwatch_cf_additional"],
        note="Zero in the current plan; use the catalog rate if Task 10.9 enables any of the eight optional metrics.",
    )

    schedule_invocations = (
        Decimal("2") * ceiling(hours)
        + Decimal("3") * ceiling(hours / HOURS_PER_DAY)
        + ceiling(hours / Decimal("168"))
        + ceiling(hours / Decimal("6"))
    )
    add(
        rows,
        scenario=scenario,
        option=option,
        provider="AWS",
        lifecycle="active",
        category="jobs",
        item="Seven EventBridge Scheduler schedules",
        quantity=schedule_invocations,
        unit="invocation",
        rate=prices["eventbridge_scheduler"],
        note="Two hourly, one weekly, three daily including restore, and one every six hours.",
    )

    for item, quantity, price_id, note in (
        ("EC2-to-RDS cross-AZ transfer", decimal(scenario["cross_az_db_gb"]), "regional_cross_az", "Aggregate bidirectional bytes; zero if Task 10 pins the active DB and host to the same AZ."),
        ("Non-CloudFront internet transfer out", decimal(scenario["non_cloudfront_egress_gb"]), "ec2_internet_data_out", "Explicit host/control-plane sensitivity; viewer traffic remains on CloudFront."),
    ):
        add(
            rows,
            scenario=scenario,
            option=option,
            provider="AWS",
            lifecycle="active",
            category="network",
            item=item,
            quantity=quantity,
            unit="GB",
            rate=prices[price_id],
            note=note,
        )

    for item, quantity, price_id, unit in (
        ("SES recipients", decimal(scenario["emails"]), "ses_recipient", "recipient"),
        ("SNS alarm and feedback requests", decimal(scenario["alarm_notifications"]) + decimal(scenario["email_feedback_events"]), "sns_request", "request"),
        ("SNS alarm email deliveries", decimal(scenario["alarm_notifications"]), "sns_email_delivery", "notification"),
        ("SNS feedback delivery to SQS", decimal(scenario["email_feedback_events"]), "sns_sqs_delivery", "notification"),
        ("SQS feedback queue requests", decimal(scenario["email_feedback_events"]) * Decimal("3"), "sqs_standard_request", "request"),
        ("SQS empty feedback polls", decimal(assumptions["sqs_empty_polls"]), "sqs_standard_request", "request"),
    ):
        add(
            rows,
            scenario=scenario,
            option=option,
            provider="AWS",
            lifecycle="active",
            category="mail",
            item=item,
            quantity=quantity,
            unit=unit,
            rate=prices[price_id],
            note="The existing shared-email stack is adopted and persists outside UAT state.",
        )

    active_key_count = Decimal("2") if environment == "uat" else Decimal("3")
    add(
        rows,
        scenario=scenario,
        option=option,
        provider="AWS",
        lifecycle="retained",
        category="security",
        item="Customer-managed KMS keys during active period",
        quantity=active_key_count * fraction,
        unit="key-month",
        rate=prices["kms_key"],
        note="One state key plus one retained secrets key per created environment.",
    )
    if environment == "uat":
        add(
            rows,
            scenario=scenario,
            option=option,
            provider="AWS",
            lifecycle="retained",
            category="security",
            item="Customer-managed KMS keys after UAT destroy",
            quantity=active_key_count * retained_months,
            unit="key-month",
            rate=prices["kms_key"],
            note="Bootstrap keys persist after synthetic UAT state is destroyed.",
        )
    add(
        rows,
        scenario=scenario,
        option=option,
        provider="AWS",
        lifecycle="retained",
        category="security",
        item="Rotated KMS key material",
        quantity=active_key_count * decimal(assumptions["kms_billable_rotations_per_key"]),
        unit="key-version-month",
        rate=prices["kms_rotated_key_material"],
        note="Zero for fresh Phase 10 keys; first and second future rotations add one billable version per retained key.",
    )
    add(
        rows,
        scenario=scenario,
        option=option,
        provider="AWS",
        lifecycle="active",
        category="security",
        item="KMS requests",
        quantity=decimal(assumptions["kms_requests_per_active_hour"]) * hours,
        unit="request",
        rate=prices["kms_request"],
        note="Placeholder request count; verify from the UAT bill.",
    )

    for item, quantity, price_id in (
        ("Shared ECR image storage", decimal(scenario["ecr_gb_month"]), "ecr_storage"),
        ("OpenTofu state bucket storage", decimal(assumptions["state_storage_gb"]), "s3_standard_storage"),
    ):
        add(
            rows,
            scenario=scenario,
            option=option,
            provider="AWS",
            lifecycle="retained",
            category="registry-and-state",
            item=item,
            quantity=quantity,
            unit="GB-month",
            rate=prices[price_id],
            note="Bootstrap resource persists outside disposable UAT state.",
        )
    add(
        rows,
        scenario=scenario,
        option=option,
        provider="AWS",
        lifecycle="setup",
        category="registry-and-state",
        item="ECR in-Region image transfer",
        quantity=decimal(scenario["ecr_gb_month"]) * decimal(scenario["github_workflow_cycles"]),
        unit="GB",
        rate=prices["ecr_in_region_transfer"],
        note="A conservative image-volume proxy; pushes into ECR and pulls to Singapore ECS or EC2 are free.",
    )
    add(
        rows,
        scenario=scenario,
        option=option,
        provider="AWS",
        lifecycle="setup",
        category="registry-and-state",
        item="ECR external image pulls",
        quantity=decimal(assumptions["ecr_external_pull_gb"]),
        unit="GB",
        rate=prices["ec2_internet_data_out"],
        note="Zero baseline: native smoke uses its locally built image. Any later external pull must replace this input.",
    )

    github_minutes = (
        decimal(scenario["github_workflow_cycles"])
        * decimal(scenario["github_billed_jobs_per_cycle"])
        * decimal(scenario["github_avg_minutes_per_job"])
    )
    github_artifact_gb_month = (
        decimal(scenario["github_workflow_cycles"])
        * decimal(scenario["github_artifacts_per_cycle"])
        * decimal(scenario["github_artifact_gb_each"])
        * decimal(scenario["github_artifact_retention_days"])
        / Decimal("30")
    )
    for item, quantity, price_id, unit in (
        ("Private ARM64 runner", github_minutes, "github_arm64_minute", "minute"),
        ("Actions artifacts", github_artifact_gb_month, "github_artifact_storage", "GB-month"),
        ("Actions cache", decimal(scenario["github_cache_peak_gb"]), "github_cache_storage", "GB-month"),
    ):
        add(
            rows,
            scenario=scenario,
            option=option,
            provider="GitHub",
            lifecycle="setup",
            category="build",
            item=item,
            quantity=quantity,
            unit=unit,
            rate=prices[price_id],
            note="Gross private-repository charge before plan allowances; shared usage is unknown.",
        )
    add(
        rows,
        scenario=scenario,
        option=option,
        provider="GitHub",
        lifecycle="conditional",
        category="approval",
        item="GitHub Enterprise Cloud user",
        quantity=Decimal("1"),
        unit="user-month",
        rate=prices["github_enterprise_user"],
        note="Excluded unresolved purchase. Private required reviewers are unavailable on Free, Pro, and Team.",
        included=False,
    )


def add_ec2_compute(
    rows: list[dict[str, str]],
    scenario: dict[str, object],
    option: str,
    assumptions: dict[str, object],
    prices: dict[str, Decimal],
) -> None:
    environment = str(scenario["environment"])
    hours = decimal(scenario["active_hours"])
    month_hours = decimal(assumptions["month_hours"])
    host_price = "ec2_t4g_small" if environment == "uat" else "ec2_t4g_medium"
    add(
        rows,
        scenario=scenario,
        option=option,
        provider="AWS",
        lifecycle="active",
        category="compute",
        item="ECS EC2 Graviton host",
        quantity=hours,
        unit="instance-hour",
        rate=prices[host_price],
        note="t4g.small UAT or t4g.medium production, On-Demand, one host, no ALB.",
    )
    add(
        rows,
        scenario=scenario,
        option=option,
        provider="AWS",
        lifecycle="active",
        category="compute",
        item="EC2 surplus CPU credits",
        quantity=decimal(scenario["ec2_surplus_vcpu_hours"]),
        unit="vCPU-hour",
        rate=prices["ec2_t4g_surplus"],
        note="Stress sensitivity assumes 0.1 vCPU above the two-vCPU 20% baseline for every active hour.",
    )
    add(
        rows,
        scenario=scenario,
        option=option,
        provider="AWS",
        lifecycle="active",
        category="compute",
        item="EC2 root gp3 volume",
        quantity=decimal(assumptions["ec2_root_gb"]) * hours / month_hours,
        unit="GB-month",
        rate=prices["ebs_gp3_storage"],
        note="30 GB with included gp3 IOPS and throughput; no extra units.",
    )
    add(
        rows,
        scenario=scenario,
        option=option,
        provider="AWS",
        lifecycle="active",
        category="network",
        item="Elastic IP public IPv4",
        quantity=hours,
        unit="IP-hour",
        rate=prices["public_ipv4"],
        note="One public IPv4 address on the single origin host.",
    )


def add_fargate_compute(
    rows: list[dict[str, str]],
    scenario: dict[str, object],
    option: str,
    assumptions: dict[str, object],
    prices: dict[str, Decimal],
) -> None:
    hours = decimal(scenario["active_hours"])
    for item, quantity, price_id, unit in (
        ("Fargate ARM application vCPU", decimal(assumptions["fargate_app_vcpu"]) * hours, "fargate_arm_vcpu", "vCPU-hour"),
        ("Fargate ARM application memory", decimal(assumptions["fargate_app_memory_gb"]) * hours, "fargate_arm_memory", "GB-hour"),
        ("Single-AZ NLB", hours, "nlb_hour", "nlb-hour"),
        ("One NLB capacity unit", decimal(assumptions["fargate_nlcu"]) * hours, "nlb_lcu", "NLCU-hour"),
        ("Public IPv4 addresses", decimal(assumptions["fargate_public_ipv4_count"]) * hours, "public_ipv4", "IP-hour"),
        ("Route 53 private discovery zone", hours / decimal(assumptions["month_hours"]), "route53_private_zone", "hosted-zone-month"),
        ("Route 53 discovery queries", decimal(scenario["requests"]), "route53_private_query", "query"),
    ):
        add(
            rows,
            scenario=scenario,
            option=option,
            provider="AWS",
            lifecycle="active",
            category="compute" if "Fargate" in item else "network",
            item=item,
            quantity=quantity,
            unit=unit,
            rate=prices[price_id],
            note="Conditional redesign estimate; not a deployable form of the accepted contract.",
        )
    minutes = assumptions["scheduled_job_minutes"]
    days = decimal(scenario["active_hours"]) / HOURS_PER_DAY
    job_minutes = (
        ceiling(hours) * decimal(minutes["idempotency_expiry"])
        + ceiling(hours) * decimal(minutes["media_deletion"])
        + ceiling(days / Decimal("7")) * decimal(minutes["media_orphan"])
        + ceiling(days) * decimal(minutes["privacy_retention"])
        + decimal(scenario["restore_runs"]) * decimal(scenario["restore_hours_per_run"]) * Decimal("60")
        + ceiling(days) * decimal(minutes["origin_tls"])
        + ceiling(hours / Decimal("6")) * decimal(minutes["cloudfront_cidr"])
    )
    job_hours = job_minutes / Decimal("60")
    for item, quantity, price_id, unit in (
        ("Fargate scheduled-job vCPU", job_hours * decimal(assumptions["fargate_job_vcpu"]), "fargate_arm_vcpu", "vCPU-hour"),
        ("Fargate scheduled-job memory", job_hours * decimal(assumptions["fargate_job_memory_gb"]), "fargate_arm_memory", "GB-hour"),
    ):
        add(
            rows,
            scenario=scenario,
            option=option,
            provider="AWS",
            lifecycle="restore-and-jobs",
            category="jobs",
            item=item,
            quantity=quantity,
            unit=unit,
            rate=prices[price_id],
            note="Per-run duration assumption includes the task waiting through the nightly restore.",
        )


def add_database(
    rows: list[dict[str, str]],
    scenario: dict[str, object],
    option: str,
    assumptions: dict[str, object],
    prices: dict[str, Decimal],
    aurora: bool,
) -> None:
    environment = str(scenario["environment"])
    hours = decimal(scenario["active_hours"])
    month_hours = decimal(assumptions["month_hours"])
    storage_gb = Decimal("20") if environment == "uat" else Decimal("50")
    restore_hours = decimal(scenario["restore_runs"]) * decimal(scenario["restore_hours_per_run"])
    if aurora:
        average_acu = decimal(scenario["aurora_average_acu"])
        for lifecycle, item, quantity, price_id, unit, note in (
            ("active", "Aurora Serverless v2 primary capacity", average_acu * hours, "aurora_serverless_v2_acu", "ACU-hour", "Average ACU is an explicit scenario input; no auto-pause assumed."),
            ("active", "Aurora cluster storage", storage_gb * hours / month_hours, "aurora_storage", "GB-month", "Same provisioned data size as the RDS comparison."),
            ("active", "Aurora I/O", decimal(scenario["aurora_io_requests"]), "aurora_io", "I/O", "I/O is an explicit workload input because HTTP requests do not determine database I/O."),
            ("restore", "Nightly Aurora restore capacity", Decimal("0.5") * restore_hours, "aurora_serverless_v2_acu", "ACU-hour", "Conditional equivalent assumes 0.5 ACU for the scenario restore duration."),
            ("restore", "Nightly Aurora restore storage", storage_gb * restore_hours / month_hours, "aurora_storage", "GB-month", "Temporary cluster storage is prorated for restore runtime."),
            ("retained", "Aurora excess backup storage", decimal(scenario["aurora_backup_extra_gb_month"]), "aurora_backup_extra", "GB-month", "Scenario sensitivity because 30-day changed-block volume is not measured."),
        ):
            add(rows, scenario=scenario, option=option, provider="AWS", lifecycle=lifecycle, category="database", item=item, quantity=quantity, unit=unit, rate=prices[price_id], note=note)
    else:
        instance_price = "rds_pg_t4g_micro" if environment == "uat" else "rds_pg_t4g_small"
        for lifecycle, item, quantity, price_id, unit, note in (
            ("active", "RDS PostgreSQL primary instance", hours, instance_price, "instance-hour", "Single-AZ db.t4g.micro UAT or db.t4g.small production."),
            ("active", "RDS PostgreSQL gp3 storage", storage_gb * hours / month_hours, "rds_pg_gp3_storage", "GB-month", "20 GB UAT or 50 GB production with 30-day backup retention."),
            ("active", "RDS surplus CPU credits", decimal(scenario["rds_surplus_vcpu_hours"]), "rds_t4g_surplus", "vCPU-hour", "Explicit T4g unlimited-mode sensitivity; low and expected cases use zero because hosted CPU is unknown."),
            ("restore", "Nightly RDS restore instance", restore_hours, instance_price, "instance-hour", "Real same-class temporary restore every night; scenario duration ranges one to four hours."),
            ("restore", "Nightly RDS restore storage", storage_gb * restore_hours / month_hours, "rds_pg_gp3_storage", "GB-month", "Temporary same-size storage is prorated for restore runtime."),
            ("retained", "RDS excess backup storage", decimal(scenario["rds_backup_extra_gb_month"]), "rds_backup_extra", "GB-month", "Scenario sensitivity because 30-day changed-block volume is not measured."),
        ):
            add(rows, scenario=scenario, option=option, provider="AWS", lifecycle=lifecycle, category="database", item=item, quantity=quantity, unit=unit, rate=prices[price_id], note=note)


def add_option_sensitivities(
    rows: list[dict[str, str]],
    scenario: dict[str, object],
    option: str,
    assumptions: dict[str, object],
    prices: dict[str, Decimal],
) -> None:
    if str(scenario["environment"]) != "uat":
        return
    month_hours = decimal(assumptions["month_hours"])
    idle_hours = Decimal("168")
    rds_storage = Decimal("20") * idle_hours / month_hours * prices["rds_pg_gp3_storage"]
    if option == "ec2_rds":
        idle_cost = (
            Decimal("30") * idle_hours / month_hours * prices["ebs_gp3_storage"]
            + idle_hours * prices["public_ipv4"]
            + rds_storage
        )
    elif option == "ec2_aurora":
        idle_cost = (
            Decimal("30") * idle_hours / month_hours * prices["ebs_gp3_storage"]
            + idle_hours * prices["public_ipv4"]
            + Decimal("20") * idle_hours / month_hours * prices["aurora_storage"]
            + Decimal("0.5") * idle_hours * prices["aurora_serverless_v2_acu"]
        )
    else:
        idle_cost = (
            idle_hours * prices["nlb_hour"]
            + idle_hours * decimal(assumptions["fargate_nlcu"]) * prices["nlb_lcu"]
            + idle_hours * prices["public_ipv4"]
            + rds_storage
        )
    add(
        rows,
        scenario=scenario,
        option=option,
        provider="AWS",
        lifecycle="idle",
        category="sensitivity",
        item="IDLE_UAT_7_DAY_SENSITIVITY",
        quantity=Decimal("1"),
        unit="total",
        rate=idle_cost,
        note="Excluded seven-day idle infrastructure cost before retained KMS, ECR, and logs; RDS then restarts automatically.",
        included=False,
    )
    if option == "fargate_rds":
        nat_cost = (
            decimal(scenario["active_hours"]) * prices["nat_gateway_hour"]
            + (
                decimal(scenario["non_cloudfront_egress_gb"])
                + decimal(scenario["ecr_gb_month"])
                * decimal(scenario["github_workflow_cycles"])
            )
            * prices["nat_gateway_data"]
        )
        add(
            rows,
            scenario=scenario,
            option=option,
            provider="AWS",
            lifecycle="sensitivity",
            category="network",
            item="PRIVATE_FARGATE_NAT_SENSITIVITY",
            quantity=Decimal("1"),
            unit="total",
            rate=nat_cost,
            note="Excluded alternative to public task IPs; one NAT gateway plus scenario data processing.",
            included=False,
        )

    drill_hours = decimal(scenario["production_shape_drill_hours"])
    if drill_hours == 0:
        return
    if option == "ec2_rds":
        drill_cost = (
            drill_hours * (prices["ec2_t4g_medium"] - prices["ec2_t4g_small"])
            + drill_hours * prices["rds_pg_t4g_small"]
            + Decimal("50") * drill_hours / month_hours * prices["rds_pg_gp3_storage"]
        )
    elif option == "fargate_rds":
        drill_cost = (
            drill_hours * prices["rds_pg_t4g_small"]
            + Decimal("50") * drill_hours / month_hours * prices["rds_pg_gp3_storage"]
        )
    else:
        drill_cost = (
            Decimal("0.5") * drill_hours * prices["aurora_serverless_v2_acu"]
            + Decimal("50") * drill_hours / month_hours * prices["aurora_storage"]
        )
    add(
        rows,
        scenario=scenario,
        option=option,
        provider="AWS",
        lifecycle="setup",
        category="drill",
        item="Production-shape drill increment",
        quantity=Decimal("1"),
        unit="total",
        rate=drill_cost,
        note="Four-hour UAT sensitivity: host class delta plus a separate temporary production-size database, deleted after the drill; no in-place storage shrink assumed.",
    )
def calculate() -> list[dict[str, str]]:
    prices = load_prices()
    with (ROOT / "scenarios.json").open(encoding="utf-8") as handle:
        data = json.load(handle, parse_float=Decimal, parse_int=Decimal)
    assumptions = dict(data["assumptions"])
    assumptions["month_hours"] = data["month_hours"]
    rows: list[dict[str, str]] = []
    for scenario in data["scenarios"]:
        for option in ("ec2_rds", "ec2_aurora", "fargate_rds"):
            add_shared(rows, scenario, option, assumptions, prices)
            if option.startswith("ec2_"):
                add_ec2_compute(rows, scenario, option, assumptions, prices)
            else:
                add_fargate_compute(rows, scenario, option, assumptions, prices)
            add_database(rows, scenario, option, assumptions, prices, aurora=option == "ec2_aurora")
            add_option_sensitivities(rows, scenario, option, assumptions, prices)
            option_rows = [
                row
                for row in rows
                if row["scenario"] == str(scenario["id"])
                and row["option"] == option
                and row["included_in_gross_total"] == "yes"
            ]
            for provider in ("AWS", "GitHub"):
                subtotal = sum(
                    (decimal(row["cost_usd"]) for row in option_rows if row["provider"] == provider),
                    Decimal("0"),
                )
                add(
                    rows,
                    scenario=scenario,
                    option=option,
                    provider=provider,
                    lifecycle="summary",
                    category="summary",
                    item=f"TOTAL_{provider.upper()}",
                    quantity=Decimal("1"),
                    unit="total",
                    rate=subtotal,
                    note="Gross subtotal before allowances, discounts, credits, tax, and the conditional Enterprise license.",
                    included=False,
                )
            gross = sum((decimal(row["cost_usd"]) for row in option_rows), Decimal("0"))
            add(
                rows,
                scenario=scenario,
                option=option,
                provider="All",
                lifecycle="summary",
                category="summary",
                item="TOTAL_GROSS",
                quantity=Decimal("1"),
                unit="total",
                rate=gross,
                note="AWS plus gross GitHub usage; conditional Enterprise Cloud is excluded.",
                included=False,
            )
    return rows


def main() -> None:
    rows = calculate()
    fieldnames = list(rows[0])
    with (ROOT / "results.csv").open("w", newline="", encoding="utf-8") as handle:
        writer = csv.DictWriter(handle, fieldnames=fieldnames, lineterminator="\n")
        writer.writeheader()
        writer.writerows(rows)


if __name__ == "__main__":
    main()
