package resumeapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
)

func TestDeleteRecoveryProofNoPhotoRequiresExactResumeTombstoneGenerationAndRecord(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created, err := h.resumes.Create(h.ctx, h.userID, "Recover", publishCompleteDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	slug := "recover-" + uuid.NewString()[:8]
	published := h.mutationRequest(t, http.MethodPost, apiResumePath+"/"+created.ID.String()+"/publish", strings.NewReader(`{"slug":"`+slug+`","live":true,"downloadEnabled":false,"seoGeoEnabled":false}`), created.Revision, uuid.NewString())
	if published.status != http.StatusOK {
		t.Fatalf("publish = %d %s", published.status, published.body)
	}
	before, err := h.queries.GetPublicState(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	key := uuid.New()
	deleted := h.mutationRequest(t, http.MethodDelete, apiResumePath+"/"+created.ID.String(), nil, created.Revision+1, key.String())
	if deleted.status != http.StatusNoContent {
		t.Fatalf("delete = %d %s", deleted.status, deleted.body)
	}
	resolver := deleteRecoveryResolver(t, h, created.ID, created.Revision+1, before.DiscoveryGeneration, key, "")
	proof, err := resolver.Resolve(context.Background())
	if err != nil || proof.Disposition != publicstate.RecoveryCommitted || len(proof.State.RetiredResumes) != 1 || proof.State.RetiredResumes[0] != created.ID || proof.State.DiscoveryGeneration == nil || *proof.State.DiscoveryGeneration != before.DiscoveryGeneration+1 {
		t.Fatalf("no-photo recovery proof = %#v err=%v", proof, err)
	}
	if _, err := h.pool.Exec(h.ctx, `DELETE FROM slug_tombstones WHERE slug = $1`, slug); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background()); err == nil {
		t.Fatal("recovery accepted missing tombstone")
	}
}

func TestDeleteRecoveryProofPhotoRequiresExactMediaDeletionJob(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created, err := h.resumes.Create(h.ctx, h.userID, "Recover photo", publishCompleteDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	uploaded := h.uploadPhotoRequest(t, created.ID, created.Revision, uuid.NewString(), "photo.png", makePhotoPNG(t))
	if uploaded.status != http.StatusOK {
		t.Fatalf("upload = %d %s", uploaded.status, uploaded.body)
	}
	slug := "photo-" + uuid.NewString()[:8]
	published := h.mutationRequest(t, http.MethodPost, apiResumePath+"/"+created.ID.String()+"/publish", strings.NewReader(`{"slug":"`+slug+`","live":true,"downloadEnabled":false,"seoGeoEnabled":false}`), created.Revision+1, uuid.NewString())
	if published.status != http.StatusOK {
		t.Fatalf("publish = %d %s", published.status, published.body)
	}
	current, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil || current.Doc.PersonalDetails.Photo == nil {
		t.Fatalf("current photo = %#v err=%v", current.Doc.PersonalDetails.Photo, err)
	}
	before, err := h.queries.GetPublicState(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	key := uuid.New()
	deleted := h.mutationRequest(t, http.MethodDelete, apiResumePath+"/"+created.ID.String(), nil, current.Revision, key.String())
	if deleted.status != http.StatusNoContent {
		t.Fatalf("delete = %d %s", deleted.status, deleted.body)
	}
	resolver := deleteRecoveryResolver(t, h, created.ID, current.Revision, before.DiscoveryGeneration, key, current.Doc.PersonalDetails.Photo.Key)
	if proof, err := resolver.Resolve(context.Background()); err != nil || proof.Disposition != publicstate.RecoveryCommitted {
		t.Fatalf("photo recovery proof = %#v err=%v", proof, err)
	}
	if _, err := h.pool.Exec(h.ctx, `DELETE FROM media_deletion_jobs WHERE resume_id = $1 AND object_key = $2`, created.ID, current.Doc.PersonalDetails.Photo.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background()); err == nil || errors.Is(err, resume.ErrNotFound) {
		t.Fatalf("recovery accepted missing exact media job: %v", err)
	}
}

func TestDeleteRecoveryProofNeverSluggedRetiresWithoutTombstoneOrGeneration(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	key := uuid.New()
	deleted := h.mutationRequest(t, http.MethodDelete, apiResumePath+"/"+created.ID.String(), nil, created.Revision, key.String())
	if deleted.status != http.StatusNoContent {
		t.Fatalf("delete = %d %s", deleted.status, deleted.body)
	}
	resolver := neverSluggedDeleteRecoveryResolver(h, created.ID, created.Revision, key, "")
	proof, err := resolver.Resolve(context.Background())
	if err != nil || proof.Disposition != publicstate.RecoveryCommitted || len(proof.State.RetiredResumes) != 1 || proof.State.RetiredResumes[0] != created.ID || proof.State.DiscoveryGeneration != nil {
		t.Fatalf("never-slugged recovery = %#v err=%v", proof, err)
	}
	if _, err := h.pool.Exec(h.ctx, `DELETE FROM idempotency_records WHERE user_id = $1 AND route = $2 AND idempotency_key = $3`, h.userID, resolver.identity.Operation, key); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background()); err == nil {
		t.Fatal("recovery accepted mixed absent-record/deleted-row state")
	}
	resolver.pool = nil
	if _, err := resolver.Resolve(context.Background()); err == nil {
		t.Fatal("recovery accepted unavailable pool")
	}
}

func TestDeleteRecoveryProofNeverSluggedPhotoRequiresExactMediaJob(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	uploaded := h.uploadPhotoRequest(t, created.ID, created.Revision, uuid.NewString(), "photo.png", makePhotoPNG(t))
	if uploaded.status != http.StatusOK {
		t.Fatalf("upload = %d %s", uploaded.status, uploaded.body)
	}
	current, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil || current.Doc.PersonalDetails.Photo == nil {
		t.Fatalf("current photo = %#v err=%v", current.Doc.PersonalDetails.Photo, err)
	}
	key := uuid.New()
	deleted := h.mutationRequest(t, http.MethodDelete, apiResumePath+"/"+created.ID.String(), nil, current.Revision, key.String())
	if deleted.status != http.StatusNoContent {
		t.Fatalf("delete = %d %s", deleted.status, deleted.body)
	}
	resolver := neverSluggedDeleteRecoveryResolver(h, created.ID, current.Revision, key, current.Doc.PersonalDetails.Photo.Key)
	if proof, err := resolver.Resolve(context.Background()); err != nil || proof.Disposition != publicstate.RecoveryCommitted {
		t.Fatalf("never-slugged photo recovery = %#v err=%v", proof, err)
	}
	if _, err := h.pool.Exec(h.ctx, `DELETE FROM media_deletion_jobs WHERE resume_id = $1 AND object_key = $2`, created.ID, current.Doc.PersonalDetails.Photo.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background()); err == nil {
		t.Fatal("recovery accepted missing never-slugged photo job")
	}
	resolver.pool = nil
	if _, err := resolver.Resolve(context.Background()); err == nil {
		t.Fatal("recovery accepted unavailable pool for never-slugged photo")
	}
}

func TestPublishRenameRecoveryRequiresExactRowClaimTombstoneAndResponse(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created, err := h.resumes.Create(h.ctx, h.userID, "Recover rename", publishCompleteDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	oldSlug := "recover-old-" + uuid.NewString()[:8]
	first := h.mutationRequest(t, http.MethodPost, apiResumePath+"/"+created.ID.String()+"/publish", strings.NewReader(`{"slug":"`+oldSlug+`","live":true,"downloadEnabled":false,"seoGeoEnabled":false}`), created.Revision, uuid.NewString())
	if first.status != http.StatusOK {
		t.Fatalf("initial publish = %d %s", first.status, first.body)
	}
	before, err := h.queries.GetPublicState(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(h.ctx, `UPDATE sessions SET reauthenticated_at = now() WHERE id = $1`, h.session.ID); err != nil {
		t.Fatal(err)
	}
	newSlug := "recover-new-" + uuid.NewString()[:8]
	key := uuid.New()
	renamed := h.mutationRequest(t, http.MethodPost, apiResumePath+"/"+created.ID.String()+"/publish", strings.NewReader(`{"slug":"`+newSlug+`","live":true,"downloadEnabled":false,"seoGeoEnabled":false}`), created.Revision+1, key.String())
	if renamed.status != http.StatusOK {
		t.Fatalf("rename = %d %s", renamed.status, renamed.body)
	}
	tombstone, err := h.queries.GetSlugTombstoneForUpdate(h.ctx, oldSlug)
	if err != nil {
		t.Fatal(err)
	}
	operation := hexDigest(operationHash(http.MethodPost, "publishResume", []string{"resume_id", created.ID.String()}))
	hash := requestHash(docmigrate.CurrentVersion, strconv.FormatInt(created.Revision+1, 10), nil, []byte(`{"slug":"`+newSlug+`","live":true,"downloadEnabled":false,"seoGeoEnabled":false}`))
	resolver := mutationRecovery{pool: h.pool, identity: mutationIdentity{UserID: h.userID, Operation: operation, Key: key, RequestHash: hash}, plan: publicstate.Plan{DiscoveryGeneration: &before.DiscoveryGeneration, Resumes: []publicstate.ResumeTarget{{ID: created.ID, ExpectedRevision: created.Revision + 1, Class: publicstate.Revoking}}}, publish: &publishRecoveryProof{ResumeID: created.ID, Effective: currentPublish{Slug: &newSlug, Live: true, Revision: created.Revision + 2}, OldSlug: &oldSlug, ReleasedAt: tombstone.ReleasedAt}}
	if proof, err := resolver.Resolve(context.Background()); err != nil || proof.Disposition != publicstate.RecoveryCommitted {
		t.Fatalf("rename recovery = %#v err=%v", proof, err)
	}
	if _, err := h.pool.Exec(h.ctx, `DELETE FROM slug_tombstones WHERE slug = $1`, oldSlug); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background()); err == nil {
		t.Fatal("rename recovery accepted missing old-slug tombstone")
	}
}

func TestPublishInitialClaimRecoveryRequiresExactClaimAndStoredResponse(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created, err := h.resumes.Create(h.ctx, h.userID, "Recover initial claim", publishCompleteDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	before, err := h.queries.GetPublicState(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	slug := "recover-initial-" + uuid.NewString()[:8]
	key := uuid.New()
	body := `{"slug":"` + slug + `","live":true,"downloadEnabled":true,"seoGeoEnabled":false}`
	claimed := h.mutationRequest(t, http.MethodPost, apiResumePath+"/"+created.ID.String()+"/publish", strings.NewReader(body), created.Revision, key.String())
	if claimed.status != http.StatusOK {
		t.Fatalf("initial claim = %d %s", claimed.status, claimed.body)
	}
	resolver := publishRecoveryResolver(h, created.ID, created.Revision, before.DiscoveryGeneration, key, body, currentPublish{Slug: &slug, Live: true, DownloadEnabled: true, Revision: created.Revision + 1}, true)
	if proof, err := resolver.Resolve(context.Background()); err != nil || proof.Disposition != publicstate.RecoveryCommitted {
		t.Fatalf("initial-claim recovery = %#v err=%v", proof, err)
	}
	if _, err := h.pool.Exec(h.ctx, `UPDATE idempotency_records SET response_status = 204 WHERE user_id = $1 AND route = $2 AND idempotency_key = $3`, h.userID, resolver.identity.Operation, key); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background()); err == nil {
		t.Fatal("initial-claim recovery accepted wrong stored response")
	}
}

func TestPublishFlagChangeRecoveryRequiresExactEffectiveRow(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created, err := h.resumes.Create(h.ctx, h.userID, "Recover flags", publishCompleteDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	slug := "recover-flags-" + uuid.NewString()[:8]
	initial := h.mutationRequest(t, http.MethodPost, apiResumePath+"/"+created.ID.String()+"/publish", strings.NewReader(`{"slug":"`+slug+`","live":true,"downloadEnabled":true,"seoGeoEnabled":false}`), created.Revision, uuid.NewString())
	if initial.status != http.StatusOK {
		t.Fatalf("initial publish = %d %s", initial.status, initial.body)
	}
	key := uuid.New()
	body := `{"slug":"` + slug + `","live":true,"downloadEnabled":false,"seoGeoEnabled":false}`
	changed := h.mutationRequest(t, http.MethodPost, apiResumePath+"/"+created.ID.String()+"/publish", strings.NewReader(body), created.Revision+1, key.String())
	if changed.status != http.StatusOK {
		t.Fatalf("flag change = %d %s", changed.status, changed.body)
	}
	resolver := publishRecoveryResolver(h, created.ID, created.Revision+1, 0, key, body, currentPublish{Slug: &slug, Live: true, DownloadEnabled: false, Revision: created.Revision + 2}, false)
	if proof, err := resolver.Resolve(context.Background()); err != nil || proof.Disposition != publicstate.RecoveryCommitted {
		t.Fatalf("flag-change recovery = %#v err=%v", proof, err)
	}
	if _, err := h.pool.Exec(h.ctx, `UPDATE resumes SET download_enabled = true WHERE id = $1`, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background()); err == nil {
		t.Fatal("flag-change recovery accepted wrong effective row/claim state")
	}
}

func publishRecoveryResolver(h *resumeAPITestHarness, resumeID uuid.UUID, revision, discovery int64, key uuid.UUID, body string, effective currentPublish, global bool) mutationRecovery {
	operation := hexDigest(operationHash(http.MethodPost, "publishResume", []string{"resume_id", resumeID.String()}))
	hash := requestHash(docmigrate.CurrentVersion, strconv.FormatInt(revision, 10), nil, []byte(body))
	plan := publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: resumeID, ExpectedRevision: revision, Class: publicstate.NonDraining}}}
	if global {
		plan.DiscoveryGeneration = &discovery
	}
	return mutationRecovery{pool: h.pool, identity: mutationIdentity{UserID: h.userID, Operation: operation, Key: key, RequestHash: hash}, plan: plan, publish: &publishRecoveryProof{ResumeID: resumeID, Effective: effective}}
}

func neverSluggedDeleteRecoveryResolver(h *resumeAPITestHarness, resumeID uuid.UUID, revision int64, key uuid.UUID, photoKey string) mutationRecovery {
	operation := hexDigest(operationHash(http.MethodDelete, "deleteResume", []string{"resume_id", resumeID.String()}))
	hash := requestHash(docmigrate.CurrentVersion, strconv.FormatInt(revision, 10), nil, nil)
	return mutationRecovery{pool: h.pool, identity: mutationIdentity{UserID: h.userID, Operation: operation, Key: key, RequestHash: hash}, plan: publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: resumeID, ExpectedRevision: revision, Class: publicstate.NonDraining}}}, retire: true, delete: &deleteRecoveryProof{ResumeID: resumeID, PhotoKey: photoKey}}
}

