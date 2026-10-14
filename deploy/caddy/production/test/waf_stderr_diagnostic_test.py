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
    '{"ts":1791966300.949110,"duration":0.949110,"elapsed":949110}',
    '2026-10-14T03:04:05.949110Z duration=949110ms',
    '{"msg":"941100","logger":"http941100","request_id":"941100"}',
    '{"msg":"949110","request_id":"949110"}',
    '{"other_id":941100}',
    '{"other_rule_id":949110,"request_id":9411000}',
    '{"rule_id":941100.1}',
    '{"rule_id":"949110ms"}',
    'rule_id=941100.1 chain_rule_id=949110ms other_rule_id=941100',
    '{"client_ip_headers":["X-Forwarded-For"],"other_remote_ip":"benign"}',
    '{"logger":"http.handlers.waf","msg":"waf_engine_diagnostic"}',
    'Coraza: engine initialized',
    json.dumps({"msg": '{"ts":1791966300.949110,"duration":"941100ms"}'}),
)
LEAK_LINES = (
    (("rule-id-text", "rule-match-marker"),
     'Coraza: Warning. [id "941100"]', "coraza-rule-match"),
    (("rule-id-text", "rule-match-marker"),
     r'Coraza: Warning. [id \"949110\"]', "coraza-rule-match"),
    (("rule-id-text", "rule-match-marker"),
     'Coraza: Warning. [id "9411000"]', "coraza-rule-match"),
    (("rule-id-text", "rule-match-marker", "client-address-text"),
     json.dumps({"logger": "http.handlers.waf",
                 "msg": '[client "203.0.113.8"] Coraza: Warning. [id "949110"]'}),
     "coraza-rule-match"),
    (("rule-id-field", "rule-match-marker"),
     '{"logger":"http.handlers.waf","msg":"waf_rule_match","rule_id":949110}',
     "waf-rule-match"),
    (("rule-id-field",), '{"rule_id" : 941100 , "msg":"benign"}', "other"),
    (("rule-id-field",), '{"rules":[{"id":"949110"}]}', "other"),
    (("rule-id-field",),
     '{"messages":[{"data":{"id":930130,"line":941100}}]}', "other"),
    (("client-ip-key",), 'Coraza diagnostic mentions "client_ip"', "other"),
    (("client-address-text",), '[client "203.0.113.8"]', "other"),
    (("client-address-text",), r'[client \"2001:db8::8\"]', "other"),
    (("client-address-text",), json.dumps({"msg": '[client ""]'}), "other"),
    (("rule-match-marker",), 'Coraza: Warning.', "coraza-rule-match"),
    (("rule-match-marker",), 'Coraza: Access allowed (phase 1).', "coraza-rule-match"),
    (("rule-match-marker",), 'Coraza: Access denied (phase 2).', "coraza-rule-match"),
    (("rule-match-marker",), 'Coraza: Access dropped (phase 2).', "coraza-rule-match"),
    (("rule-match-marker",), 'Coraza: Access redirected (phase 3).', "coraza-rule-match"),
    (("rule-match-marker",), 'Coraza: Custom disruptive action triggered (phase 4).',
     "coraza-rule-match"),
    (("rule-match-marker",), '{"msg":"Rule matched"}', "coraza-rule-match"),
    (("rule-match-marker",), '{"msg":"waf_rule_match"}', "waf-rule-match"),
    (("rule-match-marker",), '{"msg":"waf_rule_match_unparsed"}',
     "waf-rule-match-unparsed"),
    (("rule-id-text",), '[id "1001"]', "other"),
    (("rule-id-text",), '[id "949110"]', "other"),
    (("rule-id-text",), r'[id \"942100\"]', "other"),
    (("rule-id-text",), json.dumps({"msg": '[id "1941100"]'}), "other"),
    (("rule-id-text",),
     json.dumps({"msg": json.dumps({"msg": '[id "949110"]'})}), "other"),
)
LEAK_LINES += tuple(
    (("rule-id-field",), line, "other")
    for key in ("rule_id", "chain_rule_id", "id")
    for rule_id in (941100, 949110, 942100, 9411000, 1941100, 1001, 0)
    for line in (
        json.dumps({key: rule_id}),
        json.dumps({key: str(rule_id)}),
        json.dumps({"context": [{"rules": [{key: rule_id}]}]}),
        json.dumps({"msg": json.dumps({key: rule_id})}),
        json.dumps({"msg": json.dumps({"msg": json.dumps({key: str(rule_id)})})}),
        f'{key}={rule_id}',
        f'{key}="{rule_id}"',
        json.dumps({"msg": f'{key}={rule_id}'}),
    )
)
LEAK_LINES += tuple(
    ((pattern,), line, "other")
    for key, pattern in (("client_ip", "client-ip-key"),
                         ("remote_ip", "remote-ip-key"),
                         ("remote_addr", "remote-addr-key"),
                         ("source_ip", "source-ip-key"))
    for address in (("203.0.113.8:49152", "[2001:db8::8]:49152")
                    if key == "remote_addr" else ("203.0.113.8", "2001:db8::8"))
    for line in (
        json.dumps({key: address}),
        json.dumps({"request": {"context": [{key: address}]}}),
        json.dumps({"msg": json.dumps({key: address})}),
        json.dumps({"msg": json.dumps({"msg": json.dumps({key: address})})}),
        f'{key}="{address}"',
    )
)


