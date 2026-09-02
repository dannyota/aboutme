# Task 02 — Preview photo fallback and photo-free seed

**Acceptance:** AC-UI-004.

**Depends on:** T00. Runs before T01; it uses no Tailwind class.

**Owned paths:** T02 paths in `file-structure.md`.

## Contract

- The dev seed strips `personalDetails.photo` before inserting the sample
  resume. `cmd/dev-seed/testdata/full.json` stays a byte-exact copy of the
  schema fixture (the existing drift test enforces it), so the removal happens
  in Go, on the decoded document.
- `EditorPreview.vue` renders whenever it has a document. When photo metadata
  exists and `photoUrl` is undefined, it passes a projection without
  `personalDetails.photo` to `ResumeDocument` and exposes `photoState`
  (`'ready' | 'loading' | 'unavailable' | 'none'`) through a new `photoRead`
  prop so T07 can show the state in the toolbar. Until T07 lands, the preview
  shows an inline `<p role="status">` with the state text.
- Existing behavior kept: the paged render context, the estimated page count
  observer, the render-failure notice, and never rendering the stored key.

**Interfaces:**

- Consumes: `ResumeRecord.photoRead` (`PhotoReadState` from
  `app/stores/resumes.ts`: `kind` is
  `'none' | 'loading' | 'ready' | 'suspended'`).
- Produces: `EditorPreview` props `document: Resume`, `lng: string`,
  `photoUrl?: string`, `photoRead?: PhotoReadState`; exported helper
  `previewProjection(document: Resume, photoUrl: string | undefined): Resume`
  and
  `photoStateFor(read: PhotoReadState | undefined, hasPhoto: boolean): PhotoState`
  in `app/components/editor/previewProjection.ts`. T07 consumes both.

## Hook changes

- Text `Preview is waiting for the authorized photo.` is removed. New status
  texts: `Photo is loading. The preview is shown without it.` and
  `Photo unavailable. The preview is shown without it. Open the Photo panel to retry.`

## TDD cycle

- [ ] **Seed RED.** In `apps/server/cmd/dev-seed/seed_test.go`, add a literal
      byte-for-byte assertion that the returned personal-details object equals
      the fixture's personal-details bytes with only its top-level `photo`
      member removed. Also decode the result and assert the remaining required
      keys exist. The test must fail on a map marshal that reorders keys.

  ```go
  // Derive `want` as a literal from the checked-in fixture, not with the
  // production remover or a map marshal. Compare `personalDetails` directly
  // with `want`, then decode only for the key-presence assertions.
  ```

- [ ] Run it and watch it fail on the photo key:

  ```sh
  cd apps/server && go test ./cmd/dev-seed -run TestSplitResumeDocStripsPhoto -count=1
  ```

- [ ] **Seed GREEN.** Strip the top-level `photo` member from the decoded
      `personalDetails` raw JSON without round-tripping the object through a Go
      map. Preserve every remaining byte and key order, return a copied slice,
      and return a closed decode error for malformed input. The seed has no
      media backend, so the resulting sample must not carry a photo key.

  Rerun the test to GREEN, then the package with the live database:

  ```sh
  cd apps/server && TEST_DATABASE_URL='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable' go test ./cmd/dev-seed -race -count=1
  ```

