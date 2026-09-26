import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

import {
  chainRelease,
  decodeDeploymentDocument,
  type DeploymentDocument,
  isStale,
  pageState,
  type PageState,
  recomputeRelease,
  recomputeSummary,
  responseTime,
} from '../../app/utils/deploymentDocument';
import {
  chainView,
  commandTargets,
  componentRows,
  defaultTarget,
  readDigestsCommand,
  shortDigest,
  verifyImageCommand,
} from '../../app/utils/verifyView';

// Fixture edits below reach into nested JSON by path; typing each shape
// would add nothing a failing decode does not already show.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type Loose = any;

// Documents for every page state (docs/design/deployment-transparency/
// page.md#states), shared with the browser specs.
const fixtureRoot = resolve(
  import.meta.dirname,
  '../../e2e/fixtures/deployment',
);

function raw(name: string): Record<string, unknown> {
  return JSON.parse(readFileSync(resolve(fixtureRoot, `${name}.json`), 'utf8'));
}

function decoded(value: unknown): DeploymentDocument {
  const result = decodeDeploymentDocument(value);
  if (result.kind !== 'document') throw new Error(`decoded ${result.kind}`);
  return result.document;
}

/** The server clock 30 seconds after the fixture was observed. */
function freshNow(value: Record<string, unknown>): number {
  return Date.parse(value.observed_at as string) + 30_000;
}

function stateOf(name: string, now?: number): PageState {
  const value = raw(name);
  return pageState(
    { kind: 'decoded', result: decodeDeploymentDocument(value) },
    now ?? freshNow(value),
  );
}

function clone(name: string): Loose {
  return structuredClone(raw(name)) as Loose;
}

describe('page state from each fixture', () => {
  it('shows loading, then unavailable after a failed fetch', () => {
    expect(pageState({ kind: 'pending' }, null).kind).toBe('loading');
    expect(pageState({ kind: 'failed' }, null).kind).toBe('unavailable');
  });

  it('is verified only for the verified document', () => {
    const state = stateOf('verified');
    expect(state).toMatchObject({
      kind: 'verified',
      release: { version: 'v0.6.0' },
    });
    for (const name of [
      'rolling-out', 'unverified', 'mismatch-not-found', 'mismatch-invalid',
      'mismatch-missing', 'mismatch-summary', 'stale', 'outdated',
    ]) {
      expect(stateOf(name, Date.parse('2026-10-02T03:14:35Z')).kind)
        .not.toBe('verified');
    }
  });

  it('names the component moving between versions during a rollout', () => {
    expect(stateOf('rolling-out')).toMatchObject({
      kind: 'rolling_out',
      component: 'web',
      from: 'v0.5.21',
      to: 'v0.6.0',
    });
  });

  it('names the first component whose signature is not checked', () => {
    expect(stateOf('unverified')).toMatchObject({
      kind: 'unverified',
      component: 'caddy',
      more: 0,
    });
  });

  it.each([
    ['mismatch-not-found', 'not_found', 'web'],
    ['mismatch-invalid', 'invalid', 'server'],
    ['mismatch-missing', 'missing', 'caddy'],
  ] as const)('reads %s as a mismatch caused by %s', (
    name,
    reason,
    component,
  ) => {
    expect(stateOf(name)).toMatchObject({
      kind: 'mismatch',
      reason,
      component,
      more: 0,
      cause: { reason, component },
    });
  });

  it('shows a mismatch when the summary disagrees with the components', () => {
    const value = raw('mismatch-summary');
    expect(value.summary).toBe('verified');
    expect(stateOf('mismatch-summary')).toMatchObject({
      kind: 'mismatch',
      reason: 'summary',
      component: null,
      cause: null,
    });
  });

  it('shows a mismatch when the release disagrees with its components', () => {
    const value = clone('verified');
    value.release.commit = 'f'.repeat(40);
    expect(pageState(
      { kind: 'decoded', result: decodeDeploymentDocument(value) },
      freshNow(value),
    )).toMatchObject({ kind: 'mismatch', reason: 'summary' });
  });

  it('counts every failing component after the first', () => {
    const value = clone('mismatch-missing');
    value.components[0].running_images[0].signature.status = 'not_found';
    value.components[1].running_images[0].signature.status = 'invalid';
    const state = pageState(
      { kind: 'decoded', result: decodeDeploymentDocument(value) },
      freshNow(value),
    );
    expect(state).toMatchObject({
      kind: 'mismatch',
      reason: 'not_found',
      component: 'server',
      more: 2,
      cause: { reason: 'not_found', component: 'server' },
    });
  });

  it('reports an outdated page for a newer schema version', () => {
    expect(stateOf('outdated').kind).toBe('outdated');
  });

  it('treats an unknown signature status as not verified', () => {
    const value = clone('verified');
    value.components[1].running_images[0].signature.status = 'revoked';
    value.summary = 'unverified';
    value.release = null;
    const document = decoded(value);
    expect(recomputeSummary(document)).toBe('unverified');
    expect(document.components[1]!.runningImages[0]!.version).toBeNull();
    expect(pageState(
      { kind: 'decoded', result: { kind: 'document', document } },
      freshNow(value),
    ).kind).toBe('unverified');
  });

  it('treats an unknown summary value as a disagreement', () => {
    const value = clone('verified');
    value.summary = 'healthy';
    expect(pageState(
      { kind: 'decoded', result: decodeDeploymentDocument(value) },
      freshNow(value),
    )).toMatchObject({ kind: 'mismatch', reason: 'summary' });
  });
});

