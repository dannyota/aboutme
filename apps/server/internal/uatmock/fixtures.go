package uatmock

// account is one deterministic local Google identity the mock can issue. The
// mock exposes several so the native password proof can link a provider whose
// verified email differs from an account's canonical email and run a
// provider-only sign-in against a distinct subject.
type account struct {
	Subject string
	Email   string
	Name    string
}

// googleAccounts are the local identities, oldest first. The first is the
// preselected account in the authorize form so the existing Google proof
// keeps its default selection.
var googleAccounts = []account{
	{Subject: "uat-google-001", Email: "developer@example.invalid", Name: "Development User"},
	{Subject: "uat-google-002", Email: "alice@example.invalid", Name: "Alice Local"},
	{Subject: "uat-google-003", Email: "bob@example.invalid", Name: "Bob Local"},
	{Subject: "uat-google-004", Email: "pa-provider-only@example.invalid", Name: "Provider Only"},
}

// googleSubject is the first (default) account subject, retained for the
// existing protocol tests.
const googleSubject = "uat-google-001"

// accountBySubject resolves a form-selected subject to its account.
func accountBySubject(subject string) (account, bool) {
	for _, acct := range googleAccounts {
		if acct.Subject == subject {
			return acct, true
		}
	}
	return account{}, false
}
