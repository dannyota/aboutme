package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestWAFMetadataEncoder(t *testing.T) {
	t.Parallel()

	const secret = "cookiemarker7c2e"
	entry := zapcore.Entry{
		Time:       time.Unix(1_700_000_000, 0),
		Level:      zapcore.WarnLevel,
		LoggerName: "http.handlers.waf",
		Message: "[client \"203.0.113.8\"] Coraza: Warning. XSS found within " + secret +
			" [file \"@owasp_crs/REQUEST-941-APPLICATION-ATTACK-XSS.conf\"] [line \"130\"]" +
			" [id \"941100\"] [rev \"\"] [msg \"XSS Attack Detected\"] [data \"Matched Data: " + secret + "\"]",
	}

	encoded, err := newWAFMetadataEncoder().EncodeEntry(entry, []zapcore.Field{
		zap.String("request", secret),
		zap.String("client_ip", "203.0.113.8"),
	})
	if err != nil {
		t.Fatal(err)
	}
	line := encoded.String()
	if strings.Contains(line, secret) || strings.Contains(line, "203.0.113.8") || strings.Contains(line, "REQUEST-941") {
		t.Fatalf("encoded WAF diagnostic leaked source data: %s", line)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded.Bytes(), &got); err != nil {
		t.Fatalf("decode WAF diagnostic: %v", err)
	}
	if got["msg"] != "waf_rule_match" || got["rule_id"] != float64(941100) {
		t.Fatalf("encoded WAF diagnostic = %#v", got)
	}
	if got["level"] != "warn" || got["logger"] != "http.handlers.waf" || got["ts"] != float64(1_700_000_000) {
		t.Fatalf("encoded WAF diagnostic metadata = %#v", got)
	}
}

func TestWAFMetadataEncoderFailsClosed(t *testing.T) {
	t.Parallel()

	for _, message := range []string{
		"unknown shape with tokenmarker3f8a and id 941100",
		`[id "941100"] tokenmarker3f8a`,
		`[file "x"] [line "1"] [id "941100"] [rev ""] [file "x"] [line "2"] [id "941110"] [rev ""] tokenmarker3f8a`,
	} {
		encoded, err := newWAFMetadataEncoder().EncodeEntry(zapcore.Entry{Message: message}, nil)
		if err != nil {
			t.Fatal(err)
		}
		line := encoded.String()
		if strings.Contains(line, "tokenmarker3f8a") || strings.Contains(line, "941100") || strings.Contains(line, "941110") {
			t.Fatalf("unknown WAF message leaked source data: %s", line)
		}
		var got map[string]any
		if err := json.Unmarshal(encoded.Bytes(), &got); err != nil {
			t.Fatalf("decode WAF diagnostic: %v", err)
		}
		if got["msg"] != "waf_rule_match_unparsed" {
			t.Fatalf("unknown WAF diagnostic = %#v", got)
		}
		if _, exists := got["rule_id"]; exists {
			t.Fatalf("unknown WAF diagnostic has rule_id: %#v", got)
		}
	}
}
