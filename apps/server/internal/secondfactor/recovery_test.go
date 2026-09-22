package secondfactor

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/authmail"
)

func TestRecoveryCode_FormatAndParseRoundTrip(t *testing.T) {
	values := []recoveryValue{{}, {0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}}
	values = append(values, recoveryValue{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0x0f, 0xed, 0xcb, 0xa9, 0x87, 0x65, 0x43, 0x21})
	want := []string{"amr_00000-00000-00000-00000-00000-0", "amr_7ZZZZ-ZZZZZ-ZZZZZ-ZZZZZ-ZZZZZ-Z"}
	for i, value := range values {
		code := formatRecoveryCode(value)
		if i < len(want) && code != want[i] {
			t.Fatalf("formatRecoveryCode(%x) = %q, want %q", value, code, want[i])
		}
		if len(code) != len("amr_")+recoveryCodeChars+5 {
			t.Fatalf("formatted code %q has the wrong length", code)
		}
		parsed, err := parseRecoveryCode(code)
		if err != nil || parsed != value {
			t.Fatalf("parseRecoveryCode(%q) = %x, %v; want %x", code, parsed, err, value)
		}
	}
}

func TestParseRecoveryCode_CanonicalizesAcceptedInput(t *testing.T) {
	value := recoveryValue{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0x0f, 0xed, 0xcb, 0xa9, 0x87, 0x65, 0x43, 0x21}
	code := formatRecoveryCode(value)
	compact := strings.ReplaceAll(code, "-", "")
	aliased := strings.NewReplacer("0", "o", "1", "l").Replace(strings.ToLower(compact))
	for _, input := range []string{code, strings.ToLower(code), compact, " " + strings.ReplaceAll(code, "-", " ") + " ", aliased, strings.ReplaceAll(compact, "1", "I")} {
		parsed, err := parseRecoveryCode(input)
		if err != nil || parsed != value {
			t.Errorf("parseRecoveryCode(%q) = %x, %v; want %x", input, parsed, err, value)
		}
	}
}

func TestParseRecoveryCode_RejectsEveryOtherShape(t *testing.T) {
	valid := formatRecoveryCode(recoveryValue{1, 2, 3})
	for name, input := range map[string]string{
		"empty":           "",
		"missing prefix":  strings.TrimPrefix(valid, "amr_"),
		"other prefix":    "amx_" + strings.TrimPrefix(valid, "amr_"),
		"short":           valid[:len(valid)-1],
		"long":            valid + "0",
		"letter U":        strings.Replace(valid, "amr_0", "amr_U", 1),
		"top bits set":    "amr_8" + valid[5:],
		"underscore body": strings.Replace(valid, "-", "_", 1),
		"tab separator":   strings.Replace(valid, "-", "\t", 1),
		"non-ascii":       strings.Replace(valid, "-", "–", 1),
		"oversized input": valid + strings.Repeat(" ", recoveryCodeMaxInput),
	} {
		if _, err := parseRecoveryCode(input); !errors.Is(err, auth.ErrSecondFactorRequestInvalid) {
			t.Errorf("parseRecoveryCode(%s) error = %v, want ErrSecondFactorRequestInvalid", name, err)
		}
	}
}

func TestGenerateRecoveryCodes_TenDistinctCodes(t *testing.T) {
	stream := bytes.Repeat([]byte{0x42}, recoveryCodeBytes)
	for i := byte(0); i < 12; i++ {
		stream = append(stream, bytes.Repeat([]byte{i}, recoveryCodeBytes)...)
	}
	values, display, err := generateRecoveryCodes(bytes.NewReader(stream))
	if err != nil || len(values) != recoveryCodeCount || len(display) != recoveryCodeCount {
		t.Fatalf("generateRecoveryCodes() = %d values, %d codes, %v", len(values), len(display), err)
	}
	seen := map[string]bool{}
	for i, code := range display {
		if seen[code] || formatRecoveryCode(values[i]) != code {
			t.Fatalf("code %d = %q is duplicated or does not match its value", i, code)
		}
		seen[code] = true
	}
	if _, _, err = generateRecoveryCodes(bytes.NewReader(stream[:20])); err == nil {
		t.Fatal("generateRecoveryCodes() accepted a short entropy source")
	}
}

