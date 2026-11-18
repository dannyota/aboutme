package githubevidence

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"
)

type githubRepository struct {
	id        int64
	fullName  string
	ownerID   int64
	private   bool
	fork      bool
	createdAt time.Time
}

type githubPullRequest struct {
	id       int64
	number   int64
	title    string
	merged   bool
	mergedAt *time.Time
	userID   int64
	htmlURL  string
}

type githubRelease struct {
	id          int64
	tag         string
	name        string
	draft       bool
	prerelease  bool
	publishedAt *time.Time
	authorID    int64
	authorBot   bool
}

type searchResult struct {
	items      []searchItem
	incomplete bool
}

type searchItem struct {
	owner    string
	name     string
	number   int64
	title    string
	closedAt time.Time
}

type fieldReader struct {
	fields map[string]json.RawMessage
	failed *bool
}

func readObject(raw []byte, failed *bool) fieldReader {
	fields, err := strictObject(raw)
	if err != nil {
		*failed = true
	}
	return fieldReader{fields: fields, failed: failed}
}

func (f fieldReader) fail() {
	*f.failed = true
}

func (f fieldReader) null(name string) bool {
	return string(f.fields[name]) == "null"
}

func (f fieldReader) id(name string) int64 {
	value, err := strconv.ParseInt(string(f.fields[name]), 10, 64)
	if err != nil || value <= 0 {
		f.fail()
	}
	return value
}

func (f fieldReader) text(name string) string {
	value, ok := jsonString(f.fields[name])
	if !ok {
		f.fail()
	}
	return value
}

func (f fieldReader) flag(name string) bool {
	switch string(f.fields[name]) {
	case "true":
		return true
	case "false":
		return false
	}
	f.fail()
	return false
}

func (f fieldReader) time(name string) time.Time {
	value, err := time.Parse(time.RFC3339, f.text(name))
	if err != nil {
		f.fail()
	}
	return value.UTC()
}

func (f fieldReader) optionalTime(name string) *time.Time {
	if f.null(name) {
		return nil
	}
	value := f.time(name)
	return &value
}

func (f fieldReader) object(name string) fieldReader {
	return readObject(f.fields[name], f.failed)
}

func parseRepository(body []byte) (githubRepository, error) {
	var failed bool
	repo := repositoryFrom(readObject(body, &failed))
	if failed {
		return githubRepository{}, errMalformed
	}
	return repo, nil
}

func repositoryFrom(f fieldReader) githubRepository {
	repo := githubRepository{
		id:        f.id("id"),
		fullName:  f.text("full_name"),
		ownerID:   f.object("owner").id("id"),
		private:   f.flag("private"),
		fork:      f.flag("fork"),
		createdAt: f.time("created_at"),
	}
	if _, _, ok := splitFullName(repo.fullName); !ok {
		f.fail()
	}
	return repo
}

func parsePullRequest(body []byte) (githubPullRequest, error) {
	var failed bool
	f := readObject(body, &failed)
	pull := githubPullRequest{
		id:       f.id("id"),
		number:   f.id("number"),
		title:    f.text("title"),
		merged:   f.flag("merged"),
		mergedAt: f.optionalTime("merged_at"),
		htmlURL:  f.text("html_url"),
	}
	if !f.null("user") {
		pull.userID = f.object("user").id("id")
	}
	if failed {
		return githubPullRequest{}, errMalformed
	}
	return pull, nil
}

func parseRelease(body []byte) (githubRelease, error) {
	var failed bool
	release := releaseFrom(readObject(body, &failed))
	if failed {
		return githubRelease{}, errMalformed
	}
	return release, nil
}

func releaseFrom(f fieldReader) githubRelease {
	release := githubRelease{
		id:          f.id("id"),
		tag:         f.text("tag_name"),
		draft:       f.flag("draft"),
		prerelease:  f.flag("prerelease"),
		publishedAt: f.optionalTime("published_at"),
	}
	if release.tag == "" {
		f.fail()
	}
	if !f.null("name") {
		release.name = f.text("name")
	}
	if !f.null("author") {
		author := f.object("author")
		release.authorID = author.id("id")
		release.authorBot = author.text("type") == "Bot"
	}
	return release
}

func parseSearch(body []byte) (searchResult, error) {
	var failed bool
	f := readObject(body, &failed)
	result := searchResult{incomplete: f.flag("incomplete_results")}
	items, err := strictArray(f.fields["items"])
	if err != nil {
		return searchResult{}, errMalformed
	}
	for _, raw := range items {
		item := readObject(raw, &failed)
		fullName, found := strings.CutPrefix(item.text("repository_url"), productionAPIBaseURL+"/repos/")
		owner, name, ok := splitFullName(fullName)
		if !found || !ok {
			failed = true
		}
		result.items = append(result.items, searchItem{owner: owner, name: name, number: item.id("number"),
			title: item.text("title"), closedAt: item.time("closed_at")})
	}
	if failed {
		return searchResult{}, errMalformed
	}
	return result, nil
}

func parseRepositoryList(body []byte) ([]githubRepository, error) {
	return parseList(body, repositoryFrom)
}

func parseReleaseList(body []byte) ([]githubRelease, error) {
	return parseList(body, releaseFrom)
}

func parseList[T any](body []byte, from func(fieldReader) T) ([]T, error) {
	items, err := strictArray(body)
	if err != nil {
		return nil, errMalformed
	}
	var failed bool
	list := make([]T, 0, len(items))
	for _, raw := range items {
		list = append(list, from(readObject(raw, &failed)))
	}
	if failed {
		return nil, errMalformed
	}
	return list, nil
}

func strictArray(body []byte) ([]json.RawMessage, error) {
	if !json.Valid(body) {
		return nil, errMalformed
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	if open, err := dec.Token(); err != nil || open != json.Delim('[') {
		return nil, errMalformed
	}
	items := []json.RawMessage{}
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, errMalformed
		}
		items = append(items, raw)
	}
	if end, err := dec.Token(); err != nil || end != json.Delim(']') {
		return nil, errMalformed
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errMalformed
	}
	return items, nil
}
