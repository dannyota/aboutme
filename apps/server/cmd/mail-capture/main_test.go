package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	good := []string{"--secret-file", "/tmp/secret"}
	cfg, err := parseArgs(good)
	if err != nil {
		t.Fatalf("parseArgs(%v) = %v", good, err)
	}
	if cfg.secretFile != "/tmp/secret" {
		t.Errorf("secretFile = %q, want /tmp/secret", cfg.secretFile)
	}
	if cfg.addr != defaultAddr {
		t.Errorf("addr = %q, want default %q", cfg.addr, defaultAddr)
	}

	cfg, err = parseArgs([]string{"--secret-file", "/tmp/secret", "--addr", "127.0.0.1:20091"})
	if err != nil {
		t.Fatalf("parseArgs loopback = %v", err)
	}
	if cfg.addr != "127.0.0.1:20091" {
		t.Errorf("addr = %q, want 127.0.0.1:20091", cfg.addr)
	}

	cases := []struct {
		name string
		args []string
	}{
		{"missing secret-file", []string{}},
		{"wildcard addr", []string{"--secret-file", "/tmp/s", "--addr", "0.0.0.0:20091"}},
		{"non-loopback addr", []string{"--secret-file", "/tmp/s", "--addr", "10.0.0.1:20091"}},
		{"hostname addr", []string{"--secret-file", "/tmp/s", "--addr", "example.com:20091"}},
		{"invalid addr", []string{"--secret-file", "/tmp/s", "--addr", "not-an-addr"}},
		{"positional arg", []string{"--secret-file", "/tmp/s", "extra"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseArgs(tc.args); err == nil {
				t.Fatalf("parseArgs(%v) = nil, want error", tc.args)
			}
		})
	}
}

func TestReadSecret(t *testing.T) {
	dir := t.TempDir()
	exact := make([]byte, secretLen)
	for i := range exact {
		exact[i] = byte(i)
	}

	path := filepath.Join(dir, "exact")
	if err := os.WriteFile(path, exact, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readSecret(path)
	if err != nil {
		t.Fatalf("readSecret: %v", err)
	}
	if len(got) != secretLen {
		t.Fatalf("len = %d, want %d", len(got), secretLen)
	}

	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"short", make([]byte, secretLen-1)},
		{"long", make([]byte, secretLen+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "_"))
			if err := os.WriteFile(p, tc.data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readSecret(p); err == nil {
				t.Fatalf("readSecret(%s) = nil, want error", tc.name)
			}
		})
	}

	if _, err := readSecret(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("readSecret(missing) = nil, want error")
	}
}
