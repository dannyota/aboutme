package publicapi

import "testing"

var whatwgLinkVerdicts = map[string]bool{
	"https://example.org": true, "https://example.org/": true, "https://example.org:443/x": true,
	"https://example.org:65535/x": true, "https://example.org:65536/x": false, "https://example.org:70000": false,
	"https://example.org:/x": true, "https://example.org:0080/x": true, "https://example.org:99999999999999999999/x": false,
	"https://user:pass@example.org/x": true, "https://example.123/x": false, "https://1.2.3.4/x": true,
	"https://1.2.3.256/x": false, "https://0x7f.1/x": true, "https://999999999999/x": false,
	"https://example.0x/x": false, "https://exa_mple.org/x": true, "https://exa%41mple.org/x": true,
	"https://exa%2Fmple.org/x": false, "https://[::1]/x": true, "https://[fe80::1]:8443/x": true,
	"https://bücher.example/x": true, "https://xn--bcher-kva.example/x": true, "https://a..b/x": true,
	"https://-a.example/x": true, "https://a-.example/x": true, "https://例え.テスト/x": true,
	"https://\u200dzwj.example/x": false, "https://example.org/a%20b": true, "https://example.org/%zz": true,
	"https://example.org#frag": true, "https://example.org?q=1": true, "https://EXAMPLE.org/x": true,
	"https://example.org./x": true, "https://a.b.c.d.e/x": true, "https://1.2.3.4.5/x": false,
	"https://08.1/x": false, "https://0.0.0.0/x": true, "https://%30.1/x": true,
	"http://example.org/x": false, "https://": false, "https://[::1%25eth0]/x": false,
	"https://ex^ample.org/x": false, "https://ex|ample.org/x": false, "https://ex ample.org/x": false,
	"https://example.org\\x": true,
}

func TestRendererLink_AgreesWithWHATWG(t *testing.T) {
	for link, want := range whatwgLinkVerdicts {
		if got := rendererLink(link); got != want {
			t.Errorf("rendererLink(%q) = %t, want %t as WHATWG URL parses it", link, got, want)
		}
	}
}

var whatwgOriginVerdicts = map[string]bool{
	"https://aboutme.example": true, "http://127.0.0.1:20030": true, "https://aboutme.example/": false,
	"https://Aboutme.example": false, "https://aboutme.example:443": false, "http://aboutme.example:80": false,
	"https://aboutme.example:8443": true, "ftp://aboutme.example": false, "https://user@aboutme.example": false,
	"https://aboutme.example?x": false, "https://aboutme.example:70000": false, "https://xn--bcher-kva.example": true,
	"https://bücher.example": false, "http://localhost:20080": true, "https://[::1]": true, "https://1.2.3.256": false,
}

func TestDecoderNormalizedOrigin_AgreesWithWHATWG(t *testing.T) {
	for origin, want := range whatwgOriginVerdicts {
		if got := decoderNormalizedOrigin(origin); got != want {
			t.Errorf("decoderNormalizedOrigin(%q) = %t, want %t as WHATWG URL parses it", origin, got, want)
		}
	}
	for _, value := range []any{nil, 1.0, []any{"https://aboutme.example"}} {
		if decoderNormalizedOrigin(value) {
			t.Errorf("decoderNormalizedOrigin(%#v) = true, want false", value)
		}
	}
}
