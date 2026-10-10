package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestSafeMetadataEncoderDropsSourceValues(t *testing.T) {
	t.Parallel()

	for _, marker := range []string{safeMetadataHTTPError, safeMetadataCrowdSec, safeMetadataTLS} {
		enc := newSafeMetadataEncoder(marker)
		if err := enc.AddObject("request", feedRequest{
			remoteIP: "203.0.113.9",
			method:   "POST",
			uri:      "/pathmarker-4b8f?token=querymarker-2d6a",
		}); err != nil {
			t.Fatal(err)
		}
		encoded, err := enc.EncodeEntry(zapcore.Entry{
			Time:       time.Date(2026, 10, 10, 1, 2, 3, 4, time.UTC),
			Level:      zapcore.ErrorLevel,
			LoggerName: "http.handlers.crowdsec",
			Message:    "failure for decision 203.0.113.9 secret-message-marker",
		}, []zapcore.Field{
			zap.String("error", "bodymarker-7c1e"),
			zap.String("address", "http://secret-host.example/203.0.113.9"),
			zap.String("user_agent", "agentmarker-9f3c"),
		})
		if err != nil {
			t.Fatal(err)
		}
		line := encoded.String()
		encoded.Free()

		var got map[string]any
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 || got["ts"] != "2026-10-10T01:02:03.000000004Z" || got["level"] != "error" || got["msg"] != marker {
			t.Fatalf("safe metadata = %#v", got)
		}
		for _, leak := range []string{
			"203.0.113.9", "pathmarker", "querymarker", "secret-message-marker",
			"bodymarker", "secret-host", "agentmarker", "http.handlers.crowdsec",
		} {
			if strings.Contains(line, leak) {
				t.Errorf("safe metadata contains %q: %s", leak, line)
			}
		}
	}
}

func TestSafeMetadataEncoderDropsMalformedEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		marker string
		entry  zapcore.Entry
	}{
		{name: "marker", marker: "unknown", entry: zapcore.Entry{Time: time.Unix(1_700_000_000, 0), Level: zapcore.InfoLevel}},
		{name: "timestamp", marker: safeMetadataHTTPError, entry: zapcore.Entry{Level: zapcore.ErrorLevel}},
		{name: "level", marker: safeMetadataCrowdSec, entry: zapcore.Entry{Time: time.Unix(1_700_000_000, 0), Level: zapcore.InvalidLevel}},
	}
	for _, tt := range tests {
		testCase := tt
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			encoded, err := newSafeMetadataEncoder(testCase.marker).EncodeEntry(testCase.entry, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer encoded.Free()
			if encoded.Len() != 0 {
				t.Fatalf("malformed entry was emitted: %s", encoded.String())
			}
		})
	}
}

func TestSafeMetadataEncoderDropsACMEChallengeValues(t *testing.T) {
	t.Parallel()

	for _, entry := range []zapcore.Entry{
		{
			Time:       time.Unix(1_700_000_000, 0),
			Level:      zapcore.InfoLevel,
			LoggerName: "tls.issuance.acme",
			Message:    "served key authentication",
		},
		{
			Time:       time.Unix(1_700_000_000, 0),
			Level:      zapcore.WarnLevel,
			LoggerName: "tls.issuance.acme",
			Message:    "looking up info for HTTP challenge",
		},
		{
			Time:       time.Unix(1_700_000_000, 0),
			Level:      zapcore.ErrorLevel,
			LoggerName: "tls",
			Message:    "tls-alpn challenge",
		},
	} {
		encoded, err := newSafeMetadataEncoder(safeMetadataTLS).EncodeEntry(entry, []zapcore.Field{
			zap.String("uri", "/.well-known/acme-challenge/pathmarker-4b8f?token=querymarker-2d6a"),
			zap.String("remote", "203.0.113.9:49152"),
			zap.String("user_agent", "agentmarker-9f3c"),
			zap.String("identifier", "hostmarker.example"),
		})
		if err != nil {
			t.Fatal(err)
		}
		line := encoded.String()
		encoded.Free()
		if !strings.Contains(line, `"msg":"tls_event"`) {
			t.Fatalf("ACME metadata = %s", line)
		}
		for _, leak := range []string{"pathmarker", "querymarker", "203.0.113.9", "agentmarker", "hostmarker"} {
			if strings.Contains(line, leak) {
				t.Errorf("ACME metadata contains %q: %s", leak, line)
			}
		}
	}
}
