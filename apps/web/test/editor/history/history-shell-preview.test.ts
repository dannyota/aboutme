import { flushPromises } from '@vue/test-utils';
import { mountSuspended } from '@nuxt/test-utils/runtime';
import { CURRENT_VERSION } from '@aboutme/schema/released';
import { computed } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import EditorShell from '../../../app/components/editor/EditorShell.vue';
import type {
  ResumeEditorActions,
} from '../../../app/composables/useResumeEditor';
import type { ResumeRecord } from '../../../app/stores/resumes';
import { useResumeStore } from '../../../app/stores/resumes';
import { setSiteLocale } from '../../support/locale';
import { acceptedFixture } from '../fixture';
import { fakeRecord } from './support';

const photo = { key: 'photo-current' };

function body(data: unknown): Response {
  return new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

beforeEach(() => {
  setSiteLocale('en');
  const base = acceptedFixture().document;
  const { photo: _drop, ...personalDetails } = base.personalDetails;
  vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
    const path = new URL(request.url, 'http://x').pathname;
    const summary = {
      id: 'cp-1',
      startedAt: new Date().toISOString(),
      cause: 'edit',
      actor: { kind: 'owner' },
      title: 'Older title',
      byteSize: 10,
    };
    if (path.endsWith('/checkpoints')) return body([summary]);
    return body({
      ...summary,
      lng: 'vi',
      schemaVersion: CURRENT_VERSION,
      document: {
        personalDetails: { ...personalDetails, headline: 'From the past' },
        content: base.content,
        customization: base.customization,
      },
    });
  }));
});
afterEach(() => {
  document.body.innerHTML = '';
  vi.unstubAllGlobals();
  setSiteLocale(undefined);
});

describe('History preview in the shell', () => {
  it('replaces the preview, keeps the photo, writes nothing', async () => {
    const record = fakeRecord() as unknown as ResumeRecord;
    (record.current.document.personalDetails as { photo?: unknown }).photo
      = photo;
    const before = JSON.stringify(record);
    const actions = {
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
    const wrapper = await mountSuspended(EditorShell, {
      attachTo: document.body,
      props: { actions, record },
      route: '/app/resumes/resume-1',
      global: {
        stubs: {
          EditorPreview: {
            name: 'EditorPreview',
            props: ['document', 'lng', 'label'],
            template: '<div data-stub-preview />',
          },
        },
      },
    });
    const live = wrapper.findComponent({ name: 'EditorPreview' });
    expect(live.props('document').personalDetails.headline)
      .not.toBe('From the past');
    await wrapper.get('[data-action="open-history"]').trigger('click');
    await flushPromises();
    await wrapper.get('[data-version-id="cp-1"]').trigger('click');
    await flushPromises();

    const shown = wrapper.findAllComponents({ name: 'EditorPreview' });
    expect(shown).toHaveLength(1);
    expect(shown[0]!.props('document').personalDetails.headline)
      .toBe('From the past');
    expect(shown[0]!.props('document').personalDetails.photo).toEqual(photo);
    expect(shown[0]!.props('lng')).toBe('vi');
    expect(wrapper.find('[data-history-bar]').exists()).toBe(true);
    expect(JSON.stringify(record)).toBe(before);
    expect(useResumeStore().recordFor('resume-1')).toBeUndefined();

    await wrapper.get('[data-action="history-back"]').trigger('click');
    await flushPromises();
    expect(wrapper.find('[data-history-bar]').exists()).toBe(false);
    expect(
      wrapper.findComponent({ name: 'EditorPreview' }).props('document')
        .personalDetails.headline,
    ).not.toBe('From the past');
  });
});
