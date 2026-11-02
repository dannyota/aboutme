#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
python3 -I - "$ROOT" <<'PY'
import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

root = Path(sys.argv[1])
sha = "a" * 40
request_hash = "b" * 64
run_id = 123
base = f"https://api.github.com/repos/dannyota/aboutme/actions/runs/{run_id}"
comment = f"approve production deploy v0.0.0 run {run_id} attempt 1 sha256:{request_hash}"
# These identities are synthetic and have no connection to enrolled accounts.
pins = {"repository_id": 11, "workflow_id": 22, "environment_id": 33,
        "maintainer_ids": [44, 55]}
owner, reviewer = pins["maintainer_ids"]
user = {"id": reviewer, "type": "User"}
run = {"id": run_id, "repository": {"id": pins["repository_id"],
       "full_name": "dannyota/aboutme"}, "workflow_id": pins["workflow_id"],
       "path": ".github/workflows/approval-probe.yml", "event": "workflow_dispatch",
       "head_branch": "main", "head_sha": sha, "run_attempt": 1,
       "status": "completed", "conclusion": "success",
       "actor": {"id": owner, "type": "User"},
       "triggering_actor": {"id": owner, "type": "User"}}
approval = {"state": "approved", "comment": comment, "user": user,
            "environments": [{"id": pins["environment_id"], "name": "approval-probe"}]}
jobs = {"total_count": 2, "jobs": [
    {"id": index + 1, "run_id": run_id, "head_sha": sha, "name": name,
     "status": "completed", "conclusion": "success",
     "started_at": start, "completed_at": end}
    for index, (name, start, end) in enumerate([
        ("Prepare request", "2026-11-01T00:00:00Z", "2026-11-01T00:00:01Z"),
        ("Record approval", "2026-11-01T00:00:02Z", "2026-11-01T00:00:03Z")])]}

fake_curl = r'''#!/usr/bin/env python3
import json
import os
from pathlib import Path
import sys

directory = Path(__file__).resolve().parent.parent
args = sys.argv[1:]
assert args[0] == "--disable"
assert not any(key in os.environ for key in ["GH_TOKEN", "GITHUB_TOKEN", "HTTPS_PROXY"])
assert not any("authorization" in arg.lower() or "netrc" in arg.lower() for arg in args)
assert "--location" not in args and "-L" not in args
assert args[args.index("--proxy") + 1] == ""
url = args[-1]
log = directory / "requests"
with log.open("a") as output:
    output.write(url + "\n")
responses = json.loads((directory / "responses.json").read_text())
entry = responses[url]
if isinstance(entry, list):
    occurrence = log.read_text().splitlines().count(url)
    entry = entry[occurrence - 1]
Path(args[args.index("--dump-header") + 1]).write_text(entry["headers"])
Path(args[args.index("--output") + 1]).write_text(entry["body"])
sys.stdout.write(str(entry["status"]))
sys.stderr.write("synthetic-private-marker")
sys.exit(entry.get("exit", 0))
'''

def response(body, remaining, status=200, link=None):
    headers = (f"HTTP/2 {status}\r\nX-RateLimit-Limit: 60\r\n"
               f"X-RateLimit-Remaining: {remaining}\r\n"
               "X-RateLimit-Reset: 2000000000\r\nX-RateLimit-Resource: core\r\n")
    if link is not None:
        headers += f"Link: {link}\r\n"
    return {"body": json.dumps(body), "headers": headers + "\r\n", "status": status}

def fixture():
    return {base: response(copy.deepcopy(run), 59),
            base + "/approvals?per_page=100&page=1": response([copy.deepcopy(approval)], 58),
            base + "/attempts/1/jobs?per_page=100&page=1": response(copy.deepcopy(jobs), 57)}

