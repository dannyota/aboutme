// Tests for the blocklist generator: provenance, byte-exact output format,
// determinism, the absence of a plaintext runtime artifact, and --check
// mutation detection. Runs with Node's built-in test runner:
//
//   cd apps/server && node --test internal/password/blocklist/generate.test.mjs
//
// No network access: every check reads only the vendored source and the
// generated artifacts.

import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const dir = path.dirname(fileURLToPath(import.meta.url));
const sourcePath = path.join(dir, 'source', '100k-most-used-passwords-NCSC.txt');
const digestsPath = path.join(dir, 'digests.bin');
const manifestPath = path.join(dir, 'manifest.json');
const generatePath = path.join(dir, 'generate.mjs');

const MAGIC = 'ABMEBL01';
const SOURCE_SHA256 = 'c2e5696882c603b76bb67a47ee970897e5a76fc4c3f5547abe3d0ca340c576e0';
const SOURCE_LINES = 99840;
const DIGEST_COUNT = 99839;
const DIGEST_BYTES = 32;

function sha256(buffer) {
  return createHash('sha256').update(buffer).digest('hex');
}

function runGenerate(...args) {
  return spawnSync(process.execPath, [generatePath, ...args], {
    cwd: dir,
    encoding: 'utf8',
    timeout: 120_000,
  });
}

function readDigests() {
  const bin = readFileSync(digestsPath);
  const magic = bin.subarray(0, 8).toString('ascii');
  const count = bin.readUInt32BE(8);
  const digests = [];
  for (let i = 0; i < count; i++) {
    digests.push(bin.subarray(12 + i * DIGEST_BYTES, 12 + (i + 1) * DIGEST_BYTES));
  }
  return { bin, magic, count, digests };
}

test('source is the pinned SecLists commit with the exact SHA-256 and line count', () => {
  const source = readFileSync(sourcePath);
  assert.equal(sha256(source), SOURCE_SHA256, 'vendored source SHA-256 mismatch');
  const lines = source.toString('utf8').split('\n').length - 1; // trailing newline
  assert.equal(lines, SOURCE_LINES, 'vendored source line count mismatch');
  const license = readFileSync(path.join(dir, 'source', 'LICENSE'), 'utf8');
  assert.match(license, /MIT License/, 'vendored license is not the MIT license');
});

test('manifest records provenance and the exact digest count', () => {
  const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'));
  assert.equal(manifest.magic, MAGIC);
  assert.equal(manifest.algorithm, 'SHA-256');
  assert.equal(manifest.normalization, 'NFC');
  assert.equal(manifest.source.commit, 'eedc5117b3f506d874d033c18786a218e7cec34c');
  assert.equal(
    manifest.source.path,
    'Passwords/Common-Credentials/100k-most-used-passwords-NCSC.txt',
  );
  assert.equal(manifest.source.sha256, SOURCE_SHA256);
  assert.equal(manifest.source.lines, SOURCE_LINES);
  assert.equal(manifest.source.license, 'MIT');
  assert.equal(manifest.digests, DIGEST_COUNT);
  assert.equal(manifest.digests_sha256, sha256(readFileSync(digestsPath)));
});

test('digests.bin has magic, big-endian count, exact size, and strictly increasing digests', () => {
  const { bin, magic, count, digests } = readDigests();
  assert.equal(magic, MAGIC);
  assert.equal(count, DIGEST_COUNT);
  assert.equal(bin.length, 12 + DIGEST_COUNT * DIGEST_BYTES);
  for (let i = 1; i < digests.length; i++) {
    assert.ok(
      Buffer.compare(digests[i - 1], digests[i]) < 0,
      `digest ${i - 1} is not strictly less than digest ${i}`,
    );
  }
});

test('known common passwords are present as NFC-normalized SHA-256 digests', () => {
  const { digests } = readDigests();
  const present = (candidate) => {
    const digest = createHash('sha256').update(candidate.normalize('NFC'), 'utf8').digest();
    let lo = 0;
    let hi = digests.length - 1;
    while (lo <= hi) {
      const mid = (lo + hi) >> 1;
      const cmp = Buffer.compare(digests[mid], digest);
      if (cmp === 0) return true;
      if (cmp < 0) lo = mid + 1;
      else hi = mid - 1;
    }
    return false;
  };
  assert.ok(present('password'), 'password digest missing');
  assert.ok(present('123456'), '123456 digest missing');
  assert.ok(present('letmein'), 'letmein digest missing');
});

test('digests.bin is a plaintext-free runtime artifact', () => {
  const { bin } = readDigests();
  // Exact size means nothing beyond the header and digests is present.
  assert.equal(bin.length, 12 + DIGEST_COUNT * DIGEST_BYTES);
  // The plaintext of the most common source line must not appear verbatim.
  assert.ok(!bin.includes(Buffer.from('password\n')), 'plaintext password leaked into digests.bin');
  assert.ok(!bin.includes(Buffer.from('123456\n')), 'plaintext 123456 leaked into digests.bin');
});

test('generation is deterministic', () => {
  const before = sha256(readFileSync(digestsPath));
  const first = runGenerate();
  assert.equal(first.status, 0, `generate failed: ${first.stderr}`);
  const after = sha256(readFileSync(digestsPath));
  assert.equal(after, before, 'regeneration changed digests.bin');
});

test('--check passes on the committed artifacts', () => {
  const check = runGenerate('--check');
  assert.equal(check.status, 0, `--check failed: ${check.stderr}`);
});

test('--check detects a mutated digests.bin', () => {
  const original = readFileSync(digestsPath);
  const mutated = Buffer.from(original);
  mutated[100] ^= 0xff;
  writeFileSync(digestsPath, mutated);
  try {
    const check = runGenerate('--check');
    assert.notEqual(check.status, 0, '--check passed despite a mutated digests.bin');
  } finally {
    writeFileSync(digestsPath, original);
  }
});

test('--check detects a mutated manifest', () => {
  const original = readFileSync(manifestPath, 'utf8');
  writeFileSync(manifestPath, original.replace('99839', '99999'));
  try {
    const check = runGenerate('--check');
    assert.notEqual(check.status, 0, '--check passed despite a mutated manifest');
  } finally {
    writeFileSync(manifestPath, original);
  }
});

test('--check detects a mutated vendored source', () => {
  const original = readFileSync(sourcePath);
  writeFileSync(sourcePath, Buffer.concat([original, Buffer.from('mutated\n')]));
  try {
    const check = runGenerate('--check');
    assert.notEqual(check.status, 0, '--check passed despite a mutated source');
  } finally {
    writeFileSync(sourcePath, original);
  }
});
