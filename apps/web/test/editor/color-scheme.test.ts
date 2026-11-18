import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { mount } from '@vue/test-utils';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import type { Content, Customization } from '@aboutme/schema';
import { TEMPLATES } from '@aboutme/schema/templates';
import { computed, defineComponent, h, nextTick, ref } from 'vue';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import CustomizationPanel from
  '../../app/components/editor/customization/CustomizationPanel.vue';
import EditorPreview from '../../app/components/editor/EditorPreview.vue';
import { applyTemplate } from '../../app/components/resume/applyTemplate';
import type { ResumeEditorActions } from
  '../../app/composables/useResumeEditor';
import { applyIntent } from '../../app/editor/commands';
import { diffCustomization } from '../../app/editor/templateDiff';
import type { ResumeRecord } from '../../app/stores/resumes';
import { setSiteLocale } from '../support/locale';
import { acceptedFixture } from './fixture';

const locale = ref<'vi' | 'en'>('en');
mockNuxtImport('useLocale', () => () => ({ locale }));

beforeEach(() => {
  locale.value = 'en';
  setSiteLocale('en');
  window.localStorage.clear();
});

type Scheme = 'light' | 'dark' | 'system';

function withScheme(
  customization: Customization,
  colorScheme?: Scheme,
): Customization {
  const { colorScheme: _stored, ...rest } = customization;
  return colorScheme === undefined ? rest : { ...rest, colorScheme };
}

function recordFor(colorScheme?: Scheme): ResumeRecord {
  const current = acceptedFixture();
  current.document.customization = withScheme(
    current.document.customization,
    colorScheme,
  );
  return {
    current,
    accepted: current,
    issues: {},
    pending: [],
    conflicts: [],
    sessionLost: false,
  } as ResumeRecord;
}

function mountDesign(record: ResumeRecord) {
  const edit = vi.fn();
  const wrapper = mount(CustomizationPanel, {
    props: {
      actions: {
        edit,
        record: computed(() => record),
      } as unknown as ResumeEditorActions,
      record,
    },
  });
  return {
    edit,
    select: wrapper.get('[data-field="colorScheme"] select'),
    wrapper,
  };
}

describe('Web page theme field (docs/design/public-page-theme.md)', () => {
  it('is the first item of the Colors group, above the five colors', () => {
    const { wrapper } = mountDesign(recordFor());
    const group = wrapper.get('[data-customization-group="Colors"]');
    const fields = [...new Set(group.findAll('[data-field]')
      .map((field) => field.attributes('data-field')))];
    expect(fields).toEqual([
      'colorScheme',
      'colors.primary',
      'colors.text',
      'colors.background',
      'colors.accent',
      'colors.surface',
    ]);
  });

  it('shows Light for an absent value, with the English copy', () => {
    const { select, wrapper } = mountDesign(recordFor());
    expect((select.element as HTMLSelectElement).value).toBe('light');
    expect(select.findAll('option').map((option) => option.text()))
      .toEqual(['Light', 'Dark', 'Match device']);
    expect(wrapper.get('[data-field="colorScheme"] label').text())
      .toBe('Web page theme');
    expect(wrapper.get('[data-field="colorScheme"]').text()).toContain(
      'Readers see this on your public page. The PDF and print stay light.',
    );
  });

  it('uses the Vietnamese copy', async () => {
    const { wrapper } = mountDesign(recordFor());
    locale.value = 'vi';
    await nextTick();
    const field = wrapper.get('[data-field="colorScheme"]');
    expect(field.get('label').text()).toBe('Giao diện trang web');
    expect(field.findAll('option').map((option) => option.text()))
      .toEqual(['Sáng', 'Tối', 'Theo thiết bị']);
    expect(field.text()).toContain(
      'Người xem thấy giao diện này trên trang công khai. '
      + 'PDF và bản in luôn sáng.',
    );
  });

  it.each(['dark', 'system'] as const)('sets %s', async (value) => {
    const { edit, select } = mountDesign(recordFor());
    await select.setValue(value);
    expect(edit).toHaveBeenCalledWith({
      kind: 'customization',
      deltas: [{ op: 'set', path: 'colorScheme', value }],
    });
  });

  it('clears the key for Light, as text alignment does for Left', async () => {
    for (const stored of ['dark', 'system', 'light'] as const) {
      const { edit, select } = mountDesign(recordFor(stored));
      await select.setValue('light');
      expect(edit).toHaveBeenCalledWith({
        kind: 'customization',
        deltas: [{ op: 'unset', path: 'colorScheme' }],
      });
    }
  });

  it('does not write Light when the value is already absent', async () => {
    const { edit, select } = mountDesign(recordFor());
    await select.setValue('light');
    expect(edit).not.toHaveBeenCalled();
  });

  it('shows a stored value and writes nothing for it again', async () => {
    const { edit, select } = mountDesign(recordFor('dark'));
    expect((select.element as HTMLSelectElement).value).toBe('dark');
    await select.setValue('dark');
    expect(edit).not.toHaveBeenCalled();
  });
});

