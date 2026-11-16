package githubevidence

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // ADR 0017 approves HMAC-SHA-1 only for the RFC 6238 interoperability profile
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/secondfactor"
	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	ceremonyLapse   = 6 * time.Minute
	enrollmentLapse = 11 * time.Minute
)

func ceremonyLock() lockStep {
	return lockStep{waits: "getwebauthnceremonybytokendigestforupdate", sql: `SELECT 1 FROM webauthn_ceremonies WHERE user_id = $1 AND consumed_at IS NULL FOR UPDATE`, key: accountUserID}
}

func passkeyLock() lockStep {
	return lockStep{waits: "getwebauthncredentialforupdate", sql: `SELECT 1 FROM webauthn_credentials WHERE id = $1 FOR UPDATE`, key: func(c *callerRun) any { return c.passkeyID }}
}

func totpCredentialLock() lockStep {
	return lockStep{waits: "gettotpcredentialforupdate", sql: `SELECT 1 FROM totp_credentials WHERE user_id = $1 FOR UPDATE`, key: accountUserID}
}

func totpEnrollmentLock() lockStep {
	return lockStep{waits: "gettotpenrollmentforupdate", sql: `SELECT 1 FROM totp_enrollments WHERE user_id = $1 FOR UPDATE`, key: accountUserID}
}

func factorCases() []callerCase {
	expire := func(c *callerRun) error { return c.expireSession() }
	epoch := []string{"-- name: advanceuserauthepoch"}
	totpEnrollment := invariantSite{"internal/secondfactor/totp_enrollment.go", "Service.CompleteTOTPEnrollment"}
	totpReplacement := func(proofAge time.Duration) func(c *callerRun) error {
		return func(c *callerRun) error {
			if err := c.enableSecondFactor(proofAge); err != nil {
				return err
			}
			if err := c.installTOTP(); err != nil {
				return err
			}
			return c.startTOTPEnrollment()
		}
	}
	return []callerCase{
		{name: "recovery regeneration", site: invariantSite{"internal/secondfactor/recovery.go", "Service.RegenerateRecoveryCodes"},
			setup: func(c *callerRun) error { return c.enableSecondFactor(0) },
			run: func(c *callerRun) error {
				current, err := c.freshSession()
				if err != nil {
					return err
				}
				_, err = c.ih.factors.RegenerateRecoveryCodes(context.Background(), current)
				return err
			}, invalidate: expire, untouched: epoch, lastLocks: []lockStep{policyLock()}, lapse: advanceClock(proofLapse)},
		{name: "passkey enrollment", site: invariantSite{"internal/secondfactor/passkey_enrollment.go", "Service.CompletePasskeyRegistration"},
			setup: func(c *callerRun) error { return c.startPasskeyRegistration() },
			run: func(c *callerRun) error {
				current, err := c.freshSession()
				if err != nil {
					return err
				}
				_, err = c.ih.factors.CompletePasskeyRegistration(context.Background(), current, c.factor)
				return err
			}, invalidate: expire, untouched: epoch, lastLocks: []lockStep{ceremonyLock()}, lapse: advanceClock(ceremonyLapse)},
		{name: "passkey removal", site: invariantSite{"internal/secondfactor/passkey_removal.go", "Service.RemovePasskey"},
			setup: func(c *callerRun) error {
				if err := c.enableSecondFactor(0); err != nil {
					return err
				}
				return c.installPasskey()
			},
			run: func(c *callerRun) error {
				current, err := c.freshSession()
				if err != nil {
					return err
				}
				_, err = c.ih.factors.RemovePasskey(context.Background(), current, c.passkeyID)
				return err
			}, invalidate: expire, untouched: epoch, lastLocks: []lockStep{passkeyLock()}, lapse: advanceClock(proofLapse)},
		{name: "totp enrollment expiry", site: totpEnrollment, setup: totpReplacement(0), run: completeTOTPEnrollment,
			invalidate: expire, untouched: epoch, lastLocks: []lockStep{totpCredentialLock(), totpEnrollmentLock()}, lapse: advanceClock(enrollmentLapse)},
		{name: "totp enrollment recent proof", site: totpEnrollment, setup: totpReplacement(14 * time.Minute), run: completeTOTPEnrollment,
			invalidate: expire, untouched: epoch, lastLocks: []lockStep{totpCredentialLock(), totpEnrollmentLock()}, lapse: advanceClock(2 * time.Minute)},
		{name: "totp removal", site: invariantSite{"internal/secondfactor/totp_service.go", "Service.RemoveTOTP"},
			setup: func(c *callerRun) error {
				if err := c.enableSecondFactor(0); err != nil {
					return err
				}
				return c.installTOTP()
			},
			run: func(c *callerRun) error {
				current, err := c.freshSession()
				if err != nil {
					return err
				}
				_, err = c.ih.factors.RemoveTOTP(context.Background(), current)
				return err
			}, invalidate: expire, untouched: epoch, lastLocks: []lockStep{totpCredentialLock()}, lapse: advanceClock(proofLapse)},
	}
}

