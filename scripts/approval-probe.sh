#!/usr/bin/env bash
# Anonymous feasibility only. The independently chosen SHA and hash are public.
set -Eeuo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
exec python3 -I - "$ROOT/scripts/approval-probe.json" "$@" <<'PY'
import datetime
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
from urllib.parse import parse_qs, urlsplit

MAX_REQUESTS = 12
RESERVE = 12
MAX_BYTES = 1048576
requests_used = 0
rate = {"limit": None, "remaining": None, "reset": None}

class Refusal(Exception):
    pass

def require(condition, reason):
    if not condition:
        raise Refusal(reason)

def integer(value, positive=False):
    return type(value) is int and (1 if positive else 0) <= value <= 2**53 - 1

def fields(value, required):
    require(isinstance(value, dict) and all(key in value for key in required), "MISSING_FIELDS")
    return value

def unique_object(pairs):
    value = {}
    for key, item in pairs:
        require(key not in value, "JSON_INVALID")
        value[key] = item
    return value

def bad_constant(_value):
    raise Refusal("JSON_INVALID")

def decode(source):
    try:
        return json.loads(source, object_pairs_hook=unique_object, parse_constant=bad_constant)
    except (ValueError, UnicodeError, RecursionError):
        raise Refusal("JSON_INVALID") from None

def response_headers(source):
    # A proxy or an HTTP informational response can precede the final block.
    blocks = re.split(r"\r?\n\r?\n", source.strip())
    final = blocks[-1].splitlines()
    require(final and re.fullmatch(r"HTTP/\S+ \d{3}(?: .*)?", final[0]), "RATE_HEADERS")
    headers = {}
    for line in final[1:]:
        require(":" in line, "RATE_HEADERS")
        key, value = line.split(":", 1)
        key = key.lower()
        if key in {"link", "x-ratelimit-limit", "x-ratelimit-remaining",
                   "x-ratelimit-reset", "x-ratelimit-resource"}:
            require(key not in headers, "RATE_HEADERS")
            headers[key] = value.strip()
    return headers

def fetch(url):
    global requests_used, rate
    require(requests_used < MAX_REQUESTS, "REQUEST_BUDGET")
    require(rate["remaining"] is None or rate["remaining"] > RESERVE, "BUDGET")
    with tempfile.TemporaryDirectory(prefix="aboutme-approval-") as temporary:
        header_path = Path(temporary) / "headers"
        body_path = Path(temporary) / "body"
        requests_used += 1
        try:
            result = subprocess.run([
                "curl", "--disable", "--silent", "--show-error", "--proxy", "",
                "--proto", "=https", "--connect-timeout", "5", "--max-time", "15",
                "--max-filesize", str(MAX_BYTES), "--request", "GET",
                "--header", "Accept: application/vnd.github+json",
                "--header", "X-GitHub-Api-Version: 2026-03-10",
                "--dump-header", str(header_path), "--output", str(body_path),
                "--write-out", "%{http_code}", url,
            ], env={"PATH": os.environ.get("PATH", "/usr/bin:/bin"), "LC_ALL": "C"},
                stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=20)
        except (OSError, subprocess.TimeoutExpired):
            raise Refusal("TRANSPORT") from None
        # Preserve available rate counters even when curl reports a transport error.
        headers = response_headers(header_path.read_text(encoding="ascii"))
        values = [headers.get("x-ratelimit-" + key, "") for key in rate]
        require(all(re.fullmatch(r"[0-9]{1,16}", value) for value in values), "RATE_HEADERS")
        rate = dict(zip(rate, map(int, values)))
        require(rate["limit"] == 60 and 0 <= rate["remaining"] <= 60 and
                rate["reset"] > 0 and headers.get("x-ratelimit-resource") == "core", "RATE_HEADERS")
        require(result.returncode == 0, "TRANSPORT")
        status = result.stdout.decode("ascii")
        require(status not in {"403", "429"} or rate["remaining"] != 0, "HTTP_RATE_LIMIT")
        require(status == "200", "HTTP_ERROR")
        require(rate["remaining"] >= RESERVE, "BUDGET")
        require(body_path.stat().st_size <= MAX_BYTES, "RESPONSE_SIZE")
        return decode(body_path.read_bytes()), headers.get("link")