func TestRecoveryCodeDigest_BindsAccountAndDomain(t *testing.T) {
	value := recoveryValue{9}
	first, second := uuid.New(), uuid.New()
	if bytes.Equal(recoveryCodeDigest(first, value), recoveryCodeDigest(second, value)) {
		t.Fatal("the same code has the same digest for two accounts")
	}
	if len(recoveryCodeDigest(first, value)) != 32 || bytes.Contains(recoveryCodeDigest(first, value), value[:]) {
		t.Fatal("recovery digest is not a 32-byte opaque digest")
	}
}

func (h *harness) remainingInMail(userID uuid.UUID) int {
	h.t.Helper()
	var (
		jobID      uuid.UUID
		keyID      string
		nonce      []byte
		ciphertext []byte
	)
	if err := h.pool.QueryRow(testContext(h.t), "SELECT id, key_id, nonce, ciphertext FROM auth_email_jobs WHERE user_id = $1 AND kind = $2 ORDER BY created_at DESC, id DESC LIMIT 1",
		userID, string(authmail.KindRecoveryCodeUsed)).Scan(&jobID, &keyID, &nonce, &ciphertext); err != nil {
		h.t.Fatalf("read recovery mail: %v", err)
	}
	sealed := authmail.Sealed{KeyID: keyID, Ciphertext: ciphertext}
	copy(sealed.Nonce[:], nonce)
	payload, err := h.ring.Open(jobID, authmail.KindRecoveryCodeUsed, sealed)
	if err != nil || payload.RemainingRecoveryCodes == nil {
		h.t.Fatalf("open recovery mail = %+v, %v", payload, err)
	}
	return *payload.RemainingRecoveryCodes
}

func (h *harness) recoveryCredential(code string) auth.SecondFactorCredential {
	h.t.Helper()
	credential, err := h.svc.DecodeRecoveryCode(code)
	if err != nil {
		h.t.Fatalf("DecodeRecoveryCode() error = %v", err)
	}
	return credential
}

func TestRecoveryCompletion_ConsumesOneCodeAndNotifies(t *testing.T) {
	h := newHarness(t, nil)
	acct := h.newAccount()
	reg := h.register(acct.sess, newTestAuthenticator(t), nil)
	pending := h.newPending(acct.user.ID, nil)
	issue, err := h.complete(pending.RawToken, nil, h.recoveryCredential(strings.ToLower(reg.RecoveryCodes[3])))
	if err != nil || issue == nil || issue.Session.SecondFactorVerifiedAt == nil {
		t.Fatalf("recovery completion = %v, %v", issue, err)
	}
	if h.count("SELECT count(*) FROM second_factor_recovery_codes WHERE user_id = $1", acct.user.ID) != 9 || h.remainingInMail(acct.user.ID) != 9 {
		t.Fatal("recovery completion did not delete exactly one code and report nine remaining")
	}
	if h.count("SELECT count(*) FROM second_factor_policies WHERE user_id = $1", acct.user.ID) != 1 || h.epoch(acct.user.ID) != 1 {
		t.Fatal("recovery code use changed a factor or the epoch")
	}
	again := h.newPending(acct.user.ID, nil)
	var failure *auth.PendingVerificationFailure
	if _, err = h.complete(again.RawToken, nil, h.recoveryCredential(reg.RecoveryCodes[3])); !errors.As(err, &failure) {
		t.Fatalf("reused recovery code error = %v, want a counted verification failure", err)
	}
	other := h.newAccount()
	otherReg := h.register(other.sess, newTestAuthenticator(t), nil)
	if _, err = h.complete(again.RawToken, nil, h.recoveryCredential(otherReg.RecoveryCodes[0])); !errors.As(err, &failure) {
		t.Fatalf("another account's recovery code error = %v, want a counted verification failure", err)
	}
	if h.count("SELECT count(*) FROM second_factor_recovery_codes WHERE user_id = $1", other.user.ID) != 10 {
		t.Fatal("a foreign recovery code attempt consumed the other account's code")
	}
}