count = 0
def check(name, reason="ACCEPTED", mutate=None, expected_requests=None, remaining="default",
          config=None, args=None, rate_known=True, expected_urls=None):
    global count
    if expected_requests is None:
        expected_requests = 4 if reason == "ACCEPTED" else 3
    data = fixture()
    if mutate:
        mutate(data)
    if isinstance(data[base], dict):
        final = copy.deepcopy(data[base])
        final["headers"] = response(None, 60 - expected_requests)["headers"]
        data[base] = [data[base], final]
    if remaining == "default":
        remaining = 60 - expected_requests if expected_requests else None
    with tempfile.TemporaryDirectory(prefix="aboutme-approval-test-") as temporary:
        directory = Path(temporary)
        (directory / "scripts").mkdir()
        (directory / "bin").mkdir()
        script = directory / "scripts/approval-probe.sh"
        script.write_bytes((root / "scripts/approval-probe.sh").read_bytes())
        (directory / "scripts/approval-probe.json").write_text(json.dumps(pins if config is None else config))
        curl = directory / "bin/curl"
        curl.write_text(fake_curl)
        curl.chmod(0o700)
        (directory / "responses.json").write_text(json.dumps(data))
        environment = dict(os.environ, PATH=str(directory / "bin") + os.pathsep + os.environ["PATH"],
                           GH_TOKEN="synthetic-private-marker", GITHUB_TOKEN="synthetic-private-marker",
                           HTTPS_PROXY="synthetic-private-marker")
        result = subprocess.run(["bash", str(script)] + (args if args is not None else
                                [str(run_id), sha, request_hash]),
                                env=environment, capture_output=True, text=True, timeout=20)
        accepted = reason == "ACCEPTED"
        expected = {"verdict": "accept" if accepted else "refuse", "reason": reason,
                    "requests_used": expected_requests, "rate_limit": {
                        "limit": 60 if expected_requests and rate_known else None,
                        "remaining": remaining,
                        "reset": 2000000000 if expected_requests and rate_known else None}}
        assert result.returncode == (0 if accepted else 1), name
        assert result.stderr == "", name
        assert json.loads(result.stdout) == expected, (name, result.stdout)
        assert len(result.stdout.splitlines()) == 1, name
        for forbidden in ["synthetic-private-marker", "Bearer", "Authorization", "ghp_",
                          "github_pat_", "PRIVATE KEY", "@", comment, sha]:
            assert forbidden not in result.stdout + result.stderr, name
        actual_requests = (directory / "requests").read_text().splitlines() if expected_requests else []
        assert len(actual_requests) == expected_requests, name
        assert all(url.startswith(base) for url in actual_requests), name
        if expected_urls is not None:
            assert actual_requests == expected_urls, name
    count += 1

def change(path, field, value):
    def mutate(data):
        body = json.loads(data[path]["body"])
        target = body
        for key in field[:-1]:
            target = target[key]
        target[field[-1]] = value
        data[path]["body"] = json.dumps(body)
    return mutate

def final_run(field, value):
    def mutate(data):
        body = copy.deepcopy(run)
        target = body
        for key in field[:-1]:
            target = target[key]
        target[field[-1]] = value
        data[base] = [data[base], response(body, 56)]
    return mutate

approvals_url = base + "/approvals?per_page=100&page=1"
jobs_url = base + "/attempts/1/jobs?per_page=100&page=1"
check("approved", expected_urls=[base, approvals_url, jobs_url, base])
for path in [".github/workflows/approval-probe.yml", ".github/workflows/approval-probe.yml@main"]:
    check("documented workflow path", mutate=change(base, ["path"], path))
for path in [".github/workflows/approval-probe.yml@other",
             ".github/workflows/approval-probe.yml@refs/heads/main",
             ".github/workflows/../workflows/approval-probe.yml@main",
             ".github/workflows/approval-probe.yml@main/../other",
             ".github/workflows/approval-probe.yml@main@other"]:
    check("untrusted workflow path", "RUN_IDENTITY", change(base, ["path"], path), 1)
for field, value in [(["workflow_id"], 99), (["head_branch"], "topic"), (["head_sha"], "c" * 40)]:
    def qualified_mismatch(data, field=field, value=value):
        change(base, ["path"], ".github/workflows/approval-probe.yml@main")(data)
        change(base, field, value)(data)
    check("qualified path still binds identity", "RUN_IDENTITY", qualified_mismatch, 1)
