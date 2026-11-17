import { mountSuspended } from '@nuxt/test-utils/runtime';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import GuidePage from '../../app/pages/guide/mcp.vue';
import { setSiteLocale } from '../support/locale';
import {
  declared, RETIRED_STYLE, sfcRules, webSource,
} from '../support/sfcStyles';

type Wrapper = Awaited<ReturnType<typeof mountSuspended>>;

const SOURCES = [
  'app/pages/guide/mcp.vue',
  'app/components/guide/GuideCanCannot.vue',
  'app/components/guide/GuideClaudeSteps.vue',
  'app/components/guide/GuideCopyBlock.vue',
  'app/components/guide/GuideRichText.vue',
  'app/components/guide/GuideTroubleshooting.vue',
];

const mounted: Wrapper[] = [];

beforeEach(() => {
  setSiteLocale('en');
  clearNuxtData();
});

afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
});

async function mountGuide(): Promise<Wrapper> {
  const wrapper = await mountSuspended(GuidePage);
  mounted.push(wrapper);
  return wrapper;
}

function expectClasses(classes: readonly string[], names: readonly string[]) {
  for (const name of names) expect(classes, name).toContain(name);
}

function joined(classes: readonly string[]): string {
  return ` ${classes.join(' ')} `;
}

describe('guide header panel', () => {
  it('uses the Library header: a blush panel, no border', async () => {
    const wrapper = await mountGuide();
    const header = wrapper.get('[data-testid="guide-header"]');
    expectClasses(header.classes(), [
      'rounded-[var(--radius-panel)]', 'bg-surface-blush',
    ]);
    expect(joined(header.classes())).not.toMatch(
      / (?:border|bg-surface-blue|rounded-\[var\(--radius-feature\)\]) /u,
    );
  });

  it('sets the 32 px page heading, 40 from 48 rem, and a muted lead',
    async () => {
      const wrapper = await mountGuide();
      const header = wrapper.get('[data-testid="guide-header"]');
      const title = header.get('h1');
      expectClasses(title.classes(), [
        'text-2xl', 'md:text-3xl', 'font-extrabold', 'tracking-[-0.02em]',
      ]);
      expect(joined(title.classes())).not.toMatch(/ (?:text-xl|font-bold) /u);
      const [lead, explainer] = header.findAll(':scope > p');
      expectClasses(lead!.classes(), [
        'text-md', 'text-muted-foreground', 'max-w-[40rem]',
      ]);
      expectClasses(explainer!.classes(), ['text-md']);
    });
});

describe('guide cards', () => {
  it('are bordered cards with the card radius and no shadow', async () => {
    const wrapper = await mountGuide();
    const sections = wrapper.findAll(
      '[data-testid="guide-mcp-page"] > section',
    );
    expect(sections).toHaveLength(6);
    for (const section of sections) {
      const name = section.attributes('data-testid')!;
      expectClasses(section.classes(), [
        'rounded-[var(--radius-card)]', 'border', 'bg-card',
      ]);
      expect(joined(section.classes()), name).not.toMatch(
        / (?:shadow-\S+|rounded-\[var\(--radius-feature\)\]) /u,
      );
      const heading = section.get('h2');
      expectClasses(heading.classes(), ['text-lg', 'font-bold']);
      expect(joined(heading.classes()), name)
        .not.toMatch(/ (?:text-\[18px\]|font-semibold) /u);
    }
  });

  it('sets body text at 16/1.6 and keeps the 15 px labels', async () => {
    const wrapper = await mountGuide();
    const body = wrapper.findAll([
      '[data-testid="guide-can"] li',
      '[data-testid="guide-cannot"] li',
      '[data-testid="guide-claude"] > p:first-of-type',
      '[data-testid="guide-claude"] ol li p',
      '[data-testid="guide-try-request"] li',
      '[data-testid="guide-privacy"] li',
      '[data-testid="guide-other-apps"] p',
    ].join(', '));
    expect(body.length).toBeGreaterThan(20);
    for (const block of body) {
      expect(block.classes(), block.text().slice(0, 40)).toContain('text-md');
      expect(block.classes()).not.toContain('text-[15px]');
    }
    for (const label of wrapper.findAll(
      '[data-testid="guide-troubleshooting"] dt, '
      + '[data-testid="guide-troubleshooting"] dd, '
      + '[data-testid="guide-claude"] h3',
    )) {
      expect(label.classes()).toContain('text-[15px]');
    }
  });
});

describe('guide steps', () => {
  it('number steps on 24 px mist disks with no ring', async () => {
    const wrapper = await mountGuide();
    const disks = wrapper.findAll('.guide-disk');
    expect(disks).toHaveLength(9);
    for (const disk of disks) {
      expectClasses(disk.classes(), [
        'size-6', 'rounded-full', 'bg-surface-mist', 'text-[13px]',
        'font-bold', 'text-foreground',
      ]);
      expect(joined(disk.classes())).not.toMatch(
        / (?:border|border-border|bg-surface-blue|font-semibold) /u,
      );
    }
  });

  it('keeps a visible disk edge under forced colors', () => {
    expect(declared(sfcRules('app/components/guide/GuideClaudeSteps.vue'),
      '.guide-disk', ['@media (forced-colors: active)']))
      .toEqual({ border: '1px solid CanvasText' });
  });
});

describe('guide copy blocks', () => {
  it('are borderless muted blocks with a ghost circle copy button',
    async () => {
      const wrapper = await mountGuide();
      for (const id of [
        'guide-url', 'guide-command-add', 'guide-command-login',
      ]) {
        const block = wrapper.get(`[data-testid="${id}"]`);
        const pre = block.get('pre');
        expectClasses(pre.classes(), [
          'rounded-[10px]', 'bg-muted', 'font-mono', 'text-[13px]',
          'overflow-x-auto',
        ]);
        expect(joined(pre.classes()), id).not.toMatch(/ border /u);
        const button = block.get('button');
        expectClasses(button.classes(), [
          'absolute', 'right-2', 'top-2', 'size-8', 'rounded-full',
          'hover:bg-card',
        ]);
        expect(joined(button.classes()), id).not.toMatch(
          / (?:border|bg-card|rounded-md) /u,
        );
      }
    });

  it('set example requests on a muted panel with no rule', async () => {
    const wrapper = await mountGuide();
    const examples = wrapper.findAll('[data-testid="guide-try-request"] li');
    expect(examples).toHaveLength(3);
    for (const example of examples) {
      expectClasses(example.classes(), ['rounded-[10px]', 'bg-muted']);
      expect(joined(example.classes())).not.toMatch(
        / (?:border-l-2|border-brand-indigo) /u,
      );
    }
  });

  it('underline links inside sentences', async () => {
    const wrapper = await mountGuide();
    const links = wrapper.findAll('[data-testid="guide-mcp-page"] a');
    expect(links.length).toBeGreaterThan(3);
    for (const link of links) {
      expectClasses(link.classes(), [
        'text-link', 'underline', 'decoration-1', 'underline-offset-4',
      ]);
    }
  });

  it('keep hex and retired tokens out of the guide markup', () => {
    for (const path of SOURCES) {
      expect(webSource(path), path).not.toMatch(RETIRED_STYLE);
    }
  });
});
