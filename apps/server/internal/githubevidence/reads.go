package githubevidence

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

var (
	errSessionRejected = errors.New("githubevidence: no live evidence session for the read")
	errGone            = errors.New("githubevidence: GitHub no longer finds the item")
	errUpstream        = errors.New("githubevidence: GitHub read failed")
	errPageRange       = errors.New("githubevidence: listing page out of range")
)

type githubReader struct {
	client   *client
	registry *registry
	account  uuid.UUID
	session  *liveSession
}

type readResult struct {
	body     []byte
	moved    bool
	location string
}

func (rd *githubReader) fetch(ctx context.Context, path string, query url.Values, allowMoved bool) (readResult, error) {
	var rawQuery string
	if len(query) != 0 {
		rawQuery = query.Encode()
	}
	if !permittedRead(path, rawQuery) {
		return readResult{}, errDestinationRefused
	}
	token, live := rd.session.token.peek()
	if !live {
		return readResult{}, errSessionRejected
	}
	if allowed, retry := rd.client.requests.admit(rd.account, rd.client.now()); !allowed {
		return readResult{}, &rateLimitError{retry: retry}
	}
	target := rd.client.apiBase + path
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	status, header, body, err := rd.client.send(ctx, http.MethodGet, target, nil, func(h http.Header) {
		h.Set("Accept", "application/vnd.github+json")
		h.Set("X-GitHub-Api-Version", githubAPIVersion)
		h.Set("Authorization", "Bearer "+token)
	})
	if allowMoved && status == http.StatusMovedPermanently && errors.Is(err, errRedirectRefused) && len(header.Values("Location")) == 1 {
		return readResult{moved: true, location: header.Get("Location")}, nil
	}
	if err != nil {
		return readResult{}, fmt.Errorf("%w: %w", errUpstream, err)
	}
	if status == http.StatusOK && jsonMediaType(header) {
		return readResult{body: body}, nil
	}
	defer clear(body)
	switch {
	case status == http.StatusUnauthorized:
		rd.registry.endSession(rd.account, rd.session)
		return readResult{}, errSessionRejected
	case status == http.StatusTooManyRequests || (status == http.StatusForbidden && rateLimitedResponse(header, body)):
		return readResult{}, &rateLimitError{retry: githubRetry(header, rd.client.now())}
	case status == http.StatusNotFound || status == http.StatusGone:
		return readResult{}, errGone
	default:
		return readResult{}, errUpstream
	}
}

func decoded[T any](result readResult, err error, parse func([]byte) (T, error)) (T, error) {
	var zero T
	if err != nil {
		return zero, err
	}
	defer clear(result.body)
	value, err := parse(result.body)
	if err != nil {
		return zero, fmt.Errorf("%w: %w", errUpstream, err)
	}
	return value, nil
}

func repositoryPath(owner, name string) string {
	return "/repos/" + owner + "/" + name
}

func (rd *githubReader) repositoryByName(ctx context.Context, owner, name string, storedID int64) (githubRepository, error) {
	result, err := rd.fetch(ctx, repositoryPath(owner, name), nil, true)
	if err == nil && result.moved {
		id, valid := renamedRepositoryID(result.location)
		if !valid || storedID != 0 && id != storedID {
			return githubRepository{}, errUpstream
		}
		repo, hopErr := rd.repositoryByID(ctx, id)
		if hopErr == nil && repo.id != id {
			return githubRepository{}, fmt.Errorf("%w: %w", errUpstream, errMalformed)
		}
		return repo, hopErr
	}
	repo, err := decoded(result, err, parseRepository)
	if err == nil && storedID != 0 && repo.id != storedID {
		return githubRepository{}, errGone
	}
	return repo, err
}

func renamedRepositoryID(location string) (int64, bool) {
	digits, found := strings.CutPrefix(location, productionAPIBaseURL+"/repositories/")
	if !found {
		return 0, false
	}
	return canonicalID(digits)
}

func (rd *githubReader) repositoryByID(ctx context.Context, id int64) (githubRepository, error) {
	result, err := rd.fetch(ctx, "/repositories/"+strconv.FormatInt(id, 10), nil, false)
	return decoded(result, err, parseRepository)
}

func (rd *githubReader) pullRequest(ctx context.Context, owner, name string, number int64) (githubPullRequest, error) {
	result, err := rd.fetch(ctx, repositoryPath(owner, name)+"/pulls/"+strconv.FormatInt(number, 10), nil, false)
	return decoded(result, err, parsePullRequest)
}

func (rd *githubReader) releaseByID(ctx context.Context, owner, name string, id int64) (githubRelease, error) {
	result, err := rd.fetch(ctx, repositoryPath(owner, name)+"/releases/"+strconv.FormatInt(id, 10), nil, false)
	return decoded(result, err, parseRelease)
}

func (rd *githubReader) releaseByTag(ctx context.Context, owner, name, tag string) (githubRelease, error) {
	result, err := rd.fetch(ctx, repositoryPath(owner, name)+"/releases/tags/"+url.PathEscape(tag), nil, false)
	return decoded(result, err, parseRelease)
}
