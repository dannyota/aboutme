import { mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises, mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import { defineComponent, h } from 'vue';

import ShowcaseFilters from '../../app/components/showcase/ShowcaseFilters.vue';
import type { ShowcaseFilters as Filters } from '../../app/lib/showcaseQuery';
import {
  Sheet,
  SheetContent,
  SheetTitle,
} from '../../app/components/ui/sheet';

// The shared filter fields of the sheet and the rail, and the sheet
// primitive's optional built-in close (docs/design/ui/showcase.md, Bottom
// sheet and Rail from 1024 px; AC-SHOW-005).

const mounted: { unmount(): void }[] = [];

afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
});

async function mountFields(
  layout: 'sheet' | 'rail',
  filters: Filters = {},
  locale: 'en' | 'vi' = 'en',
) {
  const wrapper = await mountSuspended(ShowcaseFilters, {
    props: { filters, layout, locale },
  });
  mounted.push(wrapper);
  return wrapper;
}

describe('shared filter fields', () => {
  it.each(['sheet', 'rail'] as const)('renders every field in the %s layout',
    async (layout) => {
      const wrapper = await mountFields(layout);
      const fields = wrapper.get('[data-testid="showcase-filters"]');
      expect(fields.classes()).toContain(`showcase-filters--${layout}`);
      expect(fields.findAll('[data-testid="showcase-roles"] [data-role]'))
        .toHaveLength(11);
      expect(fields.findAll(
        '[data-testid="showcase-languages"] [data-lang]',
      ).map((option) => option.text()))
        .toEqual(['All languages', 'Vietnamese', 'English']);
      const select = fields.get('select[name="template"]');
      expect(select.findAll('option')).toHaveLength(22);
      expect(fields.findAll('.showcase-filters__heading')
        .map((heading) => heading.text())).toEqual(['Role', 'Resume language']);
      expect(fields.get('label').text()).toBe('Template');
    });

  it('names the groups: Filter by role, and the language heading', async () => {
    const wrapper = await mountFields('rail');
    expect(wrapper.get('[data-testid="showcase-roles"]')
      .attributes('aria-label')).toBe('Filter by role');
    const heading = wrapper.findAll('.showcase-filters__heading')[1]!;
    expect(wrapper.get('[data-testid="showcase-languages"]')
      .attributes('aria-labelledby')).toBe(heading.attributes('id'));
  });

  it('shows full language names in Vietnamese, never VI or EN', async () => {
    const wrapper = await mountFields('rail', {}, 'vi');
    expect(wrapper.findAll('[data-testid="showcase-languages"] [data-lang]')
      .map((option) => option.text()))
      .toEqual(['Mọi ngôn ngữ', 'Tiếng Việt', 'Tiếng Anh']);
    expect(wrapper.findAll('.showcase-filters__heading')
      .map((heading) => heading.text()))
      .toEqual(['Vị trí', 'Ngôn ngữ của CV']);
  });

  it('marks the pressed options and the chosen template', async () => {
    const wrapper = await mountFields('sheet', {
      role: 'qa',
      lang: 'vi',
      template: 'custom',
    });
    expect(wrapper.get('[data-role="qa"]').attributes('aria-pressed'))
      .toBe('true');
    expect(wrapper.get('[data-role="all"]').attributes('aria-pressed'))
      .toBe('false');
    expect(wrapper.get('[data-lang="vi"]').attributes('aria-pressed'))
      .toBe('true');
    expect((wrapper.get('select').element as HTMLSelectElement).value)
      .toBe('custom');
  });

  it('emits the whole next filter set for each change', async () => {
    const wrapper = await mountFields('sheet', { role: 'qa' });
    await wrapper.get('[data-lang="en"]').trigger('click');
    await wrapper.get('[data-role="all"]').trigger('click');
    await wrapper.get('select').setValue('custom');
    await wrapper.get('select').setValue('');
    expect(wrapper.emitted('change')).toEqual([
      [{ role: 'qa', lang: 'en' }],
      [{ role: undefined }],
      [{ role: 'qa', template: 'custom' }],
      [{ role: 'qa' }],
    ]);
  });

  it('keeps a pressed option pressed when it is pressed again', async () => {
    const wrapper = await mountFields('rail', { role: 'qa', lang: 'en' });
    await wrapper.get('[data-role="qa"]').trigger('click');
    await wrapper.get('[data-lang="en"]').trigger('click');
    expect(wrapper.emitted('change')).toBeUndefined();
  });

  it('gives the rail and the sheet copies different element ids', async () => {
    // One app holds both copies, as the page does: useId() numbers them per
    // app, so two separate mounts would both start at the same id.
    const Both = defineComponent({
      setup() {
        return () => h('div', [
          h('div', { 'data-copy': 'rail' }, [h(ShowcaseFilters, {
            filters: {}, layout: 'rail', locale: 'en',
          })]),
          h('div', { 'data-copy': 'sheet' }, [h(ShowcaseFilters, {
            filters: {}, layout: 'sheet', locale: 'en',
          })]),
        ]);
      },
    });
    const wrapper = await mountSuspended(Both);
    mounted.push(wrapper);
    const ids = (copy: string): string[] => wrapper
      .findAll(`[data-copy="${copy}"] [id]`)
      .map((element) => element.attributes('id')!);
    const railIds = ids('rail');
    expect(railIds.length).toBeGreaterThan(0);
    for (const id of ids('sheet')) expect(railIds).not.toContain(id);
    // The label points at its own select.
    const sheet = wrapper.get('[data-copy="sheet"]');
    expect(sheet.get('label').attributes('for'))
      .toBe(sheet.get('select').attributes('id'));
  });
});

describe('sheet content close button', () => {
  const Harness = defineComponent({
    props: { showClose: { type: Boolean, default: undefined } },
    setup(props) {
      return () => h(Sheet, { open: true }, () => h(
        SheetContent,
        {
          'aria-describedby': undefined,
          'showClose': props.showClose,
          'data-testid': 'harness-sheet',
        },
        () => h(SheetTitle, null, () => 'Title'),
      ));
    },
  });

  async function content(showClose?: boolean): Promise<HTMLElement> {
    const wrapper = mount(Harness, {
      props: showClose === undefined ? {} : { showClose },
      attachTo: document.body,
    });
    mounted.push(wrapper);
    await flushPromises();
    return document.body.querySelector<HTMLElement>(
      '[data-testid="harness-sheet"]',
    )!;
  }

  it('keeps the built-in close icon by default', async () => {
    const sheet = await content();
    const buttons = sheet.querySelectorAll('button');
    expect(buttons).toHaveLength(1);
    expect(buttons[0]!.textContent).toContain('Close');
  });

  it('keeps it when showClose is true', async () => {
    const sheet = await content(true);
    expect(sheet.querySelectorAll('button')).toHaveLength(1);
  });

  it('draws no built-in close when showClose is false', async () => {
    const sheet = await content(false);
    expect(sheet.querySelectorAll('button')).toHaveLength(0);
    expect(sheet.textContent).not.toContain('Close');
  });

  it('never passes showClose to the DOM', async () => {
    for (const value of [undefined, true, false]) {
      const sheet = await content(value);
      expect(sheet.hasAttribute('showclose')).toBe(false);
      expect(sheet.hasAttribute('show-close')).toBe(false);
      mounted.pop()!.unmount();
    }
  });
});
