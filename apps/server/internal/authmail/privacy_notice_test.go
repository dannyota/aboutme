package authmail

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const noticeFixtureJSON = `{"version":3,"to":"fixture@example.com","userId":"0e57079a-6cab-46c2-b28c-bca122af216d","noticeId":"crowdsec-postgres-paths-v1","generation":0}`

func TestPrivacyNoticePayloadRoundTrip(t *testing.T) {
	payload, err := decodePayloadStrict([]byte(noticeFixtureJSON))
	if err != nil {
		t.Fatal("notice payload rejected")
	}
	if validationErr := validatePayload(Kind("privacy_notice"), payload); validationErr != nil {
		t.Fatal("notice payload validation failed")
	}
	encoded, err := marshalPayload(payload)
	if err != nil || string(encoded) != noticeFixtureJSON {
		t.Fatal("notice payload fields changed")
	}
}

func TestPrivacyNoticePayloadRejectsUnclosedFields(t *testing.T) {
	for name, input := range map[string]string{
		"duplicate":             strings.Replace(noticeFixtureJSON, `"generation":0`, `"generation":0,"generation":1`, 1),
		"missing generation":    strings.Replace(noticeFixtureJSON, `,"generation":0`, ``, 1),
		"null generation":       strings.Replace(noticeFixtureJSON, `"generation":0`, `"generation":null`, 1),
		"negative generation":   strings.Replace(noticeFixtureJSON, `"generation":0`, `"generation":-1`, 1),
		"fractional generation": strings.Replace(noticeFixtureJSON, `"generation":0`, `"generation":0.5`, 1),
		"unknown field":         strings.Replace(noticeFixtureJSON, `"generation":0`, `"generation":0,"body":""`, 1),
		"link":                  strings.Replace(noticeFixtureJSON, `"generation":0`, `"generation":0,"link":""`, 1),
		"event":                 strings.Replace(noticeFixtureJSON, `"generation":0`, `"generation":0,"occurredAt":""`, 1),
		"wrong notice":          strings.Replace(noticeFixtureJSON, `crowdsec-postgres-paths-v1`, `another-notice`, 1),
		"invalid user":          strings.Replace(noticeFixtureJSON, `0e57079a-6cab-46c2-b28c-bca122af216d`, `invalid`, 1),
		"nil user":              strings.Replace(noticeFixtureJSON, `0e57079a-6cab-46c2-b28c-bca122af216d`, uuid.Nil.String(), 1),
		"noncanonical email":    strings.Replace(noticeFixtureJSON, `fixture@example.com`, `Fixture@EXAMPLE.COM`, 1),
		"wrong version":         strings.Replace(noticeFixtureJSON, `"version":3`, `"version":2`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			payload, err := decodePayloadStrict([]byte(input))
			if err == nil {
				err = validatePayload(Kind("privacy_notice"), payload)
			}
			if err == nil {
				t.Fatal("invalid notice payload accepted")
			}
		})
	}
}

func TestPrivacyNoticeFieldsRejectedByExistingKinds(t *testing.T) {
	for _, kind := range []Kind{KindVerify, KindReset, KindPasswordChanged, KindTOTPAdded} {
		payload, err := decodePayloadStrict([]byte(noticeFixtureJSON))
		if err == nil {
			err = validatePayload(kind, payload)
		}
		if err == nil {
			t.Fatal("notice fields accepted by another kind")
		}
	}
}

func TestPrivacyNoticeRequiresUserScopeWithoutToken(t *testing.T) {
	user := uuid.MustParse("0e57079a-6cab-46c2-b28c-bca122af216d")
	request := EnqueueRequest{JobID: uuid.New(), Kind: Kind("privacy_notice"), UserID: &user}
	if err := validateRequest(request); err != nil {
		t.Fatal("notice user scope rejected")
	}
	request.TokenDigest = new([32]byte)
	if validateRequest(request) == nil {
		t.Fatal("notice token digest accepted")
	}
	request.TokenDigest = nil
	request.UserID = nil
	if validateRequest(request) == nil {
		t.Fatal("notice without user accepted")
	}
}

func TestPrivacyNoticeLifetimeStartsAtEnqueue(t *testing.T) {
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	if err := validateExpiry(Kind("privacy_notice"), Payload{}, now, now.Add(24*time.Hour)); err != nil {
		t.Fatal("24-hour notice lifetime rejected")
	}
	if validateExpiry(Kind("privacy_notice"), Payload{}, now, now.Add(24*time.Hour+time.Second)) == nil {
		t.Fatal("extended notice lifetime accepted")
	}
	if validateExpiry(Kind("privacy_notice"), Payload{}, now, now.Add(23*time.Hour)) == nil {
		t.Fatal("notice lifetime did not use enqueue plus 24 hours")
	}
}