check("self approval allowed", mutate=change(approvals_url, [0, "user", "id"], owner))
check("other enrolled dispatcher", mutate=change(base, ["triggering_actor", "id"], reviewer))
check("job array order does not define execution order", mutate=change(
      jobs_url, ["jobs"], list(reversed(jobs["jobs"]))))
for field, value, reason in [
    (["id"], run_id + 1, "RUN_IDENTITY"),
    (["repository", "id"], 99, "RUN_IDENTITY"),
    (["repository", "full_name"], "other/aboutme", "RUN_IDENTITY"),
    (["workflow_id"], 99, "RUN_IDENTITY"),
    (["path"], ".github/workflows/other.yml", "RUN_IDENTITY"),
    (["event"], "push", "RUN_IDENTITY"),
    (["head_branch"], "topic", "RUN_IDENTITY"),
    (["head_sha"], "c" * 40, "RUN_IDENTITY"),
    (["run_attempt"], 2, "RUN_ATTEMPT"),
    (["run_attempt"], True, "MISSING_FIELDS"),
    (["actor", "id"], 99, "RUN_ACTOR"),
    (["triggering_actor", "id"], 99, "RUN_ACTOR"),
    (["actor", "type"], "Bot", "RUN_ACTOR"),
    (["triggering_actor", "type"], "Bot", "RUN_ACTOR"),
    (["conclusion"], "cancelled", "RUN_CANCELLED"),
    (["status"], "waiting", "RUN_INCOMPLETE"),
]:
    check(str(field), reason, change(base, field, value), 1)
check("failed run", "RUN_UNSUCCESSFUL", change(base, ["conclusion"], "failure"))
for field, value, reason in [
    (["id"], run_id + 1, "RUN_IDENTITY"),
    (["repository", "id"], 99, "RUN_IDENTITY"),
    (["repository", "full_name"], "other/aboutme", "RUN_IDENTITY"),
    (["workflow_id"], 99, "RUN_IDENTITY"),
    (["path"], ".github/workflows/approval-probe.yml@other", "RUN_IDENTITY"),
    (["event"], "push", "RUN_IDENTITY"),
    (["head_branch"], "topic", "RUN_IDENTITY"),
    (["head_sha"], "c" * 40, "RUN_IDENTITY"),
    (["run_attempt"], 2, "RUN_ATTEMPT"),
    (["actor", "id"], reviewer, "RUN_ACTOR"),
    (["triggering_actor", "id"], reviewer, "RUN_ACTOR"),
    (["actor", "type"], "Bot", "RUN_ACTOR"),
    (["triggering_actor", "type"], "Bot", "RUN_ACTOR"),
    (["status"], "in_progress", "RUN_INCOMPLETE"),
    (["conclusion"], "cancelled", "RUN_CANCELLED"),
    (["conclusion"], "failure", "RUN_UNSUCCESSFUL"),
]:
    check("final run changed " + str(field), reason, final_run(field, value), 4,
          expected_urls=[base, approvals_url, jobs_url, base])

def interleaved_rerun(data):
    body = dict(run, run_attempt=2, status="in_progress", conclusion=None)
    data[base] = [data[base], response(body, 56)]
check("rerun starts while reading evidence", "RUN_ATTEMPT", interleaved_rerun, 4,
      expected_urls=[base, approvals_url, jobs_url, base])
check("final read preserves reserve", "BUDGET", lambda data: data.update(
      {base: [data[base], response(run, 11)]}), 4, 11)
check("final read can leave exact reserve", mutate=lambda data: data.update(
      {base: [data[base], response(run, 12)]}), remaining=12)
check("reserve cannot fund final read", "BUDGET", lambda data: data.update(
      {jobs_url: response(jobs, 12)}), 3, 12)
for field, value, reason in [
    ([0, "state"], "rejected", "APPROVAL_REJECTED"),
    ([0, "state"], "unknown", "APPROVAL_FIELDS"),
    ([0, "comment"], "synthetic-private-marker", "APPROVAL_COMMENT"),
    ([0, "user", "id"], 99, "APPROVAL_IDENTITY"),
    ([0, "user", "type"], "Bot", "APPROVAL_IDENTITY"),
    ([0, "environments", 0, "id"], 99, "ENVIRONMENT"),
    ([0, "environments", 0, "name"], "production", "ENVIRONMENT"),
    ([0, "environments"], [], "APPROVAL_FIELDS"),
]:
    check(str(field), reason, change(approvals_url, field, value), 2)