func completeTOTPEnrollment(c *callerRun) error {
	current, err := c.freshSession()
	if err != nil {
		return err
	}
	code, err := totpCodeAt(c.totpSecret, c.ih.clock.now())
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]string{"enrollmentId": c.enrollmentID, "code": code})
	if err != nil {
		return err
	}
	credential, err := c.ih.factors.DecodeTOTPEnrollmentCompletion(body)
	if err != nil {
		return err
	}
	_, err = c.ih.factors.CompleteTOTPEnrollment(context.Background(), current, credential)
	return err
}

func (c *callerRun) installTOTP() error {
	secret, err := secondfactor.GenerateTOTPSecret(rand.Reader)
	if err != nil {
		return err
	}
	id := uuid.New()
	sealed, err := c.ih.ring.Seal(secondfactor.TOTPRecordKindCredential, c.ih.user.ID, id, secret)
	if err != nil {
		return err
	}
	_, err = c.ih.q.InstallTOTPCredential(context.Background(), store.InstallTOTPCredentialParams{
		ID: id, UserID: c.ih.user.ID, KeyID: sealed.KeyID, Nonce: sealed.Nonce[:], Ciphertext: sealed.Ciphertext, CreatedAt: time.Now(),
	})
	return err
}

func (c *callerRun) startTOTPEnrollment() error {
	current, err := c.freshSession()
	if err != nil {
		return err
	}
	started, err := c.ih.factors.StartTOTPEnrollment(context.Background(), current)
	if err != nil {
		return err
	}
	c.enrollmentID, c.totpSecret = started.EnrollmentID, started.Secret
	return nil
}

func (c *callerRun) installPasskey() error {
	credentialID := make([]byte, 32)
	if _, err := rand.Read(credentialID); err != nil {
		return err
	}
	stored, err := c.ih.q.CreateWebAuthnCredential(context.Background(), store.CreateWebAuthnCredentialParams{
		UserID: c.ih.user.ID, CredentialID: credentialID, PublicKey: []byte{0xa5}, Transports: []string{"internal"}, CreatedAt: time.Now(),
	})
	c.passkeyID = stored.ID
	return err
}

func (c *callerRun) startPasskeyRegistration() error {
	current, err := c.freshSession()
	if err != nil {
		return err
	}
	options, err := c.ih.factors.StartPasskeyRegistration(context.Background(), current)
	if err != nil {
		return err
	}
	body, err := passkeyRegistrationBody(options.CeremonyID, options.PublicKey)
	if err != nil {
		return err
	}
	c.factor, err = c.ih.factors.DecodePasskeyRegistration(body)
	return err
}

func totpCodeAt(display string, at time.Time) (string, error) {
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ReplaceAll(display, " ", ""))
	if err != nil {
		return "", err
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(at.Unix()/30)) //nolint:gosec // a current Unix time is positive
	mac := hmac.New(sha1.New, secret)                            //nolint:gosec // ADR 0017 approves HMAC-SHA-1 only for the RFC 6238 interoperability profile
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[offset:offset+4])&0x7fffffff)%1000000), nil
}

func passkeyRegistrationBody(ceremonyID string, publicKey json.RawMessage) ([]byte, error) {
	var options struct {
		Challenge string `json:"challenge"`
		RP        struct {
			ID string `json:"id"`
		} `json:"rp"`
	}
	if err := json.Unmarshal(publicKey, &options); err != nil {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	point, err := key.PublicKey.Bytes()
	if err != nil {
		return nil, err
	}
	credentialID := make([]byte, 32)
	if _, err = rand.Read(credentialID); err != nil {
		return nil, err
	}
	rpHash := sha256.Sum256([]byte(options.RP.ID))
	authData := append([]byte{}, rpHash[:]...)
	authData = append(authData, 0x45, 0, 0, 0, 0)
	authData = append(authData, make([]byte, 16)...)
	authData = binary.BigEndian.AppendUint16(authData, uint16(len(credentialID)))
	authData = append(authData, credentialID...)
	authData = append(authData, 0xa5, 0x01, 0x02, 0x03, 0x26, 0x20, 0x01, 0x21, 0x58, 0x20)
	authData = append(authData, point[1:33]...)
	authData = append(authData, 0x22, 0x58, 0x20)
	authData = append(authData, point[33:65]...)
	attestation, err := webauthncbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": authData})
	if err != nil {
		return nil, err
	}
	clientData, err := json.Marshal(map[string]string{"type": "webauthn.create", "challenge": options.Challenge, "origin": testOrigin})
	if err != nil {
		return nil, err
	}
	id := base64.RawURLEncoding.EncodeToString(credentialID)
	return json.Marshal(map[string]any{
		"ceremonyId": ceremonyID,
		"credential": map[string]any{
			"id": id, "rawId": id, "type": "public-key",
			"response": map[string]any{
				"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientData),
				"attestationObject": base64.RawURLEncoding.EncodeToString(attestation),
				"transports":        []string{"internal", "hybrid"},
			},
			"clientExtensionResults": map[string]any{},
		},
	})
}
