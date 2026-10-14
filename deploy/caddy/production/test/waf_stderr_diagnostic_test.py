#!/usr/bin/env python3
"""Check precise WAF leak detection and redacted failure output."""

import json
import subprocess
import tempfile
import unittest
from pathlib import Path

import waf_stderr_diagnostic as diagnostic


CLEAN_LINES = (
    '{"ts":1791966300.9411001,"logger":"http","msg":"starting server"}',
    '{"duration":0.9411001,"elapsed":941100}',
    '{"ts":"2026-10-14T03:04:05.941100Z","duration":"941100ms"}',
    '2026-10-14T03:04:05.941100Z duration=0.9411001',
    '{"msg":"941100","logger":"http941100","request_id":"941100"}',
    '{"other_id":941100}',
    '{"id":9411000}',
    '{"rule_id":941100.1}',
    '{"id":"9411000"}',
    '{"rule_id":"1941100"}',
    'Coraza: Warning. [id "9411000"]',
)
LEAK_LINES = (
    ("rule-id-text", 'Coraza: Warning. [id "941100"]'),
    ("rule-id-text", r'Coraza: Warning. [id \"941100\"]'),
    ("rule-id-text", json.dumps({"msg": 'Coraza: Warning. [id "941100"]'})),
    ("rule-id-field", '{"rule_id":941100}'),
    ("rule-id-field", '{"id":"941100"}'),
    ("rule-id-field", '{"id":941100}'),
    ("rule-id-field", '{"rule_id":"941100"}'),
    ("rule-id-field", '{"rule_id" : 941100 , "msg":"waf_rule_match"}'),
    ("rule-id-field", '{"rules":[{"id":"941100"}]}'),
    ("rule-id-field", json.dumps({"msg": '{"rule_id":941100}'})),
    ("rule-id-field", json.dumps({"msg": '{"id":"941100"}'})),
    ("client-ip-key", '{"client_ip":"203.0.113.8"}'),
    ("client-ip-key", 'Coraza diagnostic mentions "client_ip"'),
)


