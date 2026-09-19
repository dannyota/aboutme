import { mount } from '@vue/test-utils';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import type { Customization } from '@aboutme/schema';
import { nextTick, ref } from 'vue';
import { beforeEach, describe, expect, it } from 'vitest';

import PageSettings from
  '../../app/components/editor/customization/PageSettings.vue';
import {
  axisDeltas,
  formatLength,
  fromDisplay,
  marginChoice,
  presetDeltas,
  presetLabel,
  toDisplay,
} from '../../app/components/editor/customization/pageSettings';
import PDFDownloadButton from
  '../../app/components/editor/PDFDownloadButton.vue';
import type { PdfDownloadController } from '../../app/editor/pdfDownload';
import { acceptedFixture } from './fixture';

const locale = ref<'vi' | 'en'>('en');
mockNuxtImport('useLocale', () => () => ({ locale }));

beforeEach(() => {
  locale.value = 'en';
});

const UNSET = [{ op: 'unset', path: 'spacing.pageMargin' }];

function both(mm: number) {
  return [
    { op: 'set', path: 'spacing.pageMargin.x', value: mm },
    { op: 'set', path: 'spacing.pageMargin.y', value: mm },
  ];
}

describe('margin presets map to stored millimetres and back', () => {
  it.each([
    [undefined, 'normal'],
    [{ x: 15, y: 15 }, 'normal'],
    [{ x: 10, y: 10 }, 'narrow'],
    [{ x: 25, y: 25 }, 'wide'],
    [{ x: 25, y: 20 }, 'custom'],
    [{ x: 12, y: 12 }, 'custom'],
    [{ x: 0, y: 0 }, 'custom'],
  ] as const)('reads %j as %s', (margin, choice) => {
    expect(marginChoice(margin)).toBe(choice);
  });

  it.each([
    ['narrow', undefined, both(10)],
    ['wide', { x: 25, y: 20 }, both(25)],
    ['wide', { x: 25, y: 25 }, []],
    ['normal', { x: 10, y: 10 }, UNSET],
    ['normal', { x: 15, y: 15 }, UNSET],
    ['normal', undefined, []],
    ['custom', { x: 25, y: 20 }, []],
    ['custom', undefined, []],
  ] as const)('writes %s over %j', (choice, margin, deltas) => {
    expect(presetDeltas(choice, margin)).toEqual(deltas);
  });

  it('round-trips every preset through its deltas', () => {
    for (const choice of ['narrow', 'wide'] as const) {
      const [x, y] = presetDeltas(choice, undefined) as {
        value: number;
      }[];
      expect(marginChoice({ x: x!.value, y: y!.value })).toBe(choice);
    }
  });

  it('writes one custom axis, or both when none is stored', () => {
    expect(axisDeltas('x', 20, { x: 25, y: 20 })).toEqual([
      { op: 'set', path: 'spacing.pageMargin.x', value: 20 },
    ]);
    expect(axisDeltas('x', 25, { x: 25, y: 20 })).toEqual([]);
    expect(axisDeltas('y', 8, undefined)).toEqual([
      { op: 'set', path: 'spacing.pageMargin.x', value: 15 },
      { op: 'set', path: 'spacing.pageMargin.y', value: 8 },
    ]);
  });

  it('shows millimetres on A4 and inches on Letter', () => {
    expect(presetLabel('narrow', 'a4')).toBe('Narrow · 10 mm');
    expect(presetLabel('normal', 'letter')).toBe('Normal · 0.6 in');
    expect(presetLabel('wide', 'letter')).toBe('Wide · 1 in');
    expect(formatLength(5, 'letter')).toBe('0.2 in');
    expect(toDisplay(25.4, 'letter')).toBe(1);
    expect(toDisplay(15, 'a4')).toBe(15);
  });

  it('converts typed inches back to millimetres within 0–40 mm', () => {
    expect(fromDisplay(1, 'letter')).toBe(25.4);
    expect(fromDisplay(0.75, 'letter')).toBe(19.05);
    expect(fromDisplay(1.6, 'letter')).toBeUndefined();
    expect(fromDisplay(40, 'a4')).toBe(40);
    expect(fromDisplay(40.5, 'a4')).toBeUndefined();
    expect(fromDisplay(-1, 'a4')).toBeUndefined();
    expect(fromDisplay(Number.NaN, 'a4')).toBeUndefined();
  });
});

function customization(
  patch: {
    pageFormat?: Customization['pageFormat'];
    pageMargin?: { x: number; y: number };
  } = {},
): Customization {
  const base = structuredClone(acceptedFixture().document.customization);
  const { pageMargin: _stored, ...spacing } = base.spacing;
  return {
    ...base,
    pageFormat: patch.pageFormat ?? 'a4',
    spacing: patch.pageMargin === undefined
      ? spacing
      : { ...spacing, pageMargin: patch.pageMargin },
  };
}

function mountPage(value: Customization) {
  const wrapper = mount(PageSettings, { props: { customization: value } });
  return {
    wrapper,
    margins: () => wrapper.get('[data-field="spacing.pageMargin"] select'),
    commits: () => wrapper.emitted('commit') ?? [],
  };
}

