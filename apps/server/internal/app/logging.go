package app

import (
	"log/slog"
	"os"

	"danny.vn/aboutme/apps/server/internal/config"
)

func NewLogger(cfg config.Config) *slog.Logger {
	level := parseLevel(cfg.LogLevel)
	opts := &slog.HandlerOptions{Level: level}

	if cfg.Env == "dev" {
		return slog.New(slog.NewTextHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}

func parseLevel(raw string) slog.Level {
	switch raw {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
