package config

import "log/slog"

// Secret is a string value that formats and logs as "[redacted]". Read it with
// Reveal only where the value is used.
type Secret string

// Reveal returns the secret value.
func (s Secret) Reveal() string { return string(s) }

// String implements fmt.Stringer without the value.
func (Secret) String() string { return "[redacted]" }

// GoString implements fmt.GoStringer without the value.
func (Secret) GoString() string { return "[redacted]" }

// LogValue implements slog.LogValuer without the value.
func (Secret) LogValue() slog.Value { return slog.StringValue("[redacted]") }
