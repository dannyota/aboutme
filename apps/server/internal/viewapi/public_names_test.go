package viewapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCollectRejectsInexactPropertyNames(t *testing.T) {
	f := newRouteFixture(t)
	body := f.startAndSolve(t, nil)
	f.now = f.now.Add(9 * time.Second)
	cases := map[string]struct{ body, path string }{
		"case-variant top level":    {strings.Replace(body, `"token"`, `"Token"`, 1), "Token"},
		"case-variant nested":       {strings.Replace(body, `"derivedKey"`, `"derivedkey"`, 1), "solution.derivedkey"},
		"case-variant in challenge": {strings.Replace(body, `"signature"`, `"Signature"`, 1), "challenge.Signature"},
		"case-variant duplicate":    {strings.Replace(body, `"token"`, `"Token":"x","token"`, 1), "Token"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if test.body == body {
				t.Fatalf("fixture body has no key to vary: %s", body)
			}
			w := f.request(http.MethodPost, collectPath, test.body, nil)
			var envelope struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("collect = %d %q, want a request_invalid error", w.Code, w.Body.String())
			}
			if w.Code != http.StatusBadRequest || envelope.Error.Code != "request_invalid" ||
				!strings.Contains(envelope.Error.Message, `"`+test.path+`"`) {
				t.Fatalf("collect = %d %+v, want 400 request_invalid naming %s", w.Code, envelope.Error, test.path)
			}
		})
	}
	if w := f.request(http.MethodPost, collectPath, body, nil); w.Code != http.StatusNoContent {
		t.Fatalf("exact names = %d %q, want 204", w.Code, w.Body.String())
	}
	if total := f.totals(t); total.Counted != 1 {
		t.Fatalf("totals = %+v, want only the exact request counted", total)
	}
}

func TestStartRejectsAnyPropertyWithItsPath(t *testing.T) {
	f := newRouteFixture(t)
	w := f.request(http.MethodPost, startPath, `{"Mode":"ask"}`, nil)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `\"Mode\"`) {
		t.Fatalf("start = %d %q, want 400 naming Mode", w.Code, w.Body.String())
	}
}
