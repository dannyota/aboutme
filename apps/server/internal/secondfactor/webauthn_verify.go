package secondfactor

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"math"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/store"
)

type verifiedPasskey struct {
	credentialID   []byte
	publicKey      []byte
	signCount      uint32
	backupEligible bool
	backupState    bool
	transports     []string
}

type passkeyUser struct {
	handle      []byte
	name        string
	displayName string
	credentials []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte { return u.handle }

func (u passkeyUser) WebAuthnName() string { return u.name }

func (u passkeyUser) WebAuthnDisplayName() string { return u.displayName }

func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

func (rp *RelyingParty) checkClientData(clientData protocol.CollectedClientData, ceremony protocol.CeremonyType, challengeDigest []byte) error {
	if clientData.Type != ceremony || clientData.Origin != rp.origin || clientData.CrossOrigin || clientData.TopOrigin != "" {
		return auth.ErrSecondFactorVerificationFailed
	}
	challenge, err := canonicalBase64(clientData.Challenge, challengeBytes, challengeBytes)
	if err != nil {
		return auth.ErrSecondFactorVerificationFailed
	}
	sum := sha256.Sum256(challenge)
	if subtle.ConstantTimeCompare(sum[:], challengeDigest) != 1 {
		return auth.ErrSecondFactorVerificationFailed
	}
	return nil
}

func (rp *RelyingParty) verifyRegistration(reg *passkeyRegistration, challengeDigest, handle []byte, name, displayName string) (verifiedPasskey, error) {
	parsed, err := reg.response.Parse()
	if err != nil {
		return verifiedPasskey{}, auth.ErrSecondFactorVerificationFailed
	}
	clientData := parsed.Response.CollectedClientData
	if err = rp.checkClientData(clientData, protocol.CreateCeremony, challengeDigest); err != nil {
		return verifiedPasskey{}, err
	}
	session := webauthn.SessionData{
		Challenge: clientData.Challenge, RelyingPartyID: rp.id, UserID: handle,
		UserVerification: protocol.VerificationRequired, CredParams: credentialParameters,
	}
	user := passkeyUser{handle: handle, name: name, displayName: displayName}
	credential, err := rp.lib.CreateCredential(user, session, parsed)
	if err != nil {
		return verifiedPasskey{}, auth.ErrSecondFactorVerificationFailed
	}
	if len(credential.ID) < minCredentialIDBytes || len(credential.ID) > maxCredentialIDBytes ||
		len(credential.PublicKey) == 0 || len(credential.PublicKey) > maxPublicKeyBytes {
		return verifiedPasskey{}, auth.ErrSecondFactorVerificationFailed
	}
	return verifiedPasskey{
		credentialID: credential.ID, publicKey: credential.PublicKey, signCount: credential.Authenticator.SignCount,
		backupEligible: credential.Flags.BackupEligible, backupState: credential.Flags.BackupState,
		transports: reg.transports,
	}, nil
}

type assertionResult struct {
	received         uint32
	counterRegressed bool
	backupState      bool
}

func (rp *RelyingParty) verifyAssertion(a *passkeyAssertion, challengeDigest, handle []byte, active []store.WebauthnCredential, target store.WebauthnCredential) (assertionResult, error) {
	parsed, err := a.response.Parse()
	if err != nil {
		return assertionResult{}, auth.ErrSecondFactorVerificationFailed
	}
	clientData := parsed.Response.CollectedClientData
	if err = rp.checkClientData(clientData, protocol.AssertCeremony, challengeDigest); err != nil {
		return assertionResult{}, err
	}
	user := passkeyUser{handle: handle}
	allowed := make([][]byte, 0, len(active))
	for _, stored := range active {
		allowed = append(allowed, stored.CredentialID)
		user.credentials = append(user.credentials, webauthn.Credential{
			ID: stored.CredentialID, PublicKey: stored.PublicKey,
			Flags: webauthn.CredentialFlags{
				UserPresent: true, UserVerified: true,
				BackupEligible: stored.BackupEligible, BackupState: stored.BackupState,
			},
			Authenticator: webauthn.Authenticator{SignCount: storedCounter(stored)},
		})
	}
	session := webauthn.SessionData{
		Challenge: clientData.Challenge, RelyingPartyID: rp.id, UserID: handle,
		AllowedCredentialIDs: allowed, UserVerification: protocol.VerificationRequired,
	}
	credential, err := rp.lib.ValidateLogin(user, session, parsed)
	if err != nil || !bytes.Equal(credential.ID, target.CredentialID) {
		return assertionResult{}, auth.ErrSecondFactorVerificationFailed
	}
	received := parsed.Response.AuthenticatorData.Counter
	stored := storedCounter(target)
	return assertionResult{
		received:         received,
		counterRegressed: (received != 0 || stored != 0) && received <= stored,
		backupState:      parsed.Response.AuthenticatorData.Flags.HasBackupState(),
	}, nil
}

func storedCounter(credential store.WebauthnCredential) uint32 {
	if credential.SignCount < 0 || credential.SignCount > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(credential.SignCount) //nolint:gosec // bounded to the uint32 range by the check above.
}
