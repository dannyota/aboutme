package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBrowserHandoff(t *testing.T) {
	directory := t.TempDir()
	if err := atomicPrivateWrite(directory, "browser-ready", []byte("ready")); err != nil {
		t.Fatal(err)
	}
	coordinator := browserCoordinator{directory: directory, mode: "local", origin: "https://aboutme.vn", wait: time.Second}
	done := make(chan error, 1)
	go func() { done <- coordinator.Open(context.Background(), "sensitive URL") }()
	for {
		if _, err := os.Stat(directory + "/browser-request.json"); err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err := atomicPrivateWrite(directory, "browser-result.json", []byte("completed")); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Open error = %v", err)
	}
}

// Passkeys are live in production and the owner workflow signs in with
// password only. The browser helper reports a 202 secondFactorRequired login
// as browserResultSecondFactor rather than folding it into a generic failure,
// so Open must surface the matching sentinel instead of hanging or reporting
// errBrowserFailed.
func TestBrowserHandoffSecondFactorRequired(t *testing.T) {
	directory := t.TempDir()
	if err := atomicPrivateWrite(directory, "browser-ready", []byte("ready")); err != nil {
		t.Fatal(err)
	}
	coordinator := browserCoordinator{directory: directory, mode: "local", origin: "https://aboutme.vn", wait: time.Second}
	done := make(chan error, 1)
	go func() { done <- coordinator.Open(context.Background(), "sensitive URL") }()
	for {
		if _, err := os.Stat(directory + "/browser-request.json"); err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err := atomicPrivateWrite(directory, "browser-result.json", []byte(browserResultSecondFactor)); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, errSecondFactorRequired) {
		t.Fatalf("Open error = %v, want %v", err, errSecondFactorRequired)
	}
}

func TestBrowserHandoffTimeoutDoesNotExposeURL(t *testing.T) {
	coordinator := browserCoordinator{directory: t.TempDir(), wait: time.Millisecond}
	err := coordinator.Open(context.Background(), "https://aboutme.vn/authorize?code=sensitive")
	if !errors.Is(err, errBrowserTimeout) {
		t.Fatalf("Open error = %v, want %v", err, errBrowserTimeout)
	}
	if strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("error exposes authorization URL: %v", err)
	}
}
