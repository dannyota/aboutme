package directrender

import (
	"errors"
	"net/url"
)

// ParseRenderOrigin accepts only the configured direct Nuxt listener. It is
// intentionally separate from the viewer-facing public origin parser.
func ParseRenderOrigin(raw, environment string) (RenderOrigin, error) {
	if raw == "" || len(raw) > 512 {
		return RenderOrigin{}, errors.New("render origin is empty or too long")
	}
	for i := range raw {
		if raw[i] < 0x21 || raw[i] > 0x7e {
			return RenderOrigin{}, errors.New("render origin must be printable ASCII")
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Host == "" || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" {
		return RenderOrigin{}, errors.New("invalid render origin")
	}
	if !allowedRenderOrigin(raw, environment) {
		return RenderOrigin{}, errors.New("render origin is not an allowed direct listener")
	}
	return RenderOrigin{value: raw}, nil
}

func allowedRenderOrigin(raw, environment string) bool {
	if environment == "prod" || environment == "production" || environment == "staging" {
		return raw == "http://127.0.0.1:3000"
	}
	if environment == "development" || environment == "dev" || environment == "test" {
		return raw == "http://127.0.0.1:20030" || raw == "http://127.0.0.1:20440" || raw == "http://web:3000"
	}
	return false
}

func (o RenderOrigin) String() string { return o.value }
