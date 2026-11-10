#!/usr/bin/env bash

# Make target safety contracts.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-make-safety.XXXXXX")
WORK=$(cd "$WORK" && pwd -P)
trap 'rm -rf -- "$WORK"' EXIT

REPO=$WORK/repo
BIN=$WORK/bin
CALLS=$WORK/calls
mkdir -p "$REPO" "$BIN"
cp "$ROOT/Makefile" "$REPO/Makefile"
: >"$REPO/.env"
: >"$CALLS"

cat >"$BIN/podman" <<'EOF'
#!/usr/bin/env bash
printf 'podman %s\n' "$*" >>"$CALL_LOG"
if [ "${1:-}" = ps ]; then
  case ${PODMAN_PS_MODE:-fail} in
    compose-db) printf '%s\n' '[{"Names":["aboutme-postgres-1"],"Labels":{"com.docker.compose.project":"aboutme","com.docker.compose.service":"postgres"}}]' ;;
    unrelated) printf '%s\n' '[{"Names":["radar-postgres"],"Labels":{"com.docker.compose.project":"radar","com.docker.compose.service":"postgres"}}]' ;;
    malformed) printf '%s\n' 'not json' ;;
    missing-labels) printf '%s\n' '[{"Names":["aboutme-postgres-1"]}]' ;;
    invalid-labels) printf '%s\n' '[{"Names":["aboutme-postgres-1"],"Labels":[]}]' ;;
    *) exit 17 ;;
  esac
  exit 0
fi
if [ "${1:-}" = run ]; then exit 19; fi
exit 0
EOF

chmod +x "$BIN/podman"

assert_guarded() {
  local target=$1
  : >"$CALLS"
  if (
    cd "$REPO"
    PATH="$BIN:/usr/bin:/bin" CALL_LOG="$CALLS" \
      /usr/bin/make --no-print-directory "$target"
  ) >"$WORK/$target.out" 2>&1; then
    printf 'makefile-safety-test: make %s passed after podman ps failed\n' "$target" >&2
    exit 1
  fi
  if grep -Eq '^podman (compose|run)([[:space:]]|$)' "$CALLS"; then
    printf 'makefile-safety-test: make %s mutated containers after discovery failed\n' "$target" >&2
    exit 1
  fi
}

assert_guarded dev
assert_guarded test-db-up

assert_web_e2e_rejected() {
  local name=$1 target=$2 diagnostic=$3
  shift 3
  : >"$CALLS"
  if (
    cd "$REPO"
    env PATH="$BIN:/usr/bin:/bin" CALL_LOG="$CALLS" "$@" \
      /usr/bin/make --no-print-directory "$target"
  ) >"$WORK/$name.out" 2>&1; then
    printf 'makefile-safety-test: make %s accepted %s\n' "$target" "$name" >&2
    exit 1
  fi
  grep -Fq "$diagnostic" "$WORK/$name.out" || {
    printf 'makefile-safety-test: make %s lacked %s diagnostic\n' \
      "$target" "$name" >&2
    exit 1
  }
  if grep -Eq '^podman([[:space:]]|$)' "$CALLS"; then
    printf 'makefile-safety-test: make %s called Podman for %s\n' \
      "$target" "$name" >&2
    exit 1
  fi
}

assert_web_e2e_rejected invalid-run-id web-e2e \
  'WEB_E2E_RUN_ID must match [A-Za-z0-9_-]+' WEB_E2E_RUN_ID='../bad'
assert_web_e2e_rejected invalid-template-run-id web-template-pages \
  'TEMPLATE_PAGES_RUN_ID must match [A-Za-z0-9_-]+' TEMPLATE_PAGES_RUN_ID='../bad'
assert_web_e2e_rejected update-golden-present web-e2e \
  'UPDATE_GOLDEN must be absent' WEB_E2E_RUN_ID=valid UPDATE_GOLDEN=
assert_web_e2e_rejected playwright-update-present web-e2e \
  'PLAYWRIGHT_UPDATE_SNAPSHOTS must be absent' \
  WEB_E2E_RUN_ID=valid PLAYWRIGHT_UPDATE_SNAPSHOTS=
for shard in harness rest; do
  assert_web_e2e_rejected "unknown-shard-$shard" web-e2e \
    'WEB_E2E_SHARD must be all, preview-gap, rest-1, rest-2, rest-3, or rest-4' \
    WEB_E2E_RUN_ID=valid WEB_E2E_SHARD="$shard"
done

# Exercise the public diagnostic collector without a browser or container.
python3 - "$ROOT/Makefile" "$ROOT/scripts/collect-browser-diagnostics.py" "$WORK" <<'PYTHON'
import hashlib
import io
import json
import os
from pathlib import Path
import re
import struct
import subprocess
import sys
import tarfile
import zlib

makefile, collector, work = map(Path, sys.argv[1:4])
signature = b"\x89PNG\r\n\x1a\n"
def chunk(kind, data):
    return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))
header = chunk(b"IHDR", struct.pack(">IIBBBBB", 1, 1, 8, 6, 0, 0, 0))
pixels = b"\x00\x12\x34\x56\xff"
png = signature + header + chunk(b"IDAT", zlib.compress(pixels)) + chunk(b"IEND", b"")
sentinel = b"ARTIFACT_SECRET_SENTINEL"
metadata_png = signature + header + chunk(b"tEXt", b"Comment\x00" + sentinel) + png[len(signature + header):]
archive = work / "candidate.tar"
with tarfile.open(archive, "w") as tar:
    for name in ["card.spec.ts", "resume-screen.spec.ts", "chrome.spec.ts",
                 "showcase-baselines.spec.ts", "gallery-baselines.spec.ts",
                 "sign-in-gate.spec.ts", "print.spec.ts"]:
        info = tarfile.TarInfo("apps/web/e2e/" + name)
        info.size = 7
        tar.addfile(info, io.BytesIO(b"fixture"))

def attachment(path, name="screenshot", content_type="image/png"):
    return {"name": name, "contentType": content_type, "path": str(path)}

def spec(file, status, attachments, line=12):
    return {"file": file, "line": line, "title": "private-token", "tests": [
        {"results": [{"status": status, "attachments": attachments,
          "error": {"message": "private-token"}, "stdout": ["private-token"]}]}]}

def report(mode, cases):
    (mode / "harness/results.json").write_text(json.dumps({"suites": [{"specs": cases}]}))

def collect(mode, status=1):
    result = subprocess.run(["python3", "-I", str(collector), str(mode), str(archive), "a" * 40,
                             "sha256:" + "b" * 64, "c" * 64, "d" * 64, str(status)],
                            capture_output=True, timeout=5)
    assert result.returncode == 0, result.stderr
    return mode / "diagnostics"

