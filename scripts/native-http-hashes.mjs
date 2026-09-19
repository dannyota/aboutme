import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const hydrationPattern =
  /<script type="module" src="(\/_nuxt\/assets\/public-resume\.mjs\?v=([a-f0-9]{16}))"><\/script>/gu;

function sha256(value) {
  return createHash("sha256").update(value).digest("hex");
}

function hydrationScript(html) {
  const scripts = [...html.matchAll(hydrationPattern)];
  if (scripts.length !== 1) {
    throw new Error("expected one versioned public hydration script");
  }
  const [markup, path, version] = scripts[0];
  return { markup, path, version };
}

function escapeExpression(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/gu, "\\$&");
}

export function publicHydrationPath(html) {
  return hydrationScript(html).path;
}

export function canonicalizePublicHydration(asset, html, checkoutRoot) {
  const { markup, path, version } = hydrationScript(html);
  const rawAssetSHA256 = sha256(asset);
  if (!rawAssetSHA256.startsWith(version)) {
    throw new Error(
      "public hydration script version does not match its raw fingerprint",
    );
  }

  const root = escapeExpression(checkoutRoot);
  const metadataPattern = new RegExp(`(\\["__file",\\s*")${root}(?=\\/)`, "gu");
  let metadataCount = 0;
  const canonicalAsset = asset.replace(metadataPattern, (match, prefix) => {
    metadataCount += 1;
    return `${prefix}<checkout-root>`;
  });
  if (metadataCount === 0) {
    throw new Error(
      "public hydration script did not contain checkout-root __file metadata",
    );
  }

  const assetSHA256 = sha256(canonicalAsset);
  const canonicalVersion = assetSHA256.slice(0, 16);
  const canonicalHTML = html.replace(
    markup,
    markup.replace(
      path,
      `/_nuxt/assets/public-resume.mjs?v=${canonicalVersion}`,
    ),
  );
  return {
    assetSHA256,
    canonicalVersion,
    htmlSHA256: sha256(canonicalHTML),
    rawAssetSHA256,
  };
}

function run() {
  const args = process.argv.slice(2);
  if (args[0] === "--script-path" && args.length === 2) {
    process.stdout.write(
      `${publicHydrationPath(readFileSync(args[1], "utf8"))}\n`,
    );
    return;
  }
  if (args.length !== 3) {
    throw new Error(
      "usage: native-http-hashes.mjs [--script-path html] | asset html checkout-root",
    );
  }
  const [assetPath, htmlPath, checkoutRoot] = args;
  const result = canonicalizePublicHydration(
    readFileSync(assetPath, "utf8"),
    readFileSync(htmlPath, "utf8"),
    checkoutRoot,
  );
  process.stdout.write(`${JSON.stringify(result)}\n`);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  run();
}
