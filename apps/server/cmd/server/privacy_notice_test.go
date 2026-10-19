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
	for _, action := range []string{"prepare", "enqueue", "status", "activate"} {
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

func TestPrivacyNoticeCommandFixedOutputAndExits(t *testing.T) {
	for _, action := range []string{"prepare", "enqueue", "status", "activate"} {
		for _, code := range []int{0, 1, 2} {
			t.Run(action+string(rune('0'+code)), func(t *testing.T) {
				var output, diagnostics bytes.Buffer
				err := executePrivacyNoticeCommand(t.Context(), []string{action}, func(string) string { t.Fatal("output accessed configuration"); return "" }, &output, &diagnostics, func(context.Context, string, func(string) string) (privacynotice.Report, error) {
					state := "sealed"
					if code == 1 {
						state = "retired"
					}
					if action == "activate" && code == 0 {
						state = "activated"
					}
					return privacynotice.Report{Code: code, Campaign: store.PrivacyNoticeCampaign{ID: authmail.PrivacyNoticeID, ContentSha256: privacynotice.ContentDigest(), CutoffAt: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC), State: state}}, nil
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
		err := executePrivacyNoticeCommand(t.Context(), args, func(string) string { return "" }, &output, &diagnostics, func(context.Context, string, func(string) string) (privacynotice.Report, error) {
			return privacynotice.Report{}, errors.New("private fixture execution failure")
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
	_, err := executePrivacyNoticeAction(t.Context(), "status", func(name string) string {
		if name != "DATABASE_URL" {
			t.Fatal("status read unrelated credentials")
		}
		return "private-fixture-configuration"
	})
	if err == nil || strings.Contains(err.Error(), "private-fixture-configuration") {
		t.Fatal("notice action exposed raw configuration failure")
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