hostile = {
    "signature-secret": signature + sentinel,
    "png-metadata": metadata_png,
    "png-appended": png + sentinel,
    "bad-crc": png[:-1] + bytes([png[-1] ^ 1]),
    "truncated-png": png[:-12],
    "oversized-pixels": signature + chunk(b"IHDR", struct.pack(">IIBBBBB", 100000, 100000, 8, 6, 0, 0, 0)) + png[len(signature + header):],
    "invalid-filter": signature + header + chunk(b"IDAT", zlib.compress(b"\x05" + pixels[1:])) + chunk(b"IEND", b""),
    "inflated-pixels": signature + header + chunk(b"IDAT", zlib.compress(pixels * 10000)) + chunk(b"IEND", b""),
    "invalid-zlib": signature + header + chunk(b"IDAT", b"invalid") + chunk(b"IEND", b""),
}
for fixture in ["failure", "container-path", "timed-out", "interrupted", "symlink", "parent-symlink", "special-file", "non-png",
                "unknown-fixture", "malformed-report", "passed-case", "forged-metadata",
                "outside-path", "traversal-path", "raw-attachment", *hostile]:
    mode = work / fixture
    artifacts = mode / "harness/artifacts/private-title-token"
    artifacts.mkdir(parents=True)
    image = artifacts / "test-failed-1.png"
    image.write_bytes(hostile.get(fixture, png))
    # Unattached images and raw evidence are never selected.
    (artifacts / "unknown.png").write_bytes(png)
    for name in ["trace.zip", "response.json", "report.html", "output.pdf"]:
        (artifacts / name).write_bytes(sentinel)
    passed = artifacts.parent / "passed-case"
    passed.mkdir()
    (passed / "test-failed-1.png").write_bytes(png)
    if fixture in ["symlink", "special-file", "non-png"]:
        image.unlink()
        if fixture == "symlink":
            image.symlink_to(artifacts / "response.json")
        elif fixture == "special-file":
            os.mkfifo(image)
        else:
            image.write_bytes(sentinel)
    if fixture == "parent-symlink":
        linked = artifacts.parent / "linked"
        linked.symlink_to(artifacts, target_is_directory=True)
        image = linked / image.name
    if fixture == "container-path":
        image = Path("/results/harness/artifacts/private-title-token/test-failed-1.png")
    if fixture == "outside-path":
        image = work / "outside.png"
        image.write_bytes(png)
    if fixture == "traversal-path":
        image = artifacts / ".." / "private-title-token" / image.name
    if fixture == "forged-metadata":
        for folder in ["diagnostics", "review-images"]:
            (mode / folder).mkdir()
            (mode / folder / "response.json").write_bytes(sentinel)
    state = {"timed-out": "timedOut", "interrupted": "interrupted",
             "passed-case": "passed"}.get(fixture, "failed")
    cases = [spec("unknown.spec.ts" if fixture == "unknown-fixture" else "card.spec.ts",
                  state,
                  [attachment(image, "trace" if fixture == "raw-attachment" else "screenshot")]),
             spec("card.spec.ts", "passed", [attachment(passed / "test-failed-1.png")], 24)]
    report(mode, cases)
    if fixture == "malformed-report":
        (mode / "harness/results.json").write_text("invalid")
    public = collect(mode)
    files = list(public.rglob("*"))
    assert not any(path.is_symlink() for path in files)
    assert not any("private" in path.relative_to(public).as_posix() for path in files)
    for path in files:
        if path.is_file():
            assert sentinel not in path.read_bytes()
            assert b"private-token" not in path.read_bytes()
    images = [path for path in files if path.suffix == ".png"]
    accepted = ["failure", "container-path", "timed-out", "interrupted",
                "forged-metadata", "png-metadata", "png-appended"]
    assert len(images) == int(fixture in accepted), (fixture, images)
    if images:
        assert images[0].name == "card.png"
        assert images[0].read_bytes() == png
    assert all(path.suffix in [".json", ".txt", ".png"] for path in files if path.is_file())
    assert not (mode / "review-images").exists()
    summary = json.loads((public / "failure-summary.json").read_text())
    assert summary["exit_status"] == 1
    facts = summary["surfaces"]["harness"]
    assert facts["report"] == ("unavailable" if fixture == "malformed-report" else "available")
    assert facts["failures"] == ([] if fixture in ["malformed-report", "passed-case", "unknown-fixture"] else
                                  [{"file": "card.spec.ts", "line": 12, "status": state}])
    assert "commit=" + "a" * 40 in (public / "source-metadata.txt").read_text()
# A passing run selects exactly the eight review IDs, with fixed unavailable status.
reviews = {
    "public-modern-sidebar-vi-dark-390": "resume-screen.spec.ts",
    "public-executive-band-en-light-1440": "resume-screen.spec.ts",
    "home-vi-light-390": "chrome.spec.ts",
    "showcase-en-dark-1440": "showcase-baselines.spec.ts",
    "library-sheet-en-light-390": "gallery-baselines.spec.ts",
    "sign-in-error-vi-390": "sign-in-gate.spec.ts",
    "print-main-overflow-p2": "print.spec.ts",
    "print-sidebar-overflow-p2": "print.spec.ts",
}
for unavailable in [False, True]:
    mode = work / f"passing-{unavailable}"
    artifacts = mode / "harness/artifacts/private-title-token"
    artifacts.mkdir(parents=True)
    image = artifacts / "review.png"
    image.write_bytes(metadata_png + sentinel)
    cases = [spec(owner, "passed", [attachment(image, "review-" + fixture)])
             for fixture, owner in reviews.items()]
    cases += [spec("card.spec.ts", "passed", [attachment(image, "review-unknown")]),
              spec("card.spec.ts", "passed", [attachment(image, "review-home-vi-light-390")])]
    if unavailable:
        cases[0]["tests"][0]["results"][0]["attachments"] = []
    report(mode, cases)
    public = collect(mode, 0)
    review = mode / "review-images"
    assert not list(public.rglob("*.png"))
    images = sorted(path.stem for path in review.glob("*.png"))
    assert images == sorted(list(reviews)[int(unavailable):]), images
    assert all(path.read_bytes() == png for path in review.glob("*.png"))
    assert (review / "source-metadata.txt").read_bytes() == (public / "source-metadata.txt").read_bytes()
    states = json.loads((review / "capture-summary.json").read_text())
    assert states == {key: "unavailable" if unavailable and key == next(iter(reviews)) else "available" for key in reviews}
# Every PNG filter must reconstruct the same RGB pixels.
rgb_header = chunk(b"IHDR", struct.pack(">IIBBBBB", 2, 2, 8, 2, 0, 0, 0))
rgb_pixels = bytes([0, 10, 20, 30, 40, 50, 60, 0, 70, 80, 90, 100, 110, 120])
rgb_png = signature + rgb_header + chunk(b"IDAT", zlib.compress(rgb_pixels)) + chunk(b"IEND", b"")
filtered_rows = [
    ([10, 20, 30, 40, 50, 60], [70, 80, 90, 100, 110, 120]),
    ([10, 20, 30, 30, 30, 30], [70, 80, 90, 30, 30, 30]),
    ([10, 20, 30, 40, 50, 60], [60, 60, 60, 60, 60, 60]),
    ([10, 20, 30, 35, 40, 45], [65, 70, 75, 45, 45, 45]),
    ([10, 20, 30, 30, 30, 30], [60, 60, 60, 30, 30, 30]),
]
for filter_type, (first, second) in enumerate(filtered_rows):
    mode = work / f"png-filter-{filter_type}"
    artifacts = mode / "harness/artifacts/fixture"
    artifacts.mkdir(parents=True)
    filtered = bytes([filter_type, *first, filter_type, *second])
    (artifacts / "test-failed-1.png").write_bytes(
        signature + rgb_header + chunk(b"IDAT", zlib.compress(filtered)) + chunk(b"IEND", b""))
    report(mode, [spec("card.spec.ts", "failed", [attachment(artifacts / "test-failed-1.png")])])
    collect(mode)
    assert (mode / "diagnostics/harness/0/card.png").read_bytes() == rgb_png
