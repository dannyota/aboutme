package oauthsrv

import (
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestCIMDURLGrammar(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"https://client.example/metadata", "https://xn--bcher-kva.example/a%20b",
		"https://client.example/" + strings.Repeat("a", 233),
	} {
		if _, err := parseCIMDURL(raw, "aboutme.vn"); err != nil {
			t.Fatalf("valid fixture rejected: %v", err)
		}
	}
	for _, raw := range []string{
		"https://client.example/", "https://client.example", "HTTPS://client.example/a",
		"https://Client.example/a", "https://client.example./a", "https://client..example/a",
		"https://-client.example/a", "https://client-.example/a", "https://client.example:443/a",
		"https://user@client.example/a", "https://client.example/a?", "https://client.example/a?q=x",
		"https://client.example/a#", "https://client.example/a\\b", "https://client.example/a%xx",
		"https://client.example/./a", "https://client.example/%2E%2E/a", "https://client.example/a%2f..%2fb",
		"https://127.0.0.1/a", "https://2130706433/a", "https://0x7f.0.0.1/a", "https://[::1]/a",
		"https://localhost/a", "https://a.localhost/a", "https://a.local/a", "https://a.internal/a",
		"https://a.home.arpa/a", "https://a.onion/a", "https://aboutme.vn/a", "https://sub.aboutme.vn/a",
		"https://client.example/" + strings.Repeat("a", 234),
	} {
		if _, err := parseCIMDURL(raw, "aboutme.vn"); err == nil {
			t.Fatalf("invalid fixture accepted: %q", raw)
		}
	}
}

func TestCIMDAddressRefusals(t *testing.T) {
	t.Parallel()
	for _, fixture := range cimdAddressFixtures() {
		if got := cimdAddressAllowed(fixture.address); got != fixture.allowed {
			t.Fatalf("address %s: got %v, want %v", fixture.address, got, fixture.allowed)
		}
	}
}

type cimdAddressFixture struct {
	address netip.Addr
	allowed bool
}

func cimdAddressFixtures() []cimdAddressFixture {
	var fixtures []cimdAddressFixture
	// Every containing refusal range exercises its boundaries and an interior
	// address. Mapped IPv4 fixtures exercise the same policy after unmapping.
	fixtureRanges := strings.Fields(`0.0.0.0/8 10.0.0.0/8 100.64.0.0/10 127.0.0.0/8 169.254.0.0/16 172.16.0.0/12 192.0.0.0/24 192.0.2.0/24 192.31.196.0/24 192.52.193.0/24 192.88.99.0/24 192.168.0.0/16 192.175.48.0/24 198.18.0.0/15 198.51.100.0/24 203.0.113.0/24 224.0.0.0/4 240.0.0.0/4
::/128 ::1/128 64:ff9b::/96 64:ff9b:1::/48 100::/64 100:0:0:1::/64 2001::/23 2001:db8::/32 2002::/16 2620:4f:8000::/48 3fff::/20 5f00::/16 fc00::/7 fe80::/10 fec0::/10 ff00::/8
0.0.0.0/32 192.0.0.0/29 192.0.0.8/32 192.0.0.9/32 192.0.0.10/32 192.0.0.170/32 192.0.0.171/32 192.88.99.2/32 255.255.255.255/32 2001::/32 2001:1::1/128 2001:1::2/128 2001:1::3/128 2001:2::/48 2001:3::/32 2001:4:112::/48 2001:10::/28 2001:20::/28 2001:30::/28`)
	for _, raw := range fixtureRanges {
		prefix := netip.MustParsePrefix(raw)
		first := prefix.Masked().Addr()
		bytes := first.As16()
		bits := prefix.Bits()
		if first.Is4() {
			bits += 96
		}
		for bit := bits; bit < 128; bit++ {
			bytes[bit/8] |= 1 << uint(7-bit%8)
		}
		last := netip.AddrFrom16(bytes)
		if first.Is4() {
			last = last.Unmap()
		}
		addresses := []netip.Addr{first, last}
		if prefix.Contains(first.Next()) {
			addresses = append(addresses, first.Next())
		}
		for _, addr := range addresses {
			fixtures = append(fixtures, cimdAddressFixture{addr, false})
			if addr.Is4() {
				fixtures = append(fixtures, cimdAddressFixture{netip.AddrFrom16(addr.As16()), false})
			}
		}
		for _, neighbor := range []netip.Addr{first.Prev(), last.Next()} {
			if !neighbor.IsValid() {
				continue
			}
			allowed := neighbor.Is4() || netip.MustParsePrefix("2000::/3").Contains(neighbor)
			for _, fixture := range fixtureRanges {
				if netip.MustParsePrefix(fixture).Contains(neighbor) {
					allowed = false
				}
			}
			fixtures = append(fixtures, cimdAddressFixture{neighbor, allowed})
			if neighbor.Is4() {
				fixtures = append(fixtures, cimdAddressFixture{netip.AddrFrom16(neighbor.As16()), allowed})
			}
		}
	}
	for _, raw := range []string{"::7f00:1", "::8.8.8.8", "100:0:0:1::1", "5f00::1"} {
		fixtures = append(fixtures, cimdAddressFixture{netip.MustParseAddr(raw), false})
	}
	for _, raw := range []string{"8.8.8.8", "::ffff:8.8.8.8", "2606:4700::1111"} {
		fixtures = append(fixtures, cimdAddressFixture{netip.MustParseAddr(raw), true})
	}
	return fixtures
}

