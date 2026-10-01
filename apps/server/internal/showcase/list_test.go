package showcase

import (
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func (e *env) list(t *testing.T, filter Filter) Page {
	t.Helper()
	if filter.Page == 0 {
		filter.Page = 1
	}
	page, err := e.service.List(e.ctx, filter)
	if err != nil {
		t.Fatalf("List(%+v) error: %v", filter, err)
	}
	return page
}

// setFirstListed pins the first-listed time the order depends on.
func (e *env) setFirstListed(t *testing.T, f fixture, at time.Time) {
	t.Helper()
	if _, err := e.pool.Exec(e.ctx, `UPDATE resume_showcase SET first_listed_at = $2 WHERE resume_id = $1`, f.id, at); err != nil {
		t.Fatalf("set first listed: %v", err)
	}
}

// AC-SHOW-014: the listing checks all five conditions on every request: the
// opt-in row, an approved result, an approved key equal to the current key, a
// live resume, and sign in to view off.
func TestListChecksAllFiveListingConditions(t *testing.T) {
	e := newFreshEnv(t)
	shown := e.listed(t, defaultSpec(), nil)
	noRow := e.addResume(t, defaultSpec())

	pending := e.addResume(t, defaultSpec())
	e.optIn(t, pending.id, nil)

	declined := e.addResume(t, defaultSpec())
	e.optIn(t, declined.id, nil)
	declinedRow, _ := e.showcaseRow(t, declined.id)
	if done, err := e.reviewer.Decline(e.ctx, declined.slug, declinedRow.ReviewKey); err != nil || !done {
		t.Fatalf("Decline() = %t, %v", done, err)
	}

	staleSpec := defaultSpec()
	staleKey := e.listed(t, staleSpec, nil)
	staleSpec.name = "Grace Hopper"
	personal, _, _ := staleSpec.parts(t)
	if _, err := e.pool.Exec(e.ctx, `UPDATE resumes SET personal_details = $2 WHERE id = $1`, staleKey.id, personal); err != nil {
		t.Fatalf("change name: %v", err)
	}
	e.sync(t, staleKey.id)

	// The next two rows bypass the transactions that delete the row, so only
	// the listing query itself keeps them out.
	unpublished := e.listed(t, defaultSpec(), nil)
	if _, err := e.pool.Exec(e.ctx, `UPDATE resumes SET live = false WHERE id = $1`, unpublished.id); err != nil {
		t.Fatalf("unpublish: %v", err)
	}
	gated := e.listed(t, defaultSpec(), nil)
	if _, err := e.pool.Exec(e.ctx, `UPDATE resumes SET sign_in_to_view = true WHERE id = $1`, gated.id); err != nil {
		t.Fatalf("gate: %v", err)
	}

	page := e.list(t, Filter{})
	if got := slugsOf(page); len(got) != 1 || got[0] != shown.slug {
		t.Fatalf("listing = %v, want only %s (no row %s, pending %s, declined %s, stale key %s, unpublished %s, gated %s)",
			got, shown.slug, noRow.slug, pending.slug, declined.slug, staleKey.slug, unpublished.slug, gated.slug)
	}
	if page.Total != 1 || page.PageCount != 1 || page.Page != 1 {
		t.Fatalf("page = %+v, want total 1 on page 1 of 1", page)
	}
}

// AC-SHOW-005, AC-SHOW-002: newest first approval first, then resume ID; role,
// language, and template filters combine; an item holds only its closed fields.
func TestListOrderFiltersAndItems(t *testing.T) {
	e := newFreshEnv(t)
	files := readTemplateFiles(t)
	base := e.now

	oldest := defaultSpec()
	oldest.lng = "vi"
	oldestFixture := e.listed(t, oldest, strp("backend"))
	e.setFirstListed(t, oldestFixture, base.Add(-72*time.Hour))

	styled := defaultSpec()
	styled.lng = "en-US"
	styled.customization = appliedCustomization(t, files[0], []string{}, []string{})
	styledFixture := e.listed(t, styled, strp("frontend"))
	e.setFirstListed(t, styledFixture, base.Add(-24*time.Hour))

	other := defaultSpec()
	other.lng = "fr"
	otherFixture := e.listed(t, other, nil)
	e.setFirstListed(t, otherFixture, base.Add(-24*time.Hour))

	unset := defaultSpec()
	unset.lng = ""
	unsetFixture := e.listed(t, unset, strp("other"))
	e.setFirstListed(t, unsetFixture, base)

	// Equal first-listed times fall back to the resume ID, newest ID first.
	tied := []fixture{styledFixture, otherFixture}
	if tied[0].id.String() < tied[1].id.String() {
		tied[0], tied[1] = tied[1], tied[0]
	}
	want := []string{unsetFixture.slug, tied[0].slug, tied[1].slug, oldestFixture.slug}
	page := e.list(t, Filter{})
	if got := slugsOf(page); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}

	byRole := e.list(t, Filter{Role: "backend"})
	if got := slugsOf(byRole); len(got) != 1 || got[0] != oldestFixture.slug || byRole.Total != 1 {
		t.Fatalf("role backend = %v total %d", got, byRole.Total)
	}
	byLang := func(lang string) []string { return slugsOf(e.list(t, Filter{Lang: lang})) }
	if got := byLang("vi"); len(got) != 1 || got[0] != oldestFixture.slug {
		t.Fatalf("lang vi = %v", got)
	}
	if got := byLang("en"); len(got) != 1 || got[0] != styledFixture.slug {
		t.Fatalf("lang en = %v", got)
	}
	if got := slugsOf(e.list(t, Filter{Template: files[0].ID})); len(got) != 1 || got[0] != styledFixture.slug {
		t.Fatalf("template %s = %v", files[0].ID, got)
	}
	custom := e.list(t, Filter{Template: CustomTemplate})
	if custom.Total != 3 || contains(slugsOf(custom), styledFixture.slug) {
		t.Fatalf("template custom = %v total %d, want the three unstyled resumes", slugsOf(custom), custom.Total)
	}
	if got := slugsOf(e.list(t, Filter{Role: "frontend", Lang: "en", Template: files[0].ID})); len(got) != 1 || got[0] != styledFixture.slug {
		t.Fatalf("combined filters = %v", got)
	}
	if got := e.list(t, Filter{Role: "backend", Lang: "en"}); len(got.Items) != 0 || got.Total != 0 {
		t.Fatalf("filters with no match = %+v", got)
	}

	items := map[string]Item{}
	for _, item := range page.Items {
		items[item.Slug] = item
	}
	if item := items[oldestFixture.slug]; item.Language != "vi" || item.Role == nil || *item.Role != "backend" || item.TemplateID != nil {
		t.Errorf("vi item = %+v", item)
	}
	if item := items[styledFixture.slug]; item.Language != "en" || item.TemplateID == nil || *item.TemplateID != files[0].ID {
		t.Errorf("en item = %+v", item)
	}
	if item := items[otherFixture.slug]; item.Language != "other" || item.Role != nil {
		t.Errorf("fr item = %+v", item)
	}
	if item := items[unsetFixture.slug]; item.Language != "other" {
		t.Errorf("item without a language = %+v", item)
	}
	for _, f := range []fixture{oldestFixture, styledFixture, otherFixture, unsetFixture} {
		row, _ := e.showcaseRow(t, f.id)
		if items[f.slug].CardVersion != row.CardVersion || items[f.slug].ImageText != "Ada Lovelace · Engineer" {
			t.Errorf("item %+v, want card version %s and the card's image text", items[f.slug], row.CardVersion)
		}
	}

	// The wire form has exactly the closed item fields, with null for unset.
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal page: %v", err)
	}
	var wire struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err = json.Unmarshal(encoded, &wire); err != nil || len(wire.Items) != 4 {
		t.Fatalf("decode page: %v (%s)", err, encoded)
	}
	for _, item := range wire.Items {
		if len(item) != 6 {
			t.Fatalf("item fields = %v, want exactly slug, cardVersion, imageText, language, templateId, role", item)
		}
		for _, field := range []string{"slug", "cardVersion", "imageText", "language", "templateId", "role"} {
			if _, present := item[field]; !present {
				t.Fatalf("item %v lacks %s", item, field)
			}
		}
	}
}

