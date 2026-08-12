//go:build dependency_pins

// Package dependencies retains modules pinned for the next Phase 2B consumers.
// Go mod tidy evaluates this build tag, while normal builds omit the imports
// until the photo and language paths use them directly.
package dependencies

import (
	_ "golang.org/x/image/draw"
	_ "golang.org/x/text/language"
)
