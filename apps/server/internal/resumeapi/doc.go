// Package resumeapi implements the authenticated resume HTTP boundary.
//
// A csrf_rejected retry must reuse the same Idempotency-Key. CSRF failures
// occur before idempotency inspection, so the corrected retry remains the one
// logical mutation named by the key.
package resumeapi
