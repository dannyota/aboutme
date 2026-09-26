package publicroots

import "testing"

func TestReservedAPI(t *testing.T) {
	t.Parallel()

	if !Reserved("api") {
		t.Fatal("api must be reserved")
	}
}

// wantRoutes is the v9 registry in authority order. It is written by hand so a
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
	{Root: "privacy", Dispatch: DispatchNuxt},
	{Root: "readyz", Dispatch: DispatchGo},
	{Root: "register", Dispatch: DispatchNuxt},
	{Root: "reset-password", Dispatch: DispatchNuxt},
	{Root: "robots.txt", Dispatch: DispatchGo},
	{Root: "sitemap.xml", Dispatch: DispatchGo},
	{Root: "templates", Dispatch: DispatchNuxt},
	{Root: "terms", Dispatch: DispatchNuxt},
	{Root: "u", Dispatch: DispatchReserved},
	{Root: "verify", Dispatch: DispatchNuxt},
	{Root: "verify-email", Dispatch: DispatchNuxt},
}

func TestRoutesMatchTheV9Authority(t *testing.T) {
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

// TestAgentRootsDoNotOverlap proves the four agent-access roots are distinct top-level
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
			t.Errorf("agent-access root %q is missing from the registry", root)
		}
	}
	if seen["authorize"] == seen["oauth"] {
		t.Errorf("authorize and oauth must dispatch differently; both are %q", seen["authorize"])
	}
}

// TestLegalPageRootsAreNuxtAndUnclaimable proves the Nuxt privacy and terms
// pages own their roots, so neither can be claimed as a resume slug.
func TestLegalPageRootsAreNuxtAndUnclaimable(t *testing.T) {
	t.Parallel()

	for _, root := range []string{"privacy", "terms"} {
		found := false
		for _, route := range Routes {
			if route.Root == root {
				found = true
				if route.Dispatch != DispatchNuxt {
					t.Errorf("%q dispatches to %q, want %q", root, route.Dispatch, DispatchNuxt)
				}
			}
		}
		if !found {
			t.Errorf("%q is missing from the registry", root)
		}
		if ValidSlug(root) {
			t.Errorf("ValidSlug(%q) = true, want false for a reserved root", root)
		}
	}
	for _, slug := range []string{"privacy-policy", "my-terms"} {
		if !ValidSlug(slug) {
			t.Errorf("ValidSlug(%q) = false, want true: only the exact root is reserved", slug)
		}
	}
}

// TestTemplatesRootIsNuxtAndUnclaimable proves the Nuxt template gallery owns
// its root, so "templates" cannot be claimed as a resume slug.
func TestTemplatesRootIsNuxtAndUnclaimable(t *testing.T) {
	t.Parallel()

	found := false
	for _, route := range Routes {
		if route.Root == "templates" {
			found = true
			if route.Dispatch != DispatchNuxt {
				t.Errorf("templates dispatches to %q, want %q", route.Dispatch, DispatchNuxt)
			}
		}
	}
	if !found {
		t.Error("templates is missing from the registry")
	}
	if ValidSlug("templates") {
		t.Error(`ValidSlug("templates") = true, want false for a reserved root`)
	}
	for _, slug := range []string{"templates-by-ada", "my-templates"} {
		if !ValidSlug(slug) {
			t.Errorf("ValidSlug(%q) = false, want true: only the exact root is reserved", slug)
		}
	}
}

// TestVerifyRootIsNuxtAndUnclaimable proves the Nuxt deployment verify page
// owns its root, so "verify" cannot be claimed as a resume slug, while the
// separate verify-email root and longer slugs stay distinct.
func TestVerifyRootIsNuxtAndUnclaimable(t *testing.T) {
	t.Parallel()

	found := false
	for _, route := range Routes {
		if route.Root == "verify" {
			found = true
			if route.Dispatch != DispatchNuxt {
				t.Errorf("verify dispatches to %q, want %q", route.Dispatch, DispatchNuxt)
			}
		}
	}
	if !found {
		t.Error("verify is missing from the registry")
	}
	if ValidSlug("verify") {
		t.Error(`ValidSlug("verify") = true, want false for a reserved root`)
	}
	for _, slug := range []string{"verify-me", "my-verify", "verifyer"} {
		if !ValidSlug(slug) {
			t.Errorf("ValidSlug(%q) = false, want true: only the exact root is reserved", slug)
		}
	}
}
