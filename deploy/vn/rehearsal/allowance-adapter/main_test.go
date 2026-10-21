package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSelectedUsageRequiresCompleteNumericIdentity(t *testing.T) {
	valid := `{"success":true,"datas":[{"projectId":"synthetic","regionId":"region","regionName":"HCM04","totalQuota":30,"usage":1.25}]}`
	quota, usage, err := projectNumbers([]byte(valid), "synthetic", "region")
	if err != nil || quota != "30" || usage != "1.25" {
		t.Fatal("numeric allowance missing")
	}
	for _, body := range []string{
		strings.Replace(valid, `"usage":1.25`, `"usage":null`, 1),
		strings.Replace(valid, `"usage":1.25`, `"usage":"1.25"`, 1),
		strings.Replace(valid, `"usage":1.25`, `"usage":-1`, 1),
		strings.Replace(valid, `"usage":1.25`, `"usage":0,"usage":1.25`, 1),
		strings.Replace(valid, `"regionId":"region"`, `"regionId":"other"`, 1),
		strings.Replace(valid, `"success":true`, `"success":false`, 1),
		strings.Replace(valid, `"datas":[`, `"isNext":true,"datas":[`, 1),
	} {
		if _, _, err := projectNumbers([]byte(body), "synthetic", "region"); err == nil {
			t.Fatal("invalid allowance accepted")
		}
	}
}

func TestDuplicateProjectAndRegionRefused(t *testing.T) {
	project := `{"projectId":"synthetic","regionId":"region","regionName":"HCM04","totalQuota":30,"usage":1}`
	if _, _, err := projectNumbers([]byte(`{"success":true,"datas":[`+project+`,`+project+`]}`), "synthetic", "region"); err == nil {
		t.Fatal("duplicate project accepted")
	}
	region := `{"regionId":"region","regionName":"HCM04"}`
	if _, err := regionIdentity([]byte(`{"success":true,"datas":[` + region + `,` + region + `]}`)); err == nil {
		t.Fatal("duplicate region accepted")
	}
}

func TestRawGigabytesUseConservativeIntegerBounds(t *testing.T) {
	lower, err := gigabytes("10.1", true)
	if err != nil || lower != 10100000000 {
		t.Fatal("quota bound changed")
	}
	upper, err := gigabytes("10.1", false)
	if err != nil || upper != 10844792423 {
		t.Fatal("usage bound changed")
	}
	for _, input := range []string{"-1", "NaN", "Infinity", "null"} {
		if _, err := gigabytes(input, false); err == nil {
			t.Fatal("invalid numeric input accepted")
		}
	}
}

// Metadata requests cannot widen the selected read operation.
func TestMetadataTransportRefusesOtherEndpointsAndMissingHeaders(t *testing.T) {
	for _, target := range []string{
		"https://example.com/internal/v1/projects",
		"https://vstorage.console.greennode.ai/internal/v1/projects?extra=1",
		"https://vstorage.console.greennode.ai/internal/v1/buckets",
		"https://vstorage.console.greennode.ai/internal/v1/projects",
	} {
		transport := &captureTransport{base: refusingTransport{}, remaining: 1 << 20, region: "region"}
		transport.active.Store(true)
		request, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatal("request fixture failed")
		}
		if _, err := transport.RoundTrip(request); err == nil {
			t.Fatal("unbounded metadata read accepted")
		}
	}
}

type refusingTransport struct{}

func (refusingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, io.EOF
}
