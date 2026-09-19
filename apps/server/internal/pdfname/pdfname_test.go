package pdfname

import (
	"strings"
	"testing"
)

func TestDisposition(t *testing.T) {
	for _, test := range []struct {
		name, fullName, want string
	}{
		{"ascii", "Ada Lovelace",
			`attachment; filename="Ada-Lovelace-Resume.pdf"; filename*=UTF-8''Ada-Lovelace-Resume.pdf`},
		{"vietnamese", "Nguyễn Văn Đức",
			`attachment; filename="Nguyen-Van-Duc-Resume.pdf"; filename*=UTF-8''Nguy%E1%BB%85n-V%C4%83n-%C4%90%E1%BB%A9c-Resume.pdf`},
		{"decomposed input folds the same", "Nguye\u0302\u0303n",
			`attachment; filename="Nguyen-Resume.pdf"; filename*=UTF-8''Nguy%E1%BB%85n-Resume.pdf`},
		{"lowercase d stroke", "đào",
			`attachment; filename="dao-Resume.pdf"; filename*=UTF-8''%C4%91%C3%A0o-Resume.pdf`},
		{"punctuation and runs of space become one hyphen", "  O'Brien,   Mary-Jane (PhD) ",
			`attachment; filename="O-Brien-Mary-Jane-PhD-Resume.pdf"; filename*=UTF-8''O-Brien-Mary-Jane-PhD-Resume.pdf`},
		{"digits stay", "Ada 2",
			`attachment; filename="Ada-2-Resume.pdf"; filename*=UTF-8''Ada-2-Resume.pdf`},
		{"no latin letters keeps only the utf-8 name", "李小龙",
			`attachment; filename="Resume.pdf"; filename*=UTF-8''%E6%9D%8E%E5%B0%8F%E9%BE%99-Resume.pdf`},
		{"empty", "", `attachment; filename="Resume.pdf"`},
		{"blank", " \t\n", `attachment; filename="Resume.pdf"`},
		{"punctuation only", `"/\;%*`, `attachment; filename="Resume.pdf"`},
		{"quotes, slashes, and header syntax never pass through", "a\"b\\c/d;e=f%g\r\nh",
			`attachment; filename="a-b-c-d-e-f-g-h-Resume.pdf"; filename*=UTF-8''a-b-c-d-e-f-g-h-Resume.pdf`},
		{"format and bidi characters split words", "Ada\u200bLove\u202elace",
			`attachment; filename="Ada-Love-lace-Resume.pdf"; filename*=UTF-8''Ada-Love-lace-Resume.pdf`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := Disposition(test.fullName); got != test.want {
				t.Fatalf("Disposition(%q)\n got %s\nwant %s", test.fullName, got, test.want)
			}
		})
	}
}

func TestDispositionCapsTheNameAtAWordBoundary(t *testing.T) {
	name := strings.Repeat("Ab ", 60)
	got := Disposition(name)
	want := `attachment; filename="` + strings.TrimSuffix(strings.Repeat("Ab-", 21), "-") + `-Resume.pdf"`
	if !strings.HasPrefix(got, want) {
		t.Fatalf("Disposition(long) = %s, want prefix %s", got, want)
	}
	long := Disposition(strings.Repeat("Đ", 160))
	if strings.Contains(long, "filename=\"D") || !strings.Contains(long, `filename="Resume.pdf"`) {
		t.Fatalf("a single overlong word must fall back to Resume.pdf: %s", long)
	}
}
