package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

var errCandidate = errors.New("candidate_invalid")

const (
	candidateWait         = 30 * time.Minute
	candidatePollInterval = 250 * time.Millisecond
	// maxCreateRequestBytes keeps the create call well below the 4 MiB MCP
	// body limit after JSON-RPC framing.
	maxCreateRequestBytes = 3 << 20
)

type candidateReview struct {
	SourceDigest    string `json:"source_digest"`
	CandidateDigest string `json:"candidate_digest"`
	FactsPreserved  bool   `json:"facts_preserved"`
}

// writeSourceArtifact writes the canonical source document without the
// server-owned photo and returns the digest the review must name.
func writeSourceArtifact(run privateArtifacts, source sourceSnapshot) (string, error) {
	if len(source.Canonical) == 0 {
		return "", errSourceSelection
	}
	if err := run.write(sourceName, source.Canonical); err != nil {
		return "", err
	}
	return digest(source.Canonical), nil
}

// waitForCandidate polls for both handoff files for at most candidateWait.
func waitForCandidate(ctx context.Context, run privateArtifacts) error {
	deadline := time.NewTimer(candidateWait)
	defer deadline.Stop()
	ticker := time.NewTicker(candidatePollInterval)
	defer ticker.Stop()
	for {
		candidate, candidateErr := run.exists(candidateName)
		review, reviewErr := run.exists(candidateReviewName)
		if candidateErr != nil || reviewErr != nil {
			return errCandidate
		}
		if candidate && review {
			return nil
		}
		select {
		case <-ctx.Done():
			return errCandidate
		case <-deadline.C:
			return errCandidate
		case <-ticker.C:
		}
	}
}

// loadReviewedCandidate accepts only a digest-bound reviewed candidate that
// passes schema, store, size, and preservation checks. It returns the
// canonical create payload.
func loadReviewedCandidate(run privateArtifacts, source sourceSnapshot) ([]byte, error) {
	sourceBytes, err := run.read(sourceName)
	if err != nil || !bytes.Equal(sourceBytes, source.Canonical) {
		return nil, errCandidate
	}
	candidateBytes, err := run.read(candidateName)
	if err != nil {
		return nil, errCandidate
	}
	reviewBytes, err := run.read(candidateReviewName)
	if err != nil {
		return nil, errCandidate
	}
	var review candidateReview
	if !strictJSON(reviewBytes, &review) || !validDigest(review.SourceDigest) || !validDigest(review.CandidateDigest) ||
		review.SourceDigest != digest(sourceBytes) || review.CandidateDigest != digest(candidateBytes) || !review.FactsPreserved {
		return nil, errCandidate
	}
	return validateCandidate(source.Canonical, candidateBytes)
}

// validateCandidate returns the canonical payload for a candidate that omits
// the photo, passes store validation and bounds, and preserves every
// protected path of the source.
func validateCandidate(sourceCanonical, candidateBytes []byte) ([]byte, error) {
	document, canonical, photo, err := canonicalDocument(candidateBytes)
	if err != nil || photo != nil || resume.ValidateForStore(document) != nil {
		return nil, errCandidate
	}
	if preserveErr := validatePreservedDocument(sourceCanonical, canonical); preserveErr != nil {
		return nil, preserveErr
	}
	if len(canonical) > resume.MaxDocumentBytes || createRequestBytes(canonical) > maxCreateRequestBytes {
		return nil, errCandidate
	}
	return canonical, nil
}

func createRequestBytes(payload []byte) int {
	encoded, err := json.Marshal(map[string]any{
		"idempotency_key": "00000000-0000-0000-0000-000000000000", "lng": targetLanguage, "title": targetTitle,
		"document": json.RawMessage(payload),
	})
	if err != nil {
		return maxCreateRequestBytes + 1
	}
	return len(encoded)
}

func strictJSON(data []byte, target any) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	return errors.Is(decoder.Decode(&struct{}{}), io.EOF)
}

type textKind int

const (
	protectedValue textKind = iota
	plainText
	richText
)

// entryTextFields names the translatable entry fields per section type. The
// design permits translating headlines, prose, generic role names, display
// names, labels, subtitles, and rich-text text nodes. Names of people,
// organizations, products, projects, certifications, places, skills,
// languages, and degrees stay exact.
var entryTextFields = map[string]map[string]textKind{
	"profile":     {"text": richText},
	"work":        {"jobTitle": plainText, "description": richText},
	"education":   {"description": richText},
	"skill":       {"infoHtml": richText},
	"language":    {},
	"certificate": {"description": richText},
	"project":     {"subtitle": plainText, "description": richText},
	"custom":      {"subtitle": plainText, "description": richText},
}

// validatePreservedDocument compares canonical documents value by value. Only
// the translatable paths above may differ, and then only in text: rich text
// keeps its exact element tree, attributes, and link targets, and every
// translated field keeps its multiset of digit runs.
func validatePreservedDocument(source, candidate []byte) error {
	var oldValue, newValue any
	if !decodeNumbers(source, &oldValue) || !decodeNumbers(candidate, &newValue) {
		return errCandidate
	}
	return comparePreserved(nil, "", oldValue, newValue)
}

func decodeNumbers(data []byte, target *any) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	return errors.Is(decoder.Decode(&struct{}{}), io.EOF)
}

