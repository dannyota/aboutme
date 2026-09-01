import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  appManifestRows,
  buildArtifacts,
  collectManifestFiles,
  computeSourceDigest,
  parsePublicRoots,
  parseSourceManifest,
  rendererManifestRows,
} from "../../scripts/generate-public-roots.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..", "..");
const registryPath = join(here, "public-roots.v6.json");
const byteSort = (left, right) =>
  Buffer.compare(Buffer.from(left), Buffer.from(right));

const expectedRoots = [
  [".well-known", "go"],
  ["admin", "reserved"],
  ["api", "go"],
  ["app", "nuxt"],
  ["authorize", "nuxt"],
  ["forgot-password", "nuxt"],
  ["healthz", "go"],
  ["_nuxt", "nuxt"],
  ["internal-render", "deny"],
  ["llms.txt", "go"],
  ["login", "nuxt"],
  ["mcp", "go"],
  ["oauth", "go"],
  ["people", "reserved"],
  ["print", "deny"],
  ["readyz", "go"],
  ["register", "nuxt"],
  ["reset-password", "nuxt"],
  ["robots.txt", "go"],
  ["sitemap.xml", "go"],
  ["u", "reserved"],
  ["verify-email", "nuxt"],
];

test("the v6 registry is closed and authority ordered", async () => {
  const registryRaw = await readFile(registryPath);
  const registry = parsePublicRoots(registryRaw);
  assert.deepEqual(
    registry.roots.map(({ root, dispatch }) => [root, dispatch]),
    expectedRoots,
  );

  const invalidFixtures = [
    "public-roots-duplicate.json",
    "public-roots-extra.json",
    "public-roots-missing.json",
    "public-roots-nondeterministic.json",
    "public-roots-unknown-dispatch.json",
  ];
  for (const name of invalidFixtures) {
    const raw = await readFile(join(here, "testdata", name));
    assert.throws(() => parsePublicRoots(raw), name);
  }

  assert.throws(() =>
    parsePublicRoots(Buffer.from(JSON.stringify({ ...registry, version: 5 }))),
  );
  assert.throws(() =>
    parsePublicRoots(
      Buffer.from(
        JSON.stringify({
          ...registry,
          roots: registry.roots.map((row, index) =>
            index === 0 ? { ...row, extra: true } : row,
          ),
        }),
      ),
    ),
  );
  assert.throws(() =>
    parsePublicRoots(
      Buffer.from(
        registryRaw
          .toString("utf8")
          .replace('"version": 6', '"version": 5,\n  "version": 6'),
      ),
    ),
  );
});

test("generation is byte-stable and covers every consumer", async () => {
  const registry = parsePublicRoots(await readFile(registryPath));
  const first = buildArtifacts(registry);
  const second = buildArtifacts(registry);
  assert.deepEqual(first, second);
  assert.deepEqual(Object.keys(first).sort(), [
    "apps/server/internal/publicroots/generated.go",
    "apps/web/app/public-roots.generated.ts",
    "apps/web/test/public-roots.generated.test.ts",
    "deploy/caddy/public-roots.generated.caddy",
    "deploy/caddy/testdata/public-roots.generated.json",
  ]);
});

test("source manifests are closed and digests use sorted regular files", async () => {
  const appPath = join(here, "app-build-sources.v1.json");
  const rendererPath = join(here, "renderer-build-sources.v1.json");
  const app = parseSourceManifest(await readFile(appPath), appManifestRows);
  const renderer = parseSourceManifest(
    await readFile(rendererPath),
    rendererManifestRows,
  );

  const appFiles = await collectManifestFiles(repoRoot, app);
  const rendererFiles = await collectManifestFiles(repoRoot, renderer);
  assert.deepEqual(appFiles, [...appFiles].sort(byteSort));
  assert.deepEqual(rendererFiles, [...rendererFiles].sort(byteSort));
  assert.match(
    await computeSourceDigest(repoRoot, appPath, appManifestRows),
    /^sha256:[0-9a-f]{64}$/,
  );
  assert.match(
    await computeSourceDigest(repoRoot, rendererPath, rendererManifestRows),
    /^sha256:[0-9a-f]{64}$/,
  );
});

test("source digest matches the frozen length-prefixed byte stream", async () => {
  const root = await mkdtemp(join(tmpdir(), "aboutme-source-digest-"));
  await mkdir(join(root, "tree"));
  await writeFile(join(root, "a.txt"), "A");
  await writeFile(join(root, "tree", "b.txt"), "BC");
  const rows = [
    { path: "a.txt", kind: "file" },
    { path: "tree", kind: "recursive" },
  ];
  const manifestPath = join(root, "manifest.json");
  await writeFile(
    manifestPath,
    '{"version":1,"roots":[{"path":"a.txt","kind":"file"},{"path":"tree","kind":"recursive"}]}\n',
  );

  assert.equal(
    await computeSourceDigest(root, manifestPath, rows),
    "sha256:b236b6a3bfc6bc2b01388316d369c75008bd3c56400de24a8d606198141a4965",
  );
});

test("manifest traversal rejects symlinks and paths outside its closed roots", async () => {
  const root = await mkdtemp(join(tmpdir(), "aboutme-publicroots-"));
  await mkdir(join(root, "tree"));
  await writeFile(join(root, "tree", "data.txt"), "data");
  await symlink(join(root, "tree", "data.txt"), join(root, "tree", "link.txt"));

  const rows = [{ path: "tree", kind: "recursive" }];
  const manifest = parseSourceManifest(
    Buffer.from(`${JSON.stringify({ version: 1, roots: rows })}\n`),
    rows,
  );
  await assert.rejects(() => collectManifestFiles(root, manifest), /symlink/);

  for (const path of ["/tmp/outside", "../outside", "tree/../tree"]) {
    assert.throws(() =>
      parseSourceManifest(
        Buffer.from(
          JSON.stringify({
            version: 1,
            roots: [{ path, kind: "recursive" }],
          }),
        ),
        rows,
      ),
    );
  }
});
