package publicroots

import "testing"

func TestReservedAPI(t *testing.T) {
	t.Parallel()

	if !Reserved("api") {
		t.Fatal("api must be reserved")
	}
}

// wantRoutes is the v6 registry in authority order. It is written by hand so a
// regenerated generated.go that silently drops, reorders, or reclassifies a
// root fails here instead of shipping.
var wantRoutes = []Route{
	{Root: ".well-known", Dispatch: DispatchGo},
	{Root: "admin", Dispatch: DispatchReserved},
	{Root: "api", Dispatch: DispatchGo},
	{Root: "app", Dispatch: DispatchNuxt},
	{Root: "authorize", Dispatch: DispatchNuxt},
	{Root: "forgot-password", Dispatch: DispatchNuxt},
	{Root: "healthz", Dispatch: DispatchGo},
	{Root: "_nuxt", Dispatch: DispatchNuxt},
	{Root: "internal-render", Dispatch: DispatchDeny},
	{Root: "llms.txt", Dispatch: DispatchGo},
	{Root: "login", Dispatch: DispatchNuxt},
	{Root: "mcp", Dispatch: DispatchGo},
	{Root: "oauth", Dispatch: DispatchGo},
	{Root: "people", Dispatch: DispatchReserved},
	{Root: "print", Dispatch: DispatchDeny},
	{Root: "readyz", Dispatch: DispatchGo},
	{Root: "register", Dispatch: DispatchNuxt},
	{Root: "reset-password", Dispatch: DispatchNuxt},
	{Root: "robots.txt", Dispatch: DispatchGo},
	{Root: "sitemap.xml", Dispatch: DispatchGo},
	{Root: "u", Dispatch: DispatchReserved},
	{Root: "verify-email", Dispatch: DispatchNuxt},
}

func TestRoutesMatchTheV6Authority(t *testing.T) {
	t.Parallel()

	if len(Routes) != len(wantRoutes) {
		t.Fatalf("len(Routes) = %d, want %d", len(Routes), len(wantRoutes))
	}
	for index, want := range wantRoutes {
		if got := Routes[index]; got != want {
			t.Errorf("Routes[%d] = %+v, want %+v", index, got, want)
		}
	}
}

func TestReservedCoversEveryRegisteredRoot(t *testing.T) {
	t.Parallel()

	for _, route := range wantRoutes {
		if !Reserved(route.Root) {
			t.Errorf("Reserved(%q) = false, want true", route.Root)
		}
	}
	for _, root := range []string{"unregistered-root", "oauth2", "mcp-server", "well-known", "authorized"} {
		if Reserved(root) {
			t.Errorf("Reserved(%q) = true, want false", root)
		}
	}
}

// TestAgentRootsDoNotOverlap proves the four v6 roots are distinct top-level
// segments: no root is a prefix path segment of another, so `/authorize` (the
// Nuxt consent page) can never shadow `/oauth/authorize` (the Go endpoint).
func TestAgentRootsDoNotOverlap(t *testing.T) {
	t.Parallel()

	seen := make(map[string]Dispatch, len(wantRoutes))
	for _, route := range Routes {
		if previous, ok := seen[route.Root]; ok {
			t.Errorf("root %q registered twice (%q then %q)", route.Root, previous, route.Dispatch)
		}
		seen[route.Root] = route.Dispatch
	}
	for _, root := range []string{".well-known", "oauth", "mcp", "authorize"} {
		if _, ok := seen[root]; !ok {
			t.Errorf("v6 root %q is missing from the registry", root)
		}
	}
	if seen["authorize"] == seen["oauth"] {
		t.Errorf("authorize and oauth must dispatch differently; both are %q", seen["authorize"])
	}
}
