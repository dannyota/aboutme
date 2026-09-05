package config

import (
	"errors"
	"path/filepath"
	"strings"
)

func loadPrintConfig(getenv func(string) string, environment string) (string, string, error) {
	address := getenv("PRINT_LISTEN_ADDR")
	if address == "" {
		address = "127.0.0.1:8081"
		if environment == "dev" {
			address = "127.0.0.1:20082"
		}
	}
	allowed := address == "127.0.0.1:8081" && requiresProductionTrustBoundary(environment)
	if environment == "dev" {
		allowed = address == "127.0.0.1:20082" || address == "127.0.0.1:20445" || address == "10.91.0.2:8081"
	}
	if !allowed {
		return "", "", errors.New("config: PRINT_LISTEN_ADDR must be an approved private listener")
	}
	executable := getenv("CHROMIUM_PATH")
	if executable == "" {
		executable = "/opt/chromium/chrome"
	}
	if !filepath.IsAbs(executable) || filepath.Clean(executable) != executable || executable == "/" || strings.TrimSpace(executable) != executable || strings.ContainsAny(executable, "\x00\r\n") {
		return "", "", errors.New("config: CHROMIUM_PATH must be a clean absolute executable path")
	}
	return address, executable, nil
}
