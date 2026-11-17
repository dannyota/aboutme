import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { setResponseHeader } from 'h3';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import VerifyPage from '../../app/pages/verify.vue';
import { setSiteLocale } from '../support/locale';
import {
  declared, RETIRED_STYLE, sfcRules, webSource,
} from '../support/sfcStyles';

type Wrapper = Awaited<ReturnType<typeof mountSuspended>>;

const PAGE = 'app/pages/verify.vue';
const CHAIN = 'app/components/verify/VerifyChain.vue';
const COMPONENTS = 'app/components/verify/VerifyComponents.vue';
const SOURCES = [
  PAGE,
  CHAIN,
  COMPONENTS,
  'app/components/verify/VerifyCommandBlock.vue',
  'app/components/verify/VerifyCommands.vue',
  'app/components/verify/VerifyDigestChip.vue',
  'app/components/verify/VerifyLimits.vue',
  'app/components/verify/VerifyStatusCard.vue',
];

const fixturesDir = resolve(
  import.meta.dirname, '../../e2e/fixtures/deployment',
);

let served: { body: unknown; date: string } = { body: null, date: '' };

registerEndpoint('/.well-known/deployment.json', (event) => {
  setResponseHeader(event, 'date', served.date);
  return served.body;
});

function serve(name: string): void {
  const body = JSON.parse(readFileSync(resolve(fixturesDir, `${name}.json`),
    'utf8')) as { observed_at: string };
  served = {
    body,
    date: new Date(Date.parse(body.observed_at) + 30_000).toUTCString(),
  };
}

const mounted: Wrapper[] = [];

beforeEach(() => {
  setSiteLocale('en');
  clearNuxtData();
});

afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
});

async function mountVerify(fixture: string, state: string): Promise<Wrapper> {
  serve(fixture);
  const wrapper = await mountSuspended(VerifyPage);
  mounted.push(wrapper);
  await vi.waitFor(() => {
    expect(wrapper.get('[data-testid="verify-status"]').attributes(
      'data-state',
    )).toBe(state);
  });
  return wrapper;
}

function expectClasses(classes: readonly string[], names: readonly string[]) {
  for (const name of names) expect(classes, name).toContain(name);
}

function joined(classes: readonly string[]): string {
  return ` ${classes.join(' ')} `;
}

describe('verify tokens', () => {
  it('map the page tokens onto the identity on the page root', () => {
    expect(declared(sfcRules(PAGE), '.verify-page')).toEqual({
      '--verify-verified': 'var(--link)',
      '--verify-rolling': 'var(--coin)',
      '--verify-neutral': 'var(--muted-foreground)',
      '--verify-bad': 'var(--destructive)',
    });
  });

  it('keep hex and retired tokens out of the verify markup', () => {
    for (const path of SOURCES) {
      expect(webSource(path), path).not.toMatch(RETIRED_STYLE);
    }
  });

  it('leave the component selector on the ink selected state', () => {
    expect(webSource('app/components/verify/VerifyCommands.vue'))
      .not.toMatch(/data-\[state=on\]:(?:bg|shadow|text)-/u);
  });
});

describe('verify cards', () => {
  it('are bordered cards with the card radius and no shadow', async () => {
    const wrapper = await mountVerify('verified', 'verified');
    for (const id of [
      'verify-status', 'verify-chain', 'verify-components', 'verify-yourself',
    ]) {
      const card = wrapper.get(`[data-testid="${id}"]`);
      expectClasses(card.classes(), ['rounded-[var(--radius-card)]', 'border']);
      expect(joined(card.classes()), id).not.toMatch(
        / (?:shadow-\S+|rounded-\[20px\]) /u,
      );
    }
  });
});

describe('verify status marks', () => {
  it('fills the verified mark with ink and keeps the card muted',
    async () => {
      const wrapper = await mountVerify('verified', 'verified');
      const card = wrapper.get('[data-testid="verify-status"]');
      expectClasses(card.classes(), ['bg-muted']);
      expect(joined(card.classes())).not.toMatch(/ bg-surface-blue /u);
      const mark = card.get('.verify-mark');
      expectClasses(mark.classes(), [
        'verify-disk', 'bg-foreground', 'text-background',
      ]);
      expect(joined(mark.classes())).not.toMatch(/ (?:bg-link|text-card) /u);
    });

  it('sets the rollout mark in a coin with an ink icon', async () => {
    const wrapper = await mountVerify('rolling-out', 'rolling_out');
    const card = wrapper.get('[data-testid="verify-status"]');
    expectClasses(card.classes(), ['bg-muted']);
    expect(joined(card.classes())).not.toMatch(/ bg-surface-indigo /u);
    const mark = card.get('.verify-mark');
    expectClasses(mark.classes(), [
      'verify-disk', 'bg-[var(--verify-rolling)]', 'text-coin-ink',
    ]);
    expect(joined(mark.classes())).not.toMatch(
      / (?:bg-brand-indigo|text-white) /u,
    );
  });
});

