import { mount } from '@vue/test-utils';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import type { Customization } from '@aboutme/schema';
import { computed, nextTick, ref } from 'vue';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import CustomizationPanel from
  '../../app/components/editor/customization/CustomizationPanel.vue';
import type { ResumeEditorActions } from
  '../../app/composables/useResumeEditor';
import type { ResumeRecord } from '../../app/stores/resumes';
import { acceptedFixture } from './fixture';

const locale = ref<'vi' | 'en'>('en');
mockNuxtImport('useLocale', () => () => ({ locale }));

beforeEach(() => {
  locale.value = 'en';
});

// Photo position in the Design panel (ADR 0044): absent means top, and a
// side position on a resume with no header creates the default header.

const HEADER = {
  align: 'center',
  detailsLayout: 'stacked',
  iconStyle: 'none',
} as const;

function recordFor(
  header: Customization['header'],
  withPhoto: boolean,
): ResumeRecord {
  const current = acceptedFixture();
  const { header: _header, ...customization } = current.document.customization;
  current.document.customization = header === undefined
    ? customization
    : { ...customization, header };
  const { photo: _photo, ...personal } = current.document.personalDetails;
  current.document.personalDetails = withPhoto
    ? { ...personal, photo: { key: 'photo-1' } }
    : personal;
  return {
    current,
    accepted: current,
    issues: {},
    pending: [],
    conflicts: [],
    sessionLost: false,
  } as ResumeRecord;
}

function mountPanel(header: Customization['header'], withPhoto = true) {
  const edit = vi.fn();
  const record = recordFor(header, withPhoto);
  const wrapper = mount(CustomizationPanel, {
    props: {
      actions: {
        edit,
        record: computed(() => record),
      } as unknown as ResumeEditorActions,
      record,
    },
  });
  const field = wrapper.get('[data-field="header.photoPosition"]');
  return { edit, field, select: field.get('select'), wrapper };
}

describe('Photo position control', () => {
  it('offers Top, Left, and Right in the Headings group', async () => {
    const { field, select, wrapper } = mountPanel(undefined);
    expect(select.findAll('option').map((option) => option.text()))
      .toEqual(['Top', 'Left', 'Right']);
    expect((select.element as HTMLSelectElement).value).toBe('top');
    expect(field.get('label').text()).toBe('Photo position');
    expect(wrapper.get('[data-customization-group="Headings"]')
      .find('[data-field="header.photoPosition"]').exists()).toBe(true);

    locale.value = 'vi';
    await nextTick();

    expect(wrapper.get('[data-customization-group="Headings"]')
      .find('[data-field="header.photoPosition"]').exists()).toBe(true);
  });

  it('hints when there is no photo, and stays enabled', () => {
    const noPhoto = mountPanel(undefined, false);
    expect(noPhoto.field.text())
      .toContain('Takes effect when you add a photo.');
    expect(noPhoto.select.attributes('disabled')).toBeUndefined();
    expect(mountPanel(undefined).field.text())
      .not.toContain('Takes effect when you add a photo.');
  });

  it('creates the default header for a side photo on a resume without one',
    async () => {
      const { edit, select } = mountPanel(undefined);
      await select.setValue('left');
      expect(edit).toHaveBeenCalledWith({
        kind: 'customization',
        deltas: [
          { op: 'set', path: 'header.align', value: 'left' },
          { op: 'set', path: 'header.detailsLayout', value: 'inline' },
          { op: 'set', path: 'header.iconStyle', value: 'outline' },
          { op: 'set', path: 'header.photoPosition', value: 'left' },
        ],
      });
    });

  it('sets only the position on an existing header', async () => {
    const { edit, select } = mountPanel(HEADER);
    await select.setValue('right');
    expect(edit).toHaveBeenCalledWith({
      kind: 'customization',
      deltas: [{ op: 'set', path: 'header.photoPosition', value: 'right' }],
    });
  });

  it('clears a stored side position with Top, and writes nothing otherwise',
    async () => {
      const stored = mountPanel({ ...HEADER, photoPosition: 'left' });
      expect((stored.select.element as HTMLSelectElement).value).toBe('left');
      await stored.select.setValue('top');
      expect(stored.edit).toHaveBeenCalledWith({
        kind: 'customization',
        deltas: [{ op: 'unset', path: 'header.photoPosition' }],
      });

      for (const header of [undefined, HEADER] as const) {
        const { edit, select } = mountPanel(header);
        await select.setValue('top');
        expect(edit).not.toHaveBeenCalled();
      }
    });
});
