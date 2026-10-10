package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

const (
	safeMetadataHTTPError = "http_error"
	safeMetadataCrowdSec  = "crowdsec_event"
	safeMetadataTLS       = "tls_event"
)

func init() {
	caddy.RegisterModule(SafeMetadataEncoder{})
}

// SafeMetadataEncoder replaces an operational log entry with a timestamp,
// severity, and fixed marker. It is used where upstream, TLS, or CrowdSec
// messages and fields can contain request data or a decision address.
type SafeMetadataEncoder struct {
	Marker          string `json:"marker,omitempty"`
	zapcore.Encoder `json:"-"`
}

type safeMetadataRecord struct {
	Timestamp string `json:"ts"`
	Level     string `json:"level"`
	Message   string `json:"msg"`
}

func (SafeMetadataEncoder) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "caddy.logging.encoders.safe_metadata",
		New: func() caddy.Module { return new(SafeMetadataEncoder) },
	}
}

func (e *SafeMetadataEncoder) Provision(caddy.Context) error {
	if !validSafeMetadataMarker(e.Marker) {
		return fmt.Errorf("invalid safe metadata marker")
	}
	e.Encoder = discardEncoder()
	return nil
}

func (e *SafeMetadataEncoder) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next()
	if !d.NextArg() {
		return d.ArgErr()
	}
	e.Marker = d.Val()
	if !validSafeMetadataMarker(e.Marker) {
		return d.Errf("unknown safe metadata marker %q", e.Marker)
	}
	if d.NextArg() || d.NextBlock(0) {
		return d.ArgErr()
	}
	return nil
}

func newSafeMetadataEncoder(marker string) *SafeMetadataEncoder {
	return &SafeMetadataEncoder{Marker: marker, Encoder: discardEncoder()}
}

func validSafeMetadataMarker(marker string) bool {
	return marker == safeMetadataHTTPError || marker == safeMetadataCrowdSec || marker == safeMetadataTLS
}

func (*SafeMetadataEncoder) AddObject(string, zapcore.ObjectMarshaler) error {
	return nil
}

func (e *SafeMetadataEncoder) Clone() zapcore.Encoder {
	clone := *e
	clone.Encoder = e.Encoder.Clone()
	return &clone
}

func (e *SafeMetadataEncoder) EncodeEntry(entry zapcore.Entry, _ []zapcore.Field) (*buffer.Buffer, error) {
	out := feedBufferPool.Get()
	timestamp := entry.Time.UTC()
	if !validSafeMetadataMarker(e.Marker) || entry.Time.IsZero() || timestamp.Year() < 0 || timestamp.Year() > 9999 {
		return out, nil
	}
	level, ok := safeMetadataLevel(entry.Level)
	if !ok {
		return out, nil
	}
	record := safeMetadataRecord{
		Timestamp: timestamp.Format(time.RFC3339Nano),
		Level:     level,
		Message:   e.Marker,
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return out, nil
	}
	_, _ = out.Write(encoded)
	out.AppendByte('\n')
	return out, nil
}

func safeMetadataLevel(level zapcore.Level) (string, bool) {
	switch level {
	case zapcore.DebugLevel, zapcore.InfoLevel, zapcore.WarnLevel, zapcore.ErrorLevel,
		zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel:
		return level.String(), true
	default:
		return "", false
	}
}

var (
	_ caddy.Module          = (*SafeMetadataEncoder)(nil)
	_ caddy.Provisioner     = (*SafeMetadataEncoder)(nil)
	_ caddyfile.Unmarshaler = (*SafeMetadataEncoder)(nil)
	_ zapcore.Encoder       = (*SafeMetadataEncoder)(nil)
)