# Run the actual Make recipes with source, Git, architecture, and browser stubs.
repo = work / "browser-repo"
bin_dir = work / "browser-bin"
(repo / "scripts").mkdir(parents=True)
bin_dir.mkdir()
(repo / "Makefile").write_text(makefile.read_text())
(repo / "scripts/collect-browser-diagnostics.py").write_bytes(collector.read_bytes())
(repo / "scripts/web-e2e-source.manifest").write_text("fixture\n")
def executable(path, text):
    path.write_text(text)
    path.chmod(0o755)
executable(bin_dir / "git", "#!/bin/bash\nif [ \"$1\" = rev-parse ]; then printf '%s\\n' \"$FIXTURE_COMMIT\"; fi\n")
executable(bin_dir / "uname", "#!/bin/bash\nif [ \"${1-}\" = -s ]; then printf 'Linux\\n'; else printf 'x86_64\\n'; fi\n")
executable(repo / "scripts/web-e2e-source.sh", "#!/bin/bash\ncp -- \"$FIXTURE_ARCHIVE\" \"$3\"\n")
executable(repo / "scripts/generate-web-e2e-source-manifest.sh", "#!/bin/bash\n[ \"$1\" = --check ]\n")
executable(bin_dir / "podman", """#!/usr/bin/env python3
import json
import os
from pathlib import Path
import sys
if os.environ.get("FIXTURE_CALLS"):
    Path(os.environ["FIXTURE_CALLS"]).write_text(json.dumps(sys.argv[1:]))
mount = next(arg for arg in sys.argv if arg.endswith(":/results:rw"))
mode = Path(mount.removesuffix(":/results:rw"))
artifacts = mode / "harness/artifacts/fixture"
artifacts.mkdir(parents=True)
(artifacts / "test-failed-1.png").write_bytes(Path(os.environ["FIXTURE_PNG"]).read_bytes())
for folder in ["diagnostics", "review-images"]:
    (mode / folder).mkdir()
    (mode / folder / "response.json").write_text("private-token")
    if os.environ.get("FIXTURE_LOCK"):
        (mode / folder).chmod(0o555)
(mode / "harness/results.json").write_text(json.dumps({"suites": [{"specs": [{
    "file": "card.spec.ts", "line": 12, "tests": [{"results": [{
        "status": "failed" if int(os.environ["FIXTURE_STATUS"]) else "passed",
        "attachments": [{"name": "screenshot", "contentType": "image/png",
                         "path": str(artifacts / "test-failed-1.png")}],
    }]}],
}]}]}))
sys.exit(int(os.environ["FIXTURE_STATUS"]))
""")
(work / "fixture.png").write_bytes(png)
shards = ["preview-gap", "rest-1", "rest-2", "rest-3", "rest-4"]
selections = {}
for target, shard in [("web-e2e", shard) for shard in ["all", *shards]]:
    for status in [42, 0]:
        run_id = f"{target}-{shard}-{status}"
        calls = work / f"{run_id}-calls.json"
        result = subprocess.run(["/usr/bin/make", "--no-print-directory", target], cwd=repo, env={
            **os.environ, "PATH": str(bin_dir) + ":" + os.environ["PATH"],
            "FIXTURE_COMMIT": "a" * 40, "FIXTURE_ARCHIVE": str(archive),
            "FIXTURE_PNG": str(work / "fixture.png"), "FIXTURE_STATUS": str(status),
            "FIXTURE_CALLS": str(calls), "WEB_E2E_RUN_ID": run_id, "WEB_E2E_SHARD": shard,
        }, capture_output=True, timeout=10)
        assert result.returncode == (2 if status else 0), result.stderr
        if target == "web-e2e":
            args = json.loads(calls.read_text())
            assert not [arg for arg in args if arg.startswith("--net")], f"{shard}: web-e2e must use the default private network so shards can share a host"
            env = dict(args[i + 1].split("=", 1) for i, arg in enumerate(args) if arg == "-e")
            selections.setdefault(shard, []).append(
                (env["WEB_E2E_HARNESS_SPECS"].split(), env["WEB_E2E_NORMAL_SPECS"].split()))
        mode = repo / ".dev/web-e2e-results" / ("a" * 40) / run_id / "compare"
        summary = json.loads((mode / "diagnostics/failure-summary.json").read_text())
        assert summary["exit_status"] == status
        images = list((mode / "diagnostics").rglob("*.png"))
        assert len(images) == int(status != 0)
        if images:
            assert images[0].read_bytes() == png
        assert (mode / "review-images").exists() == (status == 0)
        metadata = (mode / "diagnostics/source-metadata.txt").read_text()
        assert "commit=" + "a" * 40 in metadata
        image = next(line.split(" := ", 1)[1] for line in makefile.read_text().splitlines() if line.startswith("WEB_E2E_IMAGE := "))
        assert metadata == (f"commit={'a' * 40}\n"
                            f"manifest_sha256={hashlib.sha256((repo / 'scripts/web-e2e-source.manifest').read_bytes()).hexdigest()}\n"
                            f"tar_sha256={hashlib.sha256(archive.read_bytes()).hexdigest()}\n"
                            f"browser_image_digest={image.split('@')[1]}\n")
(repo / "scripts/collect-browser-diagnostics.py").write_text("raise RuntimeError('fixture')\n")
for status in [42, 0]:
    result = subprocess.run(["/usr/bin/make", "--no-print-directory", "web-e2e"], cwd=repo, env={
        **os.environ, "PATH": str(bin_dir) + ":" + os.environ["PATH"],
        "FIXTURE_COMMIT": "a" * 40, "FIXTURE_ARCHIVE": str(archive),
        "FIXTURE_PNG": str(work / "fixture.png"), "FIXTURE_STATUS": str(status),
        "WEB_E2E_RUN_ID": f"unavailable-{status}", "WEB_E2E_SHARD": "all",
    }, capture_output=True, timeout=10)
    assert result.returncode == (2 if status else 0), result.stderr
    assert b"browser evidence unavailable" in result.stderr
    mode = repo / ".dev/web-e2e-results" / ("a" * 40) / f"unavailable-{status}" / "compare"
    assert not (mode / "diagnostics").exists()
    assert not (mode / "review-images").exists()
# Evidence the host cannot delete moves out of every upload path.
result = subprocess.run(["/usr/bin/make", "--no-print-directory", "web-e2e"], cwd=repo, env={
    **os.environ, "PATH": str(bin_dir) + ":" + os.environ["PATH"],
    "FIXTURE_COMMIT": "a" * 40, "FIXTURE_ARCHIVE": str(archive),
    "FIXTURE_PNG": str(work / "fixture.png"), "FIXTURE_STATUS": "0",
    "FIXTURE_LOCK": "1", "WEB_E2E_RUN_ID": "locked", "WEB_E2E_SHARD": "all",
}, capture_output=True, timeout=10)
mode = repo / ".dev/web-e2e-results" / ("a" * 40) / "locked" / "compare"
rejected = mode.with_name("compare.rejected")
for folder in ["diagnostics", "review-images"]:
    (rejected / folder).chmod(0o755)