def next_page(link, endpoint, page, per_page):
    if link is None:
        return None
    require(bool(link), "PAGINATION")
    links = {}
    for entry in link.split(","):
        match = re.fullmatch(r'\s*<([^<>]+)>;\s*rel="(next|prev|first|last)"\s*', entry)
        require(match is not None, "PAGINATION")
        url, relation = match.groups()
        require(relation not in links, "PAGINATION")
        parsed = urlsplit(url)
        expected = urlsplit(endpoint)
        query = parse_qs(parsed.query, keep_blank_values=True)
        require(parsed.scheme == "https" and parsed.netloc == "api.github.com" and
                parsed.path == expected.path and not parsed.fragment and
                set(query) == {"page", "per_page"} and
                query["per_page"] == [str(per_page)] and len(query["page"]) == 1 and
                re.fullmatch(r"[1-9][0-9]{0,2}", query["page"][0]), "PAGINATION")
        number = int(query["page"][0])
        if relation == "next":
            require(number == page + 1, "PAGINATION")
        elif relation == "prev":
            require(page > 1 and number == page - 1, "PAGINATION")
        elif relation == "first":
            require(number == 1, "PAGINATION")
        else:
            require(number >= page, "PAGINATION")
        links[relation] = number
    require("last" not in links or links["last"] <= page or "next" in links, "PAGINATION")
    return links.get("next")

def collect(endpoint, per_page, jobs=False):
    items = []
    page = 1
    total = None
    while True:
        body, link = fetch(f"{endpoint}?per_page={per_page}&page={page}")
        if jobs:
            fields(body, ["jobs", "total_count"])
            require(integer(body["total_count"]), "JOBS_COUNT")
            require(total is None or total == body["total_count"], "JOBS_COUNT")
            total = body["total_count"]
            body = body["jobs"]
        require(isinstance(body, list), "MISSING_FIELDS")
        require(len(body) <= per_page, "PAGINATION")
        items.extend(body)
        following = next_page(link, endpoint, page, per_page)
        if following is None:
            require(not jobs or len(items) == total, "JOBS_COUNT")
            return items
        require(len(body) == per_page, "PAGINATION")
        page = following

def inspect_run(run, run_id, sha, pins):
    fields(run, ["id", "repository", "workflow_id", "path", "event", "head_branch",
                 "head_sha", "run_attempt", "status", "conclusion", "actor", "triggering_actor"])
    fields(run["repository"], ["id", "full_name"])
    require(all(integer(value, True) for value in [run["id"], run["repository"]["id"],
                run["workflow_id"], run["run_attempt"]]), "MISSING_FIELDS")
    require(run["id"] == run_id and run["repository"]["id"] == pins["repository_id"] and
            run["repository"]["full_name"] == "dannyota/aboutme" and
            run["workflow_id"] == pins["workflow_id"] and
            run["path"] == ".github/workflows/approval-probe.yml" and
            run["event"] == "workflow_dispatch" and run["head_branch"] == "main" and
            run["head_sha"] == sha, "RUN_IDENTITY")
    require(run["run_attempt"] == 1, "RUN_ATTEMPT")
    for key in ["actor", "triggering_actor"]:
        fields(run[key], ["id", "type"])
        require(integer(run[key]["id"], True) and run[key]["id"] in pins["maintainer_ids"] and
                run[key]["type"] == "User", "RUN_ACTOR")
    require(run["conclusion"] != "cancelled", "RUN_CANCELLED")
    require(run["status"] == "completed", "RUN_INCOMPLETE")

def inspect_approvals(rows, pins, comment):
    relevant = []
    for row in rows:
        fields(row, ["state", "comment", "environments", "user"])
        require(row["state"] in {"approved", "rejected"} and isinstance(row["comment"], str) and
                isinstance(row["environments"], list) and row["environments"], "APPROVAL_FIELDS")
        fields(row["user"], ["id", "type"])
        require(integer(row["user"]["id"], True) and row["user"]["type"] == "User", "APPROVAL_IDENTITY")
        seen = set()
        matched = False
        for environment in row["environments"]:
            fields(environment, ["id", "name"])
            require(integer(environment["id"], True) and isinstance(environment["name"], str) and
                    environment["name"] and environment["id"] not in seen, "ENVIRONMENT")
            seen.add(environment["id"])
            if environment["id"] == pins["environment_id"] or environment["name"] == "approval-probe":
                require(environment["id"] == pins["environment_id"] and
                        environment["name"] == "approval-probe", "ENVIRONMENT")
                matched = True
        if matched:
            relevant.append(row)
    # No newest/oldest inference: the review API exposes no review timestamp.
    require(not any(row["state"] == "rejected" for row in relevant), "APPROVAL_REJECTED")
    require(all(row["comment"] == comment for row in relevant), "APPROVAL_COMMENT")
    require(all(row["user"]["id"] in pins["maintainer_ids"] for row in relevant), "APPROVAL_IDENTITY")
    require(bool(relevant), "APPROVAL_MISSING")
    require(len(relevant) == 1, "APPROVAL_COUNT")