check("missing approval", "APPROVAL_MISSING",
      lambda data: data.update({approvals_url: response([], 58)}), 2)
check("two approvals", "APPROVAL_COUNT",
      lambda data: data.update({approvals_url: response([approval, approval], 58)}), 2)
rejection = dict(approval, state="rejected")
for rows in [[rejection, approval], [approval, rejection]]:
    check("rejection in any order", "APPROVAL_REJECTED",
          lambda data, rows=rows: data.update({approvals_url: response(rows, 58)}), 2)
conflict = dict(approval, comment="other")
for rows in [[approval, conflict], [conflict, approval]]:
    check("conflict in any order", "APPROVAL_COMMENT",
          lambda data, rows=rows: data.update({approvals_url: response(rows, 58)}), 2)

def paginated(data):
    unrelated = dict(approval, environments=[{"id": 99, "name": "other"}])
    first_url = base + "/approvals?per_page=1&page=1"
    next_url = base + "/approvals?per_page=1&page=2"
    del data[approvals_url]
    data[first_url] = response([unrelated], 58, link=f'<{next_url}>; rel="next", <{next_url}>; rel="last"')
    data[next_url] = response([approval], 57, link=f'<{first_url}>; rel="prev"')
    data[jobs_url] = response(jobs, 56)
check("approval on page two", mutate=paginated, expected_requests=5,
      args=[str(run_id), sha, request_hash, "1"])
for second, reason in [(approval, "APPROVAL_COUNT"), (rejection, "APPROVAL_REJECTED"),
                       (conflict, "APPROVAL_COMMENT")]:
    def across_pages(data, second=second):
        paginated(data)
        first_url = base + "/approvals?per_page=1&page=1"
        data[first_url]["body"] = json.dumps([approval])
        data[base + "/approvals?per_page=1&page=2"]["body"] = json.dumps([second])
    check("conflict across pages", reason, across_pages, 3,
          args=[str(run_id), sha, request_hash, "1"])

page_one = base + "/approvals?per_page=1&page=1"
page_two = base + "/approvals?per_page=1&page=2"
page_three = base + "/approvals?per_page=1&page=3"
page_four = base + "/approvals?per_page=1&page=4"
def three_pages(data):
    paginated(data)
    data[page_one]["headers"] = response([], 58, link=(
        f'<{page_two}>; rel="next", <{page_three}>; rel="last"'))["headers"]
    data[page_two]["headers"] = response([], 57, link=(
        f'<{page_one}>; rel="prev", <{page_three}>; rel="next"'))["headers"]
    unrelated = dict(approval, environments=[{"id": 99, "name": "other"}])
    data[page_three] = response([unrelated], 56, link=f'<{page_two}>; rel="prev"')
    data[jobs_url] = response(jobs, 55)
check("retains last when later headers omit it", mutate=three_pages, expected_requests=6,
      args=[str(run_id), sha, request_hash, "1"],
      expected_urls=[base, page_one, page_two, page_three, jobs_url, base])

def final_rejection(data):
    three_pages(data)
    data[page_three]["body"] = json.dumps([rejection])
check("rejection on advertised last page", "APPROVAL_REJECTED", final_rejection, 4,
      args=[str(run_id), sha, request_hash, "1"],
      expected_urls=[base, page_one, page_two, page_three])
for link in [None, f'<{page_one}>; rel="prev"']:
    def premature_end(data, link=link):
        final_rejection(data)
        data[page_two]["headers"] = response([], 57, link=link)["headers"]
    check("refuses lost next before advertised last", "PAGINATION", premature_end, 3,
          args=[str(run_id), sha, request_hash, "1"], expected_urls=[base, page_one, page_two])
