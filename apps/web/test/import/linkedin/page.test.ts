// The Pick, Reading, Stop, Cap, and Old browser states of
// /app/import/linkedin (docs/design/linkedin-import-ui.md, "States" and
// "Messages"). useResumeList and the pdf.js loader are stubbed, as the brief
// allows for these states; the importer's outcome is stubbed per test.
import { mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { ref } from 'vue';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { buildReview } from '../../../app/import/linkedin/build';
import { resumeCreateCopy } from '../../../app/i18n/resume-create';
import LinkedinImportPage from '../../../app/pages/app/import/linkedin.vue';
import { setSiteLocale } from '../../support/locale';

const importLinkedInPdfMock = vi.fn();
const createMock = vi.fn();
let listView = ref<{ kind: string; items: readonly unknown[] }>(
  { kind: 'ready', items: [] },
);
let supportsImportMock = vi.fn(() => true);
let loadPdfReaderMock = vi.fn(async () => ({
  pdfjs: {},
  startWorker: () => ({ port: {}, terminate: vi.fn() }),
}));

vi.mock('../../../app/composables/useResumeList', async (importOriginal) => {
  const actual = await importOriginal<
    typeof import('../../../app/composables/useResumeList')
  >();
  return {
    ...actual,
    useResumeList: () => ({
      view: listView,
      items: { value: listView.value.items },
      actionNotice: ref(null),
      removalFocusId: ref(null),
      removalFocusVersion: ref(0),
      settled: async () => {},
      create: createMock,
      refreshCreate: vi.fn(),
      abandonCreate: vi.fn(),
      rename: vi.fn(),
      remove: vi.fn(),
    }),
  };
});

vi.mock('../../../app/import/linkedin/pdfWorker', () => ({
  supportsImport: (...args: unknown[]) => supportsImportMock(...args),
  loadPdfReader: (...args: unknown[]) => loadPdfReaderMock(...args),
}));

vi.mock('../../../app/import/linkedin/build', async (importOriginal) => {
  const actual = await importOriginal<
    typeof import('../../../app/import/linkedin/build')
  >();
  return {
    ...actual,
    importLinkedInPdf: (...args: unknown[]) => importLinkedInPdfMock(...args),
  };
});

describe('/app/import/linkedin', () => {
  beforeEach(() => {
    setSiteLocale('en');
    listView = ref({ kind: 'ready', items: [] });
    supportsImportMock = vi.fn(() => true);
    loadPdfReaderMock = vi.fn(async () => ({
      pdfjs: {},
      startWorker: () => ({ port: {}, terminate: vi.fn() }),
    }));
    importLinkedInPdfMock.mockReset();
    createMock.mockReset();
  });

  it('shows the old browser message when supportsImport fails', async () => {
    supportsImportMock = vi.fn(() => false);
    const wrapper = await mountSuspended(LinkedinImportPage);
    await flushPromises();
    expect(wrapper.find('[data-import-old-browser]').text())
      .toContain('This browser cannot read PDFs');
    expect(wrapper.find('[data-testid="import-file-input"]').exists())
      .toBe(false);
  });

  it('shows the cap message at 3 resumes and no file picker', async () => {
    listView.value = {
      kind: 'ready',
      items: [{ id: '1' }, { id: '2' }, { id: '3' }],
    };
    const wrapper = await mountSuspended(LinkedinImportPage);
    await flushPromises();
    expect(wrapper.find('[data-import-cap]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="import-file-input"]').exists())
      .toBe(false);
  });

  it('shows an unavailable message when the resume list fails', async () => {
    listView.value = { kind: 'unavailable', items: [] };
    const wrapper = await mountSuspended(LinkedinImportPage);
    await flushPromises();
    expect(wrapper.find('[data-import-unavailable]').exists()).toBe(true);
  });

  it(
    'shows an unavailable message when the pdf reader fails to load',
    async () => {
      loadPdfReaderMock = vi.fn(async () => {
        throw new Error('network');
      });
      const wrapper = await mountSuspended(LinkedinImportPage);
      await flushPromises();
      expect(wrapper.find('[data-import-unavailable]').text())
        .toContain('The PDF reader did not load');
    },
  );

  it.each([
    ['notPdf', 'This file is not a PDF'],
    ['notLinkedIn', 'This is not a LinkedIn profile PDF'],
    ['notEnglish', 'This LinkedIn PDF is not in English'],
    ['encrypted', 'This PDF is locked with a password'],
    ['tooLarge', 'This file is too large to import'],
    ['tooManyPages', 'This PDF has more than 20 pages'],
    ['timedOut', 'Reading this file took longer than 15 seconds'],
    ['unreadable', 'We cannot read the text in this file'],
  ] as const)(
    'shows the %s message banner after a failed pick',
    async (reason, text) => {
      importLinkedInPdfMock.mockResolvedValue({ ok: false, reason });
      const wrapper = await mountSuspended(LinkedinImportPage);
      await flushPromises();
      const input = wrapper
        .find<HTMLInputElement>('[data-testid="import-file-input"]');
      const file = new File(
        ['%PDF-1.4'], 'profile.pdf', { type: 'application/pdf' },
      );
      Object.defineProperty(input.element, 'files', { value: [file] });
      await input.trigger('change');
      await flushPromises();
      expect(wrapper.find('[data-import-error]').text()).toContain(text);
    },
  );

  it('shows dropOne when more than one file is dropped', async () => {
    const wrapper = await mountSuspended(LinkedinImportPage);
    await flushPromises();
    const zone = wrapper.get('[aria-labelledby="import-pick-heading"]');
    const files = [
      new File(['a'], 'a.pdf', { type: 'application/pdf' }),
      new File(['b'], 'b.pdf', { type: 'application/pdf' }),
    ];
    await zone.trigger('drop', { dataTransfer: { files } });
    expect(wrapper.find('[data-import-error]').text())
      .toContain('Drop one file at a time');
    expect(importLinkedInPdfMock).not.toHaveBeenCalled();
  });

  it('shows the stopped message after Stop, as an info banner', async () => {
    let resolveOutcome!: (value: unknown) => void;
    importLinkedInPdfMock.mockReturnValue(
      new Promise((resolve) => { resolveOutcome = resolve; }),
    );
    const wrapper = await mountSuspended(LinkedinImportPage);
    await flushPromises();
    const input = wrapper
      .find<HTMLInputElement>('[data-testid="import-file-input"]');
    const file = new File(
      ['%PDF-1.4'], 'profile.pdf', { type: 'application/pdf' },
    );
    Object.defineProperty(input.element, 'files', { value: [file] });
    await input.trigger('change');
    await flushPromises();

    await wrapper.get('[data-action="import-stop"]').trigger('click');
    resolveOutcome({ ok: false, reason: 'stopped' });
    await flushPromises();

    expect(wrapper.find('[data-import-stopped]').attributes('role'))
      .toBe('status');
    expect(wrapper.find('[data-import-stopped]').text()).toContain('Stopped');
    expect(wrapper.find('[data-import-error]').exists()).toBe(false);
    // The stopped banner shares the error banner's slot above the drop
    // zone, not a slot above the whole Pick state (docs/design/
    // linkedin-import-ui.md, "Messages": only one banner shows at a time).
    const banner = wrapper.get('[data-import-stopped]').element;
    const zone = wrapper.get('[aria-labelledby="import-pick-heading"]')
      .element;
    expect(banner.parentElement).toBe(zone.parentElement);
  });

  it('reviews a read file and creates it with one request', async () => {
    let n = 0;
    const review = buildReview({
      profile: { fullName: 'Sample Person', headline: 'Product Manager' },
      contacts: [{ type: 'email', value: 'sample@example.com' }],
      work: [{
        employer: 'Example Co.',
        jobTitle: 'Analyst',
        description: '',
        descriptionCut: false,
      }],
      education: [],
      skills: ['Public Speaking'],
      languages: [],
      certificates: [],
      dropped: [],
    }, () => {
      n += 1;
      return `00000000-0000-4000-8000-${n.toString(16).padStart(12, '0')}`;
    });
    const startWorker = vi.fn(() => ({ port: {}, terminate: vi.fn() }));
    loadPdfReaderMock = vi.fn(async () => ({ pdfjs: {}, startWorker }));
    importLinkedInPdfMock.mockResolvedValue({ ok: true, review });
    createMock.mockResolvedValue({ kind: 'created' });

    const wrapper = await mountSuspended(LinkedinImportPage);
    await flushPromises();
    const input = wrapper
      .find<HTMLInputElement>('[data-testid="import-file-input"]');
    const file = new File(
      ['%PDF-1.4'], 'profile.pdf', { type: 'application/pdf' },
    );
    Object.defineProperty(input.element, 'files', { value: [file] });
    await input.trigger('change');
    await flushPromises();

    const create = wrapper.get('[data-action="import-create"]');
    expect(create.text()).toBe(resumeCreateCopy.en.createAndOpen);
    expect(wrapper.text()).toContain(resumeCreateCopy.en.cancel);
    await wrapper.get('form').trigger('submit');
    await flushPromises();

    expect(createMock).toHaveBeenCalledTimes(1);
    const [title, lng, document] = createMock.mock.calls[0]!;
    expect([title, lng]).toEqual(['LinkedIn resume', 'en']);
    expect(document.personalDetails.fullName).toBe('Sample Person');
    // Email starts unselected (docs/design/linkedin-import.md, I5).
    expect(document.personalDetails.details).toEqual([]);
    // One worker, started when the page opened; none after the pick.
    expect(startWorker).toHaveBeenCalledTimes(1);
  });

  it('drops the focus ring on the review h1 and pulls chooseAnother flush '
    + 'left below 640 px', async () => {
    let n = 0;
    const review = buildReview({
      profile: { fullName: 'Sample Person', headline: 'Product Manager' },
      contacts: [],
      work: [],
      education: [],
      skills: ['Public Speaking'],
      languages: [],
      certificates: [],
      dropped: [],
    }, () => {
      n += 1;
      return `00000000-0000-4000-8000-${n.toString(16).padStart(12, '0')}`;
    });
    importLinkedInPdfMock.mockResolvedValue({ ok: true, review });

    const wrapper = await mountSuspended(LinkedinImportPage);
    await flushPromises();
    const input = wrapper
      .find<HTMLInputElement>('[data-testid="import-file-input"]');
    const file = new File(
      ['%PDF-1.4'], 'profile.pdf', { type: 'application/pdf' },
    );
    Object.defineProperty(input.element, 'files', { value: [file] });
    await input.trigger('change');
    await flushPromises();

    expect(wrapper.get('#import-review-heading').classes())
      .toContain('outline-none');
    expect(wrapper.get('[data-action="import-choose-another"]').classes())
      .toContain('max-sm:-ml-4');
  });

  it(
    'blocks Create and focuses the first marked detail before any entry',
    async () => {
      let n = 0;
      const review = buildReview({
        profile: { fullName: 'Sample Person', headline: 'Product Manager' },
        // A dangerous URL scheme fails the schema check on the detail
        // itself (packages/schema/fixtures/
        // invalid-dangerous-detail-url-scheme.json).
        contacts: [
          { type: 'website', value: 'javascript:alert(1)', label: 'Site' },
        ],
        work: [{
          employer: 'Example Co.',
          jobTitle: 'Analyst',
          dates: { start: { y: 2020, m: 1 }, end: null, present: true },
          description: '',
          descriptionCut: false,
        }],
        education: [],
        skills: [],
        languages: [],
        certificates: [],
        dropped: [],
      }, () => {
        n += 1;
        return `00000000-0000-4000-8000-${n.toString(16).padStart(12, '0')}`;
      });
      // The work entry also fails its own check (end before start), so a
      // detail and an entry are marked at once (docs/design/
      // linkedin-import-ui.md, "Action panel": details before sections).
      const work = review.sections.find((s) => s.key === 'work')!.entries[0]!;
      work.entry.dates = {
        start: { y: 2022, m: 1 }, end: { y: 2020, m: 1 }, present: false,
      };
      importLinkedInPdfMock.mockResolvedValue({ ok: true, review });

      const wrapper = await mountSuspended(
        LinkedinImportPage, { attachTo: document.body },
      );
      await flushPromises();
      const input = wrapper
        .find<HTMLInputElement>('[data-testid="import-file-input"]');
      const file = new File(
        ['%PDF-1.4'], 'profile.pdf', { type: 'application/pdf' },
      );
      Object.defineProperty(input.element, 'files', { value: [file] });
      await input.trigger('change');
      await flushPromises();

      await wrapper.get('form').trigger('submit');
      await flushPromises();

      const websiteDetail = review.details.find((d) => d.type === 'website')!;
      expect(document.activeElement?.getAttribute('data-import-entry'))
        .toBe(websiteDetail.id);
      expect(createMock).not.toHaveBeenCalled();
      expect(wrapper.text()).toContain('Some entries cannot be saved');
    },
  );

  it(
    'shows a general reason and focuses it for a failure outside any '
    + 'entry or detail',
    async () => {
      let n = 0;
      const review = buildReview({
        profile: { fullName: 'Sample Person', headline: 'Product Manager' },
        contacts: [],
        work: [{
          employer: 'Example Co.',
          jobTitle: 'Analyst',
          description: '',
          descriptionCut: false,
        }],
        education: [],
        skills: [],
        languages: [],
        certificates: [],
        dropped: [],
      }, () => {
        n += 1;
        return `00000000-0000-4000-8000-${n.toString(16).padStart(12, '0')}`;
      });
      importLinkedInPdfMock.mockResolvedValue({ ok: true, review });

      const wrapper = await mountSuspended(
        LinkedinImportPage, { attachTo: document.body },
      );
      await flushPromises();
      const input = wrapper
        .find<HTMLInputElement>('[data-testid="import-file-input"]');
      const file = new File(
        ['%PDF-1.4'], 'profile.pdf', { type: 'application/pdf' },
      );
      Object.defineProperty(input.element, 'files', { value: [file] });
      await input.trigger('change');
      await flushPromises();

      // A user edit past the schema's 160-character maxLength fails the
      // check at /personalDetails/fullName, a path outside any entry or
      // detail.
      const fullNameInput = wrapper
        .get<HTMLInputElement>('[data-field="fullName"] input');
      await fullNameInput.setValue('x'.repeat(200));
      await flushPromises();

      await wrapper.get('form').trigger('submit');
      await flushPromises();

      const reason = wrapper.get('#import-panel-block-reason');
      expect(reason.text()).toContain('This resume cannot be saved as it is');
      expect(document.activeElement).toBe(reason.element);
      expect(createMock).not.toHaveBeenCalled();
    },
  );
});
