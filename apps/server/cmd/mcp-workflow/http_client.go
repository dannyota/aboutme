package main

import (
	"errors"
	"net/http"
	"net/url"
)

var errCrossOrigin = errors.New("cross_origin_request")

type originTransport struct {
	origin *url.URL
	base   http.RoundTripper
}

func (t originTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil || req.URL.Scheme != t.origin.Scheme || req.URL.Host != t.origin.Host {
		return nil, errCrossOrigin
	}
	return t.base.RoundTrip(req)
}

func newRestrictedHTTPClient(origin *url.URL, transport http.RoundTripper) *http.Client {
	if transport == nil {
		var configuredTransport *http.Transport
		if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
			configuredTransport = defaultTransport.Clone()
		} else {
			configuredTransport = &http.Transport{}
		}
		configuredTransport.Proxy = nil
		transport = configuredTransport
	}
	return &http.Client{
		Transport: originTransport{origin: origin, base: transport},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errCrossOrigin
		},
	}
}
