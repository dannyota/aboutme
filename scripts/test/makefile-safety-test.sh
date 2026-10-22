#!/usr/bin/env bash

# Make target safety contracts.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-make-safety.XXXXXX")
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
  if [ "${PODMAN_PS_MODE:-fail}" = compose-db ]; then
    printf '%s\n' 'aboutme-postgres-1|aboutme|postgres'
    exit 0
  fi
  exit 17
fi
exit 0
EOF

cat >"$BIN/semgrep" <<'EOF'
#!/usr/bin/env bash
printf 'semgrep|%s\n' "$*" >>"$CALL_LOG"
EOF
chmod +x "$BIN/podman" "$BIN/semgrep"

assert_semgrep_products() {
  local target=$1 call
  : >"$CALLS"
  (
    cd "$REPO"
    PATH="$BIN:/usr/bin:/bin" CALL_LOG="$CALLS" \
      /usr/bin/make --no-print-directory "$target"
  ) >"$WORK/$target.out" 2>&1
  call=$(cat "$CALLS")
  [ "$(grep -c '^semgrep|' "$CALLS" || true)" -eq 1 ] || {
    printf 'makefile-safety-test: make %s did not run Semgrep exactly once\n' "$target" >&2
    exit 1
  }
  [[ $call != *'--secrets'* && $call != *'p/secrets'* ]] || {
    printf 'makefile-safety-test: make %s enabled Semgrep Secrets\n' "$target" >&2
    exit 1
  }
}

assert_semgrep_products semgrep
assert_semgrep_products semgrep-ci
SEMGREP_CI_CALL=$(cat "$CALLS")
[[ $SEMGREP_CI_CALL == *'--code'* && $SEMGREP_CI_CALL == *'--supply-chain'* ]] || {
  printf 'makefile-safety-test: make semgrep-ci did not select Code and Supply Chain\n' >&2
  exit 1
}

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
assert_web_e2e_rejected invalid-update-run-id web-e2e-update \
  'WEB_E2E_RUN_ID must match [A-Za-z0-9_-]+' WEB_E2E_RUN_ID='bad/value'
assert_web_e2e_rejected update-golden-present web-e2e \
  'UPDATE_GOLDEN must be absent' WEB_E2E_RUN_ID=valid UPDATE_GOLDEN=
assert_web_e2e_rejected playwright-update-present web-e2e \
  'PLAYWRIGHT_UPDATE_SNAPSHOTS must be absent' \
  WEB_E2E_RUN_ID=valid PLAYWRIGHT_UPDATE_SNAPSHOTS=
assert_web_e2e_rejected unknown-shard web-e2e \
  'WEB_E2E_SHARD must be all, preview-gap, or rest' \
  WEB_E2E_RUN_ID=valid WEB_E2E_SHARD=harness

# Exercise the public diagnostic collector without a browser or container.
python3 - "$ROOT/Makefile" "$ROOT/scripts/collect-browser-diagnostics.py" "$WORK" <<'PYTHON'
import hashlib
import io
import json
import os
from pathlib import Path
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
    for name, data in [("apps/web/e2e/baselines/card--no-name.png", png),
                       ("apps/web/public/templates/pages/engineer-compact-en-p1.png", metadata_png),
                       ("apps/web/e2e/card.spec.ts", b"fixture")]:
        info = tarfile.TarInfo(name)
        info.size = len(data)
        tar.addfile(info, io.BytesIO(data))