- [ ] **Preview RED.** Create `test/editor/preview-projection.test.ts`:

  ```ts
  import { describe, expect, it } from "vitest";

  import {
    photoStateFor,
    previewProjection,
  } from "../../app/components/editor/previewProjection";
  import { acceptedFixture } from "./fixture";

  describe("previewProjection", () => {
    it("drops photo metadata when no authorized URL exists", () => {
      const { document } = acceptedFixture();
      document.personalDetails.photo = { key: "resumes/r/photo-x.jpg" };
      const projected = previewProjection(document, undefined);
      expect(projected.personalDetails.photo).toBeUndefined();
      expect(projected.personalDetails.fullName).toBe(
        document.personalDetails.fullName,
      );
      expect(document.personalDetails.photo).toBeDefined();
    });

    it("keeps the document identical when a URL exists", () => {
      const { document } = acceptedFixture();
      document.personalDetails.photo = { key: "resumes/r/photo-x.jpg" };
      expect(previewProjection(document, "data:image/png;base64,AA==")).toBe(
        document,
      );
    });
  });

  describe("photoStateFor", () => {
    it.each([
      [undefined, false, "none"],
      [{ kind: "none" }, true, "unavailable"],
      [{ kind: "loading", binding: "k", generation: 1 }, true, "loading"],
      [
        {
          kind: "suspended",
          binding: "k",
          generation: 1,
          reason: "read-failed",
        },
        true,
        "unavailable",
      ],
      [
        {
          kind: "ready",
          binding: "k",
          generation: 1,
          etag: '"e"',
          dataUrl: "d",
        },
        true,
        "ready",
      ],
    ] as const)("maps %o with photo=%s to %s", (read, hasPhoto, expected) => {
      expect(photoStateFor(read, hasPhoto)).toBe(expected);
    });
  });
  ```

  In `test/editor/editor-preview.test.ts`, replace the "suspends the complete
  preview" case with:

  ```ts
  it("renders without the photo while the read is pending", () => {
    const accepted = acceptedFixture();
    accepted.document.personalDetails.photo = {
      key: "resumes/resume-1/private-object.jpg",
    };
    const wrapper = shallowMount(EditorPreview, {
      props: {
        document: accepted.document,
        lng: "en",
        photoRead: { kind: "loading", binding: "k", generation: 1 },
      },
    });

    const renderer = wrapper.getComponent({ name: "ResumeDocument" });
    expect(renderer.props("document").personalDetails.photo).toBeUndefined();
    expect(renderer.props("context")).toEqual({ lng: "en", mode: "paged" });
    expect(wrapper.get('[role="status"]').text()).toContain("Photo is loading");
    expect(wrapper.html()).not.toContain("private-object.jpg");
  });

  it("names the unavailable state and the photo panel", () => {
    const accepted = acceptedFixture();
    accepted.document.personalDetails.photo = { key: "resumes/resume-1/p.jpg" };
    const wrapper = shallowMount(EditorPreview, {
      props: {
        document: accepted.document,
        lng: "en",
        photoRead: {
          kind: "suspended",
          binding: "k",
          generation: 1,
          reason: "read-failed",
        },
      },
    });

    expect(wrapper.findComponent({ name: "ResumeDocument" }).exists()).toBe(
      true,
    );
    expect(wrapper.get('[role="status"]').text()).toContain(
      "Photo unavailable",
    );
  });
  ```

- [ ] Run and watch both files fail:

  ```sh
  cd apps/web && npx vitest run test/editor/preview-projection.test.ts test/editor/editor-preview.test.ts
  ```

- [ ] **Preview GREEN.** Create `app/components/editor/previewProjection.ts`:

  ```ts
  import type { Resume } from "@aboutme/schema";

  import type { PhotoReadState } from "../../stores/resumes";

  export type PhotoState = "ready" | "loading" | "unavailable" | "none";

  /** The document the preview renders: photo metadata only with its URL. */
  export function previewProjection(
    document: Resume,
    photoUrl: string | undefined,
  ): Resume {
    if (
      document.personalDetails.photo === undefined ||
      photoUrl !== undefined
    ) {
      return document;
    }
    const { photo: _photo, ...personalDetails } = document.personalDetails;
    return { ...document, personalDetails };
  }

  export function photoStateFor(
    read: PhotoReadState | undefined,
    hasPhoto: boolean,
  ): PhotoState {
    if (!hasPhoto) return "none";
    switch (read?.kind) {
      case "ready":
        return "ready";
      case "loading":
        return "loading";
      default:
        return "unavailable";
    }
  }
  ```

  In `EditorPreview.vue`, add the `photoRead?: PhotoReadState` prop, replace
  `requiresPhoto`/`canRender` with:

  ```ts
  const projected = computed(() =>
    previewProjection(props.document, props.photoUrl),
  );
  const photoState = computed(() =>
    photoStateFor(
      props.photoRead,
      props.document.personalDetails.photo !== undefined,
    ),
  );
  const photoNotice = computed(() => {
    switch (photoState.value) {
      case "loading":
        return "Photo is loading. The preview is shown without it.";
      case "unavailable":
        return (
          "Photo unavailable. The preview is shown without it. " +
          "Open the Photo panel to retry."
        );
      default:
        return "";
    }
  });
  ```

  Render
  `<p v-if="photoNotice !== ''" class="editor-preview__notice" role="status">{{ photoNotice }}</p>`
  above the document, drop the `v-if="!canRender"` branch, and pass
  `:document="projected"`. Keep the existing `photoUrl` context logic.

- [ ] In `EditorShell.vue`, pass `:photo-read="record.photoRead"` to
      `EditorPreview` (one attribute; T07 owns the rest of the file later).

- [ ] Rerun to GREEN, then:

  ```sh
  cd apps/web && npx vitest run test/editor && make -C ../.. web-lint web-typecheck
  ```

- [ ] Reseed the native database so the stored sample loses its photo:

  ```sh
  .dev/bin/dev-seed cleanup --database-url 'postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable'
  make dev-seed
  ```

  Then open
  `http://localhost:20080/app/resumes/5d000000-0000-4000-8000-000000000002` and
  confirm the preview shows pages and `.dev/server.log` gains no
  `resume photo key invariant failed` line.

## Adversarial checklist

- Owner: the T02 cases in `adversarial-coverage.md`.

## Handoff

Report RED and GREEN outputs, the seed test output, the reseed result, and the
new prop and helper names. Suggested commits:
`fix(dev): seed the sample resume without a photo reference` and
`fix(editor): render the preview while the photo read is pending`.
