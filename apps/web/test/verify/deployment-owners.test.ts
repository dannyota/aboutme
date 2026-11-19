import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

import {
  type ComponentName,
  decodeDeploymentDocument,
  type DeploymentDocument,
  pageState,
  type PageState,
  recomputeRelease,
  recomputeSummary,
} from '../../app/utils/deploymentDocument';
import {
  commandTargets,
  imageNamespaces,
  verifyImageCommand,
} from '../../app/utils/verifyView';

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type Loose = any;

const fixtureRoot = resolve(
  import.meta.dirname,
  '../../e2e/fixtures/deployment',
);

function raw(name: string): Loose {
  return JSON.parse(readFileSync(resolve(fixtureRoot, `${name}.json`), 'utf8'));
}

function owned<T>(value: T, owner: string): T {
  return JSON.parse(JSON.stringify(value).replaceAll('dannyota', owner)) as T;
}

function decoded(value: unknown): DeploymentDocument {
  const result = decodeDeploymentDocument(value);
  if (result.kind !== 'document') throw new Error(`decoded ${result.kind}`);
  return result.document;
}

function stateOf(value: Loose): PageState {
  return pageState(
    { kind: 'decoded', result: decodeDeploymentDocument(value) },
    Date.parse(value.observed_at) + 30_000,
  );
}

function commandFor(
  document: DeploymentDocument,
  component: ComponentName,
): string {
  const targets = commandTargets(document)
    .filter((target) => target.component === component);
  return verifyImageCommand(targets[targets.length - 1]!).copy;
}

function expectedCommand(
  owner: string,
  repository: string,
  digest: string,
  version: string,
): string {
  return [
    'gh attestation verify \\',
    `  oci://ghcr.io/${owner}/aboutme-${repository}@${digest} \\`,
    `  --repo ${owner}/aboutme \\`,
    `  --signer-workflow ${owner}/aboutme/.github/workflows/`
    + 'release-images.yml \\',
    `  --source-ref refs/tags/${version} \\`,
    '  --deny-self-hosted-runners',
  ].join('\n');
}

function image(value: Loose, index: number): Loose {
  return value.components[index].running_images[0];
}

function moveComponent(value: Loose, index: number, owner: string): void {
  const entry = value.components[index];
  entry.image = entry.image.replace('dannyota', owner);
  for (const running of entry.running_images) {
    running.signature.signer_workflow = running.signature.signer_workflow
      ?.replace('dannyota', owner) ?? null;
    running.links.build = running.links.build
      ?.replace('dannyota', owner) ?? null;
  }
}

const todayDecodes: Readonly<Record<string, string>> = {
  'verified':
    'd902d5439695951866e93fbc9dcba2551ce8023560df4102b28874de5878587e',
  'rolling-out':
    'be3b6c6804a3186d490989b3f38be672754d8e687c0e50a6e076929c3c48a5ea',
  'unverified':
    '7fc33862cc49bd50450429e7a204a75d477094417400dca39ae05d4eed17bf49',
  'mismatch-not-found':
    'fbdffbc99324a5989b599a1a015acd56408aca3d4a257fe0923ca290dc40aa01',
  'mismatch-invalid':
    '7316f10736e130c5a5aa5729c007ff53d4779f9ce2996fba449f3c23bbcccb3f',
  'mismatch-missing':
    'a453495a19c4b2204c1c54c30dd00745faa9e35947108140e5ee9f8d2054bd4f',
  'mismatch-summary':
    'ca829c440830138b2f149d9d1e3d3c0f84dc99b5cb178cd56b12d8dc6949c32f',
  'stale':
    'a6d6bda16d7bf77bc853fba69e3bf8121bc39ba8c1d9223e97216a86c19900c0',
  'outdated':
    '380a8f32d6aee2d84af5f2833657a66188a3d3e6edcb102f48e842ba1f13ab01',
};

describe('dannyota documents', () => {
  it.each(Object.entries(todayDecodes))(
    'decodes the %s fixture byte for byte as before',
    (name, digest) => {
      const output = JSON.stringify(decodeDeploymentDocument(raw(name)));
      expect(createHash('sha256').update(output).digest('hex')).toBe(digest);
    },
  );

  it('renders dannyota commands and namespace as before', () => {
    const value = raw('verified');
    const document = decoded(value);
    expect(commandFor(document, 'server')).toBe(expectedCommand(
      'dannyota', 'server', image(value, 0).digest, 'v0.6.0'));
    expect(imageNamespaces(document)).toEqual(['ghcr.io/dannyota/']);
  });
});

