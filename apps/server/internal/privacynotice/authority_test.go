package privacynotice

import (
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func authorizeNoticeFixture(t *testing.T, s *Service) {
	t.Helper()
	campaign, err := store.New(s.pool).GetPrivacyNoticeCampaign(t.Context())
	if err != nil || campaign.SealedAt == nil {
		t.Fatal("authority fixture campaign unavailable")
	}
	epoch := uuid.NewString()
	data, marshalErr := json.Marshal(map[string]any{"schema_version": 1, "campaign_id": authmail.PrivacyNoticeID, "content_sha256": hex.EncodeToString(contentDigest()), "epoch": epoch, "cutoff_at": formatTime(campaign.CutoffAt), "expected_count": campaign.ExpectedCount, "issued_at": formatTime(*campaign.SealedAt)})
	if marshalErr != nil {
		t.Fatal("authority fixture encoding failed")
	}
	s.authorityFS = fstest.MapFS{".": {Mode: fs.ModeDir | 0755}, "epoch": {Data: []byte(epoch + "\n"), Mode: 0444}, "send.json": {Data: data, Mode: 0444}}
}

func revokeNoticeFixture(t *testing.T, s *Service) {
	t.Helper()
	files, ok := s.authorityFS.(fstest.MapFS)
	if !ok {
		t.Fatal("authority fixture filesystem invalid")
	}
	delete(files, "send.json")
	delete(files, "activation.json")
}

func TestReceiptStrictWire(t *testing.T) {
	const digest = "bb128b39501f09db59915f3f75759572a49349ec78fe17ad48b6ba40d500ffdc"
	input := `{"schema_version":1,"campaign_id":"crowdsec-postgres-paths-v1","content_sha256":"` + digest + `","epoch":"79446cce-0778-4509-9bd1-b4c13ef728bb","cutoff_at":"2026-10-19T00:00:00.000000Z","expected_count":0,"issued_at":"2026-10-19T00:00:00.000001Z"}`
	if _, err := parseReceipt([]byte(input), false); err != nil {
		t.Fatal("valid receipt rejected")
	}
	for name, bad := range map[string]string{
		"duplicate":         strings.Replace(input, `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1),
		"escaped duplicate": strings.Replace(input, `"schema_version":1`, `"schema_version":1,"schema_vers\u0069on":1`, 1),
		"unknown":           strings.Replace(input, `"schema_version":1`, `"schema_version":1,"body":""`, 1),
		"negative":          strings.Replace(input, `"expected_count":0`, `"expected_count":-1`, 1),
		"fraction":          strings.Replace(input, `"expected_count":0`, `"expected_count":0.0`, 1),
		"leading zero":      strings.Replace(input, `"expected_count":0`, `"expected_count":00`, 1),
		"wrong type":        strings.Replace(input, `"expected_count":0`, `"expected_count":"0"`, 1),
		"exponent":          strings.Replace(input, `"expected_count":0`, `"expected_count":1e0`, 1),
		"overflow":          strings.Replace(input, `"expected_count":0`, `"expected_count":9223372036854775808`, 1),
		"null":              strings.Replace(input, `"expected_count":0`, `"expected_count":null`, 1),
		"precision":         strings.Replace(input, ".000000Z", "Z", 1),
		"offset":            strings.Replace(input, ".000000Z", ".000000+00:00", 1),
		"leap second":       strings.Replace(input, "00:00:00.000000Z", "00:00:60.000000Z", 1),
		"zero year":         strings.Replace(input, "2026-10-19", "0000-10-19", 1),
		"bom":               "\ufeff" + input,
		"oversize":          input + strings.Repeat(" ", 4096),
		"unicode":           strings.Replace(input, "crowdsec-postgres-paths-v1", `crowdsec-postgres-paths-v1\ud800`, 1),
		"epoch version":     strings.Replace(input, "4509", "7509", 1),
		"upper digest":      strings.Replace(input, digest, strings.ToUpper(digest), 1),
		"trailing":          input + `{}`,
		"utf8":              input + string([]byte{255}),
	} {
		t.Run(name, func(t *testing.T) {
			if bad == input {
				t.Fatal("invalid receipt fixture matches valid input")
			}
			if _, err := parseReceipt([]byte(bad), false); err == nil {
				t.Fatal("invalid receipt accepted")
			}
		})
	}
	maximum := strings.Replace(input, `"expected_count":0`, `"expected_count":9223372036854775807`, 1)
	if _, err := parseReceipt([]byte(maximum), false); err != nil {
		t.Fatal("maximum exact receipt count rejected")
	}
	activation := strings.Replace(maximum, `"issued_at":"2026-10-19T00:00:00.000001Z"`, `"accepted_count":9223372036854775807,"deleted_count":0,"activated_at":"2026-10-19T00:00:00.000001Z"`, 1)
	if _, err := parseReceipt([]byte(activation), true); err != nil {
		t.Fatal("maximum activation count rejected")
	}
	if _, err := parseReceipt([]byte(strings.Replace(activation, `"deleted_count":0`, `"deleted_count":1`, 1)), true); err == nil {
		t.Fatal("activation count sum overflow accepted")
	}
}

func TestReceiptObjectRejectsUnpairedUnicodeEscapes(t *testing.T) {
	for _, input := range []string{`{"value":"\ud800"}`, `{"value":"\udc00"}`, `{"value":"\ud800\u0061"}`, `{"\ud800":1}`} {
		if _, err := strictObject([]byte(input)); err == nil {
			t.Fatal("unpaired Unicode escape accepted")
		}
	}
	for _, input := range []string{`{"value":"\ud83d\ude00"}`, `{"value":"\\ud800"}`} {
		if _, err := strictObject([]byte(input)); err != nil {
			t.Fatal("valid Unicode or literal escape rejected")
		}
	}
}

func TestHostAuthorityRevocationAndEpochFence(t *testing.T) {
	s, _ := prepareEnqueueFixture(t, 1)
	q := store.New(s.pool)
	if !s.AuthorizeSend(t.Context(), q) {
		t.Fatal("valid authority rejected")
	}
	files, ok := s.authorityFS.(fstest.MapFS)
	if !ok {
		t.Fatal("authority fixture filesystem invalid")
	}
	files["epoch"].Data = []byte(uuid.NewString() + "\n")
	if s.AuthorizeSend(t.Context(), q) {
		t.Fatal("stale epoch authorized sending")
	}
	authorizeNoticeFixture(t, s)
	files, ok = s.authorityFS.(fstest.MapFS)
	if !ok {
		t.Fatal("authority fixture filesystem invalid")
	}
	files["restore-reconciliation"] = &fstest.MapFile{Mode: 0444}
	if s.AuthorizeSend(t.Context(), q) {
		t.Fatal("restore marker authorized sending")
	}
	delete(files, "restore-reconciliation")
	delete(files, "send.json")
	if s.AuthorizeSend(t.Context(), q) {
		t.Fatal("missing receipt authorized sending")
	}
	if report, err := s.Status(t.Context()); err != nil || report.Code != 2 {
		t.Fatal("receipt-free authorization read rejected")
	}
}

func TestSendAuthorityRejectsFutureSeal(t *testing.T) {
	s, _ := prepareEnqueueFixture(t, 1)
	q := store.New(s.pool)
	campaign, err := q.GetPrivacyNoticeCampaign(t.Context())
	if err != nil || campaign.SealedAt == nil {
		t.Fatal("sealed campaign unavailable")
	}
	clock := s.now
	s.now = func() time.Time { return campaign.SealedAt.Add(-time.Microsecond) }
	if s.AuthorizeSend(t.Context(), q) {
		t.Fatal("future seal authorized sending")
	}
	s.now = clock
	if !s.AuthorizeSend(t.Context(), q) {
		t.Fatal("valid seal rejected")
	}
}

func TestSendAuthorityRejectsUnexplainedMembershipAndFailures(t *testing.T) {
	for name, sql := range map[string]string{
		"missing member": `DELETE FROM privacy_notice_recipients`,
		"failed member":  `UPDATE privacy_notice_recipients SET state='failed',failed_at=clock_timestamp(),failure_code='job_missing'`,
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := prepareEnqueueFixture(t, 1)
			if _, err := s.pool.Exec(t.Context(), sql); err != nil {
				t.Fatal("authority refusal fixture failed")
			}
			if s.AuthorizeSend(t.Context(), store.New(s.pool)) {
				t.Fatal("invalid cohort retained sending authority")
			}
		})
	}
}
