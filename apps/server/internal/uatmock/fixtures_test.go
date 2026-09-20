package uatmock

import "testing"

func TestGoogleAccountsKeepExistingFixturesAndAddPasswordLink(t *testing.T) {
	t.Parallel()

	wantExisting := []account{
		{Subject: "uat-google-001", Email: "developer@example.invalid", Name: "Development User"},
		{Subject: "uat-google-002", Email: "alice@example.invalid", Name: "Alice Local"},
		{Subject: "uat-google-003", Email: "bob@example.invalid", Name: "Bob Local"},
		{Subject: "uat-google-004", Email: "pa-provider-only@example.invalid", Name: "Provider Only"},
	}
	if len(googleAccounts) < len(wantExisting) {
		t.Fatalf("google accounts = %d, want at least %d", len(googleAccounts), len(wantExisting))
	}
	for i, want := range wantExisting {
		if got := googleAccounts[i]; got != want {
			t.Errorf("google account %d = %#v, want %#v", i, got, want)
		}
	}

	got, ok := accountBySubject("uat-google-005")
	if !ok {
		t.Fatal("password link account was not found")
	}
	want := account{Subject: "uat-google-005", Email: "pa-link@example.invalid", Name: "Password Link"}
	if got != want {
		t.Errorf("password link account = %#v, want %#v", got, want)
	}

	got, ok = accountBySubject("uat-google-006")
	if !ok {
		t.Fatal("MCP proof account was not found")
	}
	want = account{Subject: "uat-google-006", Email: "mcp-proof@example.invalid", Name: "MCP Proof"}
	if got != want {
		t.Errorf("MCP proof account = %#v, want %#v", got, want)
	}
}