func comparePreserved(path []string, sectionType string, oldValue, newValue any) error {
	switch oldTyped := oldValue.(type) {
	case map[string]any:
		newTyped, ok := newValue.(map[string]any)
		if !ok || len(oldTyped) != len(newTyped) {
			return errCandidate
		}
		if len(path) == 2 && path[0] == "content" {
			declared, ok := oldTyped["sectionType"].(string)
			if !ok {
				return errCandidate
			}
			sectionType = declared
		}
		for key, oldChild := range oldTyped {
			newChild, present := newTyped[key]
			if !present {
				return errCandidate
			}
			if err := comparePreserved(append(path[:len(path):len(path)], key), sectionType, oldChild, newChild); err != nil {
				return err
			}
		}
		return nil
	case []any:
		newTyped, ok := newValue.([]any)
		if !ok || len(oldTyped) != len(newTyped) {
			return errCandidate
		}
		for index := range oldTyped {
			if err := comparePreserved(append(path[:len(path):len(path)], "[]"), sectionType, oldTyped[index], newTyped[index]); err != nil {
				return err
			}
		}
		return nil
	case string:
		newTyped, ok := newValue.(string)
		if !ok {
			return errCandidate
		}
		switch pathKind(path, sectionType) {
		case plainText:
			if !samePlainText(oldTyped, newTyped) {
				return errCandidate
			}
		case richText:
			if !sameRichText(oldTyped, newTyped) {
				return errCandidate
			}
		default:
			if oldTyped != newTyped {
				return errCandidate
			}
		}
		return nil
	case json.Number:
		newTyped, ok := newValue.(json.Number)
		if !ok || oldTyped.String() != newTyped.String() {
			return errCandidate
		}
		return nil
	case bool:
		newTyped, ok := newValue.(bool)
		if !ok || oldTyped != newTyped {
			return errCandidate
		}
		return nil
	case nil:
		if newValue != nil {
			return errCandidate
		}
		return nil
	default:
		return errCandidate
	}
}

// pathKind classifies a canonical path. Array indexes appear as "[]".
func pathKind(path []string, sectionType string) textKind {
	switch {
	case len(path) == 2 && path[0] == "personalDetails" && path[1] == "headline":
		return plainText
	case len(path) == 4 && path[0] == "personalDetails" && path[1] == "details" && path[2] == "[]" && path[3] == "label":
		return plainText
	case len(path) == 3 && path[0] == "content" && path[2] == "displayName":
		return plainText
	case len(path) == 5 && path[0] == "content" && path[2] == "entries" && path[3] == "[]":
		return entryTextFields[sectionType][path[4]]
	default:
		return protectedValue
	}
}

// samePlainText permits translation but keeps emptiness and numbers.
func samePlainText(oldValue, newValue string) bool {
	return (strings.TrimSpace(oldValue) == "") == (strings.TrimSpace(newValue) == "") &&
		sameDigitRuns(oldValue, newValue) && !strings.ContainsAny(newValue, "<>")
}

// sameRichText permits text-node translation while preserving the element
// tree, tag names, attributes, link targets, and numbers.
func sameRichText(oldValue, newValue string) bool {
	oldNodes, oldText, oldOK := parseRichText(oldValue)
	newNodes, newText, newOK := parseRichText(newValue)
	if !oldOK || !newOK || len(oldNodes) != len(newNodes) || !sameDigitRuns(oldText, newText) {
		return false
	}
	for index := range oldNodes {
		if !sameHTMLShape(oldNodes[index], newNodes[index], 0) {
			return false
		}
	}
	return true
}

const maxRichTextDepth = 64

func parseRichText(value string) ([]*html.Node, string, bool) {
	body := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(value), body)
	if err != nil {
		return nil, "", false
	}
	var text strings.Builder
	for _, node := range nodes {
		collectText(node, &text, 0)
	}
	return nodes, text.String(), true
}

func collectText(node *html.Node, text *strings.Builder, depth int) {
	if depth > maxRichTextDepth {
		return
	}
	if node.Type == html.TextNode {
		text.WriteString(node.Data)
		text.WriteByte(' ')
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectText(child, text, depth+1)
	}
}

func sameHTMLShape(oldNode, newNode *html.Node, depth int) bool {
	if depth > maxRichTextDepth || oldNode.Type != newNode.Type {
		return false
	}
	switch oldNode.Type {
	case html.TextNode:
		if (strings.TrimSpace(oldNode.Data) == "") != (strings.TrimSpace(newNode.Data) == "") {
			return false
		}
	case html.ElementNode:
		if oldNode.Data != newNode.Data || !sameAttributes(oldNode.Attr, newNode.Attr) {
			return false
		}
	default:
		return false
	}
	oldChild, newChild := oldNode.FirstChild, newNode.FirstChild
	for oldChild != nil && newChild != nil {
		if !sameHTMLShape(oldChild, newChild, depth+1) {
			return false
		}
		oldChild, newChild = oldChild.NextSibling, newChild.NextSibling
	}
	return oldChild == nil && newChild == nil
}

func sameAttributes(left, right []html.Attribute) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// sameDigitRuns requires the same multiset of digit runs, so dates, counts,
// percentages, and amounts cannot change, appear, or disappear in translation.
func sameDigitRuns(oldValue, newValue string) bool {
	oldRuns, newRuns := digitRuns(oldValue), digitRuns(newValue)
	if len(oldRuns) != len(newRuns) {
		return false
	}
	sort.Strings(oldRuns)
	sort.Strings(newRuns)
	for index := range oldRuns {
		if oldRuns[index] != newRuns[index] {
			return false
		}
	}
	return true
}

func digitRuns(value string) []string {
	var runs []string
	var current strings.Builder
	for _, r := range value {
		if unicode.IsDigit(r) {
			current.WriteRune(r)
			continue
		}
		if current.Len() > 0 {
			runs = append(runs, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		runs = append(runs, current.String())
	}
	return runs
}