func deleteRecoveryResolver(t *testing.T, h *resumeAPITestHarness, resumeID uuid.UUID, revision, discovery int64, key uuid.UUID, photoKey string) mutationRecovery {
	t.Helper()
	operation := hexDigest(operationHash(http.MethodDelete, "deleteResume", []string{"resume_id", resumeID.String()}))
	hash := requestHash(docmigrate.CurrentVersion, strconv.FormatInt(revision, 10), nil, nil)
	tombstone, err := h.queries.GetSlugTombstoneForUpdate(h.ctx, findDeleteRecoverySlug(t, h, resumeID))
	if err != nil {
		t.Fatal(err)
	}
	slug := tombstone.Slug
	return mutationRecovery{pool: h.pool, identity: mutationIdentity{UserID: h.userID, Operation: operation, Key: key, RequestHash: hash}, plan: publicstate.Plan{DiscoveryGeneration: &discovery, Resumes: []publicstate.ResumeTarget{{ID: resumeID, ExpectedRevision: revision, Class: publicstate.Revoking}}}, retire: true, delete: &deleteRecoveryProof{ResumeID: resumeID, Slug: &slug, ReleasedAt: tombstone.ReleasedAt, PhotoKey: photoKey}}
}

func findDeleteRecoverySlug(t *testing.T, h *resumeAPITestHarness, resumeID uuid.UUID) string {
	t.Helper()
	var slug string
	if err := h.pool.QueryRow(h.ctx, `SELECT slug FROM slug_tombstones WHERE released_by_user_id = $1 ORDER BY released_at DESC LIMIT 1`, h.userID).Scan(&slug); err != nil {
		t.Fatal(err)
	}
	return slug
}