assert result.returncode == 0, result.stderr
assert b"browser evidence unavailable" in result.stderr
assert not mode.exists()
assert (rejected / "review-images/response.json").exists()
(repo / "scripts/collect-browser-diagnostics.py").write_bytes(collector.read_bytes())
# The shards partition every spec on disk: none skipped, none run twice.
config = (makefile.parent / "apps/web/e2e/playwright.config.ts").read_text()
match = re.search(r"testMatch: surface === 'harness'\s*\?\s*\[(.*?)\]\s*:\s*\[(.*?)\]", config, re.S)
assert match, "Playwright surface spec lists are missing"
surfaces = [sorted(re.findall(r"'([^']+\.spec\.ts)'", part)) for part in match.groups()]
disk = sorted(path.name for path in (makefile.parent / "apps/web/e2e").glob("*.spec.ts"))
assert sorted(surfaces[0] + surfaces[1]) == disk, "every e2e spec must belong to exactly one surface"
for shard, runs in selections.items():
    assert all(run == runs[0] for run in runs), f"{shard}: selection depends on the result"
for index, surface in enumerate(["harness", "normal"]):
    assert sorted(selections["all"][0][index]) == surfaces[index], f"all must run every {surface} spec"
    sharded = sorted(spec for shard in shards for spec in selections[shard][0][index])
    assert sharded == surfaces[index], f"{surface} shards must run every spec exactly once: {sharded}"
prints = [spec for spec in disk if spec.startswith("print")]
assert prints and all(selections["preview-gap"][0][0].count(spec) == 1 for spec in prints), "Print spec selection drifted"
for status in [42, 0]:
    calls = work / f"template-calls-{status}.json"
    result = subprocess.run(["/usr/bin/make", "--no-print-directory", "web-template-pages"], cwd=repo, env={
        **os.environ, "PATH": str(bin_dir) + ":" + os.environ["PATH"],
        "FIXTURE_COMMIT": "a" * 40, "FIXTURE_ARCHIVE": str(archive),
        "FIXTURE_PNG": str(work / "fixture.png"), "FIXTURE_STATUS": str(status),
        "FIXTURE_CALLS": str(calls), "TEMPLATE_PAGES_RUN_ID": f"pages-{status}",
    }, capture_output=True, timeout=10)
    assert result.returncode == (2 if status else 0), result.stderr
    args = json.loads(calls.read_text())
    image = next(line.split(" := ", 1)[1] for line in makefile.read_text().splitlines() if line.startswith("WEB_E2E_IMAGE := "))
    assert image in args
    assert "--platform" in args and "linux/amd64" in args
    mounts = [args[i + 1] for i, arg in enumerate(args) if arg == "-v"]
    assert len(mounts) == 2
    assert any(mount.endswith(":/candidate.tar:ro") for mount in mounts)
    assert any(mount.endswith(":/results:rw") for mount in mounts)
    assert "TEMPLATE_PAGES_BROWSER_IMAGE=" + image in args
    command = args[-1]
    assert "cd /tmp/aboutme/apps/web" in command
    assert "NUXT_HARNESS=1 node node_modules/nuxt/bin/nuxt.mjs build" in command
    assert "node scripts/generate-template-pages.mts --candidate /results/candidate" in command
    assert "playwright test" not in command and "--update-snapshots" not in command
PYTHON

: >"$CALLS"
if (
  cd "$REPO"
  PATH="$BIN:/usr/bin:/bin" CALL_LOG="$CALLS" PODMAN_PS_MODE=compose-db \
    /usr/bin/make --no-print-directory test-db-up
) >"$WORK/compose-db.out" 2>&1; then
  printf 'makefile-safety-test: test-db-up passed beside Compose Postgres\n' >&2
  exit 1
fi
if grep -Eq '^podman run([[:space:]]|$)' "$CALLS"; then
  printf 'makefile-safety-test: test-db-up started a second Postgres\n' >&2
  exit 1
fi

for mode in unrelated malformed missing-labels invalid-labels; do
  : >"$CALLS"
  (cd "$REPO" && PATH="$BIN:/usr/bin:/bin" CALL_LOG="$CALLS" PODMAN_PS_MODE="$mode" \
    /usr/bin/make --no-print-directory test-db-up) >"$WORK/$mode.out" 2>&1 || true
  if [ "$mode" = unrelated ]; then
    grep -q '^podman run ' "$CALLS" || { echo 'unrelated project blocked test database' >&2; exit 1; }
  elif grep -q '^podman run ' "$CALLS"; then
    echo 'malformed inspection allowed container mutation' >&2
    exit 1
  fi
done

printf 'Makefile container-discovery safety tests passed\n'

HTTPS_REPO=$WORK/https-repo
HTTPS_BIN=$WORK/https-bin
HTTPS_CALLS=$WORK/https.calls
HTTPS_ID=sha256:1111111111111111111111111111111111111111111111111111111111111111
HTTPS_BASE='mcr.microsoft.com/playwright:v1.64.0-noble@sha256:bc72a8df40831ded821e3cbc79c318c21b44d6e51dff9fd89ace7a8249c5b462'
mkdir -p "$HTTPS_REPO/scripts" "$HTTPS_REPO/deploy/dev-https-browser" \
  "$HTTPS_REPO/apps/server" \
  "$HTTPS_REPO/.dev/native-https/input" "$HTTPS_BIN"
chmod 0700 "$HTTPS_REPO/.dev/native-https" \
  "$HTTPS_REPO/.dev/native-https/input"
cp "$ROOT/Makefile" "$HTTPS_REPO/Makefile"
mapfile -t spec_sources <"$ROOT/deploy/dev-https-browser/spec-sources.txt"
for file in Dockerfile package.json package-lock.json run.sh verify-evidence.mjs \
  spec-sources.txt "${spec_sources[@]}"; do
  cp "$ROOT/deploy/dev-https-browser/$file" "$HTTPS_REPO/deploy/dev-https-browser/"
done
mkdir -p "$HTTPS_REPO/scripts/lib"
cp "$ROOT/scripts/lib/dev-https-browser-stage.sh" "$ROOT/scripts/lib/platform.sh" "$HTTPS_REPO/scripts/lib/"
cp "$ROOT/scripts/dev-https-check.sh" "$HTTPS_REPO/scripts/dev-https-check.sh"
printf '%s\n' 'static test root' > \
  "$HTTPS_REPO/.dev/native-https/input/caddy-root.crt"
chmod 0600 "$HTTPS_REPO/.dev/native-https/input/caddy-root.crt"
: >"$HTTPS_CALLS"

cat >"$HTTPS_REPO/scripts/dev-https.sh" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'lifecycle\0' >>"$HTTPS_CALL_LOG"
printf '%s\0' "$@" >>"$HTTPS_CALL_LOG"
printf '\n' >>"$HTTPS_CALL_LOG"
[ "${DEV_HTTPS_FAKE_FAIL:-}" != "${1-}" ]
EOF
chmod 0700 "$HTTPS_REPO/scripts/dev-https.sh"

cat >"$HTTPS_BIN/podman" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'podman\0' >>"$HTTPS_CALL_LOG"
printf '%s\0' "$@" >>"$HTTPS_CALL_LOG"
printf '\n' >>"$HTTPS_CALL_LOG"
case ${1-}:${2-} in
build:*)
  [ "${FAKE_PODMAN_BUILD_FAIL:-0}" != 1 ] || exit 125
  iidfile=
  while [ "$#" -gt 0 ]; do
    if [ "$1" = --iidfile ]; then shift; iidfile=${1-}; break; fi
    shift
  done
  [ -n "$iidfile" ]
  printf '%s\n' "$FAKE_HTTPS_IMAGE_ID" >"$iidfile"
  ;;
image:inspect)
  if [[ " $* " == *Config.Labels* ]]; then
    printf '%s|pwuser|["/opt/aboutme-auth/run.sh","--inside"]|2|%s|1.64.0|2:3.98-1ubuntu0.3\n' \
      "${FAKE_HTTPS_IMAGE_ID#sha256:}" "$FAKE_HTTPS_BASE"
  else
    printf '%s\n' "${FAKE_HTTPS_IMAGE_ID#sha256:}"
  fi
  ;;
