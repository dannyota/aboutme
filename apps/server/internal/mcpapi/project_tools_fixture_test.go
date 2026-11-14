package mcpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	schema "danny.vn/aboutme/packages/schema/gen/go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"danny.vn/aboutme/apps/server/internal/media"
	"danny.vn/aboutme/apps/server/internal/oauthsrv"
	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/projectapi"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	projectToolAllScopes   = "resumes:read resumes:write projects:read projects:write"
	projectToolOldGrant    = "resumes:read resumes:write"
	projectToolSectionKey  = "projects"
	projectToolLockTimeout = 20 * time.Second
)

var projectToolNames = []string{
	"list_projects", "get_project", "create_project", "update_project",
	"delete_project", "link_project", "refresh_project_entry",
}

type projectToolWorld struct {
	h           *bearerHarness
	session     *mcp.ClientSession
	resumes     *resume.Store
	atlas       project.Project
	beacon      project.Project
	carto       project.Project
	resume      resume.Resume
	linkedEntry string
	plainEntry  string
	other       uuid.UUID
	foreign     project.Project
	otherResume resume.Resume
	otherEntry  string
}

func storeProjectToolGrantScopes(t *testing.T, h *bearerHarness, scopes string) {
	t.Helper()
	parsed, parseErr := oauthsrv.ParseScopes(scopes)
	stored := scopes
	if parseErr == nil {
		stored = parsed.String()
	}
	grant, err := h.q.UpsertOAuthGrant(context.Background(), store.UpsertOAuthGrantParams{
		UserID: h.user.ID, ClientID: h.client.ID, Scopes: stored, CreatedAt: h.clock.Now(), AuthEpoch: h.user.AuthEpoch,
	})
	if err != nil {
		t.Fatalf("setup: store grant scopes %q: %v", stored, err)
	}
	if parseErr != nil {
		t.Fatalf("setup: scope registry refuses grant scopes %q: %v", scopes, parseErr)
	}
	h.grant = grant
}

func newProjectToolHarness(t *testing.T, scopes string) *bearerHarness {
	t.Helper()
	h := newBearerHarness(t, "resumes:read")
	storeProjectToolGrantScopes(t, h, scopes)
	return h
}

func startProjectToolServer(t *testing.T, h *bearerHarness) *mcp.ClientSession {
	t.Helper()
	raw, _ := h.createToken(t, oauthsrv.TokenKindAccess)
	projector := docmigrate.NewIdentityProjector()
	backend, err := media.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	public, err := h.q.GetPublicState(context.Background())
	if err != nil {
		t.Fatalf("GetPublicState: %v", err)
	}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: public.DiscoveryGeneration})
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	resumes := resumeapi.New(resume.NewStore(h.pool, projector), resume.NewIdempotencyStore(h.pool), projector, backend,
		resumeapi.Options{
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Clock: h.clock.Now,
			Coordinator: coordinator, RecoveryPool: h.pool,
		})
	projects := projectapi.New(h.pool, resume.NewIdempotencyStore(h.pool), projectapi.Options{
		AllowSlug: func(uuid.UUID, time.Time) bool { return true }, Clock: h.clock.Now, Links: resumes,
	})
	handler, err := NewServer(ServerDependencies{
		Bearer: h.bearer, Resumes: resumes, Projects: projects, Rates: mustTestMCPRates(t), MaxRequestBodyBytes: maxMCPRequestBytes,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	return connectMCPClient(t, httpServer.URL, raw, "")
}

func newProjectToolWorld(t *testing.T, scopes string) *projectToolWorld {
	t.Helper()
	h := newProjectToolHarness(t, scopes)
	w := &projectToolWorld{h: h, resumes: resume.NewStore(h.pool, docmigrate.NewIdentityProjector())}
	w.session = startProjectToolServer(t, h)
	w.atlas = createProjectToolProject(t, h, h.user.ID, "atlas", "Atlas")
	w.beacon = createProjectToolProject(t, h, h.user.ID, "beacon", "Beacon")
	w.carto = createProjectToolProject(t, h, h.user.ID, "carto", "Carto")
	linked := projectToolEntry(w.atlas.ID, "Atlas, my words")
	plain := projectToolEntry(uuid.Nil, "Unlinked work")
	w.linkedEntry, w.plainEntry = linked.ID, plain.ID
	w.resume = createProjectToolResume(t, w, h.user.ID, linked, plain)

	other, err := h.q.CreateUser(context.Background(), store.CreateUserParams{
		Email: "project-tools-other-" + uuid.NewString() + "@example.test", Name: "Other project owner",
	})
	if err != nil {
		t.Fatalf("CreateUser other: %v", err)
	}
	t.Cleanup(func() {
		if _, cleanupErr := h.pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", other.ID); cleanupErr != nil {
			t.Errorf("delete other user: %v", cleanupErr)
		}
	})
	w.other = other.ID
	w.foreign = createProjectToolProject(t, h, other.ID, "foreign", "Foreign")
	otherLinked := projectToolEntry(w.foreign.ID, "Foreign, their words")
	w.otherEntry = otherLinked.ID
	w.otherResume = createProjectToolResume(t, w, other.ID, otherLinked)
	return w
}

