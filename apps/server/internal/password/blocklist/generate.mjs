#!/usr/bin/env node
// Deterministic generator for the bundled common-password blocklist
// (Phase PA D2). It reads only the vendored pinned source, normalizes every
// candidate with Unicode NFC, hashes the exact UTF-8 bytes with SHA-256, and
// writes a strictly-increasing digest blob plus a provenance manifest.
//
// Usage:
//   node generate.mjs             # (re)write digests.bin and manifest.json
//   node generate.mjs --check     # verify both files match a fresh in-memory
//                                 #   generation; read-only, no network
//
// The source is SecLists commit eedc5117b3f506d874d033c18786a218e7cec34c,
// file Passwords/Common-Credentials/100k-most-used-passwords-NCSC.txt (MIT).
// Runtime code never reads the plaintext source; it only reads digests.bin.

import { createHash } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const MAGIC = 'ABMEBL01';
const DIGEST_BYTES = 32; // SHA-256

const SOURCE = {
  repository: 'https://github.com/danielmiessler/SecLists',
  commit: 'eedc5117b3f506d874d033c18786a218e7cec34c',
  path: 'Passwords/Common-Credentials/100k-most-used-passwords-NCSC.txt',
  sha256: 'c2e5696882c603b76bb67a47ee970897e5a76fc4c3f5547abe3d0ca340c576e0',
  lines: 99840,
  license: 'MIT',
};

const dir = path.dirname(fileURLToPath(import.meta.url));
const sourcePath = path.join(dir, 'source', '100k-most-used-passwords-NCSC.txt');
const digestsPath = path.join(dir, 'digests.bin');
const manifestPath = path.join(dir, 'manifest.json');

function sha256(buffer) {
  return createHash('sha256').update(buffer).digest('hex');
}

// decodeSource reads the vendored source as strict UTF-8 and returns the
// decoded string. Invalid UTF-8 aborts generation (the source is pinned and
// must never be silently repaired with replacement characters).
async function decodeSource(buffer) {
  const decoder = new TextDecoder('utf-8', { fatal: true });
  let text;
  try {
    text = decoder.decode(buffer);
  } catch {
    throw new Error('blocklist source is not valid UTF-8; refusing to generate');
  }
  if (text.includes('\u0000')) {
    throw new Error('blocklist source contains a NUL byte; refusing to generate');
  }
  return text;
}

// buildDigests turns the decoded source text into the sorted unique list of
// SHA-256 digest Buffers. It skips the source's single empty line, NFC-
// normalizes each candidate, and deduplicates after normalization.
function buildDigests(text) {
  const seen = new Map();
  for (const line of text.split('\n')) {
    if (line.length === 0) {
      continue; // the pinned source has exactly one empty line
    }
    const normalized = line.normalize('NFC');
    if (!seen.has(normalized)) {
      seen.set(normalized, createHash('sha256').update(normalized, 'utf8').digest());
    }
  }
  const digests = [...seen.values()];
  digests.sort(Buffer.compare);
  for (let i = 1; i < digests.length; i++) {
    if (Buffer.compare(digests[i - 1], digests[i]) === 0) {
      throw new Error('internal error: duplicate digest after dedupe');
    }
  }
  return digests;
}

function encodeDigests(digests) {
  const header = Buffer.alloc(12);
  header.write(MAGIC, 0, MAGIC.length, 'ascii');
  header.writeUInt32BE(digests.length, 8);
  return Buffer.concat([header, ...digests]);
}

function buildManifest(digests, digestsBin) {
  return {
    magic: MAGIC,
    format: 'sha256-digests-v1',
    algorithm: 'SHA-256',
    normalization: 'NFC',
    source: SOURCE,
    digests: digests.length,
    digests_sha256: sha256(digestsBin),
  };
}

async function generate() {
  const buffer = await readFile(sourcePath);
  const sourceSha = sha256(buffer);
  if (sourceSha !== SOURCE.sha256) {
    throw new Error(
      `blocklist source SHA-256 mismatch: got ${sourceSha}, want ${SOURCE.sha256}; ` +
        'the vendored source was mutated — restore the pinned file and retry',
    );
  }
  const text = await decodeSource(buffer);
  const digests = buildDigests(text);
  const digestsBin = encodeDigests(digests);
  const manifest = buildManifest(digests, digestsBin);
  return { digestsBin, manifest };
}

async function runCheck() {
  const { digestsBin, manifest } = await generate();
  const [existingBin, existingManifest] = await Promise.all([
    readFile(digestsPath),
    readFile(manifestPath, 'utf8'),
  ]);
  const ok =
    existingBin.equals(digestsBin) &&
    existingManifest === `${JSON.stringify(manifest, null, 2)}\n`;
  if (!ok) {
    throw new Error('blocklist artifacts are out of date; run `node generate.mjs` to regenerate');
  }
}

async function runWrite() {
  const { digestsBin, manifest } = await generate();
  await writeFile(digestsPath, digestsBin);
  await writeFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`);
}

const mode = process.argv[2];
try {
  if (mode === '--check') {
    await runCheck();
  } else if (mode === undefined) {
    await runWrite();
  } else {
    throw new Error(`unknown argument: ${mode}`);
  }
} catch (err) {
  console.error(err instanceof Error ? err.message : err);
  process.exit(1);
}