// TestRecoveryRace_OneWinner submits one code from two pending logins at once.
func TestRecoveryRace_OneWinner(t *testing.T) {
	h := newHarness(t, nil)
	acct := h.newAccount()
	reg := h.register(acct.sess, newTestAuthenticator(t), nil)
	first, second := h.newPending(acct.user.ID, nil), h.newPending(acct.user.ID, nil)
	credential := h.recoveryCredential(reg.RecoveryCodes[0])
	results := runConcurrently(t,
		func() error { _, err := h.complete(first.RawToken, nil, credential); return err },
		func() error { _, err := h.complete(second.RawToken, nil, credential); return err },
	)
	wins := 0
	var failure *auth.PendingVerificationFailure
	for _, err := range results {
		switch {
		case err == nil:
			wins++
		case errors.As(err, &failure):
		default:
			t.Fatalf("losing recovery completion error = %v", err)
		}
	}
	if wins != 1 || h.count("SELECT count(*) FROM second_factor_recovery_codes WHERE user_id = $1", acct.user.ID) != 9 ||
		h.mails(acct.user.ID, authmail.KindRecoveryCodeUsed) != 1 {
		t.Fatalf("recovery race winners = %d; want one winner, one deleted code, and one notification", wins)
	}
}

func TestRegenerateRecoveryCodes_ReplacesEverySetAndRotates(t *testing.T) {
	h := newHarness(t, nil)
	acct := h.newAccount()
	reg := h.register(acct.sess, newTestAuthenticator(t), nil)
	sess := reg.Session.Session
	regenerated, err := h.svc.RegenerateRecoveryCodes(testContext(t), sess)
	if err != nil {
		t.Fatalf("RegenerateRecoveryCodes() error = %v", err)
	}
	if len(regenerated.Codes) != recoveryCodeCount || regenerated.Codes[0] == reg.RecoveryCodes[0] {
		t.Fatalf("regenerated codes = %v", regenerated.Codes)
	}
	next := regenerated.Session.Session
	if next.AuthEpoch != 2 || next.SecondFactorVerifiedAt == nil || !next.SecondFactorVerifiedAt.Equal(*sess.SecondFactorVerifiedAt) {
		t.Fatalf("regeneration session = %+v; want epoch 2 and the prior factor proof", next)
	}
	if h.count("SELECT count(*) FROM sessions WHERE id = $1 AND revoked_at IS NOT NULL", sess.ID) != 1 ||
		h.mails(acct.user.ID, authmail.KindRecoveryCodesRegenerated) != 1 {
		t.Fatal("regeneration did not revoke the prior session or notify")
	}
	pending := h.newPending(acct.user.ID, nil)
	var failure *auth.PendingVerificationFailure
	if _, err = h.complete(pending.RawToken, nil, h.recoveryCredential(reg.RecoveryCodes[0])); !errors.As(err, &failure) {
		t.Fatalf("replaced code error = %v, want a counted verification failure", err)
	}
	if _, err = h.complete(pending.RawToken, nil, h.recoveryCredential(regenerated.Codes[0])); err != nil {
		t.Fatalf("new code error = %v", err)
	}
}

func TestRegenerateRecoveryCodes_UnenrolledIsNotFound(t *testing.T) {
	h := newHarness(t, nil)
	acct := h.newAccount()
	if _, err := h.svc.RegenerateRecoveryCodes(testContext(t), acct.sess); !errors.Is(err, auth.ErrSecondFactorNotFound) {
		t.Fatalf("RegenerateRecoveryCodes(unenrolled) error = %v, want ErrSecondFactorNotFound", err)
	}
	if h.epoch(acct.user.ID) != 0 {
		t.Fatal("rejected regeneration advanced the epoch")
	}
}
