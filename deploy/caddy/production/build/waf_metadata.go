package main

import (
	"regexp"
	"strconv"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

const (
	wafEngineMessage  = "waf_engine_diagnostic"
	wafMatchMessage   = "waf_rule_match"
	wafUnknownMessage = "waf_rule_match_unparsed"
)

var wafRuleIDPattern = regexp.MustCompile(`\[file "[^"\r\n]*"\] \[line "[0-9]+"\] \[id "([0-9]+)"\] \[rev "`)

func init() {
	caddy.RegisterModule(WAFMetadataEncoder{})
}

// WAFMetadataEncoder keeps only fixed event metadata from Coraza match logs.
// Coraza messages contain request values and client addresses, so unknown
// message shapes must never pass through this encoder.
type WAFMetadataEncoder struct {
	zapcore.Encoder `json:"-"`
}

func (WAFMetadataEncoder) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "caddy.logging.encoders.waf_metadata",
		New: func() caddy.Module { return new(WAFMetadataEncoder) },
	}
}

func (e *WAFMetadataEncoder) Provision(caddy.Context) error {
	e.Encoder = newWAFMetadataEncoder()
	return nil
}

func (e *WAFMetadataEncoder) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next()
	if d.NextArg() || d.NextBlock(0) {
		return d.ArgErr()
	}
	return nil
}

type wafMetadataEncoder struct {
	zapcore.Encoder
}

func newWAFMetadataEncoder() zapcore.Encoder {
	return &wafMetadataEncoder{Encoder: zapcore.NewJSONEncoder(zapcore.EncoderConfig{
		MessageKey:  "msg",
		LevelKey:    "level",
		TimeKey:     "ts",
		NameKey:     "logger",
		EncodeLevel: zapcore.LowercaseLevelEncoder,
		EncodeTime:  zapcore.EpochTimeEncoder,
	})}
}

func (e *wafMetadataEncoder) Clone() zapcore.Encoder {
	return &wafMetadataEncoder{Encoder: e.Encoder.Clone()}
}

func (e *wafMetadataEncoder) EncodeEntry(entry zapcore.Entry, _ []zapcore.Field) (*buffer.Buffer, error) {
	matches := wafRuleIDPattern.FindAllStringSubmatch(entry.Message, 2)
	diagnosticKind := wafEngineDiagnosticKind(entry.Message)
	var fields []zapcore.Field
	switch {
	case diagnosticKind != "":
		entry.Message = wafEngineMessage
		fields = []zapcore.Field{zap.String("kind", diagnosticKind)}
	case len(matches) == 1:
		entry.Message = wafUnknownMessage
		if id, err := strconv.Atoi(matches[0][1]); err == nil {
			entry.Message = wafMatchMessage
			fields = []zapcore.Field{zap.Int("rule_id", id)}
		}
	default:
		entry.Message = wafUnknownMessage
	}
	return zapcore.NewJSONEncoder(zapcore.EncoderConfig{
		MessageKey:  "msg",
		LevelKey:    "level",
		TimeKey:     "ts",
		NameKey:     "logger",
		EncodeLevel: zapcore.LowercaseLevelEncoder,
		EncodeTime:  zapcore.EpochTimeEncoder,
	}).EncodeEntry(entry, fields)
}

// Coraza and its Caddy connector share the rule callback's logger. Recognize
// only their fixed diagnostic messages; discard all context and error fields.
func wafEngineDiagnosticKind(message string) string {
	switch message {
	case "key not found in collection, returning the original text":
		return "macro_key_missing"
	case "error creating serial log writer":
		return "serial_log_writer_error"
	case "Failed to initialize audit log":
		return "audit_log_init_error"
	case "Failed to write audit log":
		return "audit_log_write_error"
	case "Error converting request length to integer":
		return "request_length_invalid"
	case "collection in setvar is not a map":
		return "setvar_collection_not_map"
	case "collection in setvar is nil":
		return "setvar_collection_nil"
	case "Invalid value":
		return "invalid_value"
	case "Invalid range":
		return "invalid_range"
	case "Invalid status":
		return "invalid_status"
	case "Invalid audit log part":
		return "invalid_audit_log_part"
	case "Unknown toggle":
		return "unknown_toggle"
	case "Invalid limit":
		return "invalid_limit"
	case "Invalid rule ID":
		return "invalid_rule_id"
	case "Invalid log level":
		return "invalid_log_level"
	case "attempted to use regex with non-selectable collection":
		return "regex_collection_unsupported"
	case "attempted to use string with non-selectable collection":
		return "string_collection_unsupported"
	case "ProcessRequestHeaders has already been called":
		return "request_headers_repeated"
	case "Calling ProcessRequestHeaders but there is a preexisting interruption":
		return "request_headers_interrupted"
	case "Calling ProcessRequestBody but there is a preexisting interruption":
		return "request_body_interrupted"
	case "Failed to process request body":
		return "request_body_error"
	case "ProcessResponseHeaders has already been called":
		return "response_headers_repeated"
	case "Calling ProcessResponseHeaders but there is a preexisting interruption":
		return "response_headers_interrupted"
	case "Calling ProcessResponseBody but there is a preexisting interruption":
		return "response_body_interrupted"
	case "Failed to process response body":
		return "response_body_error"
	case "Failed to close the transaction":
		return "transaction_close_error"
	// The connector reports any interruption, including body limits,
	// with this summary. It does not establish a separate rule match.
	case "WAF rule violation detected":
		return "interruption_summary"
	default:
		return ""
	}
}

var (
	_ caddy.Module          = (*WAFMetadataEncoder)(nil)
	_ caddy.Provisioner     = (*WAFMetadataEncoder)(nil)
	_ caddyfile.Unmarshaler = (*WAFMetadataEncoder)(nil)
	_ zapcore.Encoder       = (*wafMetadataEncoder)(nil)
)
