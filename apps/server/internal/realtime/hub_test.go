package realtime

import (
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
)

var (
	accountA = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	accountB = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	resumeA  = uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	resumeB  = uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
)

func newAvailableHub(t *testing.T, cfg Config) *Hub {
	t.Helper()
	if cfg.AdmitFD == nil {
		cfg.AdmitFD = func() bool { return true }
	}
	h, err := NewHub(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h.SetAvailable(true)
	t.Cleanup(h.Close)
	return h
}

func subscribe(t *testing.T, h *Hub, scope Scope) *Subscription {
	t.Helper()
	s, err := h.Subscribe(scope)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func requireClosed(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	default:
		t.Fatal("subscription remains open")
	}
}

func requireNoEvent(t *testing.T, events <-chan Change) {
	t.Helper()
	select {
	case got, open := <-events:
		if open {
			t.Fatalf("unexpected event: %+v", got)
		}
	default:
	}
}

func TestHubStartsUnavailableAndDispatchesOnlyMatchingScopes(t *testing.T) {
	h, err := NewHub(Config{QueueDepth: 2, AdmitFD: func() bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if _, err := h.Subscribe(Scope{AccountID: accountA, IP: "127.0.0.1"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("initial admission = %v, want unavailable", err)
	}
	h.SetAvailable(true)
	owner := subscribe(t, h, Scope{AccountID: accountA, IP: "127.0.0.1"})
	otherOwner := subscribe(t, h, Scope{AccountID: accountB, IP: "127.0.0.2"})
	public := subscribe(t, h, Scope{ResumeID: resumeA, IP: "127.0.0.3"})
	otherPublic := subscribe(t, h, Scope{ResumeID: resumeB, IP: "127.0.0.4"})
	change := Change{AccountID: accountA, ResumeID: resumeA, Revision: 7}
	h.Publish(change)
	if got := <-owner.Events; got != change {
		t.Fatalf("owner event = %+v, want %+v", got, change)
	}
	if got := <-public.Events; got != change {
		t.Fatalf("public event = %+v, want %+v", got, change)
	}
	requireNoEvent(t, otherOwner.Events)
	requireNoEvent(t, otherPublic.Events)
}

func TestHubEnforcesEachAdmissionBoundaryIndependently(t *testing.T) {
	tests := []struct {
		name          string
		config        Config
		first, second Scope
	}{
		{"total across distinct keys", Config{MaxConnections: 1, MaxPerIP: 2, MaxPerAccount: 2}, Scope{AccountID: accountA, IP: "127.0.0.1"}, Scope{AccountID: accountB, IP: "127.0.0.2"}},
		{"IP across different accounts", Config{MaxConnections: 2, MaxPerIP: 1, MaxPerAccount: 2}, Scope{AccountID: accountA, IP: "127.0.0.1"}, Scope{AccountID: accountB, IP: "127.0.0.1"}},
		{"account across different IPs", Config{MaxConnections: 2, MaxPerIP: 2, MaxPerAccount: 1}, Scope{AccountID: accountA, IP: "127.0.0.1"}, Scope{AccountID: accountA, IP: "127.0.0.2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newAvailableHub(t, tt.config)
			first := subscribe(t, h, tt.first)
			if _, err := h.Subscribe(tt.second); !errors.Is(err, ErrLimited) {
				t.Fatalf("boundary admission = %v, want limited", err)
			}
			if len(h.subs) != 1 || len(h.ip) != 1 || len(h.accounts) != 1 {
				t.Fatalf("denied admission leaked keys: subs=%d IPs=%d accounts=%d", len(h.subs), len(h.ip), len(h.accounts))
			}
			first.Close()
			if _, err := h.Subscribe(tt.second); err != nil {
				t.Fatalf("admission after key reclamation: %v", err)
			}
		})
	}
}

func TestHubFDAdmissionIsFailClosedAtTwentyFivePercentHeadroom(t *testing.T) {
	for _, tt := range []struct {
		used, limit uint64
		want        bool
	}{{74, 100, true}, {75, 100, true}, {76, 100, false}, {2, 3, true}, {3, 3, false}, {0, 0, false}, {5, 4, false}} {
		if got := fdAdmission(tt.used, tt.limit); got != tt.want {
			t.Errorf("fdAdmission(%d, %d) = %t, want %t", tt.used, tt.limit, got, tt.want)
		}
	}
	h := newAvailableHub(t, Config{AdmitFD: func() bool { return false }})
	if _, err := h.Subscribe(Scope{AccountID: accountA, IP: "127.0.0.1"}); !errors.Is(err, ErrLimited) {
		t.Fatalf("failed FD probe admission = %v, want limited", err)
	}
	if len(h.subs) != 0 || len(h.ip) != 0 || len(h.accounts) != 0 {
		t.Fatal("failed FD admission changed hub accounting")
	}
}

func TestHubQueueOverflowEvictsAndReclaimsKeys(t *testing.T) {
	h := newAvailableHub(t, Config{QueueDepth: 1, MaxPerIP: 1, MaxPerAccount: 1})
	s := subscribe(t, h, Scope{AccountID: accountA, IP: "127.0.0.1"})
	h.Publish(Change{AccountID: accountA, ResumeID: resumeA, Revision: 1})
	h.Publish(Change{AccountID: accountA, ResumeID: resumeA, Revision: 2})
	requireClosed(t, s.Done)
	if len(h.subs) != 0 || len(h.ip) != 0 || len(h.accounts) != 0 {
		t.Fatalf("eviction retained keys: subs=%d IPs=%d accounts=%d", len(h.subs), len(h.ip), len(h.accounts))
	}
	if _, err := h.Subscribe(Scope{AccountID: accountA, IP: "127.0.0.1"}); err != nil {
		t.Fatalf("admission after overflow eviction: %v", err)
	}
}

func TestHubPublicDeleteClosesWhileOwnerReceivesMetadata(t *testing.T) {
	h := newAvailableHub(t, Config{QueueDepth: 1})
	owner := subscribe(t, h, Scope{AccountID: accountA, IP: "127.0.0.1"})
	public := subscribe(t, h, Scope{ResumeID: resumeA, IP: "127.0.0.2"})
	deleted := Change{AccountID: accountA, ResumeID: resumeA, Revision: 9, Deleted: true}
	h.Publish(deleted)
	if got := <-owner.Events; got != deleted {
		t.Fatalf("owner delete = %+v, want %+v", got, deleted)
	}
	requireClosed(t, public.Done)
	requireNoEvent(t, public.Events)
}

func TestHubConcurrentPublishCloseAndAvailabilityReclaimsAllKeys(t *testing.T) {
	h := newAvailableHub(t, Config{MaxConnections: 20, MaxPerIP: 20, MaxPerAccount: 20, QueueDepth: 8})
	const count = 20
	subs := make([]*Subscription, 0, count)
	for i := 0; i < count; i++ {
		subs = append(subs, subscribe(t, h, Scope{AccountID: accountA, IP: "127.0.0.1"}))
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		<-start
		for revision := int64(1); revision <= 64; revision++ {
			h.Publish(Change{AccountID: accountA, ResumeID: resumeA, Revision: revision})
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for _, s := range subs {
			s.Close()
		}
	}()
	go func() { defer wg.Done(); <-start; h.SetAvailable(false); h.SetAvailable(true) }()
	close(start)
	wg.Wait()
	for _, s := range subs {
		requireClosed(t, s.Done)
	}
	if len(h.subs) != 0 || len(h.ip) != 0 || len(h.accounts) != 0 {
		t.Fatalf("churn retained keys: subs=%d IPs=%d accounts=%d", len(h.subs), len(h.ip), len(h.accounts))
	}
}

func TestHubCloseIsPermanentAndIdempotent(t *testing.T) {
	h := newAvailableHub(t, Config{})
	s := subscribe(t, h, Scope{AccountID: accountA, IP: "127.0.0.1"})
	h.Close()
	h.Close()
	h.SetAvailable(true)
	s.Close()
	requireClosed(t, s.Done)
	if _, err := h.Subscribe(Scope{AccountID: accountA, IP: "127.0.0.1"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("admission after close = %v, want unavailable", err)
	}
}

func TestHubRejectsInvalidConfigAndScope(t *testing.T) {
	for _, cfg := range []Config{{MaxConnections: -1}, {MaxConnections: defaultMaxConnections + 1}, {QueueDepth: defaultQueueDepth + 1}} {
		if _, err := NewHub(cfg); err == nil {
			t.Fatalf("accepted invalid config %+v", cfg)
		}
	}
	h := newAvailableHub(t, Config{})
	for _, scope := range []Scope{{IP: "127.0.0.1"}, {AccountID: accountA, ResumeID: resumeA, IP: "127.0.0.1"}, {AccountID: accountA, IP: "127.000.0.1"}, {AccountID: accountA, IP: "2001:0db8::1"}} {
		if _, err := h.Subscribe(scope); err == nil {
			t.Fatalf("accepted invalid scope %+v", scope)
		}
	}
}
