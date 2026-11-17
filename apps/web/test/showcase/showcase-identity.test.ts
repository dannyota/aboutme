import { mountSuspended } from '@nuxt/test-utils/runtime';
import { afterEach, describe, expect, it } from 'vitest';

import ShowcaseInvite from '../../app/components/showcase/ShowcaseInvite.vue';
import {
  declared, RETIRED_STYLE, sfcRules, webSource,
} from '../support/sfcStyles';

const FIELDS = 'app/components/showcase/ShowcaseFilters.vue';
const BAR = 'app/components/app/FilterBar.vue';
const TILE = 'app/components/showcase/ShowcaseTile.vue';
const INVITE = 'app/components/showcase/ShowcaseInvite.vue';
const PAGE = 'app/pages/showcase.vue';
const SHEET = 'app/components/app/FilterSheet.vue';
const OPTION = '.showcase-filters :deep(.showcase-option)';

const mounted: { unmount(): void }[] = [];
afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
});

describe('showcase filter identity', () => {
  it('draws idle options transparent on the control edge', () => {
    const rules = sfcRules(FIELDS);
    expect(declared(rules, OPTION)).toMatchObject({
      'border': '1px solid var(--control)',
      'background': 'transparent',
      'color': 'var(--foreground)',
      'font-weight': '500',
    });
    const hover = '.showcase-filters :deep(.showcase-option'
      + ':not([data-state="on"]):hover)';
    expect(declared(rules, hover)).toEqual({ background: 'var(--accent)' });
  });

  it('presses a sheet option into an ink pill', () => {
    const rules = sfcRules(FIELDS);
    for (const state of ['[aria-pressed="true"]', '[data-state="on"]']) {
      expect(declared(rules,
        `.showcase-filters :deep(.showcase-option${state})`), state).toEqual({
        'border-color': 'var(--foreground)',
        'background': 'var(--foreground)',
        'color': 'var(--background)',
        'font-weight': '600',
      });
    }
  });

  it('marks the pressed rail row with an accent fill and an ink dot', () => {
    const rules = sfcRules(FIELDS);
    expect(declared(rules,
      '.showcase-filters--rail :deep(.showcase-option[data-state="on"])'))
      .toMatchObject({
        'background': 'var(--accent)',
        'color': 'var(--foreground)',
        'font-weight': '600',
      });
    const dot = '.showcase-filters--rail '
      + ':deep(.showcase-option[data-state="on"]::after)';
    expect(declared(rules, dot)).toMatchObject({
      'width': '8px',
      'height': '8px',
      'border-radius': '9999px',
      'background': 'var(--foreground)',
    });
  });

  it('turns the active Filters edge and badge ink', () => {
    const rules = sfcRules(BAR);
    expect(declared(rules, '.showcase-filters-button.is-active')).toEqual({
      'padding-right': '10px',
      'border-color': 'var(--foreground)',
    });
    expect(declared(rules, '.showcase-filters-badge')).toMatchObject({
      'min-width': '22px',
      'height': '22px',
      'padding': '0 6px',
      'border-radius': '9999px',
      'background': 'var(--foreground)',
      'color': 'var(--background)',
      'font-weight': '700',
    });
  });

  it('rounds the bottom sheet top corners with the card radius', () => {
    const source = webSource(SHEET);
    expect(source).toContain('rounded-t-[var(--radius-card)]');
    expect(source).not.toContain('radius-feature');
  });
});

describe('showcase results identity', () => {
  it('keeps tiles and their skeletons flat', () => {
    const tile = declared(sfcRules(TILE), '.showcase-tile');
    expect(tile).toMatchObject({
      'border': '1px solid var(--border)',
      'border-radius': 'var(--radius-dialog)',
      'background': 'var(--card)',
    });
    expect(tile['box-shadow']).toBeUndefined();
    expect(declared(sfcRules(PAGE), '.showcase-skeleton')['box-shadow'])
      .toBeUndefined();
  });

  it('labels the role on a mist pill', () => {
    expect(declared(sfcRules(TILE), '.showcase-tile__role')).toMatchObject({
      'height': '24px',
      'padding': '0 10px',
      'border-radius': '9999px',
      'background': 'var(--surface-mist)',
      'color': 'var(--foreground)',
      'font-weight': '600',
    });
  });

  it('sets the invite on borderless mist with the card radius', () => {
    const invite = declared(sfcRules(INVITE), '.showcase-invite');
    expect(invite).toMatchObject({
      'background': 'var(--surface-mist)',
      'border-radius': 'var(--radius-card)',
    });
    expect(invite['border']).toBeUndefined();
    expect(invite['box-shadow']).toBeUndefined();
  });

  it('makes the invite action an outline pill', async () => {
    const wrapper = await mountSuspended(ShowcaseInvite, {
      props: { locale: 'en', to: '/register' },
    });
    mounted.push(wrapper);
    const action = wrapper.get('[data-action="showcase-invite-create"]');
    expect(action.text()).toBe('Create a free resume');
    expect(action.attributes('href')).toBe('/register');
    for (const name of [
      'rounded-full', 'border-[1.5px]', 'border-foreground', 'bg-transparent',
      'h-11', 'px-4', 'text-[15px]', 'font-semibold', 'justify-self-start',
    ]) {
      expect(action.classes(), name).toContain(name);
    }
    expect(action.classes()).not.toContain('bg-primary');
  });

  it('keeps removed tokens, the main fill and hex out of the showcase', () => {
    for (const path of [FIELDS, BAR, TILE, INVITE, PAGE, SHEET,
      'app/components/app/ActiveFilters.vue']) {
      expect(webSource(path), path).not.toMatch(RETIRED_STYLE);
    }
  });
});