for link in [
    f'<{page_one}>; rel="prev", <{page_three}>; rel="next", <{page_two}>; rel="last"',
    f'<{page_one}>; rel="prev", <{page_three}>; rel="next", <{page_four}>; rel="last"',
    f'<{page_one}>; rel="prev", <{page_one}>; rel="last"',
    f'<{page_two}>; rel="prev", <{page_three}>; rel="next"',
    f'<{page_three}>; rel="next"',
    f'<{page_one}>; rel="prev", <{page_four}>; rel="next"',
    f'<{page_one}>; rel="prev", <{page_one}>; rel="next"',
    f'<{page_one}>; rel="prev", <{page_three}>; rel="next", <{page_three}>; rel="next"',
]:
    def contradictory_links(data, link=link):
        three_pages(data)
        data[page_two]["headers"] = response([], 57, link=link)["headers"]
    check("contradictory pagination", "PAGINATION", contradictory_links, 3,
          args=[str(run_id), sha, request_hash, "1"], expected_urls=[base, page_one, page_two])

def beyond_last(data):
    paginated(data)
    data[page_two]["headers"] = response([], 57, link=(
        f'<{page_one}>; rel="prev", <{page_three}>; rel="next"'))["headers"]
    data[page_three] = response([rejection], 56, link=f'<{page_two}>; rel="prev"')
check("never fetches beyond advertised last", "PAGINATION", beyond_last, 3,
      args=[str(run_id), sha, request_hash, "1"], expected_urls=[base, page_one, page_two])
def contradictory_first_page(data):
    paginated(data)
    data[page_one]["headers"] = response([], 58, link=(
        f'<{page_two}>; rel="next", <{page_one}>; rel="last"'))["headers"]
check("next contradicts last on first page", "PAGINATION", contradictory_first_page, 2,
      args=[str(run_id), sha, request_hash, "1"], expected_urls=[base, page_one])
def missing_previous(data):
    paginated(data)
    data[page_two]["headers"] = response([], 57)["headers"]
check("refuses missing previous on last page", "PAGINATION", missing_previous, 3,
      args=[str(run_id), sha, request_hash, "1"])
for page, requests in [(page_two, 3), (page_three, 4)]:
    def empty_page(data, page=page):
        three_pages(data)
        data[page]["body"] = "[]"
    check("refuses empty advertised page", "PAGINATION", empty_page, requests,
          args=[str(run_id), sha, request_hash, "1"])

def excessive_pages(data, pages=11):
    del data[approvals_url]
    unrelated = dict(approval, environments=[{"id": 99, "name": "other"}])
    for page in range(1, pages + 1):
        url = base + f"/approvals?per_page=1&page={page}"
        following = base + f"/approvals?per_page=1&page={page + 1}"
        links = [] if page == pages else [f'<{following}>; rel="next"']
        if page > 1:
            previous = base + f"/approvals?per_page=1&page={page - 1}"
            links.append(f'<{previous}>; rel="prev"')
        data[url] = response([approval if page == pages else unrelated], 59 - page,
                             link=", ".join(links))
    data[jobs_url] = response(jobs, 58 - pages)
check("bounded requests", "REQUEST_BUDGET", excessive_pages, 12,
      args=[str(run_id), sha, request_hash, "1"])
check("final read counts against request cap", "REQUEST_BUDGET",
      lambda data: excessive_pages(data, 10), 12, args=[str(run_id), sha, request_hash, "1"])
check("final read fits exact request cap", mutate=lambda data: excessive_pages(data, 9),
      expected_requests=12, args=[str(run_id), sha, request_hash, "1"])
for link in [f'<{approvals_url}>; rel="next"', '<https://example.com/>; rel="next"',
             f'<{base}/approvals?per_page=100&page=3>; rel="next"',
             f'<{base}/approvals?per_page=1&page=2>; rel="next"', "invalid"]:
    check("unsafe pagination", "PAGINATION", lambda data, link=link:
          data[approvals_url].update(headers=response([], 58, link=link)["headers"]), 2)
for field, value in [(["jobs", 0, "run_id"], 99), (["jobs", 0, "head_sha"], "c" * 40),
                     (["jobs", 0, "status"], "in_progress"), (["jobs", 1, "conclusion"], "skipped")]:
    check(str(field), "JOBS", change(jobs_url, field, value))