// AC-SHOW-005: a re-approval after an edit keeps the first-listed time, so
// editing never moves a resume up.
func TestListKeepsTheFirstApprovalOrderAcrossReapproval(t *testing.T) {
	e := newFreshEnv(t)
	editedSpec := defaultSpec()
	edited := e.listed(t, editedSpec, nil)
	e.setFirstListed(t, edited, e.now.Add(-48*time.Hour))
	newer := e.listed(t, defaultSpec(), nil)
	e.setFirstListed(t, newer, e.now.Add(-24*time.Hour))
	if got := slugsOf(e.list(t, Filter{})); strings.Join(got, ",") != newer.slug+","+edited.slug {
		t.Fatalf("order = %v, want %s first", got, newer.slug)
	}

	editedSpec.name = "Grace Hopper"
	personal, _, _ := editedSpec.parts(t)
	if _, err := e.pool.Exec(e.ctx, `UPDATE resumes SET personal_details = $2 WHERE id = $1`, edited.id, personal); err != nil {
		t.Fatalf("change name: %v", err)
	}
	e.sync(t, edited.id)
	if got := slugsOf(e.list(t, Filter{})); len(got) != 1 || got[0] != newer.slug {
		t.Fatalf("listing after the edit = %v, want only %s", got, newer.slug)
	}
	e.approve(t, edited)
	row, _ := e.showcaseRow(t, edited.id)
	if row.FirstListedAt == nil || !row.FirstListedAt.Equal(e.now.Add(-48*time.Hour)) {
		t.Fatalf("first_listed_at = %v after the re-approval, want the first approval's", row.FirstListedAt)
	}
	if got := slugsOf(e.list(t, Filter{})); strings.Join(got, ",") != newer.slug+","+edited.slug {
		t.Fatalf("order after the re-approval = %v, want %s first", got, newer.slug)
	}
}

