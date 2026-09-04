import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import StateMark from '../../app/components/app/StateMark.vue';
import SaveStatus from '../../app/components/editor/SaveStatus.vue';
import type { SaveState } from '../../app/editor/types';

describe('StateMark', () => {
  it.each([
    ['saved', 'Saved'],
    ['saving', 'Saving…'],
    ['failed', 'Save failed'],
    ['draft', 'Draft'],
  ] as const)('renders %s as fixed text', (state, text) => {
    const wrapper = mount(StateMark, { props: { state } });

    expect(wrapper.get(`[data-state-mark="${state}"]`).text()).toContain(text);
  });

  it('renders the saved pencil tick as an accessible-text companion', () => {
    const wrapper = mount(StateMark, { props: { state: 'saved' } });

    expect(
      wrapper.get('[data-state-glyph="saved"]').attributes('aria-hidden'),
    ).toBe('true');
  });

  it('announces saving politely', () => {
    const wrapper = mount(StateMark, { props: { state: 'saving' } });

    expect(wrapper.attributes('aria-live')).toBe('polite');
  });

  it('announces failure as an alert with destructive text', () => {
    const wrapper = mount(StateMark, { props: { state: 'failed' } });

    expect(wrapper.attributes('role')).toBe('alert');
    expect(wrapper.classes()).toContain('text-destructive');
  });

  it('renders the public seal mark and canonical path link', () => {
    const wrapper = mount(StateMark, {
      props: { state: 'public', link: '/ada-lovelace' },
    });

    expect(wrapper.get('[data-app-seal="mark"]').attributes('aria-label')).toBe(
      'Public at aboutme.vn/ada-lovelace',
    );
    expect(wrapper.get('[data-public-link]').attributes('href')).toBe(
      '/ada-lovelace',
    );
    expect(wrapper.get('[data-public-link]').text()).toBe(
      'aboutme.vn/ada-lovelace',
    );
    expect(wrapper.attributes('role')).toBeUndefined();
  });

  it('requires a link for the public state', () => {
    expect(() => mount(StateMark, { props: { state: 'public' } })).toThrow(
      'StateMark public state requires a link.',
    );
  });
});

describe('SaveStatus adapter', () => {
  it.each([
    ['idle', 'saved'],
    ['saved', 'saved'],
    ['saving', 'saving'],
    ['dirty', 'draft'],
    ['offline', 'failed'],
    ['error', 'failed'],
    ['conflict', 'failed'],
    ['session-lost', 'failed'],
  ] as const)('maps %s to %s', (state: SaveState, expected) => {
    const wrapper = mount(SaveStatus, { props: { state } });

    expect(wrapper.get('[data-state-mark]').attributes('data-state-mark')).toBe(
      expected,
    );
    expect(wrapper.attributes('data-state')).toBe(state);
    expect(wrapper.attributes('role')).toBe('status');
  });
});
