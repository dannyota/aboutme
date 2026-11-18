import { mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { computed, defineComponent, h, nextTick, ref } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import EvidenceDialog from '../../app/components/projects/EvidenceDialog.vue';
import ProjectEvidence
  from '../../app/components/projects/ProjectEvidence.vue';
import {
  focusAfterPicker,
  linkKind,
  useEvidencePicker,
  type EvidencePicker,
} from '../../app/components/projects/evidencePickerState';
import {
  useProjectEvidence,
} from '../../app/components/projects/projectEvidenceState';
import type {
  GitHubEvidenceApi,
} from '../../app/composables/githubEvidenceApi';
import type { CandidatePage } from '../../app/composables/projectEvidenceApi';
import { useProjectEditor } from '../../app/composables/useProjects';
import type { Locale } from '../../app/i18n/locale';
import { setSiteLocale } from '../support/locale';
import {
  candidate,
  evidenceFailed,
  evidenceItem,
  fakeApi,
  fakeAuth,
  fakeEvidenceApi,
  ok,
  project,
  sequence,
  written,
} from './fixtures';
import { inDocument, registerSignedIn } from './page-support';

const PROJECT = '0190f000-0000-7000-8000-0000000000c1';
const NOW = Date.parse('2026-11-18T09:00:00Z');
const AUTHORIZE = 'https://github.com/login/oauth/authorize'
  + '?client_id=evidence&state=s&code_challenge=c&code_challenge_method=S256';

function id(index: number): string {
  return `candidate${String(index).padStart(13, '0')}`;
}

function page(
  items: CandidatePage['items'],
  nextPage: number | null = null,
  incomplete = false,
) {
  return ok({ items, nextPage, incomplete }) as never;
}

const repositories = [
  candidate({ candidateId: id(1), title: 'octocat/hello',
    repository: { id: 101, fullName: 'octocat/hello' } }),
  candidate({ candidateId: id(2), title: 'octocat/tools', attached: true,
    repository: { id: 102, fullName: 'octocat/tools' } }),
];
const pulls = [
  candidate({ candidateId: id(3), kind: 'pullRequest', title: 'Fix the thing',
    repository: { id: null, fullName: 'acme/site' } }),
];

let viewport = { wide: true };
const listeners = new Set<(event: { matches: boolean }) => void>();

function stubViewport(wide: boolean): void {
  viewport = { wide };
  listeners.clear();
  vi.stubGlobal('matchMedia', (query: string) => {
    const tracked = query.includes('640px');
    return {
      media: query,
      onchange: null,
      get matches() {
        return tracked && viewport.wide;
      },
      addEventListener: (_type: string, listener: never) => {
        if (tracked) listeners.add(listener);
      },
      removeEventListener: (_type: string, listener: never) => {
        listeners.delete(listener);
      },
      addListener: (listener: never) => {
        if (tracked) listeners.add(listener);
      },
      removeListener: (listener: never) => {
        listeners.delete(listener);
      },
      dispatchEvent: () => true,
    };
  });
}

async function resize(wide: boolean): Promise<void> {
  viewport.wide = wide;
  for (const listener of listeners) listener({ matches: wide });
  await flushPromises();
  await nextTick();
}

interface Setup {
  readonly loaded?: ReturnType<typeof project>;
  readonly locale?: Locale;
}

const mounted: { unmount: () => void }[] = [];

async function mountPicker(options: Setup = {}) {
  const loaded = options.loaded ?? project({ id: PROJECT, evidence: [] });
  const api = fakeApi();
  const evidenceApi = fakeEvidenceApi();
  const fakes = fakeAuth();
  const github = {
    start: vi.fn<GitHubEvidenceApi['start']>()
      .mockResolvedValue({ kind: 'ok', value: AUTHORIZE }),
  };
  const navigate = vi.fn();
  const closed = vi.fn();
  const timers: { run: () => void; ms: number; cancelled: boolean }[] = [];
  const schedule = (run: () => void, ms: number) => {
    const timer = { run, ms, cancelled: false };
    timers.push(timer);
    return () => {
      timer.cancelled = true;
    };
  };
  api.read.mockResolvedValue(ok(loaded));
  evidenceApi.candidates.mockImplementation(async (_id, query) =>
    query.kind === 'pullRequest' ? page(pulls) : page(repositories));
  const context = {} as {
    picker: EvidencePicker;
    editor: ReturnType<typeof useProjectEditor>;
  };
  const Harness = defineComponent({
    setup() {
      const editor = useProjectEditor(loaded.id, {
        api, auth: fakes.auth, uuid: sequence('s'), navigate: vi.fn(),
      });
      const changed = computed(() => editor.dirty.value);
      const evidence = useProjectEvidence(editor, changed, {
        api: evidenceApi, projects: api, auth: fakes.auth,
        uuid: sequence('e'), now: () => NOW,
      });
      const picker = useEvidencePicker({
        evidence,
        project: () => editor.loaded.value,
        api: evidenceApi,
        github,
        auth: fakes.auth,
        now: () => NOW,
        navigate,
        schedule,
      });
      context.picker = picker;
      context.editor = editor;
      const locale = ref<Locale>(options.locale ?? 'en');
      return () => h('div', [
        editor.loaded.value === null
          ? null
          : h(ProjectEvidence, {
              controller: evidence,
              locale: locale.value,
              onAdd: (opener: HTMLElement) => picker.show({ opener }),
            }),
        h(EvidenceDialog, {
          picker,
          locale: locale.value,
          onClosed: () => {
            closed();
            focusAfterPicker(picker.opener());
          },
        }),
      ]);
    },
  });
  const wrapper = await mountSuspended(Harness, {
    route: `/app/projects/${loaded.id}`,
    attachTo: document.body,
  });
  mounted.push(wrapper);
  await flushPromises();
  return {
    wrapper, api, evidenceApi, github, navigate, closed, timers, ...fakes,
    ...context,
  };
}

async function openPicker() {
  inDocument('#project-evidence-add').click();
  await flushPromises();
  await new Promise((resolve) => setTimeout(resolve, 0));
}

function dialog(): HTMLElement {
  return inDocument('[data-testid="evidence-dialog"]');
}

function query<T extends Element = HTMLElement>(selector: string): T | null {
  return document.body.querySelector<T>(selector);
}

function texts(selector: string): string[] {
  return [...document.body.querySelectorAll(selector)]
    .map((node) => node.textContent?.trim() ?? '');
}

async function clickIn(selector: string): Promise<void> {
  inDocument(selector).click();
  await flushPromises();
}

async function selectTab(name: string): Promise<void> {
  const tab = [...document.body.querySelectorAll<HTMLElement>('[role="tab"]')]
    .find((node) => node.textContent?.trim() === name)!;
  tab.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
  tab.focus();
  await flushPromises();
}

function rows(): string[] {
  return texts('[data-evidence-candidate] [data-candidate-title]');
}

async function typeLink(value: string): Promise<void> {
  const input = inDocument<HTMLInputElement>('#evidence-link');
  input.value = value;
  input.dispatchEvent(new Event('input', { bubbles: true }));
  await flushPromises();
}

describe('evidence picker', () => {
  beforeEach(() => {
    setSiteLocale('en');
    clearNuxtData();
    registerSignedIn();
    stubViewport(true);
  });

  afterEach(() => {
    for (const wrapper of mounted.splice(0)) wrapper.unmount();
    vi.unstubAllGlobals();
  });

  it.each([
    ['en', ['Repositories', 'Pull requests', 'Releases', 'Paste a link'],
      'Add evidence from GitHub'],
    ['vi', ['Kho mã', 'Pull request', 'Bản phát hành', 'Dán đường dẫn'],
      'Thêm bằng chứng từ GitHub'],
  ] as const)('shows the %s tabs in the design order and moves with arrow '
    + 'keys', async (locale, names, title) => {
    setSiteLocale(locale);
    const { evidenceApi } = await mountPicker({ locale });
    await openPicker();

    expect(dialog().querySelector('h2')?.textContent?.trim()).toBe(title);
    expect(texts('[role="tab"]')).toEqual(names);
    const active = () => texts('[role="tab"][data-state="active"]');
    expect(active()).toEqual([names[0]]);
    expect(document.activeElement?.textContent?.trim()).toBe(names[0]);

    document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'ArrowRight', bubbles: true,
    }));
    await flushPromises();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(active()).toEqual([names[1]]);
    expect(evidenceApi.candidates.mock.calls.map((call) => call[1].kind))
      .toEqual(['repository', 'pullRequest']);
    document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'ArrowLeft', bubbles: true,
    }));
    await flushPromises();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(active()).toEqual([names[0]]);
    expect(evidenceApi.candidates).toHaveBeenCalledTimes(2);
  });

  it('shows loading rows with a polite status, then the rows', async () => {
    const { evidenceApi } = await mountPicker();
    let answer: (value: never) => void = () => {};
    evidenceApi.candidates.mockReturnValueOnce(new Promise((resolve) => {
      answer = resolve;
    }));
    await openPicker();

    const status = query('[data-testid="evidence-listing-loading"]');
    expect(status?.getAttribute('role')).toBe('status');
    expect(status?.textContent?.trim()).toBe('Loading');
    expect(rows()).toEqual([]);
    answer(page(repositories));
    await flushPromises();

    expect(query('[data-testid="evidence-listing-loading"]')).toBeNull();
    expect(rows()).toEqual(['octocat/hello', 'octocat/tools']);
    expect(evidenceApi.candidates.mock.calls[0])
      .toEqual([PROJECT, { kind: 'repository', page: 1 }, 'csrf-1']);
  });

  it('says when a tab is empty', async () => {
    const { evidenceApi } = await mountPicker();
    evidenceApi.candidates.mockResolvedValue(page([]));
    await openPicker();

    expect(query('[data-testid="evidence-empty"]')?.textContent?.trim())
      .toBe('Nothing found.');
  });

  it('shows the incomplete note on pull requests only', async () => {
    const { evidenceApi } = await mountPicker();
    evidenceApi.candidates.mockImplementation(async (_id, request) =>
      page(request.kind === 'pullRequest' ? pulls : repositories, null,
        request.kind === 'pullRequest'));
    await openPicker();
    expect(query('[data-testid="evidence-incomplete"]')).toBeNull();

    await selectTab('Pull requests');

    expect(query('[data-testid="evidence-incomplete"]')?.textContent?.trim())
      .toBe('GitHub returned only part of the results; paste a link to find '
        + 'more.');
  });

  it('pages with Show more and marks attached rows', async () => {
    const { evidenceApi } = await mountPicker();
    const second = candidate({ candidateId: id(9), title: 'octocat/later',
      repository: { id: 109, fullName: 'octocat/later' } });
    evidenceApi.candidates
      .mockResolvedValueOnce(page(repositories, 2))
      .mockResolvedValueOnce(page([second]));
    await openPicker();

    expect(texts('[data-evidence-attached]')).toEqual(['Attached']);
    await clickIn('[data-action="evidence-more"]');

    expect(evidenceApi.candidates.mock.calls.map((call) => call[1]))
      .toEqual([
        { kind: 'repository', page: 1 },
        { kind: 'repository', page: 2 },
      ]);
    expect(rows()).toEqual(['octocat/hello', 'octocat/tools', 'octocat/later']);
    expect(query('[data-action="evidence-more"]')).toBeNull();
  });

  it('names each Attach after its row and marks the row Attached after an '
    + 'attach', async () => {
    const { evidenceApi, picker } = await mountPicker();
    evidenceApi.attach.mockResolvedValue(written([evidenceItem()], '4'));
    await openPicker();

    const button = inDocument('[data-action="evidence-attach"]');
    const labelled = (button.getAttribute('aria-labelledby') ?? '').split(' ')
      .map((ref) => document.getElementById(ref)?.textContent?.trim());
    expect(labelled).toEqual(['Attach', 'octocat/hello']);
    button.click();
    await flushPromises();

    expect(evidenceApi.attach.mock.calls[0])
      .toEqual([PROJECT, '3', { kind: 'repository', candidateId: id(1) },
        'e1', 'csrf-1']);
    expect(texts('[data-evidence-attached]')).toEqual(['Attached', 'Attached']);
    expect(query('[data-action="evidence-attach"]')).toBeNull();
    expect(picker.open.value).toBe(true);
  });

  it('chooses a repository for releases from the loaded repository pages',
    async () => {
      const { evidenceApi } = await mountPicker();
      const release = candidate({ candidateId: id(5), kind: 'release',
        title: 'v1.0', repository: { id: 101, fullName: 'octocat/hello' } });
      evidenceApi.candidates.mockImplementation(async (_id, request) =>
        request.kind === 'release' ? page([release]) : page(repositories));
      await openPicker();
      await selectTab('Releases');

      expect(query('[data-testid="evidence-choose-title"]')?.textContent
        ?.trim()).toBe('Choose a repository');
      expect(texts('[data-action="evidence-choose-repository"]'))
        .toEqual(['octocat/hello', 'octocat/tools']);
      await clickIn('[data-action="evidence-choose-repository"]');

      expect(query('[data-testid="evidence-chosen-repository"]')?.textContent
        ?.trim()).toBe('octocat/hello');
      expect(rows()).toEqual(['v1.0']);
      await clickIn('[data-action="evidence-change-repository"]');
      expect(texts('[data-action="evidence-choose-repository"]'))
        .toEqual(['octocat/hello', 'octocat/tools']);
      expect(evidenceApi.candidates.mock.calls.map((call) => call[1]))
        .toEqual([
          { kind: 'repository', page: 1 },
          { kind: 'release', page: 1, repository: 101 },
        ]);
    });

  it.each([
    ['https://github.com/octocat/hello', 'repository',
      'https://github.com/octocat/hello'],
    ['  https://github.com/octocat/hello/pull/42/ ', 'pullRequest',
      'https://github.com/octocat/hello/pull/42/'],
    ['https://github.com/octocat/hello/releases/tag/v1.0', 'release',
      'https://github.com/octocat/hello/releases/tag/v1.0'],
    ['github.com/octocat/hello', 'repository',
      'https://github.com/octocat/hello'],
    ['  github.com/octocat/hello/pull/42 ', 'pullRequest',
      'https://github.com/octocat/hello/pull/42'],
    ['github.com/octocat/hello/releases/tag/v1.0', 'release',
      'https://github.com/octocat/hello/releases/tag/v1.0'],
    ['http://github.com/octocat/hello', 'repository',
      'http://github.com/octocat/hello'],
    ['www.github.com/octocat/hello', 'repository',
      'www.github.com/octocat/hello'],
    ['GitHub.com/octocat/hello', 'repository', 'GitHub.com/octocat/hello'],
  ])('sends %j as a %s link %j', async (link, kind, sent) => {
    const { evidenceApi } = await mountPicker();
    evidenceApi.attach.mockResolvedValue(written([evidenceItem()], '4'));
    await openPicker();
    await selectTab('Paste a link');
    await typeLink(link);

    await clickIn('[data-action="evidence-attach-link"]');

    expect(evidenceApi.attach.mock.calls[0]?.[2]).toEqual({ kind, link: sent });
    expect(inDocument<HTMLInputElement>('#evidence-link').value).toBe('');
  });

  it.each([
    ['unsupported-link',
      'This link is not a GitHub repository, pull request or release.'],
    ['not-confirmed',
      'GitHub does not confirm you as the author or owner of this item.'],
    ['duplicate', 'This item is already on the project.'],
  ] as const)('shows %s under the link field', async (failure, text) => {
    const { evidenceApi, api } = await mountPicker();
    evidenceApi.attach.mockResolvedValue(evidenceFailed(failure));
    api.read.mockResolvedValue(ok(project({
      id: PROJECT, revision: '4', evidence: [evidenceItem()],
    })));
    await openPicker();
    await selectTab('Paste a link');
    await typeLink('https://example.com/a');

    await clickIn('[data-action="evidence-attach-link"]');

    expect(query('#evidence-link-error')?.textContent?.trim()).toBe(text);
    expect(query('#evidence-link')?.getAttribute('aria-invalid'))
      .toBe('true');
    expect(inDocument<HTMLInputElement>('#evidence-link').value)
      .toBe('https://example.com/a');
  });

  it('shows the upstream copy and repeats the same page on Try again',
    async () => {
      const { evidenceApi } = await mountPicker();
      evidenceApi.candidates
        .mockResolvedValueOnce(page(repositories, 2))
        .mockResolvedValueOnce(evidenceFailed('upstream'))
        .mockResolvedValueOnce(page([]));
      await openPicker();
      await clickIn('[data-action="evidence-more"]');

      expect(query('[data-testid="evidence-listing-error"]')?.textContent
        ?.trim()).toBe('GitHub did not respond. Try again later.');
      expect(rows()).toEqual(['octocat/hello', 'octocat/tools']);
      await clickIn('[data-action="evidence-retry"]');

      expect(evidenceApi.candidates.mock.calls.map((call) => call[1].page))
        .toEqual([1, 2, 2]);
      expect(query('[data-testid="evidence-listing-error"]')).toBeNull();
    });

  it('shows when the rate limit lifts, and the generic copy without a time',
    async () => {
      const { evidenceApi } = await mountPicker();
      evidenceApi.candidates
        .mockResolvedValueOnce(evidenceFailed('rate-limited', 120))
        .mockResolvedValueOnce(evidenceFailed('rate-limited'));
      await openPicker();

      const lift = new Intl.DateTimeFormat('en-US', {
        hour: 'numeric', minute: '2-digit',
      }).format(new Date(NOW + 120_000));
      expect(query('[data-testid="evidence-listing-error"]')?.textContent
        ?.trim()).toBe(
        `The GitHub request limit is reached. Try again after ${lift}.`,
      );
      const retry = inDocument<HTMLButtonElement>(
        '[data-action="evidence-retry"]',
      );
      expect(retry.disabled).toBe(false);
      await clickIn('[data-action="evidence-retry"]');

      expect(query('[data-testid="evidence-listing-error"]')?.textContent
        ?.trim()).toBe('Too many attempts. Try again later.');
    });

  it('offers Continue with GitHub when the session ended', async () => {
    const { evidenceApi } = await mountPicker();
    evidenceApi.candidates.mockResolvedValue(
      evidenceFailed('session-required'),
    );
    await openPicker();

    expect(query('[data-testid="evidence-session-ended"]')?.textContent)
      .toContain('Your GitHub session has ended. Continue with GitHub to '
        + 'load the list again.');
    expect(texts('[data-action="evidence-continue"]'))
      .toEqual(['Continue with GitHub']);
    expect(rows()).toEqual([]);
  });

  it('Continue posts returnTo with the project id and the CSRF token, then '
    + 'navigates to the validated URL', async () => {
    const { evidenceApi, github, navigate } = await mountPicker();
    evidenceApi.candidates.mockResolvedValue(
      evidenceFailed('session-required'),
    );
    await openPicker();

    await clickIn('[data-action="evidence-continue"]');

    expect(github.start.mock.calls)
      .toEqual([['csrf-1', `/app/projects/${PROJECT}`]]);
    expect(navigate.mock.calls).toEqual([[AUTHORIZE]]);
  });

  it.each([
    ['an invalid URL', { kind: 'ok', value: 'https://evil.example/authorize' },
      'Something went wrong. Please try again.'],
    ['a rate limit', { kind: 'failed', failure: 'rate-limited' },
      'Too many attempts. Try again later.'],
    ['a shutdown', { kind: 'failed', failure: 'unavailable' },
      'GitHub evidence is not available right now. Try again later.'],
  ] as const)('Continue never navigates after %s', async (
    _name, answer, text,
  ) => {
    const { evidenceApi, github, navigate } = await mountPicker();
    evidenceApi.candidates.mockResolvedValue(
      evidenceFailed('session-required'),
    );
    github.start.mockResolvedValue(answer);
    await openPicker();

    await clickIn('[data-action="evidence-continue"]');

    expect(navigate).not.toHaveBeenCalled();
    expect(query('[data-testid="evidence-start-error"]')?.textContent
      ?.trim()).toBe(text);
  });

  it('disables Attach at the limit and after an evidence_limit answer',
    async () => {
      const ten = Array.from({ length: 10 }, (_, index) =>
        evidenceItem({ id: `item-${index}`, title: `Item ${index}` }));
      const full = await mountPicker({
        loaded: project({ id: PROJECT, evidence: ten }),
      });
      full.picker.show();
      await flushPromises();
      expect(query('[data-testid="evidence-limit"]')?.textContent?.trim())
        .toBe('A project holds at most 10 items.');
      expect(inDocument<HTMLButtonElement>('[data-action="evidence-attach"]')
        .disabled).toBe(true);
      full.wrapper.unmount();
      mounted.pop();

      const { evidenceApi } = await mountPicker();
      evidenceApi.attach.mockResolvedValue(evidenceFailed('limit'));
      await openPicker();
      await clickIn('[data-action="evidence-attach"]');
      expect(query('[data-testid="evidence-limit"]')).not.toBeNull();
      expect(inDocument<HTMLButtonElement>('[data-action="evidence-attach"]')
        .disabled).toBe(true);
    });

  it('states the first-attach note only while the project has no evidence',
    async () => {
      await mountPicker();
      await openPicker();
      expect(query('[data-testid="evidence-first-attach"]')?.textContent
        ?.trim()).toBe('The project page will link to your GitHub activity, '
          + 'and every resume that shows this project shows these items.');
      mounted.pop()!.unmount();

      await mountPicker({
        loaded: project({ id: PROJECT, evidence: [evidenceItem()] }),
      });
      await openPicker();
      expect(query('[data-testid="evidence-first-attach"]')).toBeNull();
    });

  it('disables Attach and Continue with the save-first hint while the '
    + 'project has unsaved edits', async () => {
    const { evidenceApi, editor } = await mountPicker();
    await openPicker();
    editor.draft.value!.text.en!.name = 'Typed';
    await flushPromises();

    expect(query('[data-testid="evidence-save-first"]')?.textContent?.trim())
      .toBe('Save the project first.');
    expect(inDocument<HTMLButtonElement>('[data-action="evidence-attach"]')
      .disabled).toBe(true);
    evidenceApi.candidates.mockResolvedValue(
      evidenceFailed('session-required'),
    );
    await selectTab('Pull requests');
    expect(inDocument<HTMLButtonElement>('[data-action="evidence-continue"]')
      .disabled).toBe(true);
  });

  it('is a bottom sheet below 640 px and a dialog from 640 px, keeping its '
    + 'tab and rows across a resize', async () => {
    stubViewport(false);
    const { evidenceApi } = await mountPicker();
    await openPicker();
    await selectTab('Pull requests');

    expect(query('[data-slot="sheet-content"]')).not.toBeNull();
    expect(query('[data-slot="dialog-content"]')).toBeNull();
    expect(query('[data-slot="sheet-content"]')?.className)
      .toContain('bottom-0');
    expect(rows()).toEqual(['Fix the thing']);

    await resize(true);
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(query('[data-slot="dialog-content"]')).not.toBeNull();
    expect(query('[data-slot="sheet-content"]')).toBeNull();
    expect(texts('[role="tab"][data-state="active"]'))
      .toEqual(['Pull requests']);
    expect(rows()).toEqual(['Fix the thing']);
    expect(evidenceApi.candidates).toHaveBeenCalledTimes(2);
  });

  it('keeps focus inside and returns it to Add on close', async () => {
    await mountPicker();
    await openPicker();

    expect(dialog().contains(document.activeElement)).toBe(true);
    document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'Escape', bubbles: true,
    }));
    await flushPromises();
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(query('[data-testid="evidence-dialog"]')).toBeNull();
    expect(document.activeElement?.id).toBe('project-evidence-add');
  });

  it('offers Attach again after the attached item leaves the project',
    async () => {
      const { evidenceApi } = await mountPicker();
      const attachedItem = evidenceItem({
        kind: 'repository', title: 'octocat/hello',
        url: 'https://github.com/octocat/hello',
        repositoryName: 'octocat/hello',
      });
      evidenceApi.attach.mockResolvedValue(written([attachedItem], '4'));
      evidenceApi.remove.mockResolvedValue(written([], '5'));
      await openPicker();
      await clickIn('[data-action="evidence-attach"]');
      expect(texts('[data-evidence-attached]'))
        .toEqual(['Attached', 'Attached']);
      document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', {
        key: 'Escape', bubbles: true,
      }));
      await flushPromises();
      await new Promise((resolve) => setTimeout(resolve, 0));

      await clickIn('[data-action="evidence-remove"]');
      await openPicker();

      expect(evidenceApi.candidates.mock.calls.map((call) => call[1]))
        .toEqual([
          { kind: 'repository', page: 1 },
          { kind: 'repository', page: 1 },
        ]);
      expect(texts('[data-evidence-attached]')).toEqual(['Attached']);
      expect(document.body.querySelector(
        `[data-evidence-candidate="${id(1)}"] [data-action="evidence-attach"]`,
      )).not.toBeNull();
    });

  it('disables Attach and the link submit while a section write runs',
    async () => {
      const { evidenceApi } = await mountPicker({
        loaded: project({ id: PROJECT, evidence: [evidenceItem()] }),
      });
      let answer: (value: never) => void = () => {};
      evidenceApi.recheck.mockReturnValue(new Promise((resolve) => {
        answer = resolve;
      }));
      await clickIn('[data-action="evidence-recheck"]');
      await openPicker();

      expect(inDocument<HTMLButtonElement>('[data-action="evidence-attach"]')
        .disabled).toBe(true);
      await selectTab('Paste a link');
      await typeLink('https://github.com/octocat/hello');
      const submit = inDocument<HTMLButtonElement>(
        '[data-action="evidence-attach-link"]',
      );
      expect(submit.disabled).toBe(true);
      answer(written([evidenceItem()], '4') as never);
      await flushPromises();

      expect(submit.disabled).toBe(false);
      expect(evidenceApi.attach).not.toHaveBeenCalled();
    });

  it('starts GitHub once, then enables Continue again five seconds after '
    + 'a navigation that never left', async () => {
    const { evidenceApi, github, navigate, timers } = await mountPicker();
    evidenceApi.candidates.mockResolvedValue(
      evidenceFailed('session-required'),
    );
    await openPicker();
    const button = () => inDocument<HTMLButtonElement>(
      '[data-action="evidence-continue"]',
    );

    await clickIn('[data-action="evidence-continue"]');
    await clickIn('[data-action="evidence-continue"]');

    expect(github.start).toHaveBeenCalledTimes(1);
    expect(navigate).toHaveBeenCalledTimes(1);
    expect(button().disabled).toBe(true);
    expect(timers.map((timer) => [timer.ms, timer.cancelled]))
      .toEqual([[5000, false]]);
    timers[0]!.run();
    await flushPromises();

    expect(button().disabled).toBe(false);
    await clickIn('[data-action="evidence-continue"]');
    expect(github.start).toHaveBeenCalledTimes(2);
  });

  it('cancels the Continue timer when the page goes away', async () => {
    const { evidenceApi, timers, wrapper } = await mountPicker();
    evidenceApi.candidates.mockResolvedValue(
      evidenceFailed('session-required'),
    );
    await openPicker();
    await clickIn('[data-action="evidence-continue"]');
    expect(timers.map((timer) => timer.cancelled)).toEqual([false]);

    wrapper.unmount();
    mounted.pop();

    expect(timers.map((timer) => timer.cancelled)).toEqual([true]);
  });

  it('never navigates after the page is gone', async () => {
    const { evidenceApi, github, navigate, wrapper } = await mountPicker();
    evidenceApi.candidates.mockResolvedValue(
      evidenceFailed('session-required'),
    );
    let answer: (value: never) => void = () => {};
    github.start.mockReturnValue(new Promise((resolve) => {
      answer = resolve;
    }));
    await openPicker();
    await clickIn('[data-action="evidence-continue"]');

    wrapper.unmount();
    mounted.pop();
    answer({ kind: 'ok', value: AUTHORIZE } as never);
    await flushPromises();

    expect(navigate).not.toHaveBeenCalled();
  });

  it('keeps the picker open with focus inside when the breakpoint is crossed',
    async () => {
      const { picker, closed } = await mountPicker();
      await openPicker();
      await selectTab('Pull requests');

      await resize(false);
      await new Promise((resolve) => setTimeout(resolve, 0));
      await flushPromises();

      expect(picker.open.value).toBe(true);
      expect(closed).not.toHaveBeenCalled();
      const sheet = inDocument('[data-slot="sheet-content"]');
      expect(sheet.contains(document.activeElement)).toBe(true);
      expect(document.activeElement?.textContent?.trim())
        .toBe('Pull requests');
    });

  it('disables Attach without the save-first hint while a save runs',
    async () => {
      const context = await mountPicker();
      await openPicker();
      context.api.replace.mockReturnValue(new Promise(() => {}));
      void context.editor.save();
      await flushPromises();

      expect(inDocument<HTMLButtonElement>('[data-action="evidence-attach"]')
        .disabled).toBe(true);
      expect(query('[data-testid="evidence-save-first"]')).toBeNull();
      expect(query('[data-testid="project-evidence-save-first"]')).toBeNull();
      expect(inDocument('[data-action="evidence-attach"]')
        .getAttribute('aria-describedby')).toBeNull();
    });

  it('closes when the project changes elsewhere', async () => {
    const { picker, editor } = await mountPicker();
    await openPicker();

    editor.markStale(project({ id: PROJECT, revision: '9' }));
    await flushPromises();
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(picker.open.value).toBe(false);
    expect(query('[data-testid="evidence-dialog"]')).toBeNull();
    expect(inDocument<HTMLButtonElement>('#project-evidence-add').disabled)
      .toBe(true);
  });

  it('reads the releases of a repository again after Change repository',
    async () => {
      const { evidenceApi } = await mountPicker();
      evidenceApi.candidates.mockImplementation(async (_id, request) =>
        request.kind === 'release'
          ? page([candidate({
              candidateId: id(20 + (request.repository ?? 0)),
              kind: 'release', title: `v${request.repository}`,
              repository: {
                id: request.repository ?? null, fullName: 'octocat/x',
              },
            })])
          : page(repositories));
      await openPicker();
      await selectTab('Releases');
      const choose = async (name: string) => {
        const button = [...document.body.querySelectorAll<HTMLElement>(
          '[data-action="evidence-choose-repository"]',
        )].find((node) => node.textContent?.trim() === name)!;
        button.click();
        await flushPromises();
      };

      await choose('octocat/hello');
      await clickIn('[data-action="evidence-change-repository"]');
      await choose('octocat/tools');
      await clickIn('[data-action="evidence-change-repository"]');
      await choose('octocat/hello');

      expect(evidenceApi.candidates.mock.calls
        .map((call) => call[1])
        .filter((request) => request.kind === 'release')
        .map((request) => request.repository)).toEqual([101, 102, 101]);
      expect(rows()).toEqual(['v101']);
    });

  it('waits for a discarded listing to settle before reading the same page '
    + 'again', async () => {
    const { evidenceApi, editor } = await mountPicker({
      loaded: project({ id: PROJECT, evidence: [evidenceItem()] }),
    });
    let first: (value: never) => void = () => {};
    const fresh = candidate({ candidateId: id(31), title: 'octocat/fresh',
      repository: { id: 131, fullName: 'octocat/fresh' } });
    evidenceApi.candidates
      .mockReturnValueOnce(new Promise((resolve) => {
        first = resolve;
      }))
      .mockResolvedValueOnce(page([fresh]));
    await openPicker();
    expect(evidenceApi.candidates).toHaveBeenCalledTimes(1);

    editor.adoptEvidence([], '4');
    await flushPromises();
    expect(evidenceApi.candidates).toHaveBeenCalledTimes(1);
    first(page(repositories));
    await flushPromises();

    expect(evidenceApi.candidates).toHaveBeenCalledTimes(2);
    expect([...document.body.querySelectorAll('[data-evidence-candidate]')]
      .map((row) => row.getAttribute('data-evidence-candidate')))
      .toEqual([id(31)]);
  });

  it('keeps focus and the typed link in the paste field across the '
    + 'breakpoint', async () => {
    await mountPicker();
    await openPicker();
    await selectTab('Paste a link');
    await typeLink('https://github.com/octocat/hello/pull/4');
    inDocument('#evidence-link').focus();

    await resize(false);
    await new Promise((resolve) => setTimeout(resolve, 0));
    await flushPromises();

    const field = inDocument<HTMLInputElement>('#evidence-link');
    expect(inDocument('[data-slot="sheet-content"]').contains(field))
      .toBe(true);
    expect(document.activeElement).toBe(field);
    expect(field.value).toBe('https://github.com/octocat/hello/pull/4');
  });

  it.each([
    ['https://github.com/octocat/hello', 'repository'],
    ['https://github.com/octocat/hello/', 'repository'],
    ['https://github.com/octocat/hello/pull/42', 'pullRequest'],
    ['https://github.com/octocat/hello/pull/42/', 'pullRequest'],
    ['https://github.com/octocat/hello/releases/tag/v1.0', 'release'],
    ['https://github.com/pull/repo', 'repository'],
    ['https://github.com/octocat/releases/tag/v1', 'repository'],
    ['https://github.com/a/b/c/pull/1', 'repository'],
    ['https://github.com/a/b/pull/1/files', 'repository'],
    ['https://github.com/a/b/issues/1', 'repository'],
    ['www.github.com/octocat/hello/pull/42', 'pullRequest'],
    ['https://github.com/a/b/pull/1?x=1#y', 'pullRequest'],
    ['not a link', 'repository'],
  ] as const)('classifies %j as %s by its path segments', (link, kind) => {
    expect(linkKind(link)).toBe(kind);
  });

  it('bounds the link field at 300 characters', async () => {
    await mountPicker();
    await openPicker();
    await selectTab('Paste a link');

    expect(query('#evidence-link')?.getAttribute('maxlength')).toBe('300');
  });
});