describe('dntmakers documents', () => {
  it('decodes a dntmakers document and renders dntmakers commands', () => {
    const value = owned(raw('verified'), 'dntmakers');
    const document = decoded(value);
    expect(document).toEqual(owned(decoded(raw('verified')), 'dntmakers'));
    expect(stateOf(value)).toMatchObject({
      kind: 'verified', release: { version: 'v0.6.0' },
    });
    for (const [index, name] of (['server', 'web', 'caddy'] as const)
      .entries()) {
      expect(commandFor(document, name)).toBe(expectedCommand(
        'dntmakers', name, image(value, index).digest, 'v0.6.0'));
    }
    expect(imageNamespaces(document)).toEqual(['ghcr.io/dntmakers/']);
  });

  it('decodes the dntmakers rollout and unverified fixtures', () => {
    for (const name of ['rolling-out', 'unverified', 'mismatch-missing']) {
      const value = owned(raw(name), 'dntmakers');
      expect(decoded(value))
        .toEqual(owned(decoded(raw(name)), 'dntmakers'));
      expect(stateOf(value).kind).toBe(stateOf(raw(name)).kind);
    }
  });
});

describe('a rollback keeping a newer dntmakers maintenance image', () => {
  it('stays verified and gives each image its own owner', () => {
    const value = raw('rollback');
    const document = decoded(value);
    expect(stateOf(value)).toMatchObject({
      kind: 'verified', release: { version: 'v0.6.0' },
    });
    expect(commandFor(document, 'server')).toBe(expectedCommand(
      'dannyota', 'server', image(value, 0).digest, 'v0.6.0'));
    expect(commandFor(document, 'maintenance')).toBe(expectedCommand(
      'dntmakers', 'caddy', image(value, 3).digest, 'v0.9.40'));
    expect(document.components[3]!.runningImages[0]!.links).toMatchObject({
      commit: image(value, 3).links.commit,
      build: image(value, 3).links.build,
    });
  });

  it('names only the release namespace, leaving maintenance out', () => {
    expect(imageNamespaces(decoded(raw('rollback'))))
      .toEqual(['ghcr.io/dannyota/']);
  });

  it('names each mixed app owner once, in component order', () => {
    const serverFirst = raw('verified');
    moveComponent(serverFirst, 0, 'dntmakers');
    expect(imageNamespaces(decoded(serverFirst)))
      .toEqual(['ghcr.io/dntmakers/', 'ghcr.io/dannyota/']);
    const webMoved = raw('rollback');
    moveComponent(webMoved, 1, 'dntmakers');
    webMoved.release = null;
    expect(imageNamespaces(decoded(webMoved)))
      .toEqual(['ghcr.io/dannyota/', 'ghcr.io/dntmakers/']);
    expect(imageNamespaces(decoded(raw('mixed-owners'))))
      .toEqual(['ghcr.io/dntmakers/', 'ghcr.io/dannyota/']);
  });

  it('leaves a stopped component out of the namespaces', () => {
    const value = owned(raw('mismatch-missing'), 'dntmakers');
    value.components[2].image = 'ghcr.io/dannyota/aboutme-caddy';
    expect(value.components[2].running_images).toEqual([]);
    expect(imageNamespaces(decoded(value))).toEqual(['ghcr.io/dntmakers/']);
  });

  it('falls back to the release components when nothing runs', () => {
    const value = raw('mismatch-missing');
    value.components.push(raw('rollback').components[3]);
    for (const entry of value.components) entry.running_images = [];
    expect(imageNamespaces(decoded(value))).toEqual(['ghcr.io/dannyota/']);
  });

  it('falls back to the release components when only maintenance runs',
    () => {
      const value = raw('mismatch-missing');
      value.components.push(raw('rollback').components[3]);
      for (const entry of value.components.slice(0, 3)) {
        entry.running_images = [];
      }
      expect(imageNamespaces(decoded(value))).toEqual(['ghcr.io/dannyota/']);
    });
});

