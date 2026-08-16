package password

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeBreach struct {
	breached bool
	err      error
	got      string
}

func (f *fakeBreach) Breached(_ context.Context, password string) (bool, error) {
	f.got = password
	return f.breached, f.err
}

func TestNormalizeBounds(t *testing.T) {
	t.Parallel()

	t.Run("raw byte cap", func(t *testing.T) {
		t.Parallel()
		if _, err := Normalize(strings.Repeat("a", 1025)); !errors.Is(err, ErrPasswordLength) {
			t.Errorf("Normalize(1025 bytes) error = %v, want ErrPasswordLength", err)
		}
		// 1024 bytes passes the raw cap but fails the 128-code-point cap.
		if _, err := Normalize(strings.Repeat("a", 1024)); !errors.Is(err, ErrPasswordLength) {
			t.Errorf("Normalize(1024 ascii bytes) error = %v, want ErrPasswordLength", err)
		}
	})

	t.Run("code point boundaries", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name  string
			raw   string
			valid bool
		}{
			{"14 code points", strings.Repeat("a", 14), false},
			{"15 code points", strings.Repeat("a", 15), true},
			{"128 code points", strings.Repeat("a", 128), true},
			{"129 code points", strings.Repeat("a", 129), false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				_, err := Normalize(tc.raw)
				if tc.valid && !errors.Is(err, nil) {
					t.Errorf("Normalize error = %v, want nil", err)
				}
				if !tc.valid && !errors.Is(err, ErrPasswordLength) {
					t.Errorf("Normalize error = %v, want ErrPasswordLength", err)
				}
			})
		}
	})

	t.Run("astral code points", func(t *testing.T) {
		t.Parallel()
		// U+1F600 is one code point but four UTF-8 bytes.
		if got, err := Normalize(strings.Repeat("😀", 15)); err != nil || got != strings.Repeat("😀", 15) {
			t.Errorf("Normalize(15 astral) = %q, %v; want unchanged, nil", got, err)
		}
		if _, err := Normalize(strings.Repeat("😀", 129)); !errors.Is(err, ErrPasswordLength) {
			t.Errorf("Normalize(129 astral) error = %v, want ErrPasswordLength", err)
		}
	})

	t.Run("NFC composition reduces code points", func(t *testing.T) {
		t.Parallel()
		// 129 raw code points: 127 'a' + one "e" + U+0301 pair composes to 128.
		raw := strings.Repeat("a", 127) + "é"
		got, err := Normalize(raw)
		if err != nil {
			t.Fatalf("Normalize error = %v", err)
		}
		if want := strings.Repeat("a", 127) + "é"; got != want {
			t.Errorf("Normalize = %q, want %q", got, want)
		}
		// 15 raw "e" + U+0301 pairs (30 code points) compose to 15 code points.
		raw = strings.Repeat("é", 15)
		if _, err := Normalize(raw); err != nil {
			t.Errorf("Normalize(15 composing pairs) error = %v, want nil", err)
		}
	})

	t.Run("spaces and case preserved", func(t *testing.T) {
		t.Parallel()
		raw := "My Secret Password 1"
		got, err := Normalize(raw)
		if err != nil {
			t.Fatalf("Normalize error = %v", err)
		}
		if got != raw {
			t.Errorf("Normalize = %q, want %q (spaces/case preserved)", got, raw)
		}
	})

	t.Run("controls allowed as password data", func(t *testing.T) {
		t.Parallel()
		raw := "abc\x00def\x01ghi\x02jkl" // 15 code points including controls
		got, err := Normalize(raw)
		if err != nil {
			t.Fatalf("Normalize(controls) error = %v, want nil", err)
		}
		if got != raw {
			t.Errorf("Normalize(controls) = %q, want %q (controls preserved)", got, raw)
		}
	})
}

func TestCheckNewStages(t *testing.T) {
	t.Parallel()

	blocklist, err := NewBlocklist(testBlocklistData("commonpassword123"))
	if err != nil {
		t.Fatalf("NewBlocklist error = %v", err)
	}

	t.Run("blocklist rejects common", func(t *testing.T) {
		t.Parallel()
		breach := &fakeBreach{}
		p := NewPolicy(blocklist, breach)
		_, err := p.CheckNew(context.Background(), "commonpassword123")
		if !errors.Is(err, ErrPasswordCommon) {
			t.Errorf("CheckNew error = %v, want ErrPasswordCommon", err)
		}
		if breach.got != "" {
			t.Errorf("breach checker called with %q before blocklist rejection", breach.got)
		}
	})

	t.Run("breach rejects breached", func(t *testing.T) {
		t.Parallel()
		breach := &fakeBreach{breached: true}
		p := NewPolicy(blocklist, breach)
		_, err := p.CheckNew(context.Background(), "uniquepassword123")
		if !errors.Is(err, ErrPasswordBreached) {
			t.Errorf("CheckNew error = %v, want ErrPasswordBreached", err)
		}
		if breach.got != "uniquepassword123" {
			t.Errorf("breach checker received %q, want the NFC-normalized password", breach.got)
		}
	})

	t.Run("breach failure propagates", func(t *testing.T) {
		t.Parallel()
		breach := &fakeBreach{err: ErrBreachUnavailable}
		p := NewPolicy(blocklist, breach)
		_, err := p.CheckNew(context.Background(), "uniquepassword123")
		if !errors.Is(err, ErrBreachUnavailable) {
			t.Errorf("CheckNew error = %v, want ErrBreachUnavailable", err)
		}
	})

	t.Run("clean password returns normalized", func(t *testing.T) {
		t.Parallel()
		breach := &fakeBreach{}
		p := NewPolicy(blocklist, breach)
		res, err := p.CheckNew(context.Background(), "My Very Unique Password!")
		if err != nil {
			t.Fatalf("CheckNew error = %v", err)
		}
		if res.Normalized != "My Very Unique Password!" {
			t.Errorf("Normalized = %q, want the input unchanged", res.Normalized)
		}
	})

	t.Run("length fails before any lookup", func(t *testing.T) {
		t.Parallel()
		breach := &fakeBreach{}
		p := NewPolicy(blocklist, breach)
		_, err := p.CheckNew(context.Background(), "short")
		if !errors.Is(err, ErrPasswordLength) {
			t.Errorf("CheckNew error = %v, want ErrPasswordLength", err)
		}
		if breach.got != "" {
			t.Errorf("breach checker called with %q despite a length failure", breach.got)
		}
	})
}
