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
	var fields []zapcore.Field
	switch {
	case isWAFEngineDiagnostic(entry.Message):
		entry.Message = wafEngineMessage
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
func isWAFEngineDiagnostic(message string) bool {
	switch message {
	case "key not found in collection, returning the original text",
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
		// The connector reports any interruption, including body limits,
		// with this summary. It does not establish a separate rule match.
		"WAF rule violation detected":
		return true
	default:
		return false
	}
}

var (
	_ caddy.Module          = (*WAFMetadataEncoder)(nil)
	_ caddy.Provisioner     = (*WAFMetadataEncoder)(nil)
	_ caddyfile.Unmarshaler = (*WAFMetadataEncoder)(nil)
	_ zapcore.Encoder       = (*wafMetadataEncoder)(nil)
)
