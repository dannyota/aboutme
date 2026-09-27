// The review's action panel: the sticky aside, the DOM/visual order of
// Cancel and Create, the disabled Cancel while creating, and the block
// reason (docs/design/linkedin-import-ui.md, "Action panel").
import { mountSuspended } from '@nuxt/test-utils/runtime';
import { describe, expect, it } from 'vitest';

import ImportActionPanel
  from '../../../app/components/import/ImportActionPanel.vue';
import { importCopy } from '../../../app/i18n/import';
import { resumeCreateCopy } from '../../../app/i18n/resume-create';

function baseProps() {
  return {
    copy: importCopy.en,
    createCopy: resumeCreateCopy.en,
    n: 1,
    s: 1,
    bytes: 100,
    sizeOver: false,
    schemaCheck: { ok: true } as const,
    creating: false,
    createErrorMessage: null,
    uncertain: false,
  };
}

describe('ImportActionPanel', () => {
  it('is an aside labelled by its own heading, sticky below and above '
    + '900 px', async () => {
    const wrapper = await mountSuspended(ImportActionPanel, {
      props: baseProps(),
    });
    const root = wrapper.get('aside');
    expect(root.attributes('aria-labelledby')).toBe('import-panel-heading');
    const heading = wrapper.get('#import-panel-heading');
    expect(heading.element.tagName).toBe('H2');
    expect(heading.classes()).toContain('sr-only');
    expect(heading.classes()).toContain('min-[900px]:not-sr-only');

    const classes = root.classes();
    // Below 900 px: a full-bleed sticky footer.
    expect(classes).toContain('sticky');
    expect(classes).toContain('bottom-0');
    expect(classes).toContain('-mx-4');
    expect(classes).toContain('sm:-mx-6');
    expect(classes).toContain('border-t');
    // From 900 px: the sticky aside card.
    expect(classes).toContain('min-[900px]:top-6');
    expect(classes).toContain('min-[900px]:bottom-auto');
    expect(classes).toContain('min-[900px]:mx-0');
    expect(classes).toContain('min-[900px]:rounded-lg');
    expect(classes).toContain('min-[900px]:border');
    expect(classes).toContain('min-[900px]:p-6');
  });

  it('keeps Cancel before Create in the DOM, ordered by CSS from 900 px',
    async () => {
      const wrapper = await mountSuspended(ImportActionPanel, {
        props: baseProps(),
      });
      const actions = wrapper.findAll('[data-action]')
        .filter((el) => ['import-cancel', 'import-create']
          .includes(el.attributes('data-action') ?? ''));
      expect(actions.map((el) => el.attributes('data-action')))
        .toEqual(['import-cancel', 'import-create']);
      const cancel = wrapper.get('[data-action="import-cancel"]');
      // From 900 px, Create then Cancel visually (spec "Action panel");
      // Cancel takes the later CSS order so Create renders first.
      expect(cancel.classes()).toContain('min-[900px]:order-2');
    });

  it('renders Cancel as a real disabled button while creating, not a '
    + 'styled link', async () => {
    const notCreating = await mountSuspended(ImportActionPanel, {
      props: baseProps(),
    });
    const link = notCreating.get('[data-action="import-cancel"]');
    expect(link.element.tagName).toBe('A');

    const creating = await mountSuspended(ImportActionPanel, {
      props: { ...baseProps(), creating: true },
    });
    const button = creating.get('[data-action="import-cancel"]');
    expect(button.element.tagName).toBe('BUTTON');
    expect((button.element as HTMLButtonElement).disabled).toBe(true);
  });

  it('shows the specific reason when marked entries exist, general '
    + 'otherwise, and exposes focus for both', async () => {
    const specific = await mountSuspended(ImportActionPanel, {
      props: {
        ...baseProps(),
        schemaCheck: { ok: false, entryIds: ['e1'], general: false },
      },
    });
    expect(specific.get('#import-panel-block-reason').text())
      .toBe(importCopy.en.invalid);

    const general = await mountSuspended(ImportActionPanel, {
      props: {
        ...baseProps(),
        schemaCheck: { ok: false, entryIds: [], general: true },
      },
      attachTo: document.body,
    });
    const reason = general.get('#import-panel-block-reason');
    expect(reason.text()).toBe(importCopy.en.invalidGeneral);
    (general.vm as unknown as { focusReason: () => void }).focusReason();
    expect(document.activeElement).toBe(reason.element);
  });
});
