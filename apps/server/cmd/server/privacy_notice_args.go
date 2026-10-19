package main

import "errors"

const privacyNoticeCommandName = "crowdsec-privacy-notice"

func parsePrivacyNoticeArgs(args []string) (string, error) {
	if len(args) == 1 {
		switch args[0] {
		case "prepare", "enqueue", "status", "activate":
			return args[0], nil
		}
	}
	return "", errors.New("usage: server crowdsec-privacy-notice {prepare|enqueue|status|activate}")
}