run:*)
  [ "${FAKE_PODMAN_RUN_FAIL:-0}" != 1 ] || exit 125
  ;;
*) exit 64 ;;
esac
EOF
chmod 0700 "$HTTPS_BIN/podman"

cat >"$HTTPS_BIN/go" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
[ "${1-}" = build ] || exit 64
output=
while [ "$#" -gt 0 ]; do
  if [ "$1" = -o ]; then
    shift
    output=${1-}
    break
  fi
  shift
done
[ -n "$output" ] || exit 64
cat >"$output" <<'FIXTURE'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'fixture\0' >>"$HTTPS_CALL_LOG"
printf '%s\0' "$@" >>"$HTTPS_CALL_LOG"
printf '\n' >>"$HTTPS_CALL_LOG"
FIXTURE
chmod 0700 "$output"
EOF
chmod 0700 "$HTTPS_BIN/go"

run_https_make() {
  local target=${!#}
  set -- "${@:1:$#-1}"
  (
    cd "$HTTPS_REPO"
    env PATH="$HTTPS_BIN:$PATH" HTTPS_CALL_LOG="$HTTPS_CALLS" \
      FAKE_HTTPS_IMAGE_ID="$HTTPS_ID" FAKE_HTTPS_BASE="$HTTPS_BASE" "$@" \
      /usr/bin/make --no-print-directory "$target"
  )
}

read_https_calls() {
  tr '\0' '\n' <"$HTTPS_CALLS"
}

assert_https_delegation() {
  local target=$1 expected=$2
  shift 2
  : >"$HTTPS_CALLS"
  run_https_make "$@" "$target" >"$WORK/$target.out" 2>&1 || {
    printf 'makefile-safety-test: make %s failed\n' "$target" >&2
    exit 1
  }
  mapfile -t actual < <(read_https_calls)
  [ "${actual[0]-}" = lifecycle ] && [ "${actual[1]-}" = "$expected" ] || {
    printf 'makefile-safety-test: make %s delegated incorrectly\n' "$target" >&2
    exit 1
  }
}

assert_https_delegation dev-https up
assert_https_delegation dev-https-down down
assert_https_delegation dev-https-status status
assert_https_delegation dev-https-logs logs ARGS='-f server'
mapfile -t log_call < <(read_https_calls)
[ "${log_call[2]-}" = -f ] && [ "${log_call[3]-}" = server ] || {
  printf 'makefile-safety-test: dev-https-logs lost ARGS\n' >&2
  exit 1
}

rm -f "$HTTPS_REPO/.dev/native-https/browser-image.manifest"
: >"$HTTPS_CALLS"
if run_https_make DEV_HTTPS_FAKE_FAIL=status dev-https-browser-image \
  >"$WORK/https-image-preflight.out" 2>&1; then
  printf 'makefile-safety-test: browser image passed failed status\n' >&2
  exit 1
fi
if read_https_calls | grep -Fxq podman; then
  printf 'makefile-safety-test: browser image mutated after failed status\n' >&2
  exit 1
fi
[ ! -e "$HTTPS_REPO/.dev/native-https/browser-image.manifest" ] || {
  printf 'makefile-safety-test: failed browser image left a manifest\n' >&2
  exit 1
}

if /bin/bash -c '(( BASH_VERSINFO[0] < 4 ))'; then
  : >"$HTTPS_CALLS"
  if (
    cd "$HTTPS_REPO"
    env PATH="$HTTPS_BIN:$PATH" HTTPS_CALL_LOG="$HTTPS_CALLS" \
      FAKE_HTTPS_IMAGE_ID="$HTTPS_ID" FAKE_HTTPS_BASE="$HTTPS_BASE" \
      /usr/bin/make --no-print-directory SHELL=/bin/bash dev-https-browser-image
  ) >"$WORK/https-image-old-bash.out" 2>&1; then
    printf 'makefile-safety-test: browser image accepted Bash below 4\n' >&2
    exit 1
  fi
  grep -Fq 'dev-https-browser-image requires Bash 4 or newer' \
    "$WORK/https-image-old-bash.out" || {
    printf 'makefile-safety-test: browser image lacked Bash setup message\n' >&2
    exit 1
  }
  if read_https_calls | grep -Fxq podman; then
    printf 'makefile-safety-test: old Bash reached the image build\n' >&2
    exit 1
  fi
fi

mkdir "$WORK/shell-bin"
cat >"$WORK/shell-bin/bash" <<'EOF'
#!/bin/sh
case ${2-} in
  *'podman build --tag'*) printf 'browser image\n' >>"$SHELL_CALL_LOG" ;;
esac
exec "$FIXTURE_BASH" "$@"
EOF
chmod +x "$WORK/shell-bin/bash"
: >"$WORK/shell.calls"
: >"$HTTPS_CALLS"
run_https_make PATH="$WORK/shell-bin:$HTTPS_BIN:$PATH" \
  FIXTURE_BASH="$BASH" SHELL_CALL_LOG="$WORK/shell.calls" \
  dev-https-browser-image >"$WORK/https-image.out" 2>&1 || {
  printf 'makefile-safety-test: browser image target failed\n' >&2
  exit 1
}
grep -Fxq 'browser image' "$WORK/shell.calls" || {
  printf 'makefile-safety-test: browser image did not use Bash from PATH\n' >&2
  exit 1
}
HTTPS_MANIFEST=$HTTPS_REPO/.dev/native-https/browser-image.manifest
[ -f "$HTTPS_MANIFEST" ] && [ ! -L "$HTTPS_MANIFEST" ] || {
  printf 'makefile-safety-test: browser image manifest is missing\n' >&2
  exit 1
}
[ "$(stat -c %a "$HTTPS_MANIFEST")" = 600 ] || {
  printf 'makefile-safety-test: browser image manifest mode drifted\n' >&2
  exit 1
}
grep -Fxq "image_id=$HTTPS_ID" "$HTTPS_MANIFEST" || {
  printf 'makefile-safety-test: manifest lacks immutable image ID\n' >&2
  exit 1
}
grep -Eq '^source_sha256=[0-9a-f]{64}$' "$HTTPS_MANIFEST" || {
  printf 'makefile-safety-test: manifest lacks source hash\n' >&2
  exit 1
}
read_https_calls | grep -Fxq -- '--iidfile' || {
  printf 'makefile-safety-test: image build lacks IID capture\n' >&2
  exit 1
}
read_https_calls | grep -Fxq -- '--tag' || {
  printf 'makefile-safety-test: image build lacks the fixed tag\n' >&2
  exit 1
}
read_https_calls | grep -Fxq -- 'localhost/aboutme-dev-https-browser:local' || {
  printf 'makefile-safety-test: image build tag drifted\n' >&2
  exit 1
}
read_https_calls | grep -Fxq -- 'deploy/dev-https-browser' || {
  printf 'makefile-safety-test: image build context drifted\n' >&2
  exit 1
}

# Spec drift must NOT block a check: specs are staged per run, not baked
# into the image, so an edited spec runs without an image rebuild.
cp "$HTTPS_REPO/deploy/dev-https-browser/transport.spec.ts" \
  "$WORK/transport.spec.ts.clean"
printf '%s\n' '// source-drift' >> \
  "$HTTPS_REPO/deploy/dev-https-browser/transport.spec.ts"