func createProjectToolProject(t *testing.T, h *bearerHarness, owner uuid.UUID, slug, name string) project.Project {
	t.Helper()
	created, err := project.NewStore().CreateTx(context.Background(), h.q, owner, slug, projectToolProjectDocument(name), h.clock.Now(), nil)
	if err != nil {
		t.Fatalf("create project %s: %v", slug, err)
	}
	return created
}

func projectToolProjectDocument(name string) project.Document {
	return project.Document{DefaultLanguage: "en", Text: map[string]project.Text{"en": {Name: name}}}
}

func projectToolDocumentArgument(name string) map[string]any {
	return map[string]any{"defaultLanguage": "en", "text": map[string]any{"en": map[string]any{"name": name}}}
}

func projectToolEntry(projectID uuid.UUID, title string) schema.ProjectEntry {
	entry := schema.ProjectEntry{ID: uuid.NewString(), Title: &title}
	if projectID != uuid.Nil {
		link := projectID.String()
		entry.ProjectID = &link
	}
	return entry
}

func projectToolResumeDocument(t *testing.T, entries ...schema.ProjectEntry) schema.Resume {
	t.Helper()
	raw, err := os.ReadFile("../../../../packages/schema/fixtures/minimal.json")
	if err != nil {
		t.Fatalf("read minimal fixture: %v", err)
	}
	var doc schema.Resume
	if decodeErr := json.Unmarshal(raw, &doc); decodeErr != nil {
		t.Fatalf("decode minimal fixture: %v", decodeErr)
	}
	if doc.Content == nil {
		doc.Content = map[string]schema.Section{}
	}
	doc.Content[projectToolSectionKey] = schema.Section{SectionType: schema.Project, ProjectEntries: append([]schema.ProjectEntry{}, entries...)}
	doc.Customization.Layout.Sections.Main = append(append([]string(nil), doc.Customization.Layout.Sections.Main...), projectToolSectionKey)
	return doc
}

func projectToolDocumentMap(t *testing.T, doc schema.Resume) map[string]any {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode resume document: %v", err)
	}
	var out map[string]any
	if decodeErr := json.Unmarshal(raw, &out); decodeErr != nil {
		t.Fatalf("decode resume document: %v", decodeErr)
	}
	return out
}

func createProjectToolResume(t *testing.T, w *projectToolWorld, owner uuid.UUID, entries ...schema.ProjectEntry) resume.Resume {
	t.Helper()
	created, err := w.resumes.Create(context.Background(), owner, "Project tools", projectToolResumeDocument(t, entries...))
	if err != nil {
		t.Fatalf("create resume: %v", err)
	}
	return created
}

func (w *projectToolWorld) foreignLinkedResume(t *testing.T) (resume.Resume, string) {
	t.Helper()
	entry := projectToolEntry(w.carto.ID, "Carto, soon foreign")
	created := createProjectToolResume(t, w, w.h.user.ID, entry)
	tag, err := w.h.pool.Exec(context.Background(),
		"UPDATE resumes SET content = replace(content::text, $1, $2)::jsonb WHERE id = $3 AND user_id = $4",
		w.carto.ID.String(), w.foreign.ID.String(), created.ID, w.h.user.ID)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("link an own entry to another account's project: rows %d, error %v", tag.RowsAffected(), err)
	}
	reloaded, err := w.resumes.Get(context.Background(), w.h.user.ID, created.ID)
	if err != nil {
		t.Fatalf("reload foreign-linked resume: %v", err)
	}
	if links := projectToolLinksOf(reloaded); links[entry.ID] != w.foreign.ID.String() {
		t.Fatalf("foreign-linked resume links = %v, want %s -> %s", links, entry.ID, w.foreign.ID)
	}
	return reloaded, entry.ID
}

