package config

import "log/slog"

type Secret string

func (s Secret) Reveal() string { return string(s) }

func (Secret) String() string { return "[redacted]" }

func (Secret) GoString() string { return "[redacted]" }

func (Secret) LogValue() slog.Value { return slog.StringValue("[redacted]") }
