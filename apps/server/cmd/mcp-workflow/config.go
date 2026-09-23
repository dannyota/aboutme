package main

import (
	"errors"
	"net/url"
)

var errInvalidOrigin = errors.New("invalid origin")

func parseOrigin(raw string) (*url.URL, error) {
	if raw != "https://aboutme.vn" && raw != "https://localhost:20443" {
		return nil, errInvalidOrigin
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, errInvalidOrigin
	}
	if u.String() != raw {
		return nil, errInvalidOrigin
	}
	return u, nil
}