func projectToolLinksOf(r resume.Resume) map[string]string {
	links := map[string]string{}
	for _, section := range r.Doc.Content {
		for _, entry := range section.ProjectEntries {
			if entry.ProjectID != nil {
				links[entry.ID] = *entry.ProjectID
			}
		}
	}
	return links
}

func (w *projectToolWorld) links(t *testing.T, resumeID uuid.UUID) map[string]string {
	t.Helper()
	stored, err := w.resumes.Get(context.Background(), w.h.user.ID, resumeID)
	if err != nil {
		t.Fatalf("reload resume %s: %v", resumeID, err)
	}
	return projectToolLinksOf(stored)
}

func (w *projectToolWorld) storedResume(t *testing.T) resume.Resume {
	t.Helper()
	stored, err := w.resumes.Get(context.Background(), w.h.user.ID, w.resume.ID)
	if err != nil {
		t.Fatalf("reload resume: %v", err)
	}
	return stored
}

func (w *projectToolWorld) snapshot(t *testing.T, owner uuid.UUID) string {
	t.Helper()
	var snapshot string
	err := w.h.pool.QueryRow(context.Background(), `SELECT coalesce(string_agg(row_text, '|' ORDER BY row_text), '') FROM (
		SELECT 'project ' || p::text AS row_text FROM projects p WHERE p.user_id = $1
		UNION ALL SELECT 'resume ' || r::text FROM resumes r WHERE r.user_id = $1
		UNION ALL SELECT 'hold ' || s::text FROM project_slug_holds s WHERE s.user_id = $1
	) rows`, owner).Scan(&snapshot)
	if err != nil {
		t.Fatalf("snapshot account %s: %v", owner, err)
	}
	return snapshot
}

func (w *projectToolWorld) resumeCount(t *testing.T, owner uuid.UUID) int64 {
	t.Helper()
	var count int64
	if err := w.h.pool.QueryRow(context.Background(), "SELECT count(*) FROM resumes WHERE user_id = $1", owner).Scan(&count); err != nil {
		t.Fatalf("count resumes: %v", err)
	}
	return count
}

func (w *projectToolWorld) arguments(tool string) map[string]any {
	revision := strconv.FormatInt(w.resume.Revision, 10)
	switch tool {
	case "list_projects":
		return map[string]any{}
	case "get_project":
		return map[string]any{"project_id": w.atlas.ID.String()}
	case "create_project":
		return map[string]any{"idempotency_key": uuid.NewString(), "document": projectToolDocumentArgument("Delta"), "slug": "delta"}
	case "update_project":
		return map[string]any{
			"idempotency_key": uuid.NewString(), "project_id": w.atlas.ID.String(),
			"revision": strconv.FormatInt(w.atlas.Revision, 10), "slug": w.atlas.Slug,
			"document": projectToolDocumentArgument("Atlas renamed"),
		}
	case "delete_project":
		return map[string]any{
			"idempotency_key": uuid.NewString(), "project_id": w.carto.ID.String(),
			"revision": strconv.FormatInt(w.carto.Revision, 10),
		}
	case "link_project":
		return map[string]any{
			"idempotency_key": uuid.NewString(), "resume_id": w.resume.ID.String(), "revision": revision,
			"project_id": w.beacon.ID.String(),
		}
	case "refresh_project_entry":
		return map[string]any{
			"idempotency_key": uuid.NewString(), "resume_id": w.resume.ID.String(), "revision": revision,
			"entry_id": w.linkedEntry,
		}
	default:
		return nil
	}
}

func callProjectTool(session *mcp.ClientSession, name string, arguments map[string]any) (*mcp.CallToolResult, error) {
	return session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
}

func requireProjectToolError(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any, want string) {
	t.Helper()
	result, err := callProjectTool(session, name, arguments)
	if err != nil {
		t.Fatalf("%s: protocol error %v, want tool error %s", name, err, want)
	}
	if !result.IsError || toolErrorText(result) != want {
		t.Fatalf("%s = %q (is error %v), want tool error %s", name, toolErrorText(result), result.IsError, want)
	}
}

