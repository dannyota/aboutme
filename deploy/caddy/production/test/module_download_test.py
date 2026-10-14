#!/usr/bin/env python3
"""Exercise image download commands with deterministic transport failures."""

import re
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[4]
IMAGES = (
    ("deploy/caddy/production/Dockerfile", "mod download", "off"),
    ("deploy/server.Dockerfile", "-C apps/server mod download", ""),
    ("deploy/observer/Dockerfile", "mod download", "off"),
)
STREAM_ERROR = (
    'go: example.com/module@v1.0.0: read '
    '"https://proxy.golang.org/example.com/module/@v/v1.0.0.zip": '
    "stream error: stream ID 385; INTERNAL_ERROR; received from peer"
)


class ModuleDownloadTest(unittest.TestCase):
    def run_download(self, dockerfile, failures, error=STREAM_ERROR,
                     failure_status=1, sleep_status=0):
        instructions = re.findall(
            r"^RUN ((?:.*\\\n)*.*)$",
            (ROOT / dockerfile).read_text(),
            re.MULTILINE,
        )
        commands = [instruction.replace("\\\n", "")
                    for instruction in instructions
                    if re.search(r"\bmod\s+download\b", instruction)]
        self.assertEqual(len(commands), 1, dockerfile)
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            calls = work / "calls"
            delays = work / "delays"
            go = work / "go"
            go.write_text("""#!/bin/sh
count=0
if [ -f "$DOWNLOAD_CALLS" ]; then count=$(wc -l < "$DOWNLOAD_CALLS"); fi
printf '%s|%s|%s|%s|%s|%s|%s|%s\n' "$*" "${GOWORK-}" \
    "${GOPROXY-}" "${GOSUMDB-}" "${GONOSUMDB-}" "${GOPRIVATE-}" \
    "${GOINSECURE-}" "${GOFLAGS-}" >> "$DOWNLOAD_CALLS"
if [ "$count" -lt "$DOWNLOAD_FAILURES" ]; then
    printf '%s\n' "$DOWNLOAD_ERROR" >&2
    exit "$DOWNLOAD_FAILURE_STATUS"
fi
""")
            go.chmod(0o755)
            sleep = work / "sleep"
            sleep.write_text("""#!/bin/sh
printf '%s\n' "$*" >> "$DOWNLOAD_DELAYS"
exit "$DOWNLOAD_SLEEP_STATUS"
""")
            sleep.chmod(0o755)
            result = subprocess.run(
                ["/bin/sh", "-c", commands[0]],
                cwd=work,
                env={
                    "PATH": f"{work}:/usr/bin:/bin",
                    "DOWNLOAD_CALLS": str(calls),
                    "DOWNLOAD_DELAYS": str(delays),
                    "DOWNLOAD_FAILURES": str(failures),
                    "DOWNLOAD_ERROR": error,
                    "DOWNLOAD_FAILURE_STATUS": str(failure_status),
                    "DOWNLOAD_SLEEP_STATUS": str(sleep_status),
                },
                capture_output=True,
                text=True,
                timeout=10,
                check=False,
            )
            return (result, calls.read_text().splitlines(),
                    delays.read_text().splitlines() if delays.exists() else [])

    def assert_calls(self, calls, arguments, workspace, count):
        self.assertEqual(calls, [f"{arguments}|{workspace}||||||"] * count)

    def test_success_does_not_repeat_or_sleep(self):
        for dockerfile, arguments, workspace in IMAGES:
            with self.subTest(image=dockerfile):
                result, calls, delays = self.run_download(dockerfile, 0)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assert_calls(calls, arguments, workspace, 1)
                self.assertEqual(delays, [])

    def test_recovers_from_stream_errors(self):
        for dockerfile, arguments, workspace in IMAGES:
            for failures in (1, 2):
                with self.subTest(image=dockerfile, failures=failures):
                    result, calls, delays = self.run_download(dockerfile, failures)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assert_calls(calls, arguments, workspace, failures + 1)
                    self.assertEqual(delays, ["5", "10"][:failures])
                    self.assertEqual(result.stderr.count(STREAM_ERROR), failures)

    def test_persistent_failure_is_bounded_and_preserves_status(self):
        for dockerfile, arguments, workspace in IMAGES:
            with self.subTest(image=dockerfile):
                result, calls, delays = self.run_download(
                    dockerfile, 100, failure_status=17)
                self.assertEqual(result.returncode, 17, result.stderr)
                self.assert_calls(calls, arguments, workspace, 3)
                self.assertEqual(delays, ["5", "10"])
                self.assertEqual(result.stderr.count(STREAM_ERROR), 3)

    def test_persistent_checksum_failure_stays_fatal(self):
        error = "go: verifying example.com/module@v1.0.0: checksum mismatch"
        for dockerfile, arguments, workspace in IMAGES:
            with self.subTest(image=dockerfile):
                result, calls, delays = self.run_download(dockerfile, 100, error)
                self.assertNotEqual(result.returncode, 0)
                self.assert_calls(calls, arguments, workspace, 3)
                self.assertEqual(delays, ["5", "10"])
                self.assertEqual(result.stderr.count(error), 3)

    def test_failed_sleep_stops_downloads(self):
        for dockerfile, arguments, workspace in IMAGES:
            with self.subTest(image=dockerfile):
                result, calls, delays = self.run_download(
                    dockerfile, 100, sleep_status=13)
                self.assertEqual(result.returncode, 13, result.stderr)
                self.assert_calls(calls, arguments, workspace, 1)
                self.assertEqual(delays, ["5"])


if __name__ == "__main__":
    unittest.main()
