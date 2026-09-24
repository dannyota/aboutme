# Link previews

Status: open; waits for the owner to schedule it. Two releases, numbered when they ship. Platform pages and resumes ship separately because their privacy rules differ.

Current code: platform pages set title, description, canonical URL, Open Graph, and Twitter tags in `apps/web/app/composables/useSiteSeo.ts`. Published resumes get head tags and the 1200 by 630 share image ([ADR 0032](../adr/0032-public-share-image.md)) from `apps/server/internal/publicapi/html.go`. No release has verified the cards in real apps.

|Release|Outcome|Acceptance|
|-|-|-|
|Platform-page previews|Homepage and public template pages|Localized metadata and a crawlable branded image, verified in the apps below.|
|Published-resume previews|Resume cards|Resume metadata and the existing share image verified in the apps below, including private and revoked states.|

A later release may refine the share-image crop if the checks show it is unreadable. Keep that visual change apart from metadata fixes.

## Rules

- Resume cards derive only from the current public snapshot: the sanitized projection plus validated public head settings ([ADR 0042](../adr/0042-public-page-title-and-favicon.md)). Keep an owner-set public title without adding those settings to public JSON. No account email, private field, hidden entry, or storage key in a card.
- Serve Open Graph and X card tags with absolute HTTPS image URLs, image dimensions, content type, and alt text. Update the public renderer and the Go HTML validator together so the metadata allowlist accepts only the intended escaped values.
- Crawlers get Vietnamese by default: one canonical URL, language in a cookie. Do not promise per-language platform cards without a separate URL design. Resume metadata follows the resume language.
- Cover Vietnamese and English, custom and default public titles, long names and headlines, with and without photo, discovery off, PDF download off, renamed URLs, unpublish, and delete. Keep live link, discoverability, and download permission separate; changing that contract needs a design decision first.
- Unpublishing stops fresh origin reads. Do not promise that it clears third-party caches; record the measured behavior and the refresh tools each platform offers.
- Check cards in Facebook, LinkedIn, Zalo, and one more chat app, using fictional published resumes, inspectors, and unsent drafts. Never send or publish a message as part of a check.
- References: [Open Graph](https://ogp.me/), [LinkedIn sharing](https://www.linkedin.com/help/linkedin/answer/a521928/making-your-website-shareable-on-linkedin?lang=en). They define metadata, not a guarantee that every app shows the same card.
