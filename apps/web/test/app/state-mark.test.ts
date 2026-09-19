import { mount } from '@vue/test-utils';
import { mountSuspended } from '@nuxt/test-utils/runtime';
import { describe, expect, it } from 'vitest';

import StateMark from '../../app/components/app/StateMark.vue';
import SaveStatus from '../../app/components/editor/SaveStatus.vue';
import type { SaveState } from '../../app/editor/types';
import { setSiteLocale } from '../support/locale';

describe('StateMark', () => {
  it.each([
    ['vi', 'Đã lưu'],
    ['vi', 'Chưa lưu', 'unsaved'],
    ['vi', 'Đang lưu…', 'saving'],
    ['vi', 'Lưu không thành công', 'failed'],
    ['vi', 'Bản nháp', 'draft'],
    ['en', 'Saved'],
    ['en', 'Unsaved', 'unsaved'],
    ['en', 'Saving…', 'saving'],
    ['en', 'Save failed', 'failed'],
    ['en', 'Draft', 'draft'],
  ] as const)('renders %s state copy in %s',
    async (locale, text, state = 'saved') => {
      setSiteLocale(locale);
      const wrapper = await mountSuspended(StateMark, {
        props: { state },
        route: '/app/resumes/resume-1',
      });

      expect(wrapper.get(`[data-state-mark="${state}"]`).text()).toContain(
        text,
      );
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

  it.each([
    ['vi', 'Công khai tại aboutme.vn/ada-lovelace'],
    ['en', 'Public at aboutme.vn/ada-lovelace'],
  ] as const)('renders the public seal mark in %s',
    async (locale, label) => {
      setSiteLocale(locale);
      const wrapper = await mountSuspended(StateMark, {
        props: { state: 'public', link: '/ada-lovelace' },
        route: '/app/resumes/resume-1',
      });

      expect(wrapper.get('[data-app-seal="mark"]').attributes('aria-label'))
        .toBe(label);
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
    ['dirty', 'unsaved'],
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
