#!/usr/bin/env node

import { createHash } from "node:crypto";
import {
  lstat,
  mkdir,
  readFile,
  readdir,
  realpath,
  writeFile,
} from "node:fs/promises";
import { dirname, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const scriptPath = fileURLToPath(import.meta.url);
const repositoryRoot = resolve(dirname(scriptPath), "..");
const registryRelativePath = "packages/publicroots/public-roots.v6.json";
const appManifestRelativePath =
  "packages/publicroots/app-build-sources.v1.json";
const rendererManifestRelativePath =
  "packages/publicroots/renderer-build-sources.v1.json";

const publicRootRows = [
  { root: ".well-known", dispatch: "go" },
  { root: "admin", dispatch: "reserved" },
  { root: "api", dispatch: "go" },
  { root: "app", dispatch: "nuxt" },
  { root: "authorize", dispatch: "nuxt" },
  { root: "forgot-password", dispatch: "nuxt" },
  { root: "healthz", dispatch: "go" },
  { root: "_nuxt", dispatch: "nuxt" },
  { root: "internal-render", dispatch: "deny" },
  { root: "llms.txt", dispatch: "go" },
  { root: "login", dispatch: "nuxt" },
  { root: "mcp", dispatch: "go" },
  { root: "oauth", dispatch: "go" },
  { root: "people", dispatch: "reserved" },
  { root: "print", dispatch: "deny" },
  { root: "readyz", dispatch: "go" },
  { root: "register", dispatch: "nuxt" },
  { root: "reset-password", dispatch: "nuxt" },
  { root: "robots.txt", dispatch: "go" },
  { root: "sitemap.xml", dispatch: "go" },
  { root: "u", dispatch: "reserved" },
  { root: "verify-email", dispatch: "nuxt" },
];

export const appManifestRows = [
  { path: "apps/server/cmd/server", kind: "recursive" },
  { path: "apps/server/go.mod", kind: "file" },
  { path: "apps/server/go.sum", kind: "file" },
  { path: "apps/server/internal", kind: "recursive" },
  { path: registryRelativePath, kind: "file" },
  { path: "packages/schema/gen/go", kind: "recursive" },
];

export const rendererManifestRows = [
  { path: "apps/web/app", kind: "recursive" },
  { path: "apps/web/nuxt.config.ts", kind: "file" },
  { path: "apps/web/package-lock.json", kind: "file" },
  { path: "apps/web/package.json", kind: "file" },
  { path: "apps/web/public", kind: "recursive" },
  { path: "apps/web/server", kind: "recursive" },
  { path: registryRelativePath, kind: "file" },
];

function compareBytes(left, right) {
  return Buffer.compare(Buffer.from(left), Buffer.from(right));
}

function requireClosedObject(value, keys, label) {
  if (value === null || Array.isArray(value) || typeof value !== "object") {
    throw new Error(`${label} must be an object`);
  }
  const actual = Object.keys(value).sort(compareBytes);
  const expected = [...keys].sort(compareBytes);
  if (
    actual.length !== expected.length ||
    actual.some((key, index) => key !== expected[index])
  ) {
    throw new Error(`${label} properties must be exactly ${keys.join(", ")}`);
  }
}

function rejectDuplicateObjectKeys(source, label) {
  const stack = [];
  for (let index = 0; index < source.length; index += 1) {
    const token = source[index];
    if (token === "{") {
      stack.push({ type: "object", keys: new Set() });
      continue;
    }
    if (token === "[") {
      stack.push({ type: "array" });
      continue;
    }
    if (token === "}" || token === "]") {
      stack.pop();
      continue;
    }
    if (token !== '"') continue;

    const start = index;
    index += 1;
    while (index < source.length) {
      if (source[index] === "\\") {
        index += 2;
        continue;
      }
      if (source[index] === '"') break;
      index += 1;
    }
    let next = index + 1;
    while (next < source.length && /\s/u.test(source[next])) next += 1;
    if (source[next] !== ":") continue;

    const context = stack.at(-1);
    if (context?.type !== "object") continue;
    const key = JSON.parse(source.slice(start, index + 1));
    if (context.keys.has(key)) {
      throw new Error(`${label} repeats object key ${JSON.stringify(key)}`);
    }
    context.keys.add(key);
  }
}

function parseJSON(raw, label) {
  try {
    const source = new TextDecoder("utf-8", { fatal: true }).decode(raw);
    rejectDuplicateObjectKeys(source, label);
    return JSON.parse(source);
  } catch (error) {
    throw new Error(`${label} must be valid UTF-8 JSON`, { cause: error });
  }
}

export function parsePublicRoots(raw) {
  const value = parseJSON(raw, "public roots registry");
  requireClosedObject(value, ["version", "roots"], "public roots registry");
  if (!Number.isInteger(value.version) || value.version !== 6) {
    throw new Error("public roots registry version must be integer 6");
  }
  if (
    !Array.isArray(value.roots) ||
    value.roots.length !== publicRootRows.length
  ) {
    throw new Error(
      `public roots registry must contain ${publicRootRows.length} rows`,
    );
  }

  const seen = new Set();
  value.roots.forEach((row, index) => {
    requireClosedObject(row, ["root", "dispatch"], `public roots row ${index}`);
    if (typeof row.root !== "string" || row.root.length === 0) {
      throw new Error(
        `public roots row ${index} root must be a nonempty string`,
      );
    }
    if (!["reserved", "go", "nuxt", "deny"].includes(row.dispatch)) {
      throw new Error(`public roots row ${index} has an unknown dispatch`);
    }
    if (seen.has(row.root)) {
      throw new Error(`public roots registry repeats ${row.root}`);
    }
    seen.add(row.root);

    const expected = publicRootRows[index];
    if (row.root !== expected.root || row.dispatch !== expected.dispatch) {
      throw new Error(
        `public roots row ${index} does not match the v6 authority`,
      );
    }
  });
  return value;
}

function validRelativePath(path) {
  return (
    typeof path === "string" &&
    path.length > 0 &&
    !path.startsWith("/") &&
    !path.startsWith("\\") &&
    !path.includes("\\") &&
    path
      .split("/")
      .every((part) => part !== "" && part !== "." && part !== "..")
  );
}

export function parseSourceManifest(raw, expectedRows) {
  const value = parseJSON(raw, "source manifest");
  requireClosedObject(value, ["version", "roots"], "source manifest");
  if (!Number.isInteger(value.version) || value.version !== 1) {
    throw new Error("source manifest version must be integer 1");
  }
  if (
    !Array.isArray(value.roots) ||
    value.roots.length !== expectedRows.length
  ) {
    throw new Error(
      `source manifest must contain ${expectedRows.length} roots`,
    );
  }

  value.roots.forEach((row, index) => {
    requireClosedObject(row, ["path", "kind"], `source manifest root ${index}`);
    if (!validRelativePath(row.path)) {
      throw new Error(`source manifest root ${index} has an unsafe path`);
    }
    if (row.kind !== "file" && row.kind !== "recursive") {
      throw new Error(`source manifest root ${index} has an unknown kind`);
    }
    const expected = expectedRows[index];
    if (row.path !== expected.path || row.kind !== expected.kind) {
      throw new Error(
        `source manifest root ${index} does not match its authority`,
      );
    }
    if (index > 0 && compareBytes(value.roots[index - 1].path, row.path) >= 0) {
      throw new Error(
        "source manifest roots must be raw-byte sorted and unique",
      );
    }
  });
  return value;
}

function insideRepository(root, target) {
  return target === root || target.startsWith(`${root}${sep}`);
}

async function requireUnlinkedPath(root, relativePath) {
  const target = resolve(root, relativePath);
  if (!insideRepository(root, target)) {
    throw new Error(`source path escapes repository: ${relativePath}`);
  }
  const resolved = await realpath(target);
  if (resolved !== target) {
    throw new Error(`source path contains a symlink: ${relativePath}`);
  }
  return target;
}

async function collectDirectory(root, absoluteDirectory, files) {
  const entries = await readdir(absoluteDirectory, { withFileTypes: true });
  entries.sort((left, right) => compareBytes(left.name, right.name));
  for (const entry of entries) {
    const absolute = resolve(absoluteDirectory, entry.name);
    const repoRelative = relative(root, absolute).split(sep).join("/");
    if (entry.isSymbolicLink()) {
      throw new Error(`source manifest encountered symlink: ${repoRelative}`);
    }
    if (entry.isDirectory()) {
      await collectDirectory(root, absolute, files);
      continue;
    }
    if (!entry.isFile()) {
      throw new Error(
        `source manifest encountered non-regular path: ${repoRelative}`,
      );
    }
    files.push(repoRelative);
  }
}

export async function collectManifestFiles(repoRoot, manifest) {
  const root = await realpath(repoRoot);
  const files = [];
  for (const row of manifest.roots) {
    const absolute = await requireUnlinkedPath(root, row.path);
    const stat = await lstat(absolute);
    if (stat.isSymbolicLink()) {
      throw new Error(`source manifest root is a symlink: ${row.path}`);
    }
    if (row.kind === "file") {
      if (!stat.isFile()) {
        throw new Error(
          `source manifest root is not a regular file: ${row.path}`,
        );
      }
      files.push(row.path);
      continue;
    }
    if (!stat.isDirectory()) {
      throw new Error(`source manifest root is not a directory: ${row.path}`);
    }
    await collectDirectory(root, absolute, files);
  }
  files.sort(compareBytes);
  for (let index = 1; index < files.length; index += 1) {
    if (files[index - 1] === files[index]) {
      throw new Error(`source manifests roots overlap at ${files[index]}`);
    }
  }
  return files;
}

function lengthBuffer(bytes, width) {
  const output = Buffer.alloc(width);
  if (width === 4) {
    if (bytes > 0xffff_ffff) throw new Error("source path is too long");
    output.writeUInt32BE(bytes);
  } else {
    output.writeBigUInt64BE(BigInt(bytes));
  }
  return output;
}

export async function computeSourceDigest(
  repoRoot,
  manifestPath,
  expectedRows,
) {
  const rawManifest = await readFile(manifestPath);
  const manifest = parseSourceManifest(rawManifest, expectedRows);
  const files = await collectManifestFiles(repoRoot, manifest);
  const hash = createHash("sha256");
  hash.update(Buffer.from("aboutme.source-manifest.v1\0", "ascii"));
  hash.update(lengthBuffer(rawManifest.length, 8));
  hash.update(rawManifest);
  for (const path of files) {
    const pathBytes = Buffer.from(path);
    const fileBytes = await readFile(resolve(repoRoot, path));
    hash.update(lengthBuffer(pathBytes.length, 4));
    hash.update(pathBytes);
    hash.update(lengthBuffer(fileBytes.length, 8));
    hash.update(fileBytes);
  }
  return `sha256:${hash.digest("hex")}`;
}

function goArtifact(registry) {
  const dispatchNames = ["reserved", "go", "nuxt", "deny"].map(
    (dispatch) => `Dispatch${dispatch[0].toUpperCase()}${dispatch.slice(1)}`,
  );
  const dispatchNameWidth = Math.max(
    ...dispatchNames.map((name) => name.length),
  );
  const dispatchConstants = ["reserved", "go", "nuxt", "deny"]
    .map(
      (dispatch, index) =>
        `\t${dispatchNames[index].padEnd(dispatchNameWidth)} Dispatch = "${dispatch}"`,
    )
    .join("\n");
  const routes = registry.roots
    .map(
      ({ root, dispatch }) =>
        `\t{Root: "${root}", Dispatch: Dispatch${dispatch[0].toUpperCase()}${dispatch.slice(1)}},`,
    )
    .join("\n");
  const reservedKeys = registry.roots.map(({ root }) => `"${root}":`);
  const reservedKeyWidth = Math.max(...reservedKeys.map((key) => key.length));
  const reserved = reservedKeys
    .map((key) => `\t${key.padEnd(reservedKeyWidth)} {},`)
    .join("\n");
  return `// Code generated by scripts/generate-public-roots.mjs; DO NOT EDIT.\n\npackage publicroots\n\n// Dispatch is the edge owner for one immutable public root.\ntype Dispatch string\n\nconst (\n${dispatchConstants}\n)\n\n// Route is one authority-ordered public-root dispatch row.\ntype Route struct {\n\tRoot     string\n\tDispatch Dispatch\n}\n\n// Routes is the immutable version-6 public-root registry.\nvar Routes = [...]Route{\n${routes}\n}\n\nvar reserved = map[string]struct{}{\n${reserved}\n}\n\n// Reserved reports whether root is unavailable as a public resume slug.\nfunc Reserved(root string) bool {\n\t_, ok := reserved[root]\n\treturn ok\n}\n`;
}

function tsArtifact(registry) {
  const rows = registry.roots
    .map(
      ({ root, dispatch }) => `  { root: '${root}', dispatch: '${dispatch}' },`,
    )
    .join("\n");
  return `// Code generated by scripts/generate-public-roots.mjs; DO NOT EDIT.\n\nexport type PublicRootDispatch = 'reserved' | 'go' | 'nuxt' | 'deny';\n\nexport interface PublicRootRoute {\n  readonly root: string;\n  readonly dispatch: PublicRootDispatch;\n}\n\nexport const publicRootRoutes = [\n${rows}\n] as const satisfies readonly PublicRootRoute[];\n\nconst reservedPublicRoots: ReadonlySet<string> = new Set(\n  publicRootRoutes.map(({ root }) => root),\n);\n\nexport function isReservedPublicRoot(root: string): boolean {\n  return reservedPublicRoots.has(root);\n}\n`;
}

function tsTestArtifact(registry) {
  const rows = registry.roots
    .map(
      ({ root, dispatch }) =>
        `      { root: '${root}', dispatch: '${dispatch}' },`,
    )
    .join("\n");
  return `// Code generated by scripts/generate-public-roots.mjs; DO NOT EDIT.\n\nimport { describe, expect, it } from 'vitest';\n\nimport {\n  isReservedPublicRoot,\n  publicRootRoutes,\n} from '../app/public-roots.generated';\n\ndescribe('generated public-root registry', () => {\n  it('matches the immutable v6 source in authority order', () => {\n    expect(publicRootRoutes).toEqual([\n${rows}\n    ]);\n    expect(publicRootRoutes).toHaveLength(22);\n  });\n\n  it('reserves every registered root and no unknown root', () => {\n    for (const { root } of publicRootRoutes) {\n      expect(isReservedPublicRoot(root)).toBe(true);\n    }\n    expect(isReservedPublicRoot('unregistered-root')).toBe(false);\n  });\n});\n`;
}

function caddyPaths(registry, dispatch) {
  return registry.roots
    .filter((row) => row.dispatch === dispatch)
    .flatMap(({ root }) => {
      const paths = [`/${root}`, `/${root}/*`];
      if (
        root.length >= 4 &&
        root.length <= 30 &&
        /^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(root)
      ) {
        paths.push(`/${root}.md`);
      }
      return paths;
    })
    .join(" ");
}

function caddyArtifact(registry) {
  return `# Code generated by scripts/generate-public-roots.mjs; DO NOT EDIT.\n\n@aboutme_public_deny path ${caddyPaths(registry, "deny")}\nhandle @aboutme_public_deny {\n\trespond 404\n}\n\n@aboutme_fixed_go path ${caddyPaths(registry, "go")}\nhandle @aboutme_fixed_go {\n\treverse_proxy server:8080 {\n\t\theader_up X-Real-IP {http.request.remote.host}\n\t}\n}\n\n@aboutme_fixed_nuxt path ${caddyPaths(registry, "nuxt")}\nhandle @aboutme_fixed_nuxt {\n\treverse_proxy web:3000\n}\n\n@aboutme_reserved path ${caddyPaths(registry, "reserved")}\nhandle @aboutme_reserved {\n\trespond 404\n}\n\n@aboutme_public_slug expression \`({http.request.uri.path}.matches('^/[a-z0-9]+(-[a-z0-9]+)*(\\\\.md)?$') && ((!{http.request.uri.path}.endsWith('.md') && size({http.request.uri.path}) >= 5 && size({http.request.uri.path}) <= 31) || ({http.request.uri.path}.endsWith('.md') && size({http.request.uri.path}) >= 8 && size({http.request.uri.path}) <= 34)))\`\nhandle @aboutme_public_slug {\n\treverse_proxy server:8080 {\n\t\theader_up X-Real-IP {http.request.remote.host}\n\t}\n}\n`;
}

export function buildArtifacts(registry) {
  return {
    "apps/server/internal/publicroots/generated.go": goArtifact(registry),
    "apps/web/app/public-roots.generated.ts": tsArtifact(registry),
    "apps/web/test/public-roots.generated.test.ts": tsTestArtifact(registry),
    "deploy/caddy/public-roots.generated.caddy": caddyArtifact(registry),
    "deploy/caddy/testdata/public-roots.generated.json": `${JSON.stringify(registry, null, 2)}\n`,
  };
}

async function verifyOrWriteArtifacts(artifacts, check) {
  for (const [path, content] of Object.entries(artifacts)) {
    const absolute = resolve(repositoryRoot, path);
    if (check) {
      let actual;
      try {
        actual = await readFile(absolute, "utf8");
      } catch (error) {
        throw new Error(`generated output is missing: ${path}`, {
          cause: error,
        });
      }
      if (actual !== content)
        throw new Error(`generated output is stale: ${path}`);
      continue;
    }
    await mkdir(dirname(absolute), { recursive: true });
    await writeFile(absolute, content);
  }
}

async function main() {
  const args = process.argv.slice(2);
  if (args.length > 1 || (args.length === 1 && args[0] !== "--check")) {
    throw new Error("usage: node scripts/generate-public-roots.mjs [--check]");
  }
  const check = args[0] === "--check";
  const registry = parsePublicRoots(
    await readFile(resolve(repositoryRoot, registryRelativePath)),
  );
  await verifyOrWriteArtifacts(buildArtifacts(registry), check);

  const appDigest = await computeSourceDigest(
    repositoryRoot,
    resolve(repositoryRoot, appManifestRelativePath),
    appManifestRows,
  );
  const rendererDigest = await computeSourceDigest(
    repositoryRoot,
    resolve(repositoryRoot, rendererManifestRelativePath),
    rendererManifestRows,
  );
  process.stdout.write(`APP_BUILD_DIGEST=${appDigest}\n`);
  process.stdout.write(`PUBLIC_RENDERER_BUILD_DIGEST=${rendererDigest}\n`);
}

if (process.argv[1] && resolve(process.argv[1]) === scriptPath) {
  main().catch((error) => {
    process.stderr.write(`generate-public-roots: ${error.message}\n`);
    process.exitCode = 1;
  });
}