describe('Page & PDF group', () => {
  it('names the page sizes and says what the settings affect', () => {
    const { wrapper } = mountPage(customization());
    expect(wrapper.text()).toContain(
      'Used for the PDF and printing. The web page adapts to the screen.',
    );
    expect(
      wrapper.findAll('[data-field="pageFormat"] option')
        .map((option) => option.text()),
    ).toEqual(['A4 · 210 × 297 mm', 'Letter · 8.5 × 11 in']);
  });

  it('sets the page size', async () => {
    const { wrapper, commits } = mountPage(customization());
    await wrapper.get('[data-field="pageFormat"] select').setValue('letter');
    expect(commits()).toEqual([
      [[{ op: 'set', path: 'pageFormat', value: 'letter' }]],
    ]);
  });

  it('selects a preset and hides the per-axis fields', async () => {
    const { wrapper, margins, commits } = mountPage(customization());
    expect((margins().element as HTMLSelectElement).value).toBe('normal');
    expect(wrapper.find('[data-page-margin-custom]').exists()).toBe(false);

    await margins().setValue('wide');
    expect(commits()).toEqual([[both(25)]]);
  });

  it('opens Custom on the current values without writing', async () => {
    const { wrapper, margins, commits } = mountPage(
      customization({ pageMargin: { x: 10, y: 10 } }),
    );
    await margins().setValue('custom');
    expect(commits()).toEqual([]);
    expect((margins().element as HTMLSelectElement).value).toBe('custom');
    const x = wrapper.get('[data-field="spacing.pageMargin.x"] input');
    expect((x.element as HTMLInputElement).value).toBe('10');
    expect(wrapper.get('[data-field="spacing.pageMargin.x"] label').text())
      .toBe('Left and right (mm)');

    (x.element as HTMLInputElement).value = '12';
    await x.trigger('change');
    expect(commits()).toEqual([
      [[{ op: 'set', path: 'spacing.pageMargin.x', value: 12 }]],
    ]);
  });

  it('shows Custom for stored values that match no preset', () => {
    const { wrapper, margins } = mountPage(
      customization({ pageMargin: { x: 25, y: 20 } }),
    );
    expect((margins().element as HTMLSelectElement).value).toBe('custom');
    expect(
      (wrapper.get('[data-field="spacing.pageMargin.y"] input')
        .element as HTMLInputElement).value,
    ).toBe('20');
  });

  it('edits Letter margins in inches and stores millimetres', async () => {
    const { wrapper, commits } = mountPage(
      customization({ pageFormat: 'letter', pageMargin: { x: 25.4, y: 20 } }),
    );
    const x = wrapper.get('[data-field="spacing.pageMargin.x"] input');
    expect((x.element as HTMLInputElement).value).toBe('1');
    expect(wrapper.get('[data-field="spacing.pageMargin.x"] label').text())
      .toBe('Left and right (in)');

    (x.element as HTMLInputElement).value = '0.75';
    await x.trigger('change');
    expect(commits()).toEqual([
      [[{ op: 'set', path: 'spacing.pageMargin.x', value: 19.05 }]],
    ]);

    (x.element as HTMLInputElement).value = '2';
    await x.trigger('change');
    expect(wrapper.get('[data-error-for="spacing.pageMargin.x"]').text())
      .toBe('Enter a value from 0 to 1.57 in.');
    expect(commits()).toHaveLength(1);
  });

  it('keeps an invalid custom margin draft while changing its message',
    async () => {
      const { wrapper, margins, commits } = mountPage(customization());
      await margins().setValue('custom');
      const x = wrapper.get('[data-field="spacing.pageMargin.x"] input');
      await x.setValue('41');
      await x.trigger('change');
      expect(wrapper.get('[data-error-for="spacing.pageMargin.x"]').text())
        .toBe('Enter a value from 0 to 40 mm.');
      expect(commits()).toEqual([]);

      locale.value = 'vi';
      await nextTick();

      expect((x.element as HTMLInputElement).value).toBe('41');
      expect(wrapper.get('[data-error-for="spacing.pageMargin.x"]').text())
        .toBe('Nhập giá trị từ 0 đến 40 mm.');
      expect(commits()).toEqual([]);
    },
  );

  it('warns when a margin is inside the unprintable edge', () => {
    const { wrapper } = mountPage(
      customization({ pageMargin: { x: 3, y: 12 } }),
    );
    expect(wrapper.get('[data-page-margin-edge]').text()).toBe(
      'Most printers cannot print within 5 mm of the paper edge.',
    );
  });
});

describe('Download PDF page size', () => {
  const controller = {
    state: ref({ kind: 'idle' }),
    download: async () => undefined,
    dispose: () => undefined,
  } as unknown as PdfDownloadController;

  it.each([
    ['a4', 'A4', 'PDF page size: A4 · 210 × 297 mm'],
    ['letter', 'Letter', 'PDF page size: Letter · 8.5 × 11 in'],
  ] as const)('shows %s beside the label', (pageFormat, short, title) => {
    const wrapper = mount(PDFDownloadButton, {
      props: { controller, pageFormat },
    });
    const button = wrapper.get('[data-action="download-pdf"]');
    expect(button.get('[data-download-pdf-size]').text()).toBe(short);
    expect(button.attributes('title')).toBe(title);
    expect(button.attributes('aria-label')).toBe(`Download PDF, ${short}`);
  });
});