describe('mixed app owners', () => {
  it.each([0, 1, 2])('decode with no release when component %i moves', (
    index,
  ) => {
    const value = raw('verified');
    moveComponent(value, index, 'dntmakers');
    value.release = null;
    const document = decoded(value);
    expect(recomputeSummary(document)).toBe('verified');
    expect(recomputeRelease(document)).toBeNull();
    expect(stateOf(value)).toMatchObject({ kind: 'verified', release: null });
  });
});

const ownedFields: Readonly<Record<string, (value: Loose) => [Loose, string]>>
  = {
    'source_repository': (value) => [value, 'source_repository'],
    'image': (value) => [value.components[0], 'image'],
    'signer_workflow': (value) =>
      [image(value, 0).signature, 'signer_workflow'],
    'commit link': (value) => [image(value, 0).links, 'commit'],
    'release link': (value) => [image(value, 0).links, 'release'],
    'build link': (value) => [image(value, 0).links, 'build'],
    'provenance link': (value) => [image(value, 0).links, 'provenance'],
    'sbom link': (value) => [image(value, 0).links, 'sbom'],
  };

function replaceOwner(
  value: Loose,
  field: string,
  from: string,
  to: string,
): void {
  const [node, key] = ownedFields[field]!(value);
  expect(node[key]).toContain(`/${from}/`);
  node[key] = node[key].replace(`/${from}/`, `/${to}/`);
}

describe('owner checks', () => {
  const fields = Object.keys(ownedFields);
  const strangers = ['someone', 'Dntmakers', 'DANNYOTA', 'dannyota-x'];

  it.each(fields.flatMap((field) => ['dannyota', 'dntmakers']
    .flatMap((base) => strangers.map((stranger) =>
      [field, base, stranger] as const))))(
    'refuses %s in a %s document naming %s',
    (field, base, stranger) => {
      const value = owned(raw('verified'), base);
      replaceOwner(value, field, base, stranger);
      expect(decodeDeploymentDocument(value)).toEqual({ kind: 'invalid' });
    },
  );

  it.each([
    'source_repository', 'commit link', 'release link', 'provenance link',
    'sbom link',
  ])('refuses a %s whose owner differs from the source links', (field) => {
    for (const [from, to] of [
      ['dannyota', 'dntmakers'], ['dntmakers', 'dannyota'],
    ] as const) {
      const value = owned(raw('verified'), from);
      replaceOwner(value, field, from, to);
      expect(decodeDeploymentDocument(value)).toEqual({ kind: 'invalid' });
    }
  });

  it('refuses a source link that follows the image owner in a rollback',
    () => {
      const value = raw('rollback');
      image(value, 3).links.commit = image(value, 3).links.commit
        .replace('/dannyota/', '/dntmakers/');
      expect(decodeDeploymentDocument(value)).toEqual({ kind: 'invalid' });
    });

  it.each(['signer_workflow', 'build link'])(
    'refuses a verified %s whose owner differs from its image',
    (field) => {
      for (const [from, to] of [
        ['dannyota', 'dntmakers'], ['dntmakers', 'dannyota'],
      ] as const) {
        const value = raw('verified');
        moveComponent(value, 0, from);
        value.release = null;
        expect(decodeDeploymentDocument(value).kind).toBe('document');
        replaceOwner(value, field, from, to);
        expect(decodeDeploymentDocument(value)).toEqual({ kind: 'invalid' });
      }
    },
  );

  it('accepts an unchecked image with no signer', () => {
    const value = owned(raw('unverified'), 'dntmakers');
    expect(image(value, 2).signature.signer_workflow).toBeNull();
    expect(decodeDeploymentDocument(value).kind).toBe('document');
  });

  it.each([
    [0, 'ghcr.io/dannyota/aboutme-observer'],
    [2, 'ghcr.io/dannyota/aboutme-server'],
    [3, 'ghcr.io/dntmakers/aboutme-server'],
    [3, 'ghcr.io/dntmakers/aboutme-web'],
    [3, 'ghcr.io/dntmakers/aboutme-maintenance'],
  ])('refuses component %i running %s', (index, name) => {
    const value = raw('rollback');
    value.components[index].image = name;
    expect(decodeDeploymentDocument(value)).toEqual({ kind: 'invalid' });
  });
});
