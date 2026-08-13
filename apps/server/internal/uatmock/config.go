package uatmock

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"
)

// Config defines the one local Google account and its exact OAuth boundary.
type Config struct {
	IssuerURL    string
	PublicOrigin string
	RedirectURL  string
	ClientID     string
	ClientSecret string
	Now          func() time.Time
	Random       io.Reader
}

func (c Config) validate() error {
	issuer, err := url.Parse(c.IssuerURL)
	if err != nil || issuer.Scheme != "http" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" || issuer.Path != "/google" || !isLoopbackHost(issuer.Hostname()) {
		return fmt.Errorf("uatmock: invalid issuer URL")
	}
	if issuer.Port() == "" {
		return fmt.Errorf("uatmock: issuer URL must include a port")
	}

	origin, err := url.Parse(c.PublicOrigin)
	if err != nil || origin.Scheme != "https" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" || !isLoopbackHost(origin.Hostname()) {
		return fmt.Errorf("uatmock: invalid public origin")
	}
	if origin.Port() == "" {
		return fmt.Errorf("uatmock: public origin must include a port")
	}
	if c.RedirectURL != strings.TrimSuffix(c.PublicOrigin, "/")+googleCallbackPath {
		return fmt.Errorf("uatmock: invalid redirect URL")
	}
	if c.ClientID == "" || len(c.ClientID) > maxFieldBytes {
		return fmt.Errorf("uatmock: invalid client ID")
	}
	if c.ClientSecret == "" || len(c.ClientSecret) > maxFieldBytes {
		return fmt.Errorf("uatmock: invalid client secret")
	}
	if c.Now == nil {
		return fmt.Errorf("uatmock: clock is required")
	}
	if c.Random == nil {
		return fmt.Errorf("uatmock: randomness is required")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
