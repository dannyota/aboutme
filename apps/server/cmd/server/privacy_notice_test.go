package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/privacynotice"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func TestPrivacyNoticeCommandHasClosedArguments(t *testing.T) {
	for _, action := range []string{"prepare", "enqueue", "status", "activate", "reconcile-initial"} {
		got, err := parsePrivacyNoticeArgs([]string{action})
		if err != nil || got != action {
			t.Fatal("fixed notice action rejected")
		}
		for _, extra := range []string{"--recipient=fixture@example.com", "--message=fixture", "--campaign=other", "--cutoff=2026-10-19", "--success", "--url=https://example.com"} {
			if _, err := parsePrivacyNoticeArgs([]string{action, extra}); err == nil {
				t.Fatal("notice override accepted")
			}
		}
	}
	for _, args := range [][]string{nil, {"abort"}, {"retire"}, {"recover"}, {"authorize-send"}, {"Prepare"}, {"status", "status"}} {
		if _, err := parsePrivacyNoticeArgs(args); err == nil {
			t.Fatal("unsupported notice command accepted")
		}
	}
}

func TestPrivacyNoticeInitialCommandClosedOutput(t *testing.T) {
	for _, fail := range []bool{false, true} {
		var output, diagnostics bytes.Buffer
		err := executePrivacyNoticeCommand(t.Context(), []string{"reconcile-initial"}, func(string) string { t.Fatal("output read configuration"); return "" }, &output, &diagnostics, func(context.Context, string, func(string) string) (privacyNoticeResult, error) {
			if fail {
				return privacyNoticeResult{}, errors.New("private fixture database failure")
			}
			return privacyNoticeResult{initial: &privacynotice.InitialResult{}}, nil
		})
		if fail {
			var exit privacyNoticeExitError
			if output.Len() != 0 || diagnostics.String() != "invalid_state\n" || !errors.As(err, &exit) || exit.code != 1 {
				t.Fatal("initial command leaked failure or returned wrong exit")
			}
			continue
		}
		want := `{"schema_version":1,"result":"initial_reconciled","campaign_id":"crowdsec-postgres-paths-v1","content_sha256":"bb128b39501f09db59915f3f75759572a49349ec78fe17ad48b6ba40d500ffdc","campaign_count":0,"recipient_count":0,"notice_job_count":0}` + "\n"
		if err != nil || diagnostics.Len() != 0 || output.String() != want {
			t.Fatal("initial command output differs")
		}
	}
}

func TestPrivacyNoticeCommandFixedOutputAndExits(t *testing.T) {
	for _, action := range []string{"prepare", "enqueue", "status", "activate"} {
		for _, code := range []int{0, 1, 2} {
			t.Run(action+string(rune('0'+code)), func(t *testing.T) {
				var output, diagnostics bytes.Buffer
				err := executePrivacyNoticeCommand(t.Context(), []string{action}, func(string) string { t.Fatal("output accessed configuration"); return "" }, &output, &diagnostics, func(context.Context, string, func(string) string) (privacyNoticeResult, error) {
					state := "sealed"
					if code == 1 {
						state = "retired"
					}
					if action == "activate" && code == 0 {
						state = "activated"
					}
					return privacyNoticeResult{report: privacynotice.Report{Code: code, Campaign: store.PrivacyNoticeCampaign{ID: authmail.PrivacyNoticeID, ContentSha256: privacynotice.ContentDigest(), CutoffAt: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC), State: state}}}, nil
				})
				var exit privacyNoticeExitError
				if diagnostics.Len() != 0 || output.Len() == 0 || output.Bytes()[output.Len()-1] != '\n' || (code == 0 && err != nil) || (code != 0 && (!errors.As(err, &exit) || exit.code != code)) {
					t.Fatal("notice command output or exit differs")
				}
			})
		}
	}
}

