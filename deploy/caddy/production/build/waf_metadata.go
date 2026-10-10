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
	entry.Message = wafUnknownMessage
	var fields []zapcore.Field
	if len(matches) == 1 {
		if id, err := strconv.Atoi(matches[0][1]); err == nil {
			entry.Message = wafMatchMessage
			fields = []zapcore.Field{zap.Int("rule_id", id)}
		}
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

var (
	_ caddy.Module          = (*WAFMetadataEncoder)(nil)
	_ caddy.Provisioner     = (*WAFMetadataEncoder)(nil)
	_ caddyfile.Unmarshaler = (*WAFMetadataEncoder)(nil)
	_ zapcore.Encoder       = (*wafMetadataEncoder)(nil)
)
