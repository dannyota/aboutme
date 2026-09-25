package devdb_test

import (
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/devdb"
)

func TestValidateURL(t *testing.T) {
	t.Parallel()
	const (
		database = "aboutme_dev"
		valid    = "postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable"
		secret   = "devdb-test-secret"
	)
	tests := []struct {
		name string
		raw  string
		want string // error substring; empty means accepted
	}{
		{name: "loopback url", raw: valid},
		{name: "postgresql scheme", raw: "postgresql://127.0.0.1/aboutme_dev"},
		{name: "keyword form", raw: "host=127.0.0.1 dbname=aboutme_dev"},
		{name: "empty", raw: "  ", want: "is required"},
		{name: "other scheme", raw: "mysql://127.0.0.1/aboutme_dev", want: "valid postgres"},
		{name: "localhost alias", raw: "postgres://localhost/aboutme_dev", want: "127.0.0.1"},
		{name: "remote host", raw: "postgres://db.example.invalid/aboutme_dev", want: "127.0.0.1"},
		{name: "query host", raw: valid + "&host=db.example.invalid", want: "127.0.0.1"},
		{name: "query fallback host", raw: valid + "&host=127.0.0.1,db.example.invalid", want: "127.0.0.1"},
		{name: "wrong database", raw: "postgres://127.0.0.1/aboutme", want: `"aboutme_dev"`},
		{name: "query database", raw: valid + "&dbname=aboutme", want: `"aboutme_dev"`},
		{name: "no database", raw: "postgres://127.0.0.1", want: "name a database"},
		{name: "malformed", raw: "postgres://aboutme:" + secret + "@127.0.0.1:20432/%zz", want: "valid postgres"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := devdb.ValidateURL(tt.raw, database)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("ValidateURL() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateURL() error = %v, want substring %q", err, tt.want)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("ValidateURL() exposed the password: %v", err)
			}
		})
	}
}