func requireProjectToolSuccess(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) []byte {
	t.Helper()
	result, err := callProjectTool(session, name, arguments)
	if err != nil {
		t.Fatalf("%s: protocol error %v, want success", name, err)
	}
	if result.IsError {
		t.Fatalf("%s = tool error %q, want success", name, toolErrorText(result))
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("%s: encode structured content: %v", name, err)
	}
	return raw
}

type projectToolCallOutcome struct {
	result *mcp.CallToolResult
	err    error
	waited bool
}

func narrowGrantWhileCallWaits(t *testing.T, h *bearerHarness, scopes string,
	call func() (*mcp.CallToolResult, error),
) projectToolCallOutcome {
	t.Helper()
	parsed, err := oauthsrv.ParseScopes(scopes)
	if err != nil {
		t.Fatalf("setup: scope registry refuses narrowed scopes %q: %v", scopes, err)
	}
	return changeGrantWhileCallWaits(t, h, func(ctx context.Context, qtx *store.Queries) error {
		_, upsertErr := qtx.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{
			UserID: h.user.ID, ClientID: h.client.ID, Scopes: parsed.String(), CreatedAt: h.clock.Now(), AuthEpoch: h.user.AuthEpoch,
		})
		return upsertErr
	}, call)
}

func revokeGrantWhileCallWaits(t *testing.T, h *bearerHarness, call func() (*mcp.CallToolResult, error)) projectToolCallOutcome {
	t.Helper()
	return changeGrantWhileCallWaits(t, h, func(ctx context.Context, qtx *store.Queries) error {
		now := h.clock.Now()
		if _, revokeErr := qtx.RevokeOAuthGrantForUser(ctx, store.RevokeOAuthGrantForUserParams{
			ID: h.grant.ID, UserID: h.user.ID, RevokedAt: now,
		}); revokeErr != nil {
			return revokeErr
		}
		_, revokeErr := qtx.RevokeOAuthTokensForGrant(ctx, store.RevokeOAuthTokensForGrantParams{GrantID: h.grant.ID, RevokedAt: now})
		return revokeErr
	}, call)
}

func changeGrantWhileCallWaits(t *testing.T, h *bearerHarness, change func(context.Context, *store.Queries) error,
	call func() (*mcp.CallToolResult, error),
) projectToolCallOutcome {
	t.Helper()
	ctx := context.Background()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin owner lock: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				t.Errorf("roll back owner lock: %v", rollbackErr)
			}
		}
	}()
	qtx := store.New(tx)
	if _, lockErr := qtx.LockUserForResumeWrite(ctx, h.user.ID); lockErr != nil {
		t.Fatalf("lock owner row: %v", lockErr)
	}
	if changeErr := change(ctx, qtx); changeErr != nil {
		t.Fatalf("change grant under the owner lock: %v", changeErr)
	}
	var pid int32
	if pidErr := tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); pidErr != nil {
		t.Fatalf("read owner lock backend: %v", pidErr)
	}
	done := make(chan projectToolCallOutcome, 1)
	go func() {
		result, callErr := call()
		done <- projectToolCallOutcome{result: result, err: callErr}
	}()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(projectToolLockTimeout)
	defer deadline.Stop()
	var early *projectToolCallOutcome
	waited := false
	for !waited && early == nil {
		select {
		case outcome := <-done:
			early = &outcome
		case <-ticker.C:
			var blocked int64
			if waitErr := h.pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE $1::int = ANY(pg_blocking_pids(pid))", pid).Scan(&blocked); waitErr != nil {
				t.Fatalf("read lock waiters: %v", waitErr)
			}
			waited = blocked > 0
		case <-deadline.C:
			t.Fatalf("call neither finished nor waited on the owner lock within %s", projectToolLockTimeout)
		}
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		t.Fatalf("commit narrowed grant: %v", commitErr)
	}
	committed = true
	if early != nil {
		return *early
	}
	select {
	case outcome := <-done:
		outcome.waited = true
		return outcome
	case <-time.After(projectToolLockTimeout):
		t.Fatalf("call did not finish within %s after the owner lock was released", projectToolLockTimeout)
		return projectToolCallOutcome{}
	}
}