func TestCIMDDocumentValidation(t *testing.T) {
	t.Parallel()
	id := "https://client.example/metadata"
	valid := `{"client_id":"` + id + `","client_name":"Fixture","redirect_uris":["https://client.example/callback"],"token_endpoint_auth_method":"none"}`
	for _, extra := range []string{"", `,"grant_types":["authorization_code","urn:ietf:params:oauth:grant-type:jwt-bearer"]`, `,"ignored":{"nested":[1,true,null]}`} {
		body := strings.TrimSuffix(valid, "}") + extra + "}"
		if _, err := validateCIMDDocument([]byte(body), id, "client.example"); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{
		strings.Replace(valid, id, "https://other.example/metadata", 1),
		strings.Replace(valid, `,"token_endpoint_auth_method":"none"`, "", 1),
		strings.Replace(valid, `"none"`, `"private_key_jwt"`, 1),
		strings.Replace(valid, `"Fixture"`, `null`, 1),
		strings.Replace(valid, `"Fixture"`, `true`, 1),
		strings.Replace(valid, `https://client.example/callback`, `https://other.example/callback`, 1),
		strings.TrimSuffix(valid, "}") + `,"client_secret":null}`,
		strings.TrimSuffix(valid, "}") + `,"jwks_uri":"https://client.example/keys"}`,
		strings.TrimSuffix(valid, "}") + `,"ignored":{"x":1,"x":2}}`,
		strings.TrimSuffix(valid, "}") + `,"client_name":"Second"}`,
		strings.TrimSuffix(valid, "}") + `,"response_types":["token"]}`,
		strings.TrimSuffix(valid, "}") + `,"grant_types":["refresh_token"]}`,
		strings.TrimSuffix(valid, "}") + `,"scope":"publish"}`,
		valid + ` {}`, "null", `[]`,
		strings.TrimSuffix(valid, "}") + `,"ignored":[{"nested":{"x":1,"x":2}}]}`,
		strings.TrimSuffix(valid, "}") + `,"ignored":"` + string([]byte{0xff}) + `"}`,
		strings.Replace(valid, `"Fixture"`, `"\u0000"`, 1),
		strings.Replace(valid, `["https://client.example/callback"]`, `["http://localhost:1/callback","http://localhost:2/callback","http://localhost:3/callback","http://localhost:4/callback","http://localhost:5/callback","http://localhost:6/callback"]`, 1),
		strings.Replace(valid, `["https://client.example/callback"]`, `[null]`, 1),
		strings.TrimSuffix(valid, "}") + `,"grant_types":null}`,
		strings.TrimSuffix(valid, "}") + `,"grant_types":[]}`,
		strings.TrimSuffix(valid, "}") + `,"response_types":null}`,
		strings.TrimSuffix(valid, "}") + `,"scope":null}`,
		strings.TrimSuffix(valid, "}") + `,"application_type":null}`,
		strings.TrimSuffix(valid, "}") + `,"application_type":"native"}`,
		strings.TrimSuffix(valid, "}") + `,"application_type":"unknown"}`,
		strings.TrimSuffix(valid, "}") + `,"client_secret_expires_at":0}`,
		strings.TrimSuffix(valid, "}") + `,"jwks":{}}`,
	} {
		if _, err := validateCIMDDocument([]byte(body), id, "client.example"); err == nil {
			t.Fatal("invalid document accepted")
		}
	}
	nfc := strings.Replace(valid, `"Fixture"`, `"e\u0301"`, 1)
	metadata, err := validateCIMDDocument([]byte(nfc), id, "client.example")
	if err != nil || metadata.name != "é" {
		t.Fatal("document name was not normalized to NFC")
	}
}

func TestCIMDLoopbackRedirectMatching(t *testing.T) {
	t.Parallel()
	m := CIMDMetadata{host: "client.example", redirects: []string{"http://localhost/callback", "http://127.0.0.1/callback", "http://[::1]/callback", "https://client.example/callback"}}
	for _, raw := range []string{"http://localhost:20095/callback", "http://127.0.0.1:65535/callback", "http://[::1]:1/callback", "https://client.example/callback"} {
		if !m.matchesRedirect(raw) {
			t.Fatalf("valid redirect refused: %s", raw)
		}
	}
	for _, raw := range []string{"http://localhost:0/callback", "http://localhost:65536/callback", "http://localhost/callback?", "http://localhost/Callback", "https://other.example/callback"} {
		if m.matchesRedirect(raw) {
			t.Fatalf("invalid redirect accepted: %s", raw)
		}
	}
}

func TestCIMDCacheLifetime(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		header string
		want   time.Duration
	}{
		{"", 5 * time.Minute}, {"max-age=0", 5 * time.Minute}, {"max-age=600", 10 * time.Minute},
		{"max-age=999999", time.Hour}, {"max-age=600, s-maxage=900", 15 * time.Minute},
		{"max-age=600, private", 5 * time.Minute}, {"max-age=600, no-store", 5 * time.Minute},
		{"max-age=600, no-cache", 5 * time.Minute}, {"max-age=600, max-age=600", 5 * time.Minute},
		{"max-age=600, s-maxage=no", 5 * time.Minute}, {"max-age=999999999999999999999999", 5 * time.Minute},
		{"max-age=-1", 5 * time.Minute},
	} {
		if got := cimdCacheLifetime(tc.header, 5*time.Minute, time.Hour); got != tc.want {
			t.Fatalf("%q = %v, want %v", tc.header, got, tc.want)
		}
	}
}
