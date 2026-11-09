package main

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/corazawaf/coraza/v3"
	"github.com/corazawaf/coraza/v3/debuglog"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestWAFMetadataEncoderRecognizesEngineErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		message string
		kind    string
	}{
		{"key not found in collection, returning the original text", "macro_key_missing"},
		{"error creating serial log writer", "serial_log_writer_error"},
		{"Failed to initialize audit log", "audit_log_init_error"},
		{"Failed to write audit log", "audit_log_write_error"},
		{"Error converting request length to integer", "request_length_invalid"},
		{"collection in setvar is not a map", "setvar_collection_not_map"},
		{"collection in setvar is nil", "setvar_collection_nil"},
		{"Invalid value", "invalid_value"},
		{"Invalid range", "invalid_range"},
		{"Invalid status", "invalid_status"},
		{"Invalid audit log part", "invalid_audit_log_part"},
		{"Unknown toggle", "unknown_toggle"},
		{"Invalid limit", "invalid_limit"},
		{"Invalid rule ID", "invalid_rule_id"},
		{"Invalid log level", "invalid_log_level"},
		{"attempted to use regex with non-selectable collection", "regex_collection_unsupported"},
		{"attempted to use string with non-selectable collection", "string_collection_unsupported"},
		{"ProcessRequestHeaders has already been called", "request_headers_repeated"},
		{"Calling ProcessRequestHeaders but there is a preexisting interruption", "request_headers_interrupted"},
		{"Calling ProcessRequestBody but there is a preexisting interruption", "request_body_interrupted"},
		{"Failed to process request body", "request_body_error"},
		{"ProcessResponseHeaders has already been called", "response_headers_repeated"},
		{"Calling ProcessResponseHeaders but there is a preexisting interruption", "response_headers_interrupted"},
		{"Calling ProcessResponseBody but there is a preexisting interruption", "response_body_interrupted"},
		{"Failed to process response body", "response_body_error"},
		{"Failed to close the transaction", "transaction_close_error"},
		{"WAF rule violation detected", "interruption_summary"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			assertWAFMetadata(t, tc.message, "waf_engine_diagnostic", tc.kind)
		})
	}
}

func TestWAFMetadataEncoderRejectsUnknownEngineMessages(t *testing.T) {
	t.Parallel()

	for _, message := range []string{
		"Failed to process request body: tokenmarker3f8a",
		"tokenmarker3f8a Failed to process request body",
		"Failed to process request body\n",
		"WAF rule violation detected: tokenmarker3f8a",
		"key not found in collection, returning the original text: tokenmarker3f8a",
		"request_body_error",
		"+tx.tokenmarker3f8a",
	} {
		t.Run(message, func(t *testing.T) {
			t.Parallel()
			assertWAFMetadata(t, message, "waf_rule_match_unparsed", "")
		})
	}
}

func assertWAFMetadata(t *testing.T, message, event, kind string) {
	t.Helper()

	const marker = "tokenmarker3f8a"
	encoder := newWAFMetadataEncoder()
	encoder.AddString("context", marker)
	encoder.AddString("kind", marker)
	encoded, err := encoder.Clone().EncodeEntry(zapcore.Entry{
		Time:       time.Unix(1_700_000_000, 0),
		Level:      zapcore.ErrorLevel,
		LoggerName: "http.handlers.waf",
		Message:    message,
	}, []zapcore.Field{
		zap.String("error", marker),
		zap.String("uri", "/"+marker),
		zap.String("client_ip", "203.0.113.8"),
		zap.Int("rule_id", 941100),
		zap.String("kind", marker),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(encoded.Free)
	if line := encoded.String(); strings.Contains(line, marker) || strings.Contains(line, "203.0.113.8") {
		t.Fatalf("encoded WAF message leaked source data: %s", line)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	wantFields := 4
	if kind != "" {
		wantFields++
		if got["kind"] != kind {
			t.Fatalf("encoded WAF diagnostic kind = %#v, want %s", got["kind"], kind)
		}
	} else if _, exists := got["kind"]; exists {
		t.Fatalf("unparsed WAF message has kind: %#v", got)
	}
	if len(got) != wantFields || got["msg"] != event || got["level"] != "error" ||
		got["logger"] != "http.handlers.waf" || got["ts"] != float64(1_700_000_000) {
		t.Fatalf("encoded WAF metadata = %#v, want only metadata for %s with kind %q", got, event, kind)
	}
}

func TestWAFMetadataEncoderRecognizesCorazaBodyError(t *testing.T) {
	t.Parallel()

	var upstreamMessage string
	logger := debuglog.DefaultWithPrinterFactory(func(io.Writer) debuglog.Printer {
		return func(level debuglog.Level, message, _ string) {
			if level == debuglog.LevelError {
				upstreamMessage = message
			}
		}
	}).WithLevel(debuglog.LevelError)
	waf, err := coraza.NewWAF(coraza.NewWAFConfig().WithDebugLogger(logger).WithDirectives(`
SecRuleEngine DetectionOnly
SecRequestBodyAccess On
SecAction "id:1001,phase:1,pass,nolog,ctl:requestBodyProcessor=JSON"
`))
	if err != nil {
		t.Fatal(err)
	}
	tx := waf.NewTransaction()
	t.Cleanup(func() {
		if err := tx.Close(); err != nil {
			t.Errorf("close Coraza transaction: %v", err)
		}
	})
	tx.ProcessRequestHeaders()
	if _, _, err := tx.WriteRequestBody([]byte(`{"token":"tokenmarker3f8a"`)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ProcessRequestBody(); err != nil {
		t.Fatal(err)
	}
	if upstreamMessage != "Failed to process request body" {
		t.Fatalf("Coraza body error message = %q", upstreamMessage)
	}
	assertWAFMetadata(t, upstreamMessage, "waf_engine_diagnostic", "request_body_error")
}