: >"$HTTPS_CALLS"
run_https_make dev-https-auth-check >"$WORK/https-source-drift.out" 2>&1 || {
  sed -n '1,40p' "$WORK/https-source-drift.out" >&2
  printf 'makefile-safety-test: spec drift blocked the check\n' >&2
  exit 1
}
read_https_calls | grep -Fxq podman || {
  printf 'makefile-safety-test: spec-drift check never reached the runtime\n' >&2
  exit 1
}
cp "$WORK/transport.spec.ts.clean" \
  "$HTTPS_REPO/deploy/dev-https-browser/transport.spec.ts"

# Image-side drift must still block before any runtime call.
for image_source in run.sh spec-sources.txt; do
  cp "$HTTPS_REPO/deploy/dev-https-browser/$image_source" "$WORK/$image_source.clean"
  printf '%s\n' '# image-source-drift' >> \
    "$HTTPS_REPO/deploy/dev-https-browser/$image_source"
  : >"$HTTPS_CALLS"
  if run_https_make dev-https-auth-check \
    >"$WORK/https-image-source-drift.out" 2>&1; then
    printf 'makefile-safety-test: image source drift was accepted\n' >&2
    exit 1
  fi
  grep -Fq 'browser image sources changed after image build' \
    "$WORK/https-image-source-drift.out" || {
    printf 'makefile-safety-test: image drift returned the wrong diagnostic\n' >&2
    exit 1
  }
  if read_https_calls | grep -Fxq podman; then
    printf 'makefile-safety-test: image source drift reached the browser runtime\n' >&2
    exit 1
  fi
  cp "$WORK/$image_source.clean" "$HTTPS_REPO/deploy/dev-https-browser/$image_source"
done
rm -rf "$HTTPS_REPO/.dev/native-https/evidence"

rm -f "$HTTPS_MANIFEST"
: >"$HTTPS_CALLS"
if run_https_make DEV_HTTPS_FAKE_FAIL=status dev-https-auth-check \
  >"$WORK/https-auth-preflight.out" 2>&1; then
  printf 'makefile-safety-test: auth check passed failed status\n' >&2
  exit 1
fi
if read_https_calls | grep -Fxq podman; then
  printf 'makefile-safety-test: auth check mutated after failed status\n' >&2
  exit 1
fi
[ ! -e "$HTTPS_REPO/.dev/native-https/evidence" ] || {
  printf 'makefile-safety-test: failed auth preflight created evidence\n' >&2
  exit 1
}

: >"$HTTPS_CALLS"
run_https_make dev-https-browser-image >"$WORK/https-image-2.out" 2>&1
run_https_make dev-https-auth-check >"$WORK/https-auth.out" 2>&1 || {
  sed -n '1,120p' "$WORK/https-auth.out" >&2
  printf 'makefile-safety-test: auth check target failed\n' >&2
  exit 1
}
HTTPS_EVIDENCE_ROOT=$HTTPS_REPO/.dev/native-https/evidence
[ -d "$HTTPS_EVIDENCE_ROOT" ] && [ ! -L "$HTTPS_EVIDENCE_ROOT" ] || {
  printf 'makefile-safety-test: evidence root is missing\n' >&2
  exit 1
}
[ "$(stat -c %a "$HTTPS_EVIDENCE_ROOT")" = 700 ] || {
  printf 'makefile-safety-test: evidence root mode drifted\n' >&2
  exit 1
}
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d | wc -l)" -eq 1 ] || {
  printf 'makefile-safety-test: auth check did not create one fresh evidence directory\n' >&2
  exit 1
}
HTTPS_RUNTIME_LOG=$WORK/https-runtime.calls
read_https_calls >"$HTTPS_RUNTIME_LOG"
grep -Fxq -- '--pull=never' "$HTTPS_RUNTIME_LOG" || {
  printf 'makefile-safety-test: auth run can pull an image\n' >&2
  exit 1
}
grep -Fxq -- '--network=host' "$HTTPS_RUNTIME_LOG" || {
  printf 'makefile-safety-test: auth run lacks host network\n' >&2
  exit 1
}
grep -Fxq -- '--read-only' "$HTTPS_RUNTIME_LOG" || {
  printf 'makefile-safety-test: auth run lacks read-only root\n' >&2
  exit 1
}
grep -Fxq -- '--cap-drop=all' "$HTTPS_RUNTIME_LOG" || {
  printf 'makefile-safety-test: auth run lacks capability drop\n' >&2
  exit 1
}
grep -Fxq -- '--cap-add=SYS_CHROOT' "$HTTPS_RUNTIME_LOG" || {
  printf 'makefile-safety-test: auth run lacks the Chromium sandbox capability\n' >&2
  exit 1
}
[ "$(grep -Ec '^--cap-add=' "$HTTPS_RUNTIME_LOG")" -eq 1 ] || {
  printf 'makefile-safety-test: auth run added an unexpected capability\n' >&2
  exit 1
}
grep -Fxq -- "$HTTPS_ID" "$HTTPS_RUNTIME_LOG" || {
  printf 'makefile-safety-test: auth run did not use immutable image ID\n' >&2
  exit 1
}
[ "$(grep -Ec '^--mount=type=bind,.*dst=/' "$HTTPS_RUNTIME_LOG")" -eq 3 ] || {
  printf 'makefile-safety-test: auth run has the wrong bind-mount count\n' >&2
  exit 1
}
grep -Eq '^--mount=type=bind,src=.*,dst=/uat-spec,ro=true$' "$HTTPS_RUNTIME_LOG" || {
  printf 'makefile-safety-test: auth run lacks the read-only spec mount\n' >&2
  exit 1
}
if grep -Eq '(/run/podman/podman\.sock|/var/run/docker\.sock|dst=/home|dst=/repo|dst=/workspace)' \
  "$HTTPS_RUNTIME_LOG"; then
  printf 'makefile-safety-test: auth run exposed a forbidden host path\n' >&2
  exit 1
fi
[ "$(grep -c '^transport$' "$HTTPS_RUNTIME_LOG")" -eq 0 ] || {
  printf 'makefile-safety-test: three-argument auth run gained a mode argument\n' >&2
  exit 1
}

: >"$HTTPS_CALLS"
if run_https_make DEV_HTTPS_FAKE_FAIL=status dev-https-transport-check \
  >"$WORK/https-transport-preflight.out" 2>&1; then
  printf 'makefile-safety-test: transport check passed failed status\n' >&2
  exit 1
fi
if read_https_calls | grep -Fxq podman; then
  printf 'makefile-safety-test: transport check mutated after failed status\n' >&2
  exit 1
fi
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d | wc -l)" -eq 1 ] || {
  printf 'makefile-safety-test: failed transport preflight created evidence\n' >&2
  exit 1
}