class WAFStderrDiagnosticTest(unittest.TestCase):
    def records(self, line, mode="serving", stream="stderr"):
        return list(diagnostic.diagnose([line], mode, stream))

    def test_timestamps_durations_and_unrelated_digits_are_clean(self):
        for line in CLEAN_LINES:
            with self.subTest(line=line):
                self.assertEqual(self.records(line), [])

    def test_each_leaked_form_has_a_fixed_pattern_name(self):
        for pattern, line in LEAK_LINES:
            with self.subTest(line=line):
                self.assertEqual([record["pattern"] for record in self.records(line)],
                                 [pattern])

    def test_rule_message_and_address_are_redacted(self):
        records = self.records(json.dumps({
            "logger": "http.handlers.waf",
            "msg": '[client "203.0.113.8"] Coraza: Warning. '
                   '[id "941100"] [data "tokenmarker3f8a"]',
            "client_ip": "203.0.113.8",
        }))
        self.assertEqual({record["pattern"] for record in records},
                         {"rule-id-text", "client-ip-key"})
        for record in records:
            self.assertEqual(record["logger"], "http.handlers.waf")
            self.assertEqual(record["message_kind"], "coraza-rule-match")
        self.assertNotIn("203.0.113.8", json.dumps(records))
        self.assertNotIn("tokenmarker3f8a", json.dumps(records))

    def test_unknown_metadata_never_reaches_output(self):
        records = self.records(json.dumps({
            "logger": "loggermarker3f8a",
            "msg": "messagemarker3f8a",
            "request": {"client_ip": "203.0.113.8"},
        }))
        self.assertEqual(len(records), 1)
        self.assertEqual(records[0]["logger"], "other")
        self.assertEqual(records[0]["message_kind"], "other")
        self.assertNotIn("marker3f8a", json.dumps(records))
        self.assertNotIn("203.0.113.8", json.dumps(records))

    def test_non_json_rule_message_has_a_fixed_category(self):
        record = self.records('Coraza: [id "941100"] tokenmarker3f8a')[0]
        self.assertEqual(record["message_kind"], "coraza-rule-match")
        self.assertNotIn("tokenmarker3f8a", json.dumps(record))

    def test_duplicate_json_keys_do_not_hide_a_leak(self):
        records = self.records('{"rule_id":941100,"rule_id":42}')
        self.assertEqual([record["pattern"] for record in records], ["rule-id-field"])
        records = self.records('{"rule_id":941100,"logger":"http",'
                               '"logger":"tokenmarker3f8a"}')
        self.assertEqual(records[0]["logger"], "other")

    def test_probe_marker_is_only_a_maintenance_trigger(self):
        line = '{"msg":"maintmarker6e4d"}'
        self.assertEqual(self.records(line), [])
        record = self.records(line, mode="maintenance")[0]
        self.assertEqual(record["pattern"], "probe-marker")
        self.assertNotIn("maintmarker6e4d", json.dumps(record))

    def test_only_matching_line_numbers_and_stream_reach_output(self):
        records = list(diagnostic.diagnose([
            '{"duration":0.9411001}\n',
            '{"rule_id":941100,"msg":"waf_rule_match"}\n',
            '{"msg":"benign"}\n',
        ], "serving", "stdout"))
        self.assertEqual(records, [{
            "stream": "stdout", "line": 2, "pattern": "rule-id-field",
            "logger": "other", "message_kind": "waf-rule-match",
        }])

    def test_shell_gate_accepts_numeric_collisions_in_both_modes(self):
        for mode in ("serving", "maintenance"):
            with self.subTest(mode=mode):
                result = self.run_gate(mode, "\n".join(CLEAN_LINES),
                                       "\n".join(CLEAN_LINES))
                self.assertEqual(result.returncode, 0)
                self.assertEqual(result.stdout, "")
                self.assertEqual(result.stderr, "")

    def test_shell_gate_rejects_each_leak_in_both_modes_and_streams(self):
        for mode in ("serving", "maintenance"):
            for stream in ("stdout", "stderr"):
                for pattern, line in LEAK_LINES:
                    with self.subTest(mode=mode, stream=stream, pattern=pattern):
                        stdout = line if stream == "stdout" else ""
                        stderr = line if stream == "stderr" else ""
                        result = self.run_gate(mode, stdout, stderr)
                        self.assertEqual(result.returncode, 1)
                        self.assertEqual(result.stdout, "")
                        self.assertEqual(json.loads(result.stderr), {
                            "stream": stream, "line": 1, "pattern": pattern,
                            "logger": "other", "message_kind": (
                                "coraza-rule-match" if pattern == "rule-id-text"
                                else "waf-rule-match" if "waf_rule_match" in line
                                else "other"),
                        })

    def test_shell_gate_preserves_maintenance_marker_detection(self):
        line = '{"msg":"maintmarker6e4d"}'
        self.assertEqual(self.run_gate("serving", "", line).returncode, 0)
        result = self.run_gate("maintenance", "", line)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(json.loads(result.stderr)["pattern"], "probe-marker")
        self.assertNotIn("maintmarker6e4d", result.stderr)

    def test_shell_gate_redacts_container_log_errors(self):
        result = self.run_gate("serving", "", "tokenmarker3f8a", log_status=125)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stderr, "coraza: container logs could not be read\n")

    def run_gate(self, mode, stdout, stderr, log_status=0):
        root = Path(__file__).resolve().parents[4]
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / "probe.stdout").write_text(stdout + "\n", encoding="utf-8")
            (work / "probe.stderr").write_text(stderr + "\n", encoding="utf-8")
            script = '''
root=$1
work=$2
log_status=$4
source "$root/deploy/caddy/production/test/waf_stderr.sh"
podman() {
  cat "$work/probe.stdout"
  cat "$work/probe.stderr" >&2
  return "$log_status"
}
assert_waf_stderr_private synthetic "$3"
'''
            return subprocess.run(
                ["bash", "-c", script, "waf-check-test", str(root), str(work),
                 mode, str(log_status)],
                capture_output=True, text=True, check=False,
            )


if __name__ == "__main__":
    unittest.main()
