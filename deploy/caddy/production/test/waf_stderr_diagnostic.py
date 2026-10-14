#!/usr/bin/env python3
"""Reject WAF leaks and print only redacted line categories."""

import argparse
import json
import re
import sys


class JSONObject(list):
    """Keep duplicate keys so ambiguous metadata stays redacted."""


LOGGERS = frozenset({
    "admin", "http", "http.auto_https", "http.handlers.waf", "tls",
    "tls.cache.maintenance", "tls.obtain", "tls.renew", "pki.ca.local",
})
MESSAGE_KINDS = {
    "waf_rule_match": "waf-rule-match",
    "waf_rule_match_unparsed": "waf-rule-match-unparsed",
    "Rule matched": "coraza-rule-match",
    "waf_engine_diagnostic": "waf-engine-diagnostic",
    "WAF rule violation detected": "coraza-interruption",
    "serving initial configuration": "startup",
    "using config from file": "startup",
    "adapted config to JSON": "startup",
}
RULE_TEXT = re.compile(r'\[id[ \t]+(?P<quote>\\*")[0-9]+(?P=quote)\]')
RULE_FIELD = re.compile(
    r'(?<!\\)(?P<key_quote>\\*")(?:rule_id|chain_rule_id|id)(?P=key_quote)[ \t]*:[ \t]*'
    r'(?:(?P<value_quote>\\*")[0-9]+(?P=value_quote)|[0-9]+)'
    r'(?=[ \t]*(?:[,}\]]|$))'
)
RULE_LOG_FIELD = re.compile(
    r'(?<![A-Za-z0-9_])(?:rule_id|chain_rule_id|id)[ \t]*=[ \t]*'
    r'(?:(?P<quote>\\*")[0-9]+(?P=quote)|[0-9]+)'
    r'(?=[ \t,}\]]|\\*"|$)'
)
CORAZA_MATCH = re.compile(
    r'Coraza: (?:Warning\.|Access (?:allowed|denied|dropped|redirected) '
    r'\(phase [0-9]+\)\.|Custom disruptive action triggered \(phase [0-9]+\)\.)'
)
RULE_MATCH_MARKER = re.compile(
    r'(?<![A-Za-z0-9_])(?:waf_rule_match(?:_unparsed)?|Rule matched)(?![A-Za-z0-9_])'
)
ADDRESS_FIELDS = tuple(
    (pattern, re.compile(
        rf'(?<!\\)(?P<quote>\\*"){key}(?P=quote)|'
        rf'(?<![A-Za-z0-9_]){key}[ \t]*='
    ))
    for key, pattern in (("client_ip", "client-ip-key"),
                         ("remote_ip", "remote-ip-key"),
                         ("remote_addr", "remote-addr-key"),
                         ("source_ip", "source-ip-key"))
)
CLIENT_TEXT = re.compile(r'\[client[ \t]+\\*"')
RULE_MESSAGE = re.compile(r'Coraza:.*\[id[ \t]+(?P<quote>\\*")[0-9]+(?P=quote)\]')


def single_field(value, key):
    if not isinstance(value, JSONObject):
        return ""
    values = [item for name, item in value if name == key]
    return values[0] if len(values) == 1 and isinstance(values[0], str) else ""


def message_kind(message):
    if RULE_MESSAGE.search(message) or CORAZA_MATCH.search(message):
        return "coraza-rule-match"
    return MESSAGE_KINDS.get(message, "other")


def diagnose(lines, mode, stream):
    for number, line in enumerate(lines, 1):
        patterns = []
        if RULE_TEXT.search(line):
            patterns.append("rule-id-text")
        if RULE_FIELD.search(line) or RULE_LOG_FIELD.search(line):
            patterns.append("rule-id-field")
        if CORAZA_MATCH.search(line) or RULE_MATCH_MARKER.search(line):
            patterns.append("rule-match-marker")
        for pattern, field in ADDRESS_FIELDS:
            if field.search(line):
                patterns.append(pattern)
        if CLIENT_TEXT.search(line):
            patterns.append("client-address-text")
        if mode == "maintenance" and "maintmarker6e4d" in line:
            patterns.append("probe-marker")
        if not patterns:
            continue

        logger = "other"
        kind = message_kind(line)
        try:
            value = json.loads(line, object_pairs_hook=JSONObject)
            source_logger = single_field(value, "logger")
            if source_logger in LOGGERS:
                logger = source_logger
            kind = message_kind(single_field(value, "msg"))
        except (ValueError, RecursionError):
            pass

        for pattern in patterns:
            yield {
                "stream": stream, "line": number, "pattern": pattern,
                "logger": logger, "message_kind": kind,
            }


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mode", choices=("serving", "maintenance"), required=True)
    parser.add_argument("--stdout", required=True)
    parser.add_argument("--stderr", required=True)
    args = parser.parse_args(argv)
    status = 0
    try:
        for stream, path in (("stdout", args.stdout), ("stderr", args.stderr)):
            with open(path, encoding="utf-8", errors="replace") as source:
                for record in diagnose(source, args.mode, stream):
                    # Only fixed categories and line numbers reach the job log.
                    print(json.dumps(record, separators=(",", ":")))
                    status = 1
    except OSError:
        print("coraza: container log files could not be read", file=sys.stderr)
        return 1
    return status


if __name__ == "__main__":
    sys.exit(main())