describe('colorScheme set and unset commands', () => {
  it('sets the leaf and removes the key on unset', () => {
    const snapshot = acceptedFixture();
    const set = applyIntent(snapshot, {
      kind: 'customization',
      deltas: [{ op: 'set', path: 'colorScheme', value: 'system' }],
    });
    expect(set.document.customization.colorScheme).toBe('system');
    const cleared = applyIntent(set, {
      kind: 'customization',
      deltas: [{ op: 'unset', path: 'colorScheme' }],
    });
    expect(cleared.document.customization).not.toHaveProperty('colorScheme');
    expect(snapshot.document.customization).not.toHaveProperty('colorScheme');
  });
});

describe('applyTemplate keeps the color scheme', () => {
  const fixture = acceptedFixture().document;
  const content: Content = fixture.content;

  it.each(TEMPLATES.map((preset) => [preset.id, preset] as const))(
    'carries a stored value over and keeps absence absent for %s',
    (_id, preset) => {
      for (const scheme of ['light', 'dark', 'system'] as const) {
        const stored = withScheme(fixture.customization, scheme);
        expect(applyTemplate(stored, preset, content, 'en').colorScheme)
          .toBe(scheme);
      }
      const absent = withScheme(fixture.customization);
      expect('colorScheme' in applyTemplate(absent, preset, content, 'en'))
        .toBe(false);
    },
  );

  it('ignores a scheme carried by a preset', () => {
    const preset = TEMPLATES[0]!;
    const withPresetScheme = {
      ...preset,
      customization: { ...preset.customization, colorScheme: 'dark' },
    } as typeof preset;
    const next = applyTemplate(
      withScheme(fixture.customization),
      withPresetScheme,
      content,
      'en',
    );
    expect('colorScheme' in next).toBe(false);
  });

  it('leaves the owner value out of the template diff', () => {
    for (const preset of TEMPLATES) {
      for (const scheme of [undefined, 'dark', 'system'] as const) {
        const current = withScheme(fixture.customization, scheme);
        const intended = applyTemplate(current, preset, content, 'en');
        expect(
          diffCustomization(current, intended)
            .filter(({ path }) => path === 'colorScheme'),
          `${preset.id} ${String(scheme)}`,
        ).toEqual([]);
      }
    }
  });

  it('does not change the scheme when only the column count changes', () => {
    const current = withScheme(fixture.customization, 'dark');
    const intended = {
      ...current,
      layout: { ...current.layout, columns: 2 as const },
    };
    expect(diffCustomization(current, intended)).toEqual([
      { op: 'set', path: 'layout.columns', value: 2 },
    ]);
  });
});

