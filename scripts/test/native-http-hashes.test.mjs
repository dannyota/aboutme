import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { canonicalizePublicHydration } from "../native-http-hashes.mjs";

const temporary = mkdtempSync(join(tmpdir(), "aboutme-native-http-hashes-"));

try {
  const rootA = "/checkout-a";
  const rootB = "/checkout-b";
  const source = (root) =>
    [
      "const Resume={",
      `["__file", "${root}/apps/web/app/components/resume/EntryHeader.vue"],`,
      'render(){return "same"}',
      "};",
    ].join("");
  const assetA = join(temporary, "public-resume-a.mjs");
  const assetB = join(temporary, "public-resume-b.mjs");
  writeFileSync(assetA, source(rootA));
  writeFileSync(assetB, source(rootB));

  const versionA = createHash("sha256")
    .update(readFileSync(assetA))
    .digest("hex")
    .slice(0, 16);
  const versionB = createHash("sha256")
    .update(readFileSync(assetB))
    .digest("hex")
    .slice(0, 16);
  const htmlA = `<!doctype html><script type="module" src="/_nuxt/assets/public-resume.mjs?v=${versionA}"></script>`;
  const htmlB = `<!doctype html><script type="module" src="/_nuxt/assets/public-resume.mjs?v=${versionB}"></script>`;

  const canonicalA = canonicalizePublicHydration(
    readFileSync(assetA, "utf8"),
    htmlA,
    rootA,
  );
  const canonicalB = canonicalizePublicHydration(
    readFileSync(assetB, "utf8"),
    htmlB,
    rootB,
  );
  assert.equal(canonicalA.assetSHA256, canonicalB.assetSHA256);
  assert.equal(canonicalA.htmlSHA256, canonicalB.htmlSHA256);
  assert.match(canonicalA.canonicalVersion, /^[a-f0-9]{16}$/u);

  const ordinaryLinkHTML = `<a href="/_nuxt/assets/public-resume.mjs?v=${versionA}">asset</a>${htmlA}`;
  const ordinaryLink = canonicalizePublicHydration(
    readFileSync(assetA, "utf8"),
    ordinaryLinkHTML,
    rootA,
  );
  const expectedOrdinaryLinkHTML = `<a href="/_nuxt/assets/public-resume.mjs?v=${versionA}">asset</a><!doctype html><script type="module" src="/_nuxt/assets/public-resume.mjs?v=${canonicalA.canonicalVersion}"></script>`;
  assert.equal(
    ordinaryLink.htmlSHA256,
    createHash("sha256").update(expectedOrdinaryLinkHTML).digest("hex"),
  );

  assert.throws(
    () =>
      canonicalizePublicHydration(
        `${readFileSync(assetB, "utf8")}changed`,
        htmlB,
        rootB,
      ),
    /fingerprint/u,
  );
  const changedAsset = `${readFileSync(assetB, "utf8")}changed`;
  const changedVersion = createHash("sha256")
    .update(changedAsset)
    .digest("hex")
    .slice(0, 16);
  const changedHTML = `<!doctype html><script type="module" src="/_nuxt/assets/public-resume.mjs?v=${changedVersion}"></script>`;
  const changed = canonicalizePublicHydration(changedAsset, changedHTML, rootB);
  assert.notEqual(canonicalA.assetSHA256, changed.assetSHA256);
  assert.notEqual(canonicalA.htmlSHA256, changed.htmlSHA256);
  assert.notEqual(
    canonicalA.htmlSHA256,
    canonicalizePublicHydration(
      readFileSync(assetA, "utf8"),
      `${htmlA}<p>changed HTML</p>`,
      rootA,
    ).htmlSHA256,
  );
} finally {
  rmSync(temporary, { force: true, recursive: true });
}
