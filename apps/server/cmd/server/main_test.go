package main

import (
	"errors"
	"testing"
)

func TestHostOperationCommandDispatch(t *testing.T) {
	for _, test := range []struct {
		args  []string
		usage string
		code  int
	}{
		{[]string{"account-delete", "status", "invalid"}, accountDeleteUsage, 2},
		{[]string{"media-deletion-status", "extra"}, "usage: server media-deletion-status", 2},
		{[]string{"totp-key-reencrypt", "extra"}, "usage: server totp-key-reencrypt", 1},
	} {
		err := dispatch(test.args)
		if err == nil || err.Error() != test.usage {
			t.Fatalf("dispatch(%q) = %v", test.args, err)
		}
		var exit accountDeleteExitError
		code := 1
		if errors.As(err, &exit) {
			code = exit.code
		}
		if code != test.code {
			t.Fatalf("dispatch(%q) exit = %d", test.args, code)
		}
	}
	t.Setenv("DATABASE_URL", "")
	err := dispatch([]string{"account-delete", "status", "018f5b6a-9a3e-7c21-8b1e-000000000030"})
	if err == nil || err.Error() != "account-delete status failed" {
		t.Fatalf("valid status dispatch = %v", err)
	}
}