describe('EditorPreview color scheme', () => {
  const Probe = defineComponent({
    props: { context: { type: Object, required: true } },
    setup: (props) => () => h('div', {
      'data-probe': '',
      'data-mode': props.context.mode,
      'data-scheme': props.context.colorScheme,
    }),
  });

  async function mountPreview(scheme: Scheme | undefined, mode: 'pdf' | 'web') {
    window.localStorage.setItem('aboutme.editorPreviewMode', mode);
    const accepted = acceptedFixture();
    accepted.document.customization = withScheme(
      accepted.document.customization,
      scheme,
    );
    const wrapper = mount(EditorPreview, {
      props: { document: accepted.document, lng: accepted.metadata.lng },
      global: { stubs: { ResumeDocument: Probe } },
    });
    await nextTick();
    return wrapper;
  }

  it.each(['dark', 'system'] as const)(
    'passes %s to the Web sheets, which paint it without a frame',
    async (scheme) => {
      const wrapper = await mountPreview(scheme, 'web');
      expect(wrapper.find('[data-testid="preview-sheet"]').exists())
        .toBe(false);
      expect(wrapper.find('[data-color-scheme]').exists()).toBe(false);
      expect(wrapper.get('[data-probe]').attributes('data-scheme'))
        .toBe(scheme);
      expect(wrapper.get('[data-probe]').attributes('data-mode'))
        .toBe('sheets');
    },
  );

  it.each([undefined, 'light'] as const)(
    'keeps a %s scheme light on the Web preview',
    async (scheme) => {
      const wrapper = await mountPreview(scheme, 'web');
      expect(wrapper.find('[data-testid="preview-sheet"]').exists())
        .toBe(false);
      expect(wrapper.get('[data-probe]').attributes('data-scheme'))
        .toBeUndefined();
      expect(wrapper.get('[data-probe]').attributes('data-mode'))
        .toBe('sheets');
    },
  );

  it.each(['dark', 'system', 'light', undefined] as const)(
    'keeps the white PDF sheet light for %s',
    async (scheme) => {
      const wrapper = await mountPreview(scheme, 'pdf');
      const sheet = wrapper.get('[data-testid="preview-sheet"]');
      expect(sheet.attributes('data-color-scheme')).toBeUndefined();
      expect(sheet.classes('bg-white')).toBe(true);
      expect(wrapper.get('[data-probe]').attributes('data-scheme'))
        .toBeUndefined();
      expect(wrapper.get('[data-probe]').attributes('data-mode'))
        .toBe('paged');
    },
  );

  it('shows the scheme when the reader switches from PDF to Web', async () => {
    const wrapper = await mountPreview('dark', 'pdf');
    await wrapper.get('[data-mode="web"]').trigger('click');
    expect(wrapper.find('[data-testid="preview-sheet"]').exists()).toBe(false);
    expect(wrapper.get('[data-probe]').attributes('data-scheme'))
      .toBe('dark');
    expect(wrapper.get('[data-probe]').attributes('data-mode'))
      .toBe('sheets');
  });
});

describe('public page skip link focus colors', () => {
  const css = readFileSync(
    resolve(__dirname, '../../app/components/resume/resumeColorScheme.css'),
    'utf8',
  );
  const body = `{
    background: #141210;
    color: #b59bdb;
    outline-color: #cdbcd9;
  }`;
  const luminance = (hex: string): number => {
    const [red, green, blue] = [1, 3, 5].map((start) => {
      const value = Number.parseInt(hex.slice(start, start + 2), 16) / 255;
      return value <= 0.04045
        ? value / 12.92
        : ((value + 0.055) / 1.055) ** 2.4;
    });
    return 0.2126 * red! + 0.7152 * green! + 0.0722 * blue!;
  };
  const contrast = (one: string, two: string): number => {
    const [light, dark] = [luminance(one), luminance(two)]
      .sort((a, b) => b - a);
    return (light! + 0.05) / (dark! + 0.05);
  };

  it('grounds the dark skip link on the dark desk with legible text', () => {
    expect(contrast('#b59bdb', '#141210')).toBeGreaterThanOrEqual(4.5);
    expect(css).not.toContain('#0e0a0d');
  });

  it.each([
    ['dark', '@media screen {'],
    ['system', '@media screen and (prefers-color-scheme: dark) {'],
  ] as const)('maps the %s page skip link to the bar tokens', (scheme, at) => {
    const selector = `:root:has(.public-resume-page[data-color-scheme="${
      scheme}"]) a[href="#public-resume"]:focus`;
    const start = css.indexOf(at);
    const end = css.indexOf('\n}\n', start);
    const block = css.slice(start, end);
    expect(block).toContain(`${selector} ${body}`);
  });
});
