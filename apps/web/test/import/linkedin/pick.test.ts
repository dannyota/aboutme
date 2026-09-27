// The Pick/Reading drop zone: the banner-to-zone gap, the progress track's
// unfilled color on the blue zone, and the zone keeping the same height in
// both states (docs/design/linkedin-import-ui.md, "Pick state" and "Reading
// state").
import { mountSuspended } from '@nuxt/test-utils/runtime';
import { describe, expect, it } from 'vitest';

import ImportPick from '../../../app/components/import/ImportPick.vue';
import { importCopy } from '../../../app/i18n/import';

function baseProps() {
  return {
    copy: importCopy.en,
    error: null,
    stopped: false,
    reading: false,
    fileName: null,
    pagesRead: 0,
    pageCount: null,
  };
}

describe('ImportPick', () => {
  it('pulls the banner 8px closer to the zone for a 16px gap', async () => {
    const wrapper = await mountSuspended(ImportPick, {
      props: { ...baseProps(), error: 'notPdf' },
    });
    expect(wrapper.get('[data-import-error]').classes()).toContain('-mb-2');
  });

  it('shows the stopped banner in the same slot, with the same gap',
    async () => {
      const wrapper = await mountSuspended(ImportPick, {
        props: { ...baseProps(), stopped: true },
      });
      const banner = wrapper.get('[data-import-stopped]');
      expect(banner.classes()).toContain('-mb-2');
      expect(banner.text()).toContain(importCopy.en.stopped);
      expect(wrapper.find('[data-import-error]').exists()).toBe(false);
    });

  it('uses an existing track token visible on the blue drop zone',
    async () => {
      const wrapper = await mountSuspended(ImportPick, {
        props: { ...baseProps(), reading: true, fileName: 'profile.pdf' },
      });
      const track = wrapper.get('[role="progressbar"]');
      expect(track.classes()).toContain('bg-input');
      expect(track.classes()).not.toContain('bg-muted');
    });

  it('keeps both the Pick and Reading content mounted, hiding only the '
    + 'inactive one, so the zone keeps one height', async () => {
    const picking = await mountSuspended(ImportPick, {
      props: baseProps(),
    });
    expect(picking.get('[data-action="import-choose"]').classes())
      .not.toContain('invisible');
    const readingBlock = picking.get('[data-action="import-stop"]')
      .element.closest('div')!;
    expect(readingBlock.className).toContain('invisible');
    expect(readingBlock.getAttribute('aria-hidden')).toBe('true');

    const reading = await mountSuspended(ImportPick, {
      props: { ...baseProps(), reading: true, fileName: 'profile.pdf' },
    });
    expect(reading.get('[data-action="import-stop"]').classes())
      .not.toContain('invisible');
    const pickBlock = reading.get('[data-action="import-choose"]')
      .element.closest('div')!;
    expect(pickBlock.className).toContain('invisible');
    expect(pickBlock.getAttribute('aria-hidden')).toBe('true');
  });
});
