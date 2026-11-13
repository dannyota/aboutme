package config

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

func loadPasskeyEnrollmentFlag(raw string) (bool, error) {
	switch strings.TrimSpace(raw) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("config: PASSKEY_ENROLLMENT_ENABLED must be true or false")
	}
}

func validatePasskeyRelyingPartyOrigin(publicOrigin string) error {
	u, err := url.Parse(publicOrigin)
	if err != nil || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") || u.Host == "" {
		return errors.New("config: PASSKEY_ENROLLMENT_ENABLED requires a plain PUBLIC_ORIGIN")
	}
	host := u.Hostname()
	if host != strings.ToLower(host) || net.ParseIP(host) != nil || !validRelyingPartyHost(host) {
		return errors.New("config: PASSKEY_ENROLLMENT_ENABLED requires a PUBLIC_ORIGIN host that is a valid WebAuthn relying party ID")
	}
	if u.Scheme != "https" && (u.Scheme != "http" || host != "localhost") {
		return errors.New("config: PASSKEY_ENROLLMENT_ENABLED requires an https PUBLIC_ORIGIN, or http on localhost")
	}
	return nil
}

func validRelyingPartyHost(host string) bool {
	if host == "localhost" {
		return true
	}
	labels := strings.Split(host, ".")
	if len(host) > 253 || len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}