describe('staleness from the response clock', () => {
  const value = raw('verified');
  const document = decoded(value);
  const observed = Date.parse(value.observed_at as string);

  it('reads the server clock from Date plus Age', () => {
    expect(responseTime('Fri, 02 Oct 2026 03:14:35 GMT', '25'))
      .toBe(Date.parse('2026-10-02T03:15:00Z'));
    expect(responseTime('Fri, 02 Oct 2026 03:14:35 GMT', null))
      .toBe(Date.parse('2026-10-02T03:14:35Z'));
    expect(responseTime('Fri, 02 Oct 2026 03:14:35 GMT', 'soon'))
      .toBe(Date.parse('2026-10-02T03:14:35Z'));
    expect(responseTime(null, '25')).toBeNull();
    expect(responseTime('not a date', null)).toBeNull();
  });

  it('is fresh until stale_after and stale after it', () => {
    expect(isStale(document, observed + 180_000)).toBe(false);
    expect(isStale(document, observed + 180_001)).toBe(true);
  });

  it('is stale after 600 seconds whatever stale_after says', () => {
    const late = clone('verified');
    late.stale_after = '2026-10-02T04:14:05Z';
    const extended = decoded(late);
    expect(isStale(extended, observed + 600_000)).toBe(false);
    expect(isStale(extended, observed + 600_001)).toBe(true);
  });

  it('counts a response without a usable Date as stale', () => {
    expect(isStale(document, null)).toBe(true);
  });

  it('makes a cached copy stale by its Age, whatever the browser clock', () => {
    // The visitor's clock may say anything; only Date plus Age counts.
    const now = responseTime('Fri, 02 Oct 2026 03:14:35 GMT', '600');
    expect(pageState(
      { kind: 'decoded', result: { kind: 'document', document } },
      now,
    ).kind).toBe('stale');
    expect(pageState(
      { kind: 'decoded', result: { kind: 'document', document } },
      responseTime('Fri, 02 Oct 2026 03:14:35 GMT', '0'),
    ).kind).toBe('verified');
  });

  it('puts stale before every other document state', () => {
    for (const name of ['stale', 'mismatch-not-found', 'rolling-out']) {
      expect(stateOf(name, Date.parse('2026-10-02T04:00:00Z')).kind)
        .toBe('stale');
    }
    expect(stateOf('stale', Date.parse('2026-10-02T03:14:35Z')).kind)
      .toBe('stale');
  });
});

