package main

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/store"
)

// withSessionDefaults returns databaseURL with the migrate connection's
// session defaults added: statement_timeout 0, because DDL may run long, and
// lock_timeout 5s, so a migration blocked behind live traffic fails instead
// of queueing every later query behind its lock request. A setting the URL
// already has wins, and a migration file's SET LOCAL wins over both.
//
// migrations.Open takes a connection string, so the settings are appended
// as URL query or keyword/value parameters, which pgx sends as startup
// parameters. The result is parsed again to prove the target and login did
// not change.
func withSessionDefaults(databaseURL string) (string, error) {
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return "", err
	}
	var added []string
	for _, setting := range []struct{ name, value string }{
		{name: "statement_timeout", value: "0"},
		{name: "lock_timeout", value: "5s"},
	} {
		if !store.HasRuntimeSetting(config.RuntimeParams, setting.name) {
			added = append(added, setting.name+"="+setting.value)
		}
	}
	if len(added) == 0 {
		return databaseURL, nil
	}

	var result string
	if rest, isURL := cutURLScheme(databaseURL); isURL {
		separator := "?"
		if strings.HasSuffix(rest, "?") || strings.HasSuffix(rest, "&") {
			separator = ""
		} else if hasURLQuery(rest) {
			separator = "&"
		}
		result = databaseURL + separator + strings.Join(added, "&")
	} else {
		result = databaseURL + " " + strings.Join(added, " ")
	}

	check, err := pgx.ParseConfig(result)
	if err != nil {
		return "", err
	}
	if check.Host != config.Host || check.Port != config.Port || check.Database != config.Database ||
		check.User != config.User || check.Password != config.Password {
		return "", errors.New("session defaults changed the connection target")
	}
	return result, nil
}

func cutURLScheme(databaseURL string) (string, bool) {
	if rest, ok := strings.CutPrefix(databaseURL, "postgresql://"); ok {
		return rest, true
	}
	return strings.CutPrefix(databaseURL, "postgres://")
}

// hasURLQuery reports whether a connection URL, without its scheme, has a
// query. Like libpq, a '@' before the first '/' ends the user information,
// which may itself hold a raw '?'.
func hasURLQuery(rest string) bool {
	if i := strings.IndexAny(rest, "@/"); i >= 0 && rest[i] == '@' {
		rest = rest[i+1:]
	}
	return strings.Contains(rest, "?")
}