: >"$HTTPS_CALLS"
run_https_make dev-https-transport-check >"$WORK/https-transport.out" 2>&1 || {
  sed -n '1,120p' "$WORK/https-transport.out" >&2
  printf 'makefile-safety-test: transport check target failed\n' >&2
  exit 1
}
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d | wc -l)" -eq 2 ] || {
  printf 'makefile-safety-test: transport check did not create separate evidence\n' >&2
  exit 1
}
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d -name 'google-auth.*' | wc -l)" -eq 1 ] || {
  printf 'makefile-safety-test: auth evidence directory naming drifted\n' >&2
  exit 1
}
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d -name 'transport.*' | wc -l)" -eq 1 ] || {
  printf 'makefile-safety-test: transport evidence directory naming drifted\n' >&2
  exit 1
}
find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d -printf '%m\n' |
  awk '$0 != "700" { exit 1 }' || {
  printf 'makefile-safety-test: evidence directory mode drifted\n' >&2
  exit 1
}
HTTPS_TRANSPORT_LOG=$WORK/https-transport-runtime.calls
read_https_calls >"$HTTPS_TRANSPORT_LOG"
grep -Fxq transport "$HTTPS_TRANSPORT_LOG" || {
  printf 'makefile-safety-test: transport mode did not reach the runner\n' >&2
  exit 1
}
grep -Fxq -- '--pull=never' "$HTTPS_TRANSPORT_LOG" || {
  printf 'makefile-safety-test: transport run can pull an image\n' >&2
  exit 1
}
grep -Fxq -- '--network=host' "$HTTPS_TRANSPORT_LOG" || {
  printf 'makefile-safety-test: transport run lacks host network\n' >&2
  exit 1
}
grep -Fxq -- '--read-only' "$HTTPS_TRANSPORT_LOG" || {
  printf 'makefile-safety-test: transport run lacks read-only root\n' >&2
  exit 1
}
[ "$(grep -Ec '^--mount=type=bind,.*dst=/' "$HTTPS_TRANSPORT_LOG")" -eq 3 ] || {
  printf 'makefile-safety-test: transport run has the wrong bind-mount count\n' >&2
  exit 1
}

: >"$HTTPS_CALLS"
if run_https_make DEV_HTTPS_FAKE_FAIL=status dev-https-editor-check \
  >"$WORK/https-editor-preflight.out" 2>&1; then
  printf 'makefile-safety-test: editor check passed failed status\n' >&2
  exit 1
fi
if read_https_calls | grep -Fxq podman; then
  printf 'makefile-safety-test: editor check mutated after failed status\n' >&2
  exit 1
fi
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d | wc -l)" -eq 2 ] || {
  printf 'makefile-safety-test: failed editor preflight created evidence\n' >&2
  exit 1
}

: >"$HTTPS_CALLS"
run_https_make dev-https-editor-check >"$WORK/https-editor.out" 2>&1 || {
  sed -n '1,120p' "$WORK/https-editor.out" >&2
  printf 'makefile-safety-test: editor check target failed\n' >&2
  exit 1
}
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d | wc -l)" -eq 3 ] || {
  printf 'makefile-safety-test: editor check did not create separate evidence\n' >&2
  exit 1
}
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d -name 'editor.*' | wc -l)" -eq 1 ] || {
  printf 'makefile-safety-test: editor evidence directory naming drifted\n' >&2
  exit 1
}
HTTPS_EDITOR_LOG=$WORK/https-editor-runtime.calls
read_https_calls >"$HTTPS_EDITOR_LOG"
grep -Fxq editor "$HTTPS_EDITOR_LOG" || {
  printf 'makefile-safety-test: editor mode did not reach the runner\n' >&2
  exit 1
}
grep -Fxq -- '--pull=never' "$HTTPS_EDITOR_LOG" || {
  printf 'makefile-safety-test: editor run can pull an image\n' >&2
  exit 1
}
grep -Fxq -- '--network=host' "$HTTPS_EDITOR_LOG" || {
  printf 'makefile-safety-test: editor run lacks host network\n' >&2
  exit 1
}
grep -Fxq -- '--read-only' "$HTTPS_EDITOR_LOG" || {
  printf 'makefile-safety-test: editor run lacks read-only root\n' >&2
  exit 1
}
[ "$(grep -Ec '^--mount=type=bind,.*dst=/' "$HTTPS_EDITOR_LOG")" -eq 3 ] || {
  printf 'makefile-safety-test: editor run has the wrong bind-mount count\n' >&2
  exit 1
}

: >"$HTTPS_CALLS"
if run_https_make DEV_HTTPS_FAKE_FAIL=status dev-https-mcp-check \
  >"$WORK/https-mcp-preflight.out" 2>&1; then
  printf 'makefile-safety-test: mcp check passed failed status\n' >&2
  exit 1
fi
if read_https_calls | grep -Eq '^(podman|fixture)$'; then
  printf 'makefile-safety-test: mcp check mutated after failed status\n' >&2
  exit 1
fi
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d | wc -l)" -eq 3 ] || {
  printf 'makefile-safety-test: failed mcp preflight created evidence\n' >&2
  exit 1
}

: >"$HTTPS_CALLS"
mkdir "$WORK/uuid-bin"
cat >"$WORK/uuid-bin/python3" <<'EOF'
#!/bin/bash
if [ "${1-}" = -I ] && [ "${2-}" = -c ] &&
  [ "${3-}" = 'import uuid; print(uuid.uuid4())' ]; then
  printf '%s\n' '12345678-1234-4123-8123-123456789abc'
else
  exec "$FIXTURE_PYTHON" "$@"
fi
EOF
chmod +x "$WORK/uuid-bin/python3"
run_https_make PATH="$WORK/uuid-bin:$HTTPS_BIN:$PATH" \
  FIXTURE_PYTHON="$(command -v python3)" dev-https-mcp-check >"$WORK/https-mcp.out" 2>&1 || {
  sed -n '1,120p' "$WORK/https-mcp.out" >&2
  printf 'makefile-safety-test: mcp check target failed\n' >&2
  exit 1
}
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d | wc -l)" -eq 4 ] || {
  printf 'makefile-safety-test: mcp check did not create separate evidence\n' >&2
  exit 1
}
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d -name 'mcp.*' | wc -l)" -eq 1 ] || {
  printf 'makefile-safety-test: mcp evidence directory naming drifted\n' >&2
  exit 1
}
HTTPS_MCP_LOG=$WORK/https-mcp-runtime.calls
read_https_calls >"$HTTPS_MCP_LOG"
[ "$(grep -Fxc 'aboutme MCP UAT 12345678-1234-4123-8123-123456789abc' "$HTTPS_MCP_LOG")" -eq 3 ] || {
  printf 'makefile-safety-test: mcp fixture did not use the generated client name\n' >&2
  exit 1
}
grep -Fxq mcp "$HTTPS_MCP_LOG" || {
  printf 'makefile-safety-test: mcp mode did not reach the runner\n' >&2
  exit 1
}
[ "$(grep -c '^fixture$' "$HTTPS_MCP_LOG")" -eq 3 ] || {
  printf 'makefile-safety-test: mcp fixture lifecycle drifted\n' >&2
  exit 1
}
[ "$(grep -c '^cleanup$' "$HTTPS_MCP_LOG")" -eq 2 ] &&
  [ "$(grep -c '^seed$' "$HTTPS_MCP_LOG")" -eq 1 ] || {
  printf 'makefile-safety-test: mcp fixture seed/cleanup drifted\n' >&2
  exit 1
}

: >"$HTTPS_CALLS"
if run_https_make DEV_HTTPS_FAKE_FAIL=status dev-https-entry-check \
  >"$WORK/https-entry-preflight.out" 2>&1; then
  printf 'makefile-safety-test: entry check passed failed status\n' >&2
  exit 1
fi
if read_https_calls | grep -Eq '^(podman|fixture)$'; then
  printf 'makefile-safety-test: entry check mutated after failed status\n' >&2
  exit 1
fi
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d | wc -l)" -eq 4 ] || {
  printf 'makefile-safety-test: failed entry preflight created evidence\n' >&2
  exit 1
}

