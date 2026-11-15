package main

import "errors"

type privacyCommand struct {
	name   string
	dryRun bool
}

func parsePrivacyCommand(args []string) (privacyCommand, error) {
	invalid := errors.New("usage: server {idempotency-expiry-sweep|media-deletion-sweep|media-orphan-sweep [--dry-run]|privacy-retention-sweep}")
	if len(args) == 0 || len(args) > 2 {
		return privacyCommand{}, invalid
	}
	command := privacyCommand{name: args[0]}
	switch command.name {
	case "idempotency-expiry-sweep", "media-deletion-sweep", "media-orphan-sweep", "privacy-retention-sweep":
	default:
		return privacyCommand{}, invalid
	}
	if len(args) == 2 {
		if command.name != "media-orphan-sweep" || args[1] != "--dry-run" {
			return privacyCommand{}, invalid
		}
		command.dryRun = true
	}
	return command, nil
}
