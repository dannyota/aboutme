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

// TestCollectRejectsInexactChallengeParameters covers the challenge
// parameters, which the ALTCHA library decodes with folded names, the last of
// repeated keys, and unknown keys ignored. Each variant answers the route's
// 400 before any counting layer runs, so it reveals no layer outcome and
// records nothing; the exact body still counts once.
func TestCollectRejectsInexactChallengeParameters(t *testing.T) {
	f := newRouteFixture(t)
	body := f.startAndSolve(t, nil)
	f.now = f.now.Add(9 * time.Second)
	if !strings.Contains(body, `"cost":1000`) {
		t.Fatalf("fixture challenge has no cost to vary: %s", body)
	}
	cases := map[string]struct{ body, path string }{
		"case variant": {strings.Replace(body, `"cost":1000`, `"Cost":1000`, 1), "challenge.parameters.Cost"},
		"unknown key":  {strings.Replace(body, `"cost":1000`, `"cost":1000,"rounds":1`, 1), "challenge.parameters.rounds"},
		"duplicate":    {strings.Replace(body, `"cost":1000`, `"cost":0,"cost":1000`, 1), "challenge.parameters.cost"},
		"wrong type":   {strings.Replace(body, `"cost":1000`, `"cost":"1000"`, 1), "challenge.parameters.cost"},
		"data repeat":  {strings.Replace(body, `"data":{`, `"data":{"view":"x",`, 1), "challenge.parameters.data.view"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
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
		t.Fatalf("exact body = %d %q, want 204", w.Code, w.Body.String())
	}
	if total := f.totals(t); total.Counted != 1 || total.Invalid != 0 {
		t.Fatalf("totals = %+v, want one count and no recorded outcome for the rejected bodies", total)
	}
}
