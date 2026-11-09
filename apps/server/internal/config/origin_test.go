package config

import "testing"

// TestLoadLogLevel pins the accepted LOG_LEVEL spellings and the exact
// rejection text.
func TestLoadLogLevel(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		raw     string
		want    string
		wantErr string
	}{
		{raw: "", want: "info"},
		{raw: "  ", want: "info"},
		{raw: "debug", want: "debug"},
		{raw: "info", want: "info"},
		{raw: "warn", want: "warn"},
		{raw: "error", want: "error"},
		{raw: " Error ", want: "error"},
		{raw: "DEBUG", want: "debug"},
		{raw: "trace", wantErr: `config: LOG_LEVEL: invalid value "trace": must be one of debug, info, warn, error`},
		{raw: "warning", wantErr: `config: LOG_LEVEL: invalid value "warning": must be one of debug, info, warn, error`},
		{raw: " fatal ", wantErr: `config: LOG_LEVEL: invalid value "fatal": must be one of debug, info, warn, error`},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			got, err := loadLogLevel(tc.raw)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("loadLogLevel(%q) error = %v, want %q", tc.raw, err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("loadLogLevel(%q) = %q, %v; want %q, nil", tc.raw, got, err, tc.want)
			}
		})
	}
}

// TestLoadEnv pins the accepted ENV spellings and the exact rejection text.
func TestLoadEnv(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		raw     string
		want    string
		wantErr string
	}{
		{raw: "dev", want: "dev"},
		{raw: "staging", want: "staging"},
		{raw: "prod", want: "prod"},
		{raw: " PROD ", want: "prod"},
		{raw: "Staging", want: "staging"},
		{raw: "", wantErr: "config: ENV is required: must be one of dev, staging, prod"},
		{raw: " ", wantErr: "config: ENV is required: must be one of dev, staging, prod"},
		{raw: "production", wantErr: `config: ENV: invalid value "production": must be one of dev, staging, prod`},
		{raw: " test ", wantErr: `config: ENV: invalid value "test": must be one of dev, staging, prod`},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			got, err := loadEnv(tc.raw)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("loadEnv(%q) error = %v, want %q", tc.raw, err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("loadEnv(%q) = %q, %v; want %q, nil", tc.raw, got, err, tc.want)
			}
		})
	}
}
