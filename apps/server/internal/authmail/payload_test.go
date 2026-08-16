package authmail

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateKind(t *testing.T) {
	valid := []Kind{KindVerify, KindReset, KindPasswordChanged}
	for _, k := range valid {
		if err := validateKind(k); err != nil {
			t.Errorf("validateKind(%q) = %v, want nil", k, err)
		}
	}
	invalid := []Kind{"", "verifyx", "Verify", "password_changed_extra"}
	for _, k := range invalid {
		if err := validateKind(k); !errors.Is(err, ErrInvalidKind) {
			t.Errorf("validateKind(%q) = %v, want ErrInvalidKind", k, err)
		}
	}
}

func TestValidateEmailMinimalStructural(t *testing.T) {
	valid := []string{
		"a@b.c",
		"alice@example.com",
		"a.b+c@sub.example.co",
		"a@b.c.d",
	}
	for _, e := range valid {
		if err := validateEmail(e); err != nil {
			t.Errorf("validateEmail(%q) = %v, want nil", e, err)
		}
	}

	invalid := []string{
		"",
		"a",
		"@b.c",
		"a@",
		"a@b@c",
		"a b@c",
		"a@b\tc",
		"a@b.c\n",
		"a\r@b.c",
	}
	for _, e := range invalid {
		if err := validateEmail(e); !errors.Is(err, ErrInvalidEmail) {
			t.Errorf("validateEmail(%q) = %v, want ErrInvalidEmail", e, err)
		}
	}
}

func TestValidateLink(t *testing.T) {
	cases := []struct {
		name string
		kind Kind
		link string
		want error
	}{
		{"verify valid", KindVerify, verifyLinkPrefix + "tok", nil},
		{"reset valid", KindReset, resetLinkPrefix + "tok", nil},
		{"password_changed empty", KindPasswordChanged, "", nil},
		{"verify empty token", KindVerify, verifyLinkPrefix, ErrInvalidLink},
		{"reset empty token", KindReset, resetLinkPrefix, ErrInvalidLink},
		{"verify wrong origin", KindVerify, "https://evil.example/verify-email#token=tok", ErrInvalidLink},
		{"verify reset prefix", KindVerify, resetLinkPrefix + "tok", ErrInvalidLink},
		{"reset verify prefix", KindReset, verifyLinkPrefix + "tok", ErrInvalidLink},
		{"password_changed with link", KindPasswordChanged, verifyLinkPrefix + "tok", ErrInvalidLink},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateLink(tc.kind, tc.link)
			if tc.want == nil && err != nil {
				t.Fatalf("validateLink = %v, want nil", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("validateLink = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestMarshalPayloadFieldOrder(t *testing.T) {
	b, err := marshalPayload(Payload{Version: 1, To: "a@b.c", Link: verifyLinkPrefix + "t"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"version":1,"to":"a@b.c","link":"` + verifyLinkPrefix + `t"}`
	if string(b) != want {
		t.Fatalf("marshal = %s, want %s", b, want)
	}
}

func TestMarshalPayloadOmitsEmptyLink(t *testing.T) {
	b, err := marshalPayload(Payload{Version: 1, To: "a@b.c"})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"version":1,"to":"a@b.c"}` {
		t.Fatalf("marshal = %s, want empty link omitted", b)
	}
}

func TestDecodePayloadStrictRejectsMalformed(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"non-object array", `[1,2,3]`},
		{"non-object string", `"hi"`},
		{"duplicate key", `{"version":1,"version":2,"to":"a@b.c"}`},
		{"unknown field", `{"version":1,"to":"a@b.c","evil":1}`},
		{"trailing value", `{"version":1,"to":"a@b.c"} {"x":1}`},
		{"trailing garbage", `{"version":1,"to":"a@b.c"} nope`},
		{"version string", `{"version":"1","to":"a@b.c"}`},
		{"version float", `{"version":1.5,"to":"a@b.c"}`},
		{"to number", `{"version":1,"to":42}`},
		{"missing version", `{"to":"a@b.c"}`},
		{"missing to", `{"version":1}`},
		{"empty object", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decodePayloadStrict([]byte(tc.in)); !errors.Is(err, ErrStrictJSON) {
				t.Fatalf("decodePayloadStrict(%q) = %v, want ErrStrictJSON", tc.in, err)
			}
		})
	}
}

func TestDecodePayloadStrictRoundtrip(t *testing.T) {
	p := validVerifyPayload()
	b, err := marshalPayload(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodePayloadStrict(b)
	if err != nil {
		t.Fatalf("decodePayloadStrict: %v", err)
	}
	if got != p {
		t.Fatalf("decoded = %+v, want %+v", got, p)
	}
}

func TestLinkPrefixConstants(t *testing.T) {
	if !strings.HasPrefix(verifyLinkPrefix, canonicalLinkOrigin+"/") {
		t.Fatalf("verifyLinkPrefix %q not under canonical origin", verifyLinkPrefix)
	}
	if !strings.HasPrefix(resetLinkPrefix, canonicalLinkOrigin+"/") {
		t.Fatalf("resetLinkPrefix %q not under canonical origin", resetLinkPrefix)
	}
	if verifyLinkPrefix == resetLinkPrefix {
		t.Fatal("verify and reset prefixes must differ")
	}
}
