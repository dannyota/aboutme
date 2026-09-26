package ecs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"

	"github.com/dannyota/aboutme/deploy/observer/internal/platform"
)

// fixture reads a recorded-shape awsJson1_1 response body from testdata.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "ecs", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// listTasksBody is the subset of a ListTasksInput this test server reads to
// pick the right fixture, without importing the platform's own request
// type.
type listTasksBody struct {
	ServiceName string `json:"serviceName"`
}

// route names the fixture or HTTP status this test server returns for one
// call. A zero status means 200 with body.
type route struct {
	body   []byte
	status int
}

// newServer starts a test server standing in for the ECS control plane. It
// dispatches ListTasks by the request's serviceName and DescribeTasks
// unconditionally, since a run makes exactly one DescribeTasks call.
func newServer(t *testing.T, listByService map[string]route, describe route) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			_ = r.Body.Close() //nolint:errcheck // best-effort close once the request is decoded
		}()
		target := r.Header.Get("X-Amz-Target")
		var rt route
		switch {
		case strings.HasSuffix(target, ".ListTasks"):
			var in listTasksBody
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Fatalf("decode ListTasks request: %v", err)
			}
			var ok bool
			rt, ok = listByService[in.ServiceName]
			if !ok {
				t.Fatalf("unexpected ListTasks for service %q", in.ServiceName)
			}
		case strings.HasSuffix(target, ".DescribeTasks"):
			rt = describe
		default:
			t.Fatalf("unexpected X-Amz-Target %q", target)
		}
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		if rt.status != 0 {
			w.WriteHeader(rt.status)
		}
		if _, err := w.Write(rt.body); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newLister(t *testing.T, srv *httptest.Server) *Lister {
	t.Helper()
	cfg := aws.Config{
		Region:      "ap-southeast-1",
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
	}
	client := awsecs.NewFromConfig(cfg, func(o *awsecs.Options) {
		o.BaseEndpoint = aws.String(srv.URL)
	})
	return New(client, Config{
		Cluster:            "aboutme-prod",
		AppService:         "aboutme-prod-app",
		WebService:         "aboutme-prod-web",
		MaintenanceService: "aboutme-prod-maintenance",
	})
}

// TestRunningMapsCountsAndReplicas covers the component mapping table, an
// ignored container, a PENDING task, a STOPPED container, and two tasks with
// the same digest counted as replicas 2 with the earliest start
// (docs/design/deployment-transparency/document.md, "Component mapping").
func TestRunningMapsCountsAndReplicas(t *testing.T) {
	srv := newServer(t, map[string]route{
		"aboutme-prod-app":         {body: fixture(t, "list_tasks_app.json")},
		"aboutme-prod-web":         {body: fixture(t, "list_tasks_web.json")},
		"aboutme-prod-maintenance": {body: fixture(t, "list_tasks_maintenance.json")},
	}, route{body: fixture(t, "describe_tasks.json")})
	l := newLister(t, srv)

	got, err := l.Running(context.Background())
	if err != nil {
		t.Fatalf("Running: %v", err)
	}

	want := []platform.Image{
		{
			Component:    platform.Caddy,
			Digest:       "sha256:b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2",
			Replicas:     1,
			RunningSince: time.Unix(1750000000, 0).UTC(),
		},
		{
			Component:    platform.Maintenance,
			Digest:       "sha256:d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4",
			Replicas:     1,
			RunningSince: time.Unix(1750000300, 0).UTC(),
		},
		{
			Component:    platform.Server,
			Digest:       "sha256:a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1",
			Replicas:     2,
			RunningSince: time.Unix(1750000000, 0).UTC(),
		},
		{
			Component:    platform.Web,
			Digest:       "sha256:c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3",
			Replicas:     1,
			RunningSince: time.Unix(1750000200, 0).UTC(),
		},
	}
	if len(got) != len(want) {
		t.Fatalf("Running: got %d images, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("image %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestRunningFailsOnMissingDigest asserts a RUNNING container without an
// image digest fails the whole run rather than being dropped
// (docs/design/deployment-transparency/document.md, "Sanitizer").
func TestRunningFailsOnMissingDigest(t *testing.T) {
	srv := newServer(t, map[string]route{
		"aboutme-prod-app":         {body: fixture(t, "list_tasks_app_single.json")},
		"aboutme-prod-web":         {body: fixture(t, "list_tasks_empty.json")},
		"aboutme-prod-maintenance": {body: fixture(t, "list_tasks_empty.json")},
	}, route{body: fixture(t, "describe_tasks_missing_digest.json")})
	l := newLister(t, srv)

	if _, err := l.Running(context.Background()); err == nil {
		t.Fatal("Running: got nil error for a RUNNING container with no image digest")
	}
}

// TestRunningFailsOnListTasksError asserts a platform read failure fails the
// whole run and writes nothing (docs/design/deployment-transparency/README.md,
// "Run, freshness, and staleness").
func TestRunningFailsOnListTasksError(t *testing.T) {
	errBody := []byte(`{"__type":"com.amazonaws.ecs#ServerException","message":"internal error"}`)
	srv := newServer(t, map[string]route{
		"aboutme-prod-app":         {body: errBody, status: http.StatusInternalServerError},
		"aboutme-prod-web":         {body: fixture(t, "list_tasks_empty.json")},
		"aboutme-prod-maintenance": {body: fixture(t, "list_tasks_empty.json")},
	}, route{body: fixture(t, "describe_tasks.json")})
	l := newLister(t, srv)

	if _, err := l.Running(context.Background()); err == nil {
		t.Fatal("Running: got nil error for a failed ListTasks call")
	}
}

// TestRunningFailsOnDescribeFailures: a task ListTasks returned but
// DescribeTasks could not describe fails the read instead of dropping out
// of the document.
func TestRunningFailsOnDescribeFailures(t *testing.T) {
	var describe map[string]any
	if err := json.Unmarshal(fixture(t, "describe_tasks.json"), &describe); err != nil {
		t.Fatal(err)
	}
	describe["failures"] = []any{map[string]any{"arn": "arn:aws:ecs:ap-southeast-1:111122223333:task/aboutme-prod/0", "reason": "MISSING"}}
	body, err := json.Marshal(describe)
	if err != nil {
		t.Fatal(err)
	}
	srv := newServer(t, map[string]route{
		"aboutme-prod-app":         {body: fixture(t, "list_tasks_app.json")},
		"aboutme-prod-web":         {body: fixture(t, "list_tasks_web.json")},
		"aboutme-prod-maintenance": {body: fixture(t, "list_tasks_maintenance.json")},
	}, route{body: body})
	if _, err := newLister(t, srv).Running(context.Background()); err == nil {
		t.Fatal("Running succeeded with a task DescribeTasks could not describe")
	}
}

// TestMapComponent is the pure form of the component mapping table.
func TestMapComponent(t *testing.T) {
	cases := []struct {
		role      role
		container string
		want      string
		wantOK    bool
	}{
		{roleApp, "server", platform.Server, true},
		{roleApp, "caddy", platform.Caddy, true},
		{roleApp, "sidecar", "", false},
		{roleWeb, "web", platform.Web, true},
		{roleWeb, "caddy", "", false},
		{roleMaintenance, "caddy", platform.Maintenance, true},
		{roleMaintenance, "server", "", false},
	}
	for _, c := range cases {
		got, ok := mapComponent(c.role, c.container)
		if got != c.want || ok != c.wantOK {
			t.Errorf("mapComponent(%s, %q) = (%q, %v), want (%q, %v)", c.role, c.container, got, ok, c.want, c.wantOK)
		}
	}
}
