package publicapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSelectedResponseGETHEADAnd304(t *testing.T) {
	selected, err := NewSelectedResponse(http.StatusOK, "application/json; charset=utf-8", "no-cache, must-revalidate", []byte("{\"data\":1}\n"), http.Header{"X-Test": {"yes"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, method, inm string
		status            int
		body              string
		length            string
	}{
		{"get", http.MethodGet, "", http.StatusOK, "{\"data\":1}\n", "11"},
		{"head", http.MethodHead, "", http.StatusOK, "", "11"},
		{"not modified", http.MethodGet, selected.Header.Get("ETag"), http.StatusNotModified, "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(context.Background(), test.method, "/", nil)
			if test.inm != "" {
				req.Header.Set("If-None-Match", test.inm)
			}
			w := httptest.NewRecorder()
			selected.ServeHTTP(w, req)
			if w.Code != test.status || w.Body.String() != test.body || w.Header().Get("Content-Length") != test.length {
				t.Fatalf("response = status %d body %q headers %#v", w.Code, w.Body.String(), w.Header())
			}
			if w.Header().Get("ETag") != selected.Header.Get("ETag") {
				t.Fatalf("ETag = %q", w.Header().Get("ETag"))
			}
		})
	}
}

func TestSelectedResponseRejectsMalformedConditional(t *testing.T) {
	selected, err := NewSelectedResponse(http.StatusOK, "image/png", "no-cache, must-revalidate", []byte("bytes"), nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.Header.Add("If-None-Match", `"ok"`)
	req.Header.Add("If-None-Match", `"also"`)
	w := httptest.NewRecorder()
	selected.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || w.Header().Get("ETag") != "" {
		t.Fatalf("response = %d %#v", w.Code, w.Header())
	}
}

func TestSelectedResponseRejectsContractBypasses(t *testing.T) {
	validBody := []byte("body")
	for _, test := range []struct {
		name         string
		status       int
		contentType  string
		cacheControl string
		body         []byte
		extra        http.Header
	}{
		{"wrong status", http.StatusCreated, "image/png", "no-cache, must-revalidate", validBody, nil},
		{"wrong cache policy", http.StatusOK, "image/png", "public, max-age=60", validBody, nil},
		{"empty type", http.StatusOK, "", "no-cache, must-revalidate", validBody, nil},
		{"empty body", http.StatusOK, "image/png", "no-cache, must-revalidate", nil, nil},
		{"oversize body", http.StatusOK, "image/png", "no-cache, must-revalidate", make([]byte, 2_097_153), nil},
		{"lowercase cookie", http.StatusOK, "image/png", "no-cache, must-revalidate", validBody, http.Header{"set-cookie": {"x=y"}}},
		{"mixed cookie", http.StatusOK, "image/png", "no-cache, must-revalidate", validBody, http.Header{"SeT-CoOkIe": {"x=y"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewSelectedResponse(test.status, test.contentType, test.cacheControl, test.body, test.extra); !errors.Is(err, ErrInvalidSelectedResponse) {
				t.Fatalf("NewSelectedResponse() error = %v, want %v", err, ErrInvalidSelectedResponse)
			}
		})
	}
}