for fixture in ["mismatch", "symlink", "unknown-image", "raw-response", "malformed-report",
                "special-file", "non-png", "print-actual", "forged-metadata", "template-page",
                "signature-secret", "png-metadata", "png-appended", "bad-crc",
                "truncated-png", "oversized-pixels", "invalid-filter", "invalid-zlib", "inflated-pixels"]:
    mode = work / fixture
    artifacts = mode / "harness/artifacts/private-title-token"
    artifacts.mkdir(parents=True)
    for kind in ["expected", "actual", "diff"]:
        (artifacts / f"card--no-name-{kind}.png").write_bytes(png)
    (artifacts / "test-failed-1.png").write_bytes(png)
    (artifacts / "trace.zip").write_text("private-token")
    (artifacts / "response.json").write_text("private-token")
    (artifacts / "unknown-actual.png").write_bytes(png)
    if fixture == "symlink":
        (artifacts / "card--no-name-actual.png").unlink()
        (artifacts / "card--no-name-actual.png").symlink_to(artifacts / "response.json")
    if fixture in ["special-file", "non-png"]:
        actual = artifacts / "card--no-name-actual.png"
        actual.unlink()
        if fixture == "special-file":
            os.mkfifo(actual)
        else:
            actual.write_text("private-token")
    if fixture == "print-actual":
        (artifacts / "card--no-name-actual.png").rename(artifacts / "card--no-name.png")
        (artifacts / "card--no-name-expected.png").unlink()
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
    if fixture in hostile:
        (artifacts / "card--no-name-actual.png").write_bytes(hostile[fixture])
    if fixture == "template-page":
        (artifacts / "engineer-compact-en-p1.png").write_bytes(png)
        (artifacts / "engineer-compact-en-p1-diff.png").write_bytes(png)
    if fixture == "forged-metadata":
        (mode / "diagnostics").mkdir()
        (mode / "diagnostics/source-metadata.txt").write_text("private-token")
        (mode / "diagnostics/response.json").write_text("private-token")
    normal = mode / "normal/artifacts/private-title-token"
    normal.mkdir(parents=True)
    (normal / "card--no-name-diff.png").write_bytes(png)
    report = {"suites": [{"specs": [{"file": "card.spec.ts", "line": 12,
              "title": "private-token", "tests": [{"results": [{"status": "failed",
              "error": {"message": "private-token"}, "stdout": ["private-token"]}]}]}]}]}
    (mode / "harness/results.json").write_text("invalid" if fixture == "malformed-report" else json.dumps(report))
    result = subprocess.run(["python3", "-I", str(collector), str(mode), str(archive), "a" * 40,
                             "sha256:" + "b" * 64, "c" * 64, "d" * 64, "1"],
                            capture_output=True, timeout=5)
    assert result.returncode == 0, result.stderr
    public = mode / "diagnostics"
    files = list(public.rglob("*"))
    assert not any(path.is_symlink() for path in files)
    assert not any("private" in path.relative_to(public).as_posix() for path in files)
    assert not any(b"private-token" in path.read_bytes() for path in files if path.is_file())
    assert not any(sentinel in path.read_bytes() for path in files if path.is_file())
    for path in files:
        if path.suffix == ".png":
            assert path.read_bytes() == png, path
    assert not any(path.suffix in [".zip", ".html"] for path in files)
    images = [path.name for path in files if path.suffix == ".png" and "harness" in path.parts]
    rejected = ["symlink", "special-file", "non-png", "signature-secret", "bad-crc",
                "truncated-png", "oversized-pixels", "invalid-filter", "invalid-zlib", "inflated-pixels"]
    assert sorted(images) == sorted(["card--no-name-expected.png", "card--no-name-diff.png"] +
                                   ([] if fixture in rejected else ["card--no-name-actual.png"]) +
                                   (["engineer-compact-en-p1-expected.png", "engineer-compact-en-p1-actual.png",
                                     "engineer-compact-en-p1-diff.png"] if fixture == "template-page" else []))
    assert (public / "normal/0/card--no-name-diff.png").is_file()
    assert (public / "normal/0/card--no-name-expected.png").is_file()
    summary = json.loads((public / "failure-summary.json").read_text())
    assert summary["exit_status"] == 1
    assert summary["surfaces"]["harness"]["report"] == ("unavailable" if fixture == "malformed-report" else "available")
    assert "commit=" + "a" * 40 in (public / "source-metadata.txt").read_text()
    assert "browser_image_digest=sha256:" + "b" * 64 in (public / "source-metadata.txt").read_text()
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
    (artifacts / "card--no-name-actual.png").write_bytes(
        signature + rgb_header + chunk(b"IDAT", zlib.compress(filtered)) + chunk(b"IEND", b""))
    result = subprocess.run(["python3", "-I", str(collector), str(mode), str(archive), "a" * 40,
                             "sha256:" + "b" * 64, "c" * 64, "d" * 64, "1"],
                            capture_output=True, timeout=5)
    assert result.returncode == 0, result.stderr
    assert (mode / "diagnostics/harness/0/card--no-name-actual.png").read_bytes() == rgb_png
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
executable(bin_dir / "uname", "#!/bin/bash\nprintf 'x86_64\\n'\n")
executable(repo / "scripts/web-e2e-source.sh", "#!/bin/bash\ncp -- \"$FIXTURE_ARCHIVE\" \"$3\"\n")
executable(bin_dir / "podman", """#!/usr/bin/env python3
import os
from pathlib import Path
import sys
mount = next(arg for arg in sys.argv if arg.endswith(":/results:rw"))
mode = Path(mount.removesuffix(":/results:rw"))
artifacts = mode / "harness/artifacts/fixture"
artifacts.mkdir(parents=True)
(artifacts / "card--no-name-actual.png").write_bytes(Path(os.environ["FIXTURE_PNG"]).read_bytes())
if mode.name == "update":
    (mode / "candidate-baselines").mkdir()
sys.exit(int(os.environ["FIXTURE_STATUS"]))
""")
(work / "fixture.png").write_bytes(png)
for target, shard in [("web-e2e", "preview-gap"), ("web-e2e", "rest"), ("web-e2e-update", "all")]:
    for status in [42, 0]:
        run_id = f"{target}-{shard}-{status}"
        result = subprocess.run(["/usr/bin/make", "--no-print-directory", target], cwd=repo, env={
            **os.environ, "PATH": str(bin_dir) + ":/usr/bin:/bin",
            "FIXTURE_COMMIT": "a" * 40, "FIXTURE_ARCHIVE": str(archive),
            "FIXTURE_PNG": str(work / "fixture.png"), "FIXTURE_STATUS": str(status),
            "WEB_E2E_RUN_ID": run_id, "WEB_E2E_SHARD": shard,
        }, capture_output=True, timeout=10)
        assert result.returncode == (2 if status else 0), result.stderr
        mode = repo / ".dev/web-e2e-results" / ("a" * 40) / run_id / ("update" if target.endswith("update") else "compare")
        summary = json.loads((mode / "diagnostics/failure-summary.json").read_text())
        assert summary["exit_status"] == status
        assert (mode / "diagnostics/harness/0/card--no-name-actual.png").read_bytes() == png
        metadata = (mode / "diagnostics/source-metadata.txt").read_text()
        assert "commit=" + "a" * 40 in metadata
        image = next(line.split(" := ", 1)[1] for line in makefile.read_text().splitlines() if line.startswith("WEB_E2E_IMAGE := "))
        assert metadata == (f"commit={'a' * 40}\n"
                            f"manifest_sha256={hashlib.sha256((repo / 'scripts/web-e2e-source.manifest').read_bytes()).hexdigest()}\n"
                            f"tar_sha256={hashlib.sha256(archive.read_bytes()).hexdigest()}\n"
                            f"browser_image_digest={image.split('@')[1]}\n")
        candidate_metadata = mode / "candidate-baselines/source-metadata.txt"
        assert candidate_metadata.exists() == (target == "web-e2e-update" and status == 0)
        if candidate_metadata.exists():
            assert candidate_metadata.read_text() == metadata
# Metadata joins only successful, validated candidates.
mode = work / "update"
(mode / "candidate-baselines").mkdir(parents=True)
for status in [1, 0]:
    result = subprocess.run(["python3", "-I", str(collector), str(mode), str(archive), "a" * 40,
                             "sha256:" + "b" * 64, "c" * 64, "d" * 64, str(status)],
                            capture_output=True, timeout=5)
    assert result.returncode == 0, result.stderr
    assert (mode / "candidate-baselines/source-metadata.txt").exists() == (status == 0)
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
cp "$ROOT/scripts/lib/dev-https-browser-stage.sh" "$HTTPS_REPO/scripts/lib/"
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
    env PATH="$HTTPS_BIN:/usr/bin:/bin" HTTPS_CALL_LOG="$HTTPS_CALLS" \
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

: >"$HTTPS_CALLS"
run_https_make dev-https-browser-image >"$WORK/https-image.out" 2>&1 || {
  printf 'makefile-safety-test: browser image target failed\n' >&2
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
run_https_make dev-https-mcp-check >"$WORK/https-mcp.out" 2>&1 || {
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
