package publicstate

import (
	"context"
	"testing"
)

func TestMetricsTrackAdmissionMismatchAndActiveRepresentationLeases(t *testing.T) {
	t.Parallel()

	coordinator := newTestCoordinator(t, 41)
	lease, err := coordinator.AcquireDiscovery(context.Background(), 41, RepresentationSitemap)
	if err != nil {
		t.Fatalf("AcquireDiscovery() error = %v", err)
	}
	if _, err := coordinator.AcquireDiscovery(context.Background(), 40, RepresentationSitemap); err == nil {
		t.Fatal("AcquireDiscovery(mismatch) error = nil, want mismatch")
	}
	metrics := coordinator.metrics.snapshot()
	if metrics.mismatches != 1 || metrics.leases[RepresentationSitemap] != 1 {
		t.Fatalf("metrics = %+v, want one mismatch and one sitemap lease", metrics)
	}
	lease.Release()
	metrics = coordinator.metrics.snapshot()
	if metrics.leases[RepresentationSitemap] != 0 {
		t.Fatalf("metrics after Release() = %+v, want no sitemap leases", metrics)
	}
}
