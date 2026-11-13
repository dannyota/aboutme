package project

import (
	"errors"
	"testing"
)

func TestPrepare_NameIsBlankAsTheRendererTrims(t *testing.T) {
	for name, test := range map[string]struct {
		value string
		err   error
	}{
		"byte order mark":          {string(rune(0xfeff)), ErrInvalid},
		"no-break space":           {" ", ErrInvalid},
		"ideographic space":        {"　", ErrInvalid},
		"line separator":           {" ", ErrInvalid},
		"next line":                {string(rune(0x85)), nil},
		"next line around a space": {string(rune(0x85)) + " ", nil},
		"text":                     {"Dự án", nil},
	} {
		document := Document{DefaultLanguage: "vi", Text: map[string]Text{"vi": {Name: test.value}}}
		if _, err := Prepare(document); !errors.Is(err, test.err) {
			t.Errorf("%s: Prepare() error = %v, want %v", name, err, test.err)
		}
	}
}
