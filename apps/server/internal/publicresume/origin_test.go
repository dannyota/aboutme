package publicresume

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type originCase struct {
	Origin      string `json:"origin"`
	Environment string `json:"environment"`
	Accept      bool   `json:"accept"`
}

func TestPublicOriginParityTable(t *testing.T) {
	raw, err := os.ReadFile("testdata/public-origins.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Cases []originCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &table); err != nil || len(table.Cases) == 0 {
		t.Fatalf("parity table unreadable: %v", err)
	}
	for _, test := range table.Cases {
		t.Run(test.Environment+" "+test.Origin, func(t *testing.T) {
			origin, err := ParsePublicOrigin(test.Origin, test.Environment)
			if test.Accept != (err == nil) {
				t.Fatalf("ParsePublicOrigin() error = %v, want accept = %v", err, test.Accept)
			}
			if err == nil && origin.String() != test.Origin {
				t.Fatalf("String() = %q, want the input unchanged", origin.String())
			}
		})
	}
}

func TestPublicOriginLengthBound(t *testing.T) {
	host := strings.Repeat(strings.Repeat("a", 63)+".", 8)
	longest := "https://" + host[:512-len("https://")-len(".vn")] + ".vn"
	if _, err := ParsePublicOrigin(longest, "production"); err != nil || len(longest) != 512 {
		t.Fatalf("512-byte origin: %v", err)
	}
	if _, err := ParsePublicOrigin(longest+"n", "production"); err == nil {
		t.Fatal("513-byte origin accepted")
	}
	if _, err := ParsePublicOrigin("", "production"); err == nil {
		t.Fatal("empty origin accepted")
	}
}

func TestPublicOriginResolve(t *testing.T) {
	origin, err := ParsePublicOrigin("https://resume.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := origin.Resolve("/api/v1/public/resumes/ada/photo"), "https://resume.example/api/v1/public/resumes/ada/photo"; got != want {
		t.Fatalf("Resolve() = %q, want %q", got, want)
	}
}