// AC-SHOW-013: twelve listings a page, a page past the last is empty with the
// real page count, and no listings give a count of zero.
func TestListPagesTwelveAtATime(t *testing.T) {
	e := newFreshEnv(t)
	for i := range 13 {
		f := e.listed(t, defaultSpec(), nil)
		e.setFirstListed(t, f, e.now.Add(-time.Duration(i)*time.Hour))
	}
	first := e.list(t, Filter{Page: 1})
	if len(first.Items) != 12 || first.Page != 1 || first.PageCount != 2 || first.Total != 13 {
		t.Fatalf("page 1 = %d items %+v", len(first.Items), first)
	}
	second := e.list(t, Filter{Page: 2})
	if len(second.Items) != 1 || second.Page != 2 || second.PageCount != 2 || second.Total != 13 {
		t.Fatalf("page 2 = %d items %+v", len(second.Items), second)
	}
	if contains(slugsOf(first), second.Items[0].Slug) {
		t.Fatal("a listing appears on both pages")
	}
	past := e.list(t, Filter{Page: MaxPage})
	if past.Items == nil || len(past.Items) != 0 || past.Page != MaxPage || past.PageCount != 2 || past.Total != 13 {
		t.Fatalf("page %d = %+v, want empty items with the real page count", MaxPage, past)
	}
	none := e.list(t, Filter{Role: "qa"})
	if none.Items == nil || len(none.Items) != 0 || none.Total != 0 || none.PageCount != 0 || none.Page != 1 {
		t.Fatalf("no match = %+v, want empty items, total 0, page count 0", none)
	}
	encoded, err := json.Marshal(none)
	if err != nil || string(encoded) != `{"items":[],"page":1,"pageCount":0,"total":0}` {
		t.Fatalf("empty page encodes as %s, %v", encoded, err)
	}
}

// AC-SHOW-002: the image text follows the card's rule, so a headline holding a
// contact value is left off.
func TestListImageTextHoldsNoContactDetail(t *testing.T) {
	e := newFreshEnv(t)
	clean := defaultSpec()
	cleanFixture := e.listed(t, clean, nil)
	leaky := defaultSpec()
	leaky.headline = "Write to sentinel-mail@example.test"
	leaky.contacts = []schema.PersonalDetail{{ID: uuid.NewString(), Type: schema.Email, Value: "sentinel-mail@example.test"}}
	leakyFixture := e.listed(t, leaky, nil)
	nameless := defaultSpec()
	nameless.name = ""
	namelessFixture := e.listed(t, nameless, nil)

	texts := map[string]string{}
	for _, item := range e.list(t, Filter{}).Items {
		texts[item.Slug] = item.ImageText
	}
	if texts[cleanFixture.slug] != "Ada Lovelace · Engineer" {
		t.Errorf("clean image text = %q", texts[cleanFixture.slug])
	}
	if texts[leakyFixture.slug] != "Ada Lovelace" {
		t.Errorf("image text with a contact in the headline = %q, want the name alone", texts[leakyFixture.slug])
	}
	if text := texts[namelessFixture.slug]; text != "aboutme.vn/"+namelessFixture.slug && !strings.Contains(text, namelessFixture.slug) {
		t.Errorf("image text without a name = %q, want the site and slug", text)
	}
}

