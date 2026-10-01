// Command document-check checks a fetched copy of
// https://aboutme.vn/.well-known/deployment.json the way a reader must
// (docs/design/deployment-transparency/document.md): at most 64 KiB, valid
// against the closed version 1 schema, free of the identifiers the
// sanitizer forbids, and not stale at the given time
// (docs/design/deployment-transparency/README.md, "Run, freshness, and
// staleness"). On success it prints one line per running image,
// "<image> <digest> <version>", with "-" for a null version, so the caller
// can verify each digest with gh attestation verify.
//
// Usage: document-check -now <RFC 3339 time> <document.json
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dannyota/aboutme/deploy/observer/internal/leakscan"
	"github.com/dannyota/aboutme/deploy/observer/schema"
)

// maxBytes is the document cap the observer enforces before it writes.
const maxBytes = 64 << 10

// maxAge is how long after observed_at a reader treats the document as
// stale, whatever stale_after says.
const maxAge = 600 * time.Second

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "document-check:", err)
		os.Exit(1)
	}
}

type document struct {
	ObservedAt time.Time `json:"observed_at"`
	StaleAfter time.Time `json:"stale_after"`
	Components []struct {
		Image         string `json:"image"`
		RunningImages []struct {
			Digest  string  `json:"digest"`
			Version *string `json:"version"`
		} `json:"running_images"`
	} `json:"components"`
}

func run(args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("document-check", flag.ContinueOnError)
	nowFlag := fs.String("now", "", "the time to judge staleness at, RFC 3339")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *nowFlag == "" {
		return errors.New("usage: document-check -now <RFC 3339 time> <document.json")
	}
	now, err := time.Parse(time.RFC3339, *nowFlag)
	if err != nil {
		return fmt.Errorf("-now: %w", err)
	}
	b, err := readCapped(in)
	if err != nil {
		return err
	}
	if err = schema.Validate(b); err != nil {
		return err
	}
	if found := leakscan.Find(b); len(found) > 0 {
		return fmt.Errorf("document %s", strings.Join(found, "; document "))
	}
	var doc document
	if err = json.Unmarshal(b, &doc); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if now.After(doc.StaleAfter) || now.Sub(doc.ObservedAt) > maxAge {
		return fmt.Errorf("document is stale: observed_at %s, stale_after %s, now %s",
			doc.ObservedAt.Format(time.RFC3339), doc.StaleAfter.Format(time.RFC3339), now.Format(time.RFC3339))
	}
	for _, c := range doc.Components {
		for _, ri := range c.RunningImages {
			v := "-"
			if ri.Version != nil {
				v = *ri.Version
			}
			if _, err = fmt.Fprintf(out, "%s %s %s\n", c.Image, ri.Digest, v); err != nil {
				return err
			}
		}
	}
	return nil
}

// readCapped reads r and fails when it holds more than maxBytes.
func readCapped(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	if len(b) > maxBytes {
		return nil, fmt.Errorf("document is over %d bytes", maxBytes)
	}
	return b, nil
}
