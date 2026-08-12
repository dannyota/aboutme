package sanitizetest

import "testing"

func TestAssertNeutralizedNegativeControls(t *testing.T) {
	t.Parallel()

	unsafe := []string{
		"<script>alert(1)</script>",
		`<p onclick="alert(1)">x</p>`,
		`<a href="javascript:alert(1)" rel="noopener noreferrer">x</a>`,
		`<a href="https://example.com">x</a>`,
		`<a href="https://example.com" rel="opener">x</a>`,
		`<a href="https://example.com" rel="noopener noreferrer" target="other">x</a>`,
	}
	for _, fragment := range unsafe {
		fragment := fragment
		t.Run(fragment, func(t *testing.T) {
			t.Parallel()
			if err := AssertNeutralized(fragment); err == nil {
				t.Fatalf("AssertNeutralized(%q) accepted a violation", fragment)
			}
		})
	}
}

func TestAssertNeutralizedAcceptsDangerousLookingText(t *testing.T) {
	t.Parallel()
	if err := AssertNeutralized("javascript:alert(1)"); err != nil {
		t.Fatalf("plain text: %v", err)
	}
}