func TestPrivacyNoticeCommandInvalidStateNeverWritesJSON(t *testing.T) {
	for _, args := range [][]string{{"status"}, {"status", "--success"}, {"unknown"}} {
		var output, diagnostics bytes.Buffer
		err := executePrivacyNoticeCommand(t.Context(), args, func(string) string { return "" }, &output, &diagnostics, func(context.Context, string, func(string) string) (privacyNoticeResult, error) {
			return privacyNoticeResult{}, errors.New("private fixture execution failure")
		})
		var exit privacyNoticeExitError
		if output.Len() != 0 || diagnostics.String() != "invalid_state\n" || !errors.As(err, &exit) || exit.code != 1 {
			t.Fatal("invalid notice command leaked output or wrong status")
		}
	}
}

func TestPrivacyNoticeDispatchDoesNotFallThroughToServer(t *testing.T) {
	var exit privacyNoticeExitError
	if err := dispatch([]string{privacyNoticeCommandName, "unsupported"}); !errors.As(err, &exit) || exit.code != 1 {
		t.Fatal("notice dispatch fell through to server initialization")
	}
}

func TestPrivacyNoticeActionFailureDoesNotExposeConfiguration(t *testing.T) {
	for _, action := range []string{"status", "reconcile-initial"} {
		_, err := executePrivacyNoticeAction(t.Context(), action, func(name string) string {
			if name != "DATABASE_URL" {
				t.Fatal("status read unrelated credentials")
			}
			return "private-fixture-configuration"
		})
		if err == nil || strings.Contains(err.Error(), "private-fixture-configuration") {
			t.Fatal("notice action exposed raw configuration failure")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := executePrivacyNoticeAction(ctx, "prepare", func(string) string { t.Fatal("canceled action read configuration"); return "" }); err == nil {
		t.Fatal("canceled action succeeded")
	}
}

func TestPrivacyNoticeEnqueueLoadsOnlyEncryptionKeys(t *testing.T) {
	values := map[string]string{"AUTH_EMAIL_ACTIVE_KEY_ID": "fixture", "AUTH_EMAIL_ACTIVE_KEY": base64.RawURLEncoding.EncodeToString(make([]byte, 32))}
	getenv := func(name string) string {
		switch name {
		case "AUTH_EMAIL_ACTIVE_KEY_ID", "AUTH_EMAIL_ACTIVE_KEY", "AUTH_EMAIL_PREVIOUS_KEY_ID", "AUTH_EMAIL_PREVIOUS_KEY":
			return values[name]
		default:
			t.Fatal("enqueue read unrelated credentials")
			return ""
		}
	}
	if _, err := privacyNoticeKeyRing(getenv); err != nil {
		t.Fatal("notice encryption keys rejected")
	}
	values["AUTH_EMAIL_ACTIVE_KEY_ID"] = " fixture "
	values["AUTH_EMAIL_ACTIVE_KEY"] = " " + values["AUTH_EMAIL_ACTIVE_KEY"] + " "
	if _, err := privacyNoticeKeyRing(getenv); err != nil {
		t.Fatal("notice key parsing differs from server key parsing")
	}
	values["AUTH_EMAIL_ACTIVE_KEY_ID"] = "fixture"
	values["AUTH_EMAIL_ACTIVE_KEY"] = strings.TrimSpace(values["AUTH_EMAIL_ACTIVE_KEY"])
	values["AUTH_EMAIL_PREVIOUS_KEY_ID"] = "fixture"
	values["AUTH_EMAIL_PREVIOUS_KEY"] = values["AUTH_EMAIL_ACTIVE_KEY"]
	if _, err := privacyNoticeKeyRing(getenv); err == nil {
		t.Fatal("duplicate key identities accepted")
	}
	delete(values, "AUTH_EMAIL_PREVIOUS_KEY_ID")
	delete(values, "AUTH_EMAIL_PREVIOUS_KEY")
	delete(values, "AUTH_EMAIL_ACTIVE_KEY")
	if _, err := privacyNoticeKeyRing(getenv); err == nil {
		t.Fatal("missing active key accepted")
	}
}
