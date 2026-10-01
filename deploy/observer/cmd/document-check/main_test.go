package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func example(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func check(args []string, doc []byte) (string, error) {
	var out bytes.Buffer
	err := run(args, bytes.NewReader(doc), &out)
	return out.String(), err
}

func TestFreshDocumentListsEveryRunningImage(t *testing.T) {
	out, err := check([]string{"-now", "2026-10-02T03:21:00Z"}, example(t, "verified.json"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := "ghcr.io/dannyota/aboutme-server sha256:" + strings.Repeat("9f2c", 16) + " v0.6.5\n"
	if !strings.HasPrefix(out, want) {
		t.Errorf("first line = %q, want %q", out, want)
	}
	if n := strings.Count(out, "\n"); n != 3 {
		t.Errorf("got %d lines, want 3:\n%s", n, out)
	}
}

func TestRejects(t *testing.T) {
	verified := example(t, "verified.json")
	// stale_after 20 minutes out still goes stale 600 seconds after
	// observed_at.
	lateStaleAfter := bytes.Replace(verified,
		[]byte(`"stale_after": "2026-10-02T03:23:00Z"`), []byte(`"stale_after": "2026-10-02T03:40:00Z"`), 1)
	if bytes.Equal(lateStaleAfter, verified) {
		t.Fatal("verified.json no longer holds the stale_after this test replaces")
	}
	cases := []struct {
		name string
		args []string
		doc  []byte
		want string
	}{
		{"unverified summary", []string{"-now", "2026-10-02T03:21:00Z"}, example(t, "unverified.json"), "summary"},
		{"a component with no image", []string{"-now", "2026-10-02T03:21:00Z"}, example(t, "mismatch-missing-component.json"), "summary"},
		{"sbom not found", []string{"-now", "2026-10-02T03:21:00Z"}, example(t, "sbom-not-found.json"), "sbom"},
		{"-now well before observed_at", []string{"-now", "2026-10-02T03:18:59Z"}, verified, "before"},
		{"past stale_after", []string{"-now", "2026-10-02T03:23:01Z"}, verified, "stale"},
		{"over 600 seconds old", []string{"-now", "2026-10-02T03:30:01Z"}, lateStaleAfter, "stale"},
		{"unknown schema version", []string{"-now", "2027-03-01T00:01:00Z"}, example(t, "future-version.json"), "schema"},
		{"not the schema", []string{"-now", "2026-10-02T03:21:00Z"}, []byte(`{}`), "schema"},
		{"over 64 KiB", []string{"-now", "2026-10-02T03:21:00Z"}, append(verified, bytes.Repeat([]byte(" "), maxBytes)...), "over"},
		{"no -now", nil, verified, "usage"},
		{"a file argument", []string{"-now", "2026-10-02T03:21:00Z", "doc.json"}, verified, "usage"},
		{"bad -now", []string{"-now", "yesterday"}, verified, "-now"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := check(tc.args, tc.doc)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one containing %q", err, tc.want)
			}
			if out != "" {
				t.Errorf("printed %q on failure", out)
			}
		})
	}
}
