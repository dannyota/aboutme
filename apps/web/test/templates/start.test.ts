import { flushPromises, mount } from '@vue/test-utils';
import type { Resume } from '@aboutme/schema';
import { loadSample } from '@aboutme/schema/samples';
import { TEMPLATES } from '@aboutme/schema/templates';
import { validateDocument } from '@aboutme/schema/validation';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { nextTick } from 'vue';

// eslint-disable-next-line max-len -- dialog component import.
import CreateResumeDialog from '../../app/components/editor/list/CreateResumeDialog.vue';
import { freezeCreateAttempt } from '../../app/editor/resumeApi';
import { galleryTemplate } from '../../app/templates/catalog';
import {
  blankTemplateDocument,
  parseNewResumeQuery,
  startDocument,
  suggestedTitle,
} from '../../app/templates/startDocument';
import { setSiteLocale } from '../support/locale';

// Starting a resume from a gallery sample or a template: the /app/new query,
// the starting document, and the create dialog's sample choice.

const mounted = new Set<{ unmount: () => void }>();

afterEach(() => {
  for (const wrapper of mounted) wrapper.unmount();
  mounted.clear();
  document.body.innerHTML = '';
});

describe('/app/new query', () => {
  it('accepts a known sample in vi or en, else the site language', () => {
    expect(parseNewResumeQuery({ sample: 'ats-plain', lng: 'en' }, 'vi'))
      .toMatchObject({ kind: 'sample', lng: 'en' });
    expect(parseNewResumeQuery({ sample: 'ats-plain', lng: 'fr' }, 'vi'))
      .toMatchObject({ kind: 'sample', lng: 'vi' });
    expect(parseNewResumeQuery({ sample: 'ats-plain' }, 'en'))
      .toMatchObject({ kind: 'sample', lng: 'en' });
  });

  it('accepts any catalog template for a blank resume', () => {
    expect(parseNewResumeQuery({ template: 'classic-serif' }, 'vi'))
      .toMatchObject({ kind: 'template' });
  });

  it.each([
    [{}],
    [{ sample: 'classic-serif', lng: 'vi' }],
    [{ sample: 'unknown' }],
    [{ sample: ['ats-plain'] }],
    [{ template: 'constructor' }],
    [{ template: '../ats-plain' }],
  ])('refuses %j', (query) => {
    expect(parseNewResumeQuery(query, 'vi')).toEqual({ kind: 'invalid' });
  });
});

describe('starting documents', () => {
  it('starts a blank resume that wears each template', () => {
    for (const preset of TEMPLATES) {
      const document = blankTemplateDocument(preset);
      expect(document.content).toEqual({});
      expect(document.customization.font).toEqual(preset.customization.font);
      expect(document.customization.layout.sections)
        .toEqual({ main: [], sidebar: [] });
      expect(validateDocument(document as never), preset.id).toEqual([]);
    }
  });

  it('starts a sample resume from its file and titles it by role',
    async () => {
      const template = galleryTemplate('engineer-compact')!;
      const request = { kind: 'sample', template, lng: 'en' } as const;
      const document = await startDocument(request);
      expect(document).toEqual(await loadSample('engineer-compact', 'en'));
      expect(suggestedTitle(request)).toBe('Backend engineer resume');
      const vi = { kind: 'sample', template, lng: 'vi' } as const;
      expect(suggestedTitle(vi)).toBe('CV kỹ sư frontend');
      const fpa = {
        kind: 'sample',
        template: galleryTemplate('ats-plain')!,
        lng: 'vi',
      } as const;
      expect(suggestedTitle(fpa)).toBe('CV trưởng nhóm vận hành kho');
      expect(suggestedTitle({ kind: 'template', template }))
        .toBe('Engineer Compact resume');
    });

  it('sends the starting document with the create request', async () => {
    const document = (await loadSample('ats-plain', 'vi'))!;
    const attempt = freezeCreateAttempt({
      kind: 'resumeCreate',
      id: 'intent-1',
      ownerId: 'owner-1',
      sequence: 0,
      title: 'CV mẫu',
      lng: 'vi',
      document,
    }, { nowEpochMs: () => 0, uuid: () => 'key-1', delay: async () => {} });
    expect(attempt.payload.kind).toBe('json');
    const utf8 = attempt.payload.kind === 'json' ? attempt.payload.utf8 : '';
    expect(JSON.parse(utf8)).toEqual({
      title: 'CV mẫu',
      lng: 'vi',
      document,
    });
  });
});

