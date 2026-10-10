package main

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/corazawaf/coraza/v3"
	"github.com/corazawaf/coraza/v3/debuglog"
	"github.com/corazawaf/coraza/v3/experimental/plugins/macro"
	"github.com/corazawaf/coraza/v3/experimental/plugins/plugintypes"
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

func TestWAFMetadataEncoderRecognizesCorazaMacroDiagnostic(t *testing.T) {
	t.Parallel()

	var upstreamMessage string
	logger := debuglog.DefaultWithPrinterFactory(func(io.Writer) debuglog.Printer {
		return func(_ debuglog.Level, message, _ string) {
			upstreamMessage = message
		}
	})
	waf, err := coraza.NewWAF(coraza.NewWAFConfig().WithDebugLogger(logger))
	if err != nil {
		t.Fatal(err)
	}
	corazaTx := waf.NewTransaction()
	tx, ok := corazaTx.(plugintypes.TransactionState)
	if !ok {
		t.Fatal("Coraza transaction does not implement TransactionState")
	}
	t.Cleanup(func() {
		if err := corazaTx.Close(); err != nil {
			t.Errorf("close Coraza transaction: %v", err)
		}
	})
	missing, err := macro.NewMacro(`%{tx.missing}`)
	if err != nil {
		t.Fatal(err)
	}
	missing.Expand(tx)
	if upstreamMessage == "" {
		t.Fatal("Coraza macro expansion did not emit its missing-key diagnostic")
	}

	encoded, err := newWAFMetadataEncoder().EncodeEntry(zapcore.Entry{Message: upstreamMessage}, []zapcore.Field{
		zap.String("variable", "TX"),
		zap.String("key", "missing"),
	})
	if err != nil {
		t.Fatal(err)
	}
	line := encoded.String()
	if strings.Contains(line, "missing") || strings.Contains(line, "TX") {
		t.Fatalf("encoded Coraza diagnostic leaked source fields: %s", line)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded.Bytes(), &got); err != nil {
		t.Fatalf("decode Coraza diagnostic: %v", err)
	}
	if got["msg"] != "waf_engine_diagnostic" {
		t.Fatalf("encoded Coraza diagnostic = %#v", got)
	}
	if _, exists := got["rule_id"]; exists {
		t.Fatalf("Coraza diagnostic has rule_id: %#v", got)
	}
}