describe('closed decoding', () => {
  it('decodes every version 1 fixture', () => {
    for (const name of [
      'verified', 'rolling-out', 'unverified', 'mismatch-not-found',
      'mismatch-invalid', 'mismatch-missing', 'mismatch-summary', 'stale',
    ]) {
      expect(decodeDeploymentDocument(raw(name)).kind).toBe('document');
    }
  });

  it('ignores fields it does not know', () => {
    const value = clone('verified');
    value.future_field = { anything: true };
    value.components[0].running_images[0].attestation_count = 3;
    expect(decodeDeploymentDocument(value).kind).toBe('document');
  });

  it.each([
    ['a non-object body', () => []],
    ['a null body', () => null],
    ['schema version 0', (value: Loose) => { value.schema_version = 0; }],
    ['a string schema version', (value: Loose) => {
      value.schema_version = '1';
    }],
    ['another site', (value: Loose) => { value.site = 'https://example.com'; }],
    ['a short digest', (value: Loose) => {
      value.components[0].running_images[0].digest = 'sha256:abc';
    }],
    ['shell text in a digest', (value: Loose) => {
      value.components[0].running_images[0].digest
        = `sha256:${'a'.repeat(63)}; rm -rf ~`;
    }],
    ['shell text in a version', (value: Loose) => {
      value.components[0].running_images[0].version = 'v1.0.0 && curl x';
    }],
    ['a link to another host', (value: Loose) => {
      value.components[0].running_images[0].links.build
        = 'https://example.com/actions/runs/1/attempts/1';
    }],
    ['a javascript link', (value: Loose) => {
      value.components[0].running_images[0].links.transparency_log
        = 'javascript:alert(1)';
    }],
    ['an unknown component', (value: Loose) => {
      value.components[0].name = 'worker';
    }],
    ['a component image that does not match its name', (value: Loose) => {
      value.components[0].image = 'ghcr.io/dannyota/aboutme-web';
    }],
    ['components out of order', (value: Loose) => {
      value.components.reverse();
    }],
    ['a duplicate component', (value: Loose) => {
      value.components[1] = structuredClone(value.components[0]);
    }],
    ['zero replicas', (value: Loose) => {
      value.components[0].running_images[0].replicas = 0;
    }],
    ['too many replicas', (value: Loose) => {
      value.components[0].running_images[0].replicas = 65;
    }],
    ['a local timestamp', (value: Loose) => {
      value.observed_at = '2026-10-02T10:14:05+07:00';
    }],
    ['a verified image without a version', (value: Loose) => {
      value.components[0].running_images[0].version = null;
    }],
    ['a duplicate digest', (value: Loose) => {
      const [image] = value.components[1].running_images;
      value.components[1].running_images = [image, structuredClone(image)];
    }],
  ])('refuses %s', (_label, change) => {
    const value = clone('verified');
    const replaced = change(value);
    expect(decodeDeploymentDocument(replaced === undefined ? value : replaced))
      .toEqual({ kind: 'invalid' });
  });

  it('drops a version the signature does not back', () => {
    const value = clone('unverified');
    value.components[2].running_images[0].version = 'v9.9.9';
    value.components[2].running_images[0].links.build
      = 'https://github.com/dannyota/aboutme/actions/runs/1/attempts/1';
    const image = decoded(value).components[2]!.runningImages[0]!;
    expect(image.version).toBeNull();
    expect(image.links.build).toBeNull();
    expect(image.links.provenance).toMatch(/^https:\/\/api\.github\.com\//u);
  });

  it('orders a component\'s images by running_since', () => {
    const value = clone('rolling-out');
    value.components[1].running_images.reverse();
    const images = decoded(value).components[1]!.runningImages;
    expect(images.map(({ version }) => version)).toEqual(['v0.5.21', 'v0.6.0']);
  });
});

describe('summary and release rules', () => {
  it('recomputes the observer summary for every consistent fixture', () => {
    for (const name of [
      'verified', 'rolling-out', 'unverified', 'mismatch-not-found',
      'mismatch-invalid', 'mismatch-missing', 'stale',
    ]) {
      const value = raw(name);
      expect(recomputeSummary(decoded(value))).toBe(value.summary);
    }
  });

  it('recomputes the release with the latest start time', () => {
    const value = raw('verified');
    expect(recomputeRelease(decoded(value))).toEqual({
      version: 'v0.6.0',
      commit: (value.release as { commit: string }).commit,
      deployedAt: '2026-10-02T03:12:02Z',
    });
    expect(recomputeRelease(decoded(raw('rolling-out')))).toBeNull();
  });

  it('keeps maintenance out of the release', () => {
    const value = clone('verified');
    const caddy = structuredClone(value.components[2]);
    caddy.name = 'maintenance';
    caddy.running_images[0].running_since = '2026-10-02T03:13:00Z';
    value.components.push(caddy);
    expect(recomputeRelease(decoded(value))?.deployedAt)
      .toBe('2026-10-02T03:12:02Z');
  });

  it('has no release when signed components run different versions', () => {
    const value = clone('rolling-out');
    value.components[1].running_images.pop();
    value.summary = 'verified';
    const document = decoded(value);
    expect(recomputeSummary(document)).toBe('verified');
    expect(recomputeRelease(document)).toBeNull();
    expect(pageState(
      { kind: 'decoded', result: { kind: 'document', document } },
      freshNow(value),
    )).toMatchObject({ kind: 'verified', release: null });
  });

  it('shows the newest verified version when there is no release', () => {
    expect(chainRelease(decoded(raw('rolling-out')))?.version).toBe('v0.6.0');
    expect(chainRelease(decoded(raw('mismatch-not-found')))?.version)
      .toBe('v0.6.0');
  });
});

describe('chain view', () => {
  it.each([
    ['verified', ['match', 'match', 'match', 'match'],
      ['agree', 'agree', 'agree']],
    ['rolling-out', ['match', 'match', 'match', 'updating'],
      ['agree', 'agree', 'updating']],
    ['unverified', ['match', 'match', 'not_verified', 'not_verified'],
      ['agree', 'unknown', 'unknown']],
    ['mismatch-not-found', ['match', 'match', 'failed', 'not_verified'],
      ['agree', 'disagree', 'unknown']],
    ['mismatch-invalid', ['match', 'match', 'failed', 'not_verified'],
      ['agree', 'disagree', 'unknown']],
    ['mismatch-missing', ['match', 'match', 'match', 'failed'],
      ['agree', 'agree', 'disagree']],
    ['mismatch-summary',
      ['not_verified', 'not_verified', 'not_verified', 'not_verified'],
      ['unknown', 'unknown', 'unknown']],
  ] as const)('shows %s', (name, chips, connectors) => {
    const view = chainView(stateOf(name))!;
    expect(view.chips).toEqual(chips);
    expect(view.connectors).toEqual(connectors);
  });

  it('shows every stale chip as not rechecked', () => {
    const view = chainView(stateOf('stale'))!;
    expect(view.chips).toEqual(Array(4).fill('not_rechecked'));
    expect(view.connectors).toEqual(Array(3).fill('unknown'));
    expect(view.release?.version).toBe('v0.6.0');
  });

  it('counts serving replicas and the release start', () => {
    const view = chainView(stateOf('verified'))!;
    expect(view.replicas).toBe(3);
    expect(view.runningSince).toBe('2026-10-02T03:12:02Z');
  });

  it('has no chain without a document', () => {
    expect(chainView({ kind: 'unavailable' })).toBeNull();
    expect(chainView({ kind: 'outdated' })).toBeNull();
    expect(chainView({ kind: 'loading' })).toBeNull();
  });
});

describe('component rows and commands', () => {
  it('lists serving rows, maintenance while it runs, and rollout ages', () => {
    const rolling = componentRows(decoded(raw('rolling-out')));
    expect(rolling.map(({ name }) => name)).toEqual(['server', 'web', 'caddy']);
    expect(rolling[1]!.lines.map(({ age }) => age))
      .toEqual(['older', 'newest']);
    expect(componentRows(decoded(raw('unverified'))).map(({ name }) => name))
      .toEqual(['server', 'web', 'caddy', 'maintenance']);
    const missing = componentRows(decoded(raw('mismatch-missing')));
    expect(missing[2])
      .toMatchObject({ name: 'caddy', lines: [], failing: true });
  });

  it('starts the selector on server, or on the failing image', () => {
    for (const [name, component] of [
      ['verified', 'server'],
      ['mismatch-not-found', 'web'],
      ['mismatch-invalid', 'server'],
      ['mismatch-missing', 'server'],
    ] as const) {
      const state = stateOf(name);
      const targets = commandTargets(decoded(raw(name)));
      expect(defaultTarget(state, targets)?.component).toBe(component);
    }
    const rolling = commandTargets(decoded(raw('rolling-out')));
    expect(rolling.map(({ label }) => label)).toEqual([
      'server', 'web v0.5.21', 'web v0.6.0', 'caddy',
    ]);
  });

  it('fills the verify command with live values and no prompts', () => {
    const document = decoded(raw('unverified'));
    const targets = commandTargets(document);
    const server = targets[0]!;
    const command = verifyImageCommand(server);
    expect(command.copy).toBe([
      'gh attestation verify \\',
      `  oci://ghcr.io/dannyota/aboutme-server@${server.image.digest} \\`,
      '  --repo dannyota/aboutme \\',
      '  --signer-workflow '
      + 'dannyota/aboutme/.github/workflows/release-images.yml \\',
      '  --source-ref refs/tags/v0.6.0 \\',
      '  --deny-self-hosted-runners',
    ].join('\n'));
    expect(command.lines.flat().filter(({ kind }) => kind === 'live')
      .map(({ text }) => text)).toEqual([
      'server', server.image.digest.slice(7), 'v0.6.0',
    ]);

    const maintenance = targets.find(({ component }) =>
      component === 'maintenance')!;
    expect(verifyImageCommand(maintenance).copy)
      .toContain('oci://ghcr.io/dannyota/aboutme-caddy@sha256:');

    const unchecked = targets.find(({ component }) => component === 'caddy')!;
    expect(verifyImageCommand(unchecked).copy)
      .toContain('--source-ref refs/tags/<version> \\');
  });

  it('shows placeholders when there is no document', () => {
    const command = verifyImageCommand(null);
    expect(command.copy).toContain('aboutme-server@sha256:<digest> \\');
    expect(command.copy).toContain('refs/tags/<version> \\');
    expect(command.lines.flat().some(({ kind }) => kind === 'live'))
      .toBe(false);
    expect(readDigestsCommand.copy).toBe(
      'curl -fsS https://aboutme.vn/.well-known/deployment.json \\\n'
      + '  | jq -r \'.components[] | .name as $n | .running_images[] '
      + '| "\\($n) \\(.digest) \\(.version)"\'',
    );
  });

  it('shortens a digest to its first 8 and last 4 hex characters', () => {
    expect(shortDigest(`sha256:d1c33a0a${'0'.repeat(52)}945c`))
      .toBe('sha256:d1c33a0a…945c');
  });
});
