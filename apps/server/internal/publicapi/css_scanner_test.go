package publicapi

import "testing"

// TestCSSWithoutComments_IsStringAware proves the shared CSS scanner removes
// only comments outside strings, honors escapes inside strings, and reports
// an unterminated string or comment.
func TestCSSWithoutComments_IsStringAware(t *testing.T) {
	for _, test := range []struct {
		name, css, want string
		ok              bool
	}{
		{"comment between rules", `a{color:red}/* x */b{}`, `a{color:red} b{}`, true},
		{"comment between tokens", `a{back/**/ground:red}`, `a{back ground:red}`, true},
		{"markers in a double-quoted string", `a{content:"/*x*/"}`, `a{content:"/*x*/"}`, true},
		{"markers in a single-quoted string", `a{content:'/*x*/'}`, `a{content:'/*x*/'}`, true},
		{"escaped quote keeps the string open", `a{content:"\"/*x*/"}`, `a{content:"\"/*x*/"}`, true},
		{"escaped backslash closes the string", `a{content:"\\"/*x*/}`, `a{content:"\\" }`, true},
		{"other quote inside a string", `a{content:"'/*"}`, `a{content:"'/*"}`, true},
		{"escaped newline continues the string", "a{content:\"x\\\ny\"}", "a{content:\"x\\\ny\"}", true},
		{"unterminated string", `a{content:"x}`, "", false},
		{"string broken by a newline", "a{content:\"x\ny\"}", "", false},
		{"escape at the end", `a{content:"x\`, "", false},
		{"unterminated comment", `a{color:red}/* open`, "", false},
		{"comment opened in a closed string's wake", `a{content:"x"/*}`, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := cssWithoutComments(test.css)
			if got != test.want || ok != test.ok {
				t.Fatalf("cssWithoutComments(%q) = %q, %t; want %q, %t", test.css, got, ok, test.want, test.ok)
			}
		})
	}
}