check("attempt mismatch in job", "JOBS", change(jobs_url, ["jobs", 0, "run_attempt"], 2))
for field, value in [(["total_count"], 3), (["jobs"], [jobs["jobs"][0]]),
                     (["jobs", 1, "name"], "Prepare request"),
                     (["jobs", 1, "id"], jobs["jobs"][0]["id"])]:
    check(str(field), "JOBS_COUNT", change(jobs_url, field, value))
check("ambiguous job order", "JOBS_ORDER",
      change(jobs_url, ["jobs", 1, "started_at"], "2026-11-01T00:00:00Z"))
check("missing job time", "MISSING_FIELDS", change(jobs_url, ["jobs", 1, "started_at"], None))
for url, fields, requests in [(base, list(run), 1), (approvals_url, list(approval), 2),
                              (jobs_url, ["total_count", "jobs"], 3)]:
    for field in fields:
        def remove(data, url=url, field=field):
            body = json.loads(data[url]["body"])
            del (body[0] if url == approvals_url else body)[field]
            data[url]["body"] = json.dumps(body)
        check("missing " + field, "MISSING_FIELDS", remove, requests)
for url, parents in [(base, [["repository"], ["actor"], ["triggering_actor"]]),
                     (approvals_url, [[0, "user"], [0, "environments", 0]]),
                     (jobs_url, [["jobs", 0], ["jobs", 1]])]:
    original = json.loads(fixture()[url]["body"])
    for parent in parents:
        nested = original
        for key in parent:
            nested = nested[key]
        for field in nested:
            def remove_nested(data, parent=parent, field=field, url=url):
                body = json.loads(data[url]["body"])
                target = body
                for key in parent:
                    target = target[key]
                del target[field]
                data[url]["body"] = json.dumps(body)
            check("missing nested " + field, "MISSING_FIELDS", remove_nested,
                  1 if url == base else 2 if url == approvals_url else 3)
for url, requests in [(base, 1), (approvals_url, 2), (jobs_url, 3)]:
    for malformed in ["{", '{"id":1,"id":2}', "NaN"]:
        check("malformed json", "JSON_INVALID", lambda data, url=url, malformed=malformed:
              data[url].update(body=malformed), requests)
    for status, reason, left in [(403, "HTTP_RATE_LIMIT", 0), (429, "HTTP_RATE_LIMIT", 0),
                                 (403, "HTTP_ERROR", 59), (404, "HTTP_ERROR", 59),
                                 (500, "HTTP_ERROR", 59), (302, "HTTP_ERROR", 59)]:
        check("HTTP failure", reason, lambda data, url=url, status=status, left=left:
              data.update({url: response({"message": "synthetic-private-marker"}, left, status)}),
              requests, left)
check("transport failure", "TRANSPORT", lambda data: data[base].update(exit=7), 1)
check("oversized response", "RESPONSE_SIZE", lambda data: data[base].update(body="x" * 1048577), 1)
check("budget exhausted", "BUDGET", lambda data: data.update({base: response(run, 11)}), 1, 11)
check("missing rate header", "RATE_HEADERS", lambda data: data[base].update(
      headers="HTTP/2 200\r\n\r\n"), 1, None, rate_known=False)
check("reserve cannot fund next request", "BUDGET", lambda data: data.update(
      {base: response(run, 12)}), 1, 12)
check("unfilled pins", "CONFIG_INVALID", expected_requests=0,
      config={"repository_id": None, "workflow_id": None, "environment_id": None,
              "maintainer_ids": []}, remaining=None)
for config in [{}, dict(pins, environment_id=None), dict(pins, maintainer_ids=[]),
               dict(pins, repository_id=True), dict(pins, maintainer_ids=[owner, owner])]:
    check("invalid pins", "CONFIG_INVALID", expected_requests=0, config=config, remaining=None)
for args in [[], ["0", sha, request_hash], [str(run_id), "bad", request_hash],
             [str(run_id), sha, request_hash, "101"]]:
    check("invalid input", "INPUT_INVALID", expected_requests=0, args=args, remaining=None)
print(f"approval probe fixtures passed: {count}")
PY
