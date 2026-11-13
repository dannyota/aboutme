package devdb

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// pgx lets query parameters such as host= and dbname= override the URL, so the URL is parsed as pgx parses it.
func ValidateURL(raw, database string) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New("--database-url is required")
	}
	config, err := pgx.ParseConfig(raw)
	if err != nil {
		return errors.New("--database-url must be a valid postgres connection string")
	}
	if config.Host != "127.0.0.1" {
		return fmt.Errorf("--database-url must target loopback 127.0.0.1, got %q", config.Host)
	}
	for _, fallback := range config.Fallbacks {
		if fallback.Host != "127.0.0.1" {
			return fmt.Errorf("--database-url must target loopback 127.0.0.1, got %q", fallback.Host)
		}
	}
	if config.Database == "" {
		return errors.New("--database-url must name a database")
	}
	if config.Database != database {
		return fmt.Errorf("--database-url must target database %q, got %q", database, config.Database)
	}
	return nil
}