describe('verify chain', () => {
  it('sets a match chip in link on mist', async () => {
    const wrapper = await mountVerify('verified', 'verified');
    const chips = wrapper.findAll('[data-chip="match"]');
    expect(chips).toHaveLength(4);
    for (const chip of chips) {
      expectClasses(chip.classes(), ['bg-surface-mist', 'text-link']);
      expect(chip.classes()).not.toContain('bg-surface-blue');
    }
  });

  it('sets the updating chip in coin ink on a coin', async () => {
    const wrapper = await mountVerify('rolling-out', 'rolling_out');
    const chip = wrapper.get('[data-chip="updating"]');
    expectClasses(chip.classes(), ['bg-coin', 'text-coin-ink']);
    expect(joined(chip.classes())).not.toMatch(/ bg-surface-indigo /u);
    expect(chip.get('svg').classes()).not.toContain('text-brand-indigo');
  });

  it('draws the updating connector in the coin edge with an ink icon',
    () => {
      const rules = sfcRules(CHAIN);
      expect(declared(rules,
        '.verify-connector[data-connector="updating"]'))
        .toEqual({ '--line': 'var(--coin-edge)' });
      expect(declared(rules,
        '.verify-connector[data-connector="updating"] .verify-connector-dot',
        ['@media (forced-colors: none)']))
        .toEqual({ color: 'var(--coin-ink)' });
      expect(declared(rules,
        '.verify-connector[data-connector="agree"]'))
        .toEqual({ '--line': 'var(--link)' });
    });
});

describe('verify components', () => {
  it('rails a rollout in the coin edge', () => {
    expect(declared(sfcRules(COMPONENTS), '.verify-rail-rolling::before'))
      .toEqual({ background: 'var(--coin-edge)' });
    expect(declared(sfcRules(COMPONENTS), '.verify-rail-bad::before'))
      .toEqual({ background: 'var(--destructive)' });
  });

  it('tags the newest image in a coin and paints replicas', async () => {
    const wrapper = await mountVerify('rolling-out', 'rolling_out');
    const web = wrapper.get('[data-component="web"]');
    const newest = web.findAll('span').filter((span) =>
      span.text() === 'Newest');
    expect(newest).toHaveLength(1);
    expectClasses(newest[0]!.classes(), [
      'bg-coin', 'text-coin-ink', 'rounded-full', 'font-bold',
    ]);
    expect(newest[0]!.classes()).not.toContain('bg-surface-indigo');
    const squares = web.findAll('.verify-replica');
    expect(squares.length).toBeGreaterThan(0);
    for (const square of squares) {
      expect(joined(square.classes())).toMatch(/ (?:bg-chart-bar|bg-input) /u);
      expect(square.classes()).not.toContain('bg-brand-blue');
    }
  });
});

describe('verify it yourself', () => {
  it('numbers steps on mist disks with link digits', async () => {
    const wrapper = await mountVerify('verified', 'verified');
    const steps = wrapper.get('[data-testid="verify-yourself"]')
      .findAll('ol > li');
    expect(steps).toHaveLength(2);
    for (const step of steps) {
      const disk = step.get('span');
      expectClasses(disk.classes(), [
        'size-[22px]', 'rounded-full', 'bg-surface-mist', 'text-link',
      ]);
      expect(disk.classes()).not.toContain('bg-surface-blue');
    }
  });

  it('draws command blocks borderless with sun highlights', async () => {
    const wrapper = await mountVerify('verified', 'verified');
    for (const id of ['verify-command-1', 'verify-command-2']) {
      const block = wrapper.get(`[data-testid="${id}"]`);
      const pre = block.get('pre');
      expectClasses(pre.classes(), ['rounded-[10px]', 'bg-muted']);
      expect(joined(pre.classes()), id).not.toMatch(/ border /u);
      const button = block.get('button');
      expectClasses(button.classes(), [
        'size-8', 'rounded-full', 'border', 'bg-card',
      ]);
      expect(button.classes()).not.toContain('rounded-md');
    }
    const live = wrapper.get('[data-testid="verify-command-2"]')
      .findAll('code span.font-bold');
    expect(live.length).toBeGreaterThan(0);
    for (const part of live) {
      expectClasses(part.classes(), ['bg-surface-sun', 'text-foreground']);
      expect(part.classes()).not.toContain('bg-surface-blue');
    }
  });
});
