package uatmock

type account struct {
	Subject string
	Email   string
	Name    string
}

var googleAccounts = []account{
	{Subject: "uat-google-001", Email: "developer@example.invalid", Name: "Development User"},
	{Subject: "uat-google-002", Email: "alice@example.invalid", Name: "Alice Local"},
	{Subject: "uat-google-003", Email: "bob@example.invalid", Name: "Bob Local"},
	{Subject: "uat-google-004", Email: "pa-provider-only@example.invalid", Name: "Provider Only"},
	{Subject: "uat-google-005", Email: "pa-link@example.invalid", Name: "Password Link"},
	{Subject: "uat-google-006", Email: "mcp-proof@example.invalid", Name: "MCP Proof"},
}

const googleSubject = "uat-google-001"

func accountBySubject(subject string) (account, bool) {
	for _, acct := range googleAccounts {
		if acct.Subject == subject {
			return acct, true
		}
	}
	return account{}, false
}
