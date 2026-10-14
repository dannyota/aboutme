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

	for _, message := range []string{
		"error creating serial log writer",
		"Failed to initialize audit log",
		"Failed to write audit log",
		"Error converting request length to integer",
		"collection in setvar is not a map",
		"collection in setvar is nil",
		"Invalid value",
		"Invalid range",
		"Invalid status",
		"Invalid audit log part",
		"Unknown toggle",
		"Invalid limit",
		"Invalid rule ID",
		"Invalid log level",
		"attempted to use regex with non-selectable collection",
		"attempted to use string with non-selectable collection",
		"ProcessRequestHeaders has already been called",
		"Calling ProcessRequestHeaders but there is a preexisting interruption",
		"Calling ProcessRequestBody but there is a preexisting interruption",
		"Failed to process request body",
		"ProcessResponseHeaders has already been called",
		"Calling ProcessResponseHeaders but there is a preexisting interruption",
		"Calling ProcessResponseBody but there is a preexisting interruption",
		"Failed to process response body",
		"Failed to close the transaction",
	} {
		t.Run(message, func(t *testing.T) {
			t.Parallel()
			assertWAFMessageKind(t, message, "waf_engine_diagnostic")
		})
	}
}

func TestWAFMetadataEncoderRecognizesViolationSummary(t *testing.T) {
	t.Parallel()
	assertWAFMessageKind(t, "WAF rule violation detected", "waf_engine_diagnostic")
}

func TestWAFMetadataEncoderRejectsUnknownEngineMessages(t *testing.T) {
	t.Parallel()

	for _, message := range []string{
		"Failed to process request body: tokenmarker3f8a",
		"tokenmarker3f8a Failed to process request body",
		"Failed to process request body\n",
		"WAF rule violation detected: tokenmarker3f8a",
		"+tx.tokenmarker3f8a",
	} {
		t.Run(message, func(t *testing.T) {
			t.Parallel()
			assertWAFMessageKind(t, message, "waf_rule_match_unparsed")
		})
	}
}

func assertWAFMessageKind(t *testing.T, message, kind string) {
	t.Helper()

	const marker = "tokenmarker3f8a"
	encoder := newWAFMetadataEncoder()
	encoder.AddString("context", marker)
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
	if len(got) != 4 || got["msg"] != kind || got["level"] != "error" ||
		got["logger"] != "http.handlers.waf" || got["ts"] != float64(1_700_000_000) {
		t.Fatalf("encoded WAF metadata = %#v, want only metadata with kind %s", got, kind)
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
	assertWAFMessageKind(t, upstreamMessage, "waf_engine_diagnostic")
}