class WAFStderrDiagnosticTest(unittest.TestCase):
    def records(self, line, mode="serving", stream="stderr"):
        return list(diagnostic.diagnose([line], mode, stream))

    def test_timestamps_durations_and_unrelated_digits_are_clean(self):
        for line in CLEAN_LINES:
            with self.subTest(line=line):
                self.assertEqual(self.records(line), [])

    def test_each_leaked_form_has_a_fixed_pattern_name(self):
        for patterns, line, _ in LEAK_LINES:
            with self.subTest(line=line):
                self.assertEqual([record["pattern"] for record in self.records(line)],
                                 list(patterns))

    def test_encoded_markers_and_addresses_remain_detectable(self):
        for patterns, line, _ in LEAK_LINES:
            for _ in range(3):
                line = json.dumps({"msg": line})
                with self.subTest(line=line):
                    records = self.records(line)
                    self.assertEqual([record["pattern"] for record in records],
                                     list(patterns))
                    self.assertNotIn("203.0.113.8", json.dumps(records))
                    self.assertNotIn("2001:db8", json.dumps(records))

    def test_rule_message_and_address_are_redacted(self):
        records = self.records(json.dumps({
            "logger": "http.handlers.waf",
            "msg": '[client "203.0.113.8"] Coraza: Warning. '
                   '[id "941100"] [data "tokenmarker3f8a"]',
            "client_ip": "203.0.113.8",
        }))
        self.assertEqual({record["pattern"] for record in records},
                         {"rule-id-text", "rule-match-marker", "client-ip-key",
                          "client-address-text"})
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
            '{"rule_id":949110,"msg":"benign"}\n',
            '{"msg":"benign"}\n',
        ], "serving", "stdout"))
        self.assertEqual(records, [{
            "stream": "stdout", "line": 2, "pattern": "rule-id-field",
            "logger": "other", "message_kind": "other",
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
                for patterns, line, kind in LEAK_LINES:
                    with self.subTest(mode=mode, stream=stream, line=line):
                        stdout = line if stream == "stdout" else ""
                        stderr = line if stream == "stderr" else ""
                        result = self.run_gate(mode, stdout, stderr)
                        self.assertEqual(result.returncode, 1)
                        self.assertEqual(result.stdout, "")
                        records = [json.loads(record) for record in result.stderr.splitlines()]
                        logger = "http.handlers.waf" if '"logger"' in line else "other"
                        self.assertEqual(records, [{
                            "stream": stream, "line": 1, "pattern": pattern,
                            "logger": logger, "message_kind": kind,
                        } for pattern in patterns])
                        self.assertNotIn("203.0.113.8", result.stderr)
                        self.assertNotIn("2001:db8", result.stderr)

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
