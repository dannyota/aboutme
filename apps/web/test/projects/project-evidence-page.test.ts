import { mockNuxtImport, mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import ProjectEditorPage from '../../app/pages/app/projects/[id].vue';
import { setSiteLocale } from '../support/locale';
import {
  EVIDENCE_IDS,
  evidenceFailed,
  evidenceItem,
  failed,
  fakeApi,
  fakeEvidenceApi,
  ok,
  project,
  stale,
  written,
} from './fixtures';
import { inDocument, registerSignedIn, typeInto } from './page-support';

const fake = vi.hoisted(() => ({
  api: null as unknown,
  evidence: null as unknown,
  github: null as unknown,
}));
vi.mock('../../app/composables/projectsApi', async (importOriginal) => ({
  ...await importOriginal<
    typeof import('../../app/composables/projectsApi')>(),
  createProjectsApi: () => fake.api,
}));
vi.mock('../../app/composables/projectEvidenceApi', async (importOriginal) => ({
  ...await importOriginal<
    typeof import('../../app/composables/projectEvidenceApi')>(),
  createProjectEvidenceApi: () => fake.evidence,
}));
vi.mock('../../app/composables/githubEvidenceApi', async (importOriginal) => ({
  ...await importOriginal<
    typeof import('../../app/composables/githubEvidenceApi')>(),
  createGitHubEvidenceApi: () => fake.github,
}));
mockNuxtImport('navigateTo', () => vi.fn());

const PROJECT = '0190f000-0000-7000-8000-0000000000c1';
const [E1, E2] = EVIDENCE_IDS;

let api: ReturnType<typeof fakeApi>;
let evidenceApi: ReturnType<typeof fakeEvidenceApi>;
let github: { start: ReturnType<typeof vi.fn> };
const mounted: { unmount: () => void }[] = [];

const two = () => [
  evidenceItem({ id: E1, title: 'First fix' }),
  evidenceItem({ id: E2, title: 'Second fix', kind: 'repository',
    repositoryName: 'octocat/hello' }),
];

async function mountEditor(
  loaded = project({ id: PROJECT, evidence: two() }),
  path = `/app/projects/${loaded.id}`,
) {
  api.read.mockResolvedValue(ok(loaded));
  const wrapper = await mountSuspended(ProjectEditorPage, {
    route: path,
    attachTo: document.body,
  });
  mounted.push(wrapper);
  await flushPromises();
  await new Promise((resolve) => setTimeout(resolve, 0));
  await flushPromises();
  return wrapper;
}

function sectionOrder(): string[] {
  return [...document.body.querySelectorAll(
    '[data-testid="project-form"] > section',
  )].map((section) => section.querySelector('h2')?.id ?? '');
}

function dialog(): HTMLElement | null {
  return document.body.querySelector('[data-testid="evidence-dialog"]');
}

describe('project editor evidence section', () => {
  beforeEach(() => {
    setSiteLocale('en');
    clearNuxtData();
    registerSignedIn({ githubEvidence: true });
    api = fakeApi();
    evidenceApi = fakeEvidenceApi();
    evidenceApi.candidates.mockResolvedValue(
      ok({ items: [], nextPage: null, incomplete: false }) as never,
    );
    github = { start: vi.fn() };
    fake.api = api;
    fake.evidence = evidenceApi;
    fake.github = github;
    vi.mocked(navigateTo).mockClear();
  });

  afterEach(() => {
    for (const wrapper of mounted.splice(0)) wrapper.unmount();
  });

  it('sits after Details and before the resumes section', async () => {
    await mountEditor();

    expect(sectionOrder()).toEqual([
      'project-text-title',
      'project-details-title',
      'project-evidence-title',
      'project-resumes-title',
      'project-address-title',
      'project-delete-title',
    ]);
    expect(inDocument('#project-evidence-title').textContent?.trim())
      .toBe('GitHub evidence');
    expect([...document.body.querySelectorAll('[data-evidence-title]')]
      .map((link) => link.textContent?.trim()))
      .toEqual(['First fix', 'Second fix']);
  });

  it('shows a confirmed row with its label, dates and a check date in UTC',
    async () => {
      await mountEditor(project({ id: PROJECT, evidence: [evidenceItem({
        id: E1, itemDate: '2026-01-02T23:30:00Z',
        checkedAt: '2026-11-17T08:00:00Z',
      })] }));

      const row = inDocument(`[data-evidence-item="${E1}"]`);
      expect(row.querySelector('[data-evidence-label]')?.textContent)
        .toBe('Author of a merged pull request');
      expect(row.querySelector('[data-evidence-title]')
        ?.getAttribute('href'))
        .toBe('https://github.com/octocat/hello/pull/42');
      expect(row.querySelector('[data-evidence-date]')?.textContent)
        .toBe('January 2, 2026');
      expect(row.querySelector('[data-evidence-checked]')?.textContent)
        .toBe('checked on November 17, 2026');
    });

  it.each([
    ['rule_failed', 'The last check no longer met the rule.'],
    ['gone', 'GitHub no longer finds this item.'],
  ] as const)('shows a %s row with its reason and no label', async (
    flagReason, text,
  ) => {
    await mountEditor(project({ id: PROJECT, evidence: [evidenceItem({
      id: E1, confirmed: false, flagReason,
    })] }));

    const row = inDocument(`[data-evidence-item="${E1}"]`);
    expect(row.querySelector('[data-evidence-flag]')?.textContent).toBe(text);
    expect(row.querySelector('[data-evidence-label]')).toBeNull();
    expect(row.querySelector('[data-evidence-checked]')).toBeNull();
  });

  it('names move buttons after the row and disables the ends', async () => {
    await mountEditor();

    const up = [...document.body.querySelectorAll<HTMLButtonElement>(
      '[data-action="evidence-move-up"]',
    )];
    const down = [...document.body.querySelectorAll<HTMLButtonElement>(
      '[data-action="evidence-move-down"]',
    )];
    expect(up.map((button) => button.getAttribute('aria-label')))
      .toEqual(['Move First fix up', 'Move Second fix up']);
    expect(up.map((button) => button.disabled)).toEqual([true, false]);
    expect(down.map((button) => button.disabled)).toEqual([false, true]);
  });

  it('is absent when the capability is off', async () => {
    registerSignedIn({ githubEvidence: false });
    await mountEditor();

    expect(document.body.querySelector('[data-testid="project-evidence"]'))
      .toBeNull();
  });

  it('is absent when the read carries no evidence', async () => {
    await mountEditor(project({ id: PROJECT }));

    expect(document.body.querySelector('[data-testid="project-evidence"]'))
      .toBeNull();
  });

  it.each([
    ['connected', 'GitHub is connected.'],
    ['failed', 'GitHub was not connected. Try again.'],
    ['different_account',
      'This GitHub account differs from the connected one. Disconnect first.'],
  ])('reopens the dialog for evidence=%s and clears only that parameter',
    async (value, text) => {
      const router = useRouter();
      const replace = vi.spyOn(router, 'replace');
      const push = vi.spyOn(router, 'push');
      try {
        await mountEditor(undefined,
          `/app/projects/${PROJECT}?tab=a&evidence=${value}#evidence`);

        expect(dialog()).not.toBeNull();
        const outcome = inDocument('[data-testid="evidence-outcome"]');
        expect(outcome.textContent).toContain(text);
        expect(dialog()!.querySelector('[role="tab"][data-state="active"]')
          ?.textContent?.trim()).toBe('Repositories');
        expect(evidenceApi.candidates.mock.calls[0]?.[1])
          .toEqual({ kind: 'repository', page: 1 });
        expect(replace).toHaveBeenCalledWith({
          path: `/app/projects/${PROJECT}`,
          query: { tab: 'a' },
          hash: '#evidence',
        });
        expect(push).not.toHaveBeenCalled();
        const link = outcome.querySelector('a');
        expect(link?.getAttribute('href') ?? null).toBe(
          value === 'different_account' ? '/app/settings/sessions' : null,
        );
      } finally {
        replace.mockRestore();
        push.mockRestore();
      }
    });

  it.each([
    ['another value', '?evidence=Connected'],
    ['an empty value', '?evidence='],
    ['a repeated value', '?evidence=connected&evidence=connected'],
  ])('ignores %s and clears it', async (_name, search) => {
    await mountEditor(undefined, `/app/projects/${PROJECT}${search}`);

    expect(dialog()).toBeNull();
    await vi.waitFor(() => expect(useRouter().currentRoute.value.fullPath)
      .toBe(`/app/projects/${PROJECT}`));
  });

  it('returns focus to Add when the dialog closes', async () => {
    await mountEditor();
    inDocument('#project-evidence-add').click();
    await flushPromises();
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(dialog()?.contains(document.activeElement)).toBe(true);
    document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'Escape', bubbles: true,
    }));
    await flushPromises();
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(dialog()).toBeNull();
    expect(document.activeElement?.id).toBe('project-evidence-add');
  });

  it('returns focus to Add after a failed save left a try-again notice',
    async () => {
      const wrapper = await mountEditor();
      await typeInto('#project-en-name', 'Typed');
      api.replace.mockResolvedValue(failed('unavailable'));
      await wrapper.get('[data-testid="project-form"]').trigger('submit');
      await flushPromises();
      expect(inDocument('[data-testid="project-notice"]').textContent)
        .toContain('Try again');

      inDocument('#project-evidence-add').click();
      await flushPromises();
      await new Promise((resolve) => setTimeout(resolve, 0));
      expect(dialog()?.contains(document.activeElement)).toBe(true);
      document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', {
        key: 'Escape', bubbles: true,
      }));
      await flushPromises();
      await new Promise((resolve) => setTimeout(resolve, 0));

      expect(dialog()).toBeNull();
      expect(document.activeElement?.id).toBe('project-evidence-add');
    });

  it('starts Continue with the server project id', async () => {
    evidenceApi.candidates.mockResolvedValue(
      evidenceFailed('session-required'),
    );
    github.start.mockResolvedValue({ kind: 'failed', failure: 'failed' });
    await mountEditor(undefined, `/app/projects/${PROJECT.toUpperCase()}`);
    inDocument('#project-evidence-add').click();
    await flushPromises();

    inDocument('[data-action="evidence-continue"]').click();
    await flushPromises();

    expect(github.start.mock.calls)
      .toEqual([['csrf-1', `/app/projects/${PROJECT}`]]);
  });

  it('disables Save while an evidence write runs', async () => {
    let answer: (value: unknown) => void = () => {};
    evidenceApi.recheck.mockReturnValue(new Promise((resolve) => {
      answer = resolve;
    }) as never);
    await mountEditor();

    inDocument(`[data-evidence-item="${E1}"] [data-action="evidence-recheck"]`)
      .click();
    await flushPromises();
    await typeInto('#project-en-name', 'Typed during the check');

    const save = inDocument<HTMLButtonElement>('[data-action="project-save"]');
    expect(save.disabled).toBe(true);
    answer(written(two(), '4'));
    await flushPromises();

    expect(save.disabled).toBe(false);
    expect(inDocument<HTMLInputElement>('#project-en-name').value)
      .toBe('Typed during the check');
  });

  it('shows changed-elsewhere with Reload on a revision conflict, and Reload '
    + 'takes the winner with its evidence', async () => {
    const winner = project({
      id: PROJECT, revision: '9', evidence: [two()[1]!],
    });
    evidenceApi.remove.mockResolvedValue(stale(winner));
    await mountEditor();

    inDocument(`[data-evidence-item="${E1}"] [data-action="evidence-remove"]`)
      .click();
    await flushPromises();

    expect(inDocument('[data-testid="project-notice"]').textContent)
      .toContain('This project changed elsewhere.');
    expect(evidenceApi.remove).toHaveBeenCalledTimes(1);
    expect(inDocument<HTMLButtonElement>(
      `[data-evidence-item="${E1}"] [data-action="evidence-recheck"]`,
    ).disabled).toBe(true);
    inDocument('[data-action="project-reload"]').click();
    await flushPromises();

    expect([...document.body.querySelectorAll('[data-evidence-item]')]
      .map((row) => row.getAttribute('data-evidence-item'))).toEqual([E2]);
  });

  it('disables Change address and Delete while an evidence write runs',
    async () => {
      let answer: (value: unknown) => void = () => {};
      evidenceApi.recheck.mockReturnValue(new Promise((resolve) => {
        answer = resolve;
      }) as never);
      await mountEditor();
      const address = inDocument<HTMLButtonElement>('#project-change-address');
      const remove = inDocument<HTMLButtonElement>('#project-delete');

      inDocument(
        `[data-evidence-item="${E1}"] [data-action="evidence-recheck"]`,
      ).click();
      await flushPromises();

      expect([address.disabled, remove.disabled]).toEqual([true, true]);
      answer(written(two(), '4'));
      await flushPromises();
      expect([address.disabled, remove.disabled]).toEqual([false, false]);
    });

  it('returns focus to the Check again that opened the dialog', async () => {
    evidenceApi.recheck.mockResolvedValue(evidenceFailed('session-required'));
    evidenceApi.candidates.mockResolvedValue(
      evidenceFailed('session-required'),
    );
    await mountEditor();
    const recheck = inDocument<HTMLButtonElement>(
      `[data-evidence-item="${E2}"] [data-action="evidence-recheck"]`,
    );
    recheck.focus();
    recheck.click();
    await flushPromises();
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(inDocument('[data-testid="evidence-session-ended"]')).toBeTruthy();
    expect(dialog()?.contains(document.activeElement)).toBe(true);
    document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'Escape', bubbles: true,
    }));
    await flushPromises();
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(dialog()).toBeNull();
    expect(document.activeElement).toBe(recheck);
  });

  it('returns focus to Add after a dialog the return reopened', async () => {
    await mountEditor(undefined, `/app/projects/${PROJECT}?evidence=connected`);
    expect(dialog()).not.toBeNull();

    document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'Escape', bubbles: true,
    }));
    await flushPromises();
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(document.activeElement?.id).toBe('project-evidence-add');
  });

  it('shows no save-first hint in the changed-elsewhere state', async () => {
    evidenceApi.remove.mockResolvedValue(stale(project({
      id: PROJECT, revision: '9', evidence: two(),
    })));
    await mountEditor();

    inDocument(`[data-evidence-item="${E1}"] [data-action="evidence-remove"]`)
      .click();
    await flushPromises();

    expect(inDocument('[data-testid="project-notice"]').textContent)
      .toContain('This project changed elsewhere.');
    expect(inDocument<HTMLButtonElement>(
      `[data-evidence-item="${E1}"] [data-action="evidence-recheck"]`,
    ).disabled).toBe(true);
    expect(document.body.querySelector(
      '[data-testid="project-evidence-save-first"]',
    )).toBeNull();
  });

  it('shows the not-found view when a listing finds no project', async () => {
    await mountEditor();
    evidenceApi.candidates.mockResolvedValue(
      evidenceFailed('project-not-found'),
    );

    inDocument('#project-evidence-add').click();
    await flushPromises();
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(document.body.querySelector('[data-testid="project-not-found"]'))
      .not.toBeNull();
    expect(dialog()).toBeNull();
  });

  it('disables Add while the project changed elsewhere, until Reload',
    async () => {
      evidenceApi.remove.mockResolvedValue(stale(project({
        id: PROJECT, revision: '9', evidence: two(),
      })));
      await mountEditor();
      const add = inDocument<HTMLButtonElement>('#project-evidence-add');
      expect(add.disabled).toBe(false);

      inDocument(`[data-evidence-item="${E1}"] [data-action="evidence-remove"]`)
        .click();
      await flushPromises();
      expect(inDocument<HTMLButtonElement>('#project-evidence-add').disabled)
        .toBe(true);

      inDocument('[data-action="project-reload"]').click();
      await flushPromises();
      expect(inDocument<HTMLButtonElement>('#project-evidence-add').disabled)
        .toBe(false);
    });

  it('closes the dialog onto the changed-elsewhere banner', async () => {
    evidenceApi.attach.mockResolvedValue(evidenceFailed('duplicate'));
    await mountEditor();
    api.read.mockResolvedValue(ok(project({
      id: PROJECT, revision: '7', evidence: two(),
      text: { en: { name: 'Elsewhere' } },
    })));
    inDocument('#project-evidence-add').click();
    await flushPromises();
    const tab = [...document.body.querySelectorAll<HTMLElement>(
      '[role="tab"]',
    )].find((node) => node.textContent?.trim() === 'Paste a link')!;
    tab.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    await flushPromises();
    await typeInto('#evidence-link', 'https://github.com/octocat/hello');

    inDocument('[data-action="evidence-attach-link"]').click();
    await flushPromises();
    await new Promise((resolve) => setTimeout(resolve, 0));
    await flushPromises();

    expect(dialog()).toBeNull();
    const banner = inDocument('[data-testid="project-notice"]');
    expect(banner.textContent).toContain('This project changed elsewhere.');
    expect(document.activeElement).toBe(banner);
    expect(inDocument<HTMLButtonElement>('#project-evidence-add').disabled)
      .toBe(true);
  });
});