def timestamp(value):
    require(isinstance(value, str) and re.fullmatch(r"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ", value), "MISSING_FIELDS")
    try:
        return datetime.datetime.strptime(value, "%Y-%m-%dT%H:%M:%SZ")
    except ValueError:
        raise Refusal("JOBS_ORDER") from None

def inspect_jobs(rows, run_id, sha):
    require(len(rows) == 2, "JOBS_COUNT")
    for row in rows:
        fields(row, ["id", "run_id", "head_sha", "name", "status", "conclusion", "started_at", "completed_at"])
        require(integer(row["id"], True) and integer(row["run_id"], True), "MISSING_FIELDS")
        require(row["run_id"] == run_id and row["head_sha"] == sha and
                row["status"] == "completed" and row["conclusion"] == "success", "JOBS")
        # The endpoint binds attempt one; do not require an undocumented field.
        if "run_attempt" in row:
            require(type(row["run_attempt"]) is int and row["run_attempt"] == 1, "JOBS")
    require(len({row["id"] for row in rows}) == 2 and
            sorted(row["name"] for row in rows) == ["Prepare request", "Record approval"], "JOBS_COUNT")
    ordered = {row["name"]: row for row in rows}
    prepare, record = ordered["Prepare request"], ordered["Record approval"]
    require(timestamp(prepare["started_at"]) <= timestamp(prepare["completed_at"]) <=
            timestamp(record["started_at"]) <= timestamp(record["completed_at"]), "JOBS_ORDER")

reason = "ACCEPTED"
try:
    require(len(sys.argv) in {5, 6}, "INPUT_INVALID")
    config_path, run_text, sha, request_hash = sys.argv[1:5]
    per_page_text = sys.argv[5] if len(sys.argv) == 6 else "100"
    require(re.fullmatch(r"[1-9][0-9]{0,15}", run_text) and integer(int(run_text), True) and
            re.fullmatch(r"[0-9a-f]{40}", sha) and re.fullmatch(r"[0-9a-f]{64}", request_hash) and
            re.fullmatch(r"[1-9][0-9]{0,2}", per_page_text) and int(per_page_text) <= 100, "INPUT_INVALID")
    try:
        pins = decode(Path(config_path).read_bytes())
        require(isinstance(pins, dict) and set(pins) == {
            "repository_id", "workflow_id", "environment_id", "maintainer_ids"}, "CONFIG_INVALID")
        require(all(integer(pins[key], True) for key in ["repository_id", "workflow_id", "environment_id"]) and
                isinstance(pins["maintainer_ids"], list) and pins["maintainer_ids"] and
                all(integer(value, True) for value in pins["maintainer_ids"]) and
                len(set(pins["maintainer_ids"])) == len(pins["maintainer_ids"]), "CONFIG_INVALID")
    except (Refusal, OSError):
        raise Refusal("CONFIG_INVALID") from None
    run_id, per_page = int(run_text), int(per_page_text)
    endpoint = f"https://api.github.com/repos/dannyota/aboutme/actions/runs/{run_id}"
    comment = f"approve production deploy v0.0.0 run {run_id} attempt 1 sha256:{request_hash}"
    run, link = fetch(endpoint)
    require(link is None, "PAGINATION")
    inspect_run(run, run_id, sha, pins)
    inspect_approvals(collect(endpoint + "/approvals", per_page), pins, comment)
    inspect_jobs(collect(endpoint + "/attempts/1/jobs", 100, jobs=True), run_id, sha)
    require(run["conclusion"] == "success", "RUN_UNSUCCESSFUL")
except Refusal as error:
    reason = str(error)
except (OSError, ValueError, TypeError, KeyError, RecursionError):
    reason = "MISSING_FIELDS"

accepted = reason == "ACCEPTED"
print(json.dumps({"verdict": "accept" if accepted else "refuse", "reason": reason,
                  "requests_used": requests_used, "rate_limit": rate}, separators=(",", ":")))
sys.exit(0 if accepted else 1)
PY