describe('create dialog samples', () => {
  function mountDialog(resumeCount: number) {
    const wrapper = mount(CreateResumeDialog, {
      attachTo: document.body,
      props: { open: true, busy: false, retained: null, resumeCount },
    });
    mounted.add(wrapper);
    return wrapper;
  }

  async function settle(): Promise<void> {
    await flushPromises();
    await nextTick();
  }

  async function waitForSample(): Promise<void> {
    await vi.waitFor(() => expect(document.body.querySelector<
      HTMLButtonElement
    >('[role="dialog"] button[type="submit"]')?.disabled).toBe(false));
  }

  it('opens on the samples for a first resume and blank otherwise',
    async () => {
      setSiteLocale('vi');
      const samples = mountDialog(0);
      await settle();
      expect(document.body.querySelector('[data-create-mode="sample"]')
        ?.getAttribute('aria-pressed')).toBe('true');
      expect(document.body.querySelectorAll('[data-sample]')).toHaveLength(5);
      samples.unmount();
      mounted.delete(samples);
      document.body.innerHTML = '';
      mountDialog(2);
      await settle();
      expect(document.body.querySelector('[data-create-mode="blank"]')
        ?.getAttribute('aria-pressed')).toBe('true');
      expect(document.body.querySelector('[data-sample]')).toBeNull();
    });

  it('creates from the chosen sample in the resume language', async () => {
    setSiteLocale('en');
    const wrapper = mountDialog(0);
    await settle();
    document.body.querySelector<HTMLButtonElement>(
      '[data-sample="engineer-compact"]',
    )!.click();
    await settle();
    const title = document.body.querySelector<HTMLInputElement>(
      '[role="dialog"] input[name="title"]',
    )!;
    expect(title.value).toBe('Backend engineer resume');
    await waitForSample();
    document.body.querySelector<HTMLFormElement>('[role="dialog"] form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await settle();
    const [emitted] = wrapper.emitted('submit') as [
      [string, string, Resume],
    ];
    expect(emitted[0]).toBe('Backend engineer resume');
    expect(emitted[1]).toBe('en');
    expect(emitted[2]).toEqual(await loadSample('engineer-compact', 'en'));
  });

  it('keeps a sample start unchanged while the interface locale changes',
    async () => {
      setSiteLocale('vi');
      const wrapper = mountDialog(0);
      await settle();
      document.body.querySelector<HTMLButtonElement>(
        '[data-sample="engineer-compact"]',
      )!.click();
      await settle();
      await waitForSample();

      const selected = document.body.querySelector(
        '[data-sample="engineer-compact"]',
      )!;
      const title = document.body.querySelector<HTMLInputElement>(
        '[role="dialog"] input[name="title"]',
      )!;
      const language = document.body.querySelector<HTMLSelectElement>(
        '[role="dialog"] [data-action="resume-language"]',
      )!;
      const before = {
        selected: selected.getAttribute('aria-pressed'),
        title: title.value,
        language: language.value,
        document: await loadSample('engineer-compact', 'vi'),
      };

      useState('aboutme-locale').value = 'en';
      await settle();

      expect(selected.getAttribute('aria-pressed')).toBe(before.selected);
      expect(title.value).toBe(before.title);
      expect(language.value).toBe(before.language);
      document.body.querySelector<HTMLFormElement>('[role="dialog"] form')!
        .dispatchEvent(new Event(
          'submit', { bubbles: true, cancelable: true },
        ));
      await settle();
      expect(wrapper.emitted('submit')).toEqual([[
        before.title,
        before.language,
        before.document,
      ]]);
    });
});