: >"$HTTPS_CALLS"
run_https_make dev-https-entry-check >"$WORK/https-entry.out" 2>&1 || {
  sed -n '1,120p' "$WORK/https-entry.out" >&2
  printf 'makefile-safety-test: entry check target failed\n' >&2
  exit 1
}
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d | wc -l)" -eq 5 ] || {
  printf 'makefile-safety-test: entry check did not create separate evidence\n' >&2
  exit 1
}
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d -name 'entry.*' | wc -l)" -eq 1 ] || {
  printf 'makefile-safety-test: entry evidence directory naming drifted\n' >&2
  exit 1
}
HTTPS_ENTRY_LOG=$WORK/https-entry-runtime.calls
read_https_calls >"$HTTPS_ENTRY_LOG"
grep -Fxq entry "$HTTPS_ENTRY_LOG" || {
  printf 'makefile-safety-test: entry mode did not reach the runner\n' >&2
  exit 1
}
[ "$(grep -c '^fixture$' "$HTTPS_ENTRY_LOG")" -eq 1 ] &&
  [ "$(grep -c '^seed$' "$HTTPS_ENTRY_LOG")" -eq 1 ] &&
  [ "$(grep -c '^cleanup$' "$HTTPS_ENTRY_LOG")" -eq 0 ] || {
  printf 'makefile-safety-test: entry seed lifecycle drifted\n' >&2
  exit 1
}

: >"$HTTPS_CALLS"
if run_https_make DEV_HTTPS_FAKE_FAIL=status dev-https-publish-check \
  >"$WORK/https-publish-preflight.out" 2>&1; then
  printf 'makefile-safety-test: publish check passed failed status\n' >&2
  exit 1
fi
if read_https_calls | grep -Eq '^(podman|fixture)$'; then
  printf 'makefile-safety-test: publish check mutated after failed status\n' >&2
  exit 1
fi
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d | wc -l)" -eq 5 ] || {
  printf 'makefile-safety-test: failed publish preflight created evidence\n' >&2
  exit 1
}

: >"$HTTPS_CALLS"
run_https_make dev-https-publish-check >"$WORK/https-publish.out" 2>&1 || {
  sed -n '1,120p' "$WORK/https-publish.out" >&2
  printf 'makefile-safety-test: publish check target failed\n' >&2
  exit 1
}
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d | wc -l)" -eq 6 ] || {
  printf 'makefile-safety-test: publish check did not create separate evidence\n' >&2
  exit 1
}
[ "$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d -name 'publish.*' | wc -l)" -eq 1 ] || {
  printf 'makefile-safety-test: publish evidence directory naming drifted\n' >&2
  exit 1
}
HTTPS_PUBLISH_LOG=$WORK/https-publish-runtime.calls
read_https_calls >"$HTTPS_PUBLISH_LOG"
grep -Fxq publish "$HTTPS_PUBLISH_LOG" || {
  printf 'makefile-safety-test: publish mode did not reach the runner\n' >&2
  exit 1
}
[ "$(grep -c '^fixture$' "$HTTPS_PUBLISH_LOG")" -eq 1 ] &&
  [ "$(grep -c '^seed$' "$HTTPS_PUBLISH_LOG")" -eq 1 ] &&
  [ "$(grep -c '^cleanup$' "$HTTPS_PUBLISH_LOG")" -eq 0 ] || {
  printf 'makefile-safety-test: publish seed lifecycle drifted\n' >&2
  exit 1
}

: >"$HTTPS_CALLS"
if run_https_make FAKE_PODMAN_RUN_FAIL=1 dev-https-publish-check \
  >"$WORK/https-publish-browser-failure.out" 2>&1; then
  printf 'makefile-safety-test: failed publish browser was accepted\n' >&2
  exit 1
fi
if find "$HTTPS_REPO/.dev/native-https" -mindepth 1 -maxdepth 1 \
  -type d -name 'spec-input.*' -print -quit | grep -q .; then
  printf 'makefile-safety-test: publish failure retained staged specs\n' >&2
  exit 1
fi

HTTPS_EVIDENCE_KEEP=$(awk -F= '$1 == "EVIDENCE_KEEP" { print $2; exit }' \
  "$HTTPS_REPO/scripts/dev-https-check.sh")
[[ $HTTPS_EVIDENCE_KEEP =~ ^[1-9][0-9]*$ ]] || {
  printf 'makefile-safety-test: could not read publish evidence bound\n' >&2
  exit 1
}
find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d \
  -name 'publish.*' -exec rm -rf -- {} +
for ((failure_run = 1; failure_run <= HTTPS_EVIDENCE_KEEP + 1; failure_run++)); do
  : >"$HTTPS_CALLS"
  if run_https_make FAKE_PODMAN_RUN_FAIL=1 dev-https-publish-check \
    >"$WORK/https-publish-failure-$failure_run.out" 2>&1; then
    printf 'makefile-safety-test: failed publish run %s was accepted\n' \
      "$failure_run" >&2
    exit 1
  fi
done
HTTPS_PUBLISH_FAILURE_COUNT=$(find "$HTTPS_EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 \
  -type d -name 'publish.*' | wc -l)
[ "$HTTPS_PUBLISH_FAILURE_COUNT" -le "$HTTPS_EVIDENCE_KEEP" ] || {
  printf 'makefile-safety-test: failed publish retained %s evidence directories (keep=%s)\n' \
    "$HTTPS_PUBLISH_FAILURE_COUNT" "$HTTPS_EVIDENCE_KEEP" >&2
  exit 1
}

ROUTE_PLAN=$(cd "$ROOT" && /usr/bin/make --no-print-directory -n route-table-test)
GENERATOR_LINE=$(grep -Fn 'node scripts/generate-public-roots.mjs --check' <<<"$ROUTE_PLAN" | head -n 1 | cut -d: -f1)
ROUTE_LINE=$(grep -Fn 'go test ./internal/routetable/' <<<"$ROUTE_PLAN" | head -n 1 | cut -d: -f1)
if [ -z "$GENERATOR_LINE" ] || [ -z "$ROUTE_LINE" ] ||
  [ "$GENERATOR_LINE" -ge "$ROUTE_LINE" ]; then
  printf 'makefile-safety-test: route-table-test does not verify generated roots first\n' >&2
  exit 1
fi

DEV_DOWN_PLAN=$(cd "$ROOT" && /usr/bin/make --no-print-directory -n dev-down)
[[ $DEV_DOWN_PLAN != *'node scripts/generate-public-roots.mjs --check'* ]] || {
  printf 'makefile-safety-test: dev-down depends on source generation\n' >&2
  exit 1
}
[[ $DEV_DOWN_PLAN == *'APP_BUILD_DIGEST=sha256:0000000000000000000000000000000000000000000000000000000000000000'* ]] || {
  printf 'makefile-safety-test: dev-down lacks a safe application digest placeholder\n' >&2
  exit 1
}
[[ $DEV_DOWN_PLAN == *'PUBLIC_RENDERER_BUILD_DIGEST=sha256:0000000000000000000000000000000000000000000000000000000000000000'* ]] || {
  printf 'makefile-safety-test: dev-down lacks a safe renderer digest placeholder\n' >&2
  exit 1
}
[[ $DEV_DOWN_PLAN == *'podman compose --env-file .env -f deploy/compose.yml down'* ]] || {
  printf 'makefile-safety-test: dev-down no longer invokes the expected Compose teardown\n' >&2
  exit 1
}

printf 'Makefile native HTTPS safety tests passed\n'
