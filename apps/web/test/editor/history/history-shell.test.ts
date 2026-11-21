import { flushPromises } from '@vue/test-utils';
import { mountSuspended } from '@nuxt/test-utils/runtime';
import { computed } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import EditorShell from '../../../app/components/editor/EditorShell.vue';
import type {
  ResumeEditorActions,
} from '../../../app/composables/useResumeEditor';
import type { ResumeRecord } from '../../../app/stores/resumes';
import { setSiteLocale } from '../../support/locale';
import { fakeRecord } from './support';

beforeEach(() => setSiteLocale('en'));
afterEach(() => {
  document.body.innerHTML = '';
  vi.unstubAllGlobals();
  setSiteLocale(undefined);
});

function actionsFor(record: ResumeRecord): ResumeEditorActions {
  return {
    record: computed(() => record),
    refresh: vi.fn(),
    discard: vi.fn(),
    downloadPdf: {
      state: computed(() => ({ kind: 'idle' })),
      start: vi.fn(),
      cancel: vi.fn(),
    },
    publish: { state: computed(() => ({ kind: 'idle' })), cancel: vi.fn() },
  } as unknown as ResumeEditorActions;
}

describe('History in the editor shell', () => {
  it('adds a pressed History rail tool that opens the panel', async () => {
    const record = fakeRecord() as unknown as ResumeRecord;
    const wrapper = await mountSuspended(EditorShell, {
      attachTo: document.body,
      props: { actions: actionsFor(record), record },
      route: '/app/resumes/resume-1',
      global: {
        stubs: {
          EditorPreview: { name: 'EditorPreview', template: '<div />' },
          PersonalDetailsPanel: { template: '<div />' },
        },
      },
    });
    const rail = wrapper.get('[data-region="app-rail"]');
    const tools = rail.findAll('[data-action]');
    expect(tools.at(-1)!.attributes('data-action')).toBe('open-history');
    expect(tools).toHaveLength(6);
    expect(rail.classes()).toContain('max-[22.5rem]:gap-1');

    const button = wrapper.get('[data-action="open-history"]');
    expect(button.attributes('aria-label')).toBe('History');
    expect(button.attributes('aria-pressed')).toBe('false');
    await button.trigger('click');
    await flushPromises();
    expect(
      wrapper.get('[data-action="open-history"]').attributes('aria-pressed'),
    ).toBe('true');
    expect(wrapper.get('[data-region="inspector"] h2').text()).toBe('History');

    await wrapper.get('[data-action="open-design"]').trigger('click');
    expect(wrapper.find('#history-title').exists()).toBe(false);
  });
});