// revokeWhileListing runs revoke while readers list continuously, then checks
// that no request admitted after revoke returned lists the resume.
func revokeWhileListing(t *testing.T, e *env, slug string, revoke func()) {
	t.Helper()
	const readers = 3
	var committed atomic.Bool
	var violations atomic.Int64
	var afterCommit [readers]atomic.Int64
	var warmed [readers]atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for r := range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				admittedAfter := committed.Load()
				page, err := e.service.List(e.ctx, Filter{Page: 1})
				if err != nil {
					violations.Add(1)
					return
				}
				if admittedAfter && contains(slugsOf(page), slug) {
					violations.Add(1)
				}
				warmed[r].Add(1)
				if admittedAfter {
					afterCommit[r].Add(1)
				}
			}
		}()
	}
	waitFor(t, func() bool {
		for r := range readers {
			if warmed[r].Load() < 3 {
				return false
			}
		}
		return true
	})
	revoke()
	committed.Store(true)
	waitFor(t, func() bool {
		for r := range readers {
			if afterCommit[r].Load() < 5 {
				return false
			}
		}
		return true
	})
	close(stop)
	wg.Wait()
	if violations.Load() != 0 {
		t.Fatalf("%d requests admitted after the revocation committed listed %s or failed", violations.Load(), slug)
	}
	if contains(slugsOf(e.list(t, Filter{})), slug) {
		t.Fatalf("%s is still listed after the revocation", slug)
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the readers")
		}
		time.Sleep(time.Millisecond)
	}
}

// AC-SHOW-014: a decline, an opt-out, or an unpublish that commits while
// listing requests are in flight takes effect for every request admitted
// after it, because nothing is cached.
func TestListOmitsAResumeAfterARevocationCommits(t *testing.T) {
	e := newFreshEnv(t)
	stays := e.listed(t, defaultSpec(), nil)

	t.Run("decline", func(t *testing.T) {
		f := e.listed(t, defaultSpec(), nil)
		row, _ := e.showcaseRow(t, f.id)
		revokeWhileListing(t, e, f.slug, func() {
			if done, err := e.reviewer.Decline(e.ctx, f.slug, row.ReviewKey); err != nil || !done {
				t.Errorf("Decline() = %t, %v", done, err)
			}
		})
	})
	t.Run("opt out", func(t *testing.T) {
		f := e.listed(t, defaultSpec(), nil)
		revokeWhileListing(t, e, f.slug, func() {
			e.publish(t, PublishChange{ResumeID: f.id, Enabled: false})
		})
	})
	t.Run("unpublish", func(t *testing.T) {
		f := e.listed(t, defaultSpec(), nil)
		revokeWhileListing(t, e, f.slug, func() {
			tx, err := e.pool.Begin(e.ctx)
			if err != nil {
				t.Errorf("begin: %v", err)
				return
			}
			queries := store.New(tx)
			if _, err = tx.Exec(e.ctx, `UPDATE resumes SET live = false WHERE id = $1`, f.id); err != nil {
				t.Errorf("unpublish: %v", err)
			}
			if err = e.service.PublishTx(e.ctx, queries, PublishChange{ResumeID: f.id, Enabled: false}); err != nil {
				t.Errorf("PublishTx() error: %v", err)
			}
			if err = tx.Commit(e.ctx); err != nil {
				t.Errorf("commit: %v", err)
			}
		})
	})
	t.Run("sign in to view", func(t *testing.T) {
		f := e.listed(t, defaultSpec(), nil)
		revokeWhileListing(t, e, f.slug, func() {
			if _, err := e.pool.Exec(e.ctx, `UPDATE resumes SET sign_in_to_view = true WHERE id = $1`, f.id); err != nil {
				t.Errorf("gate: %v", err)
			}
		})
	})
	if !contains(slugsOf(e.list(t, Filter{})), stays.slug) {
		t.Fatalf("%s left the listing without a revocation", stays.slug)
	}
}
