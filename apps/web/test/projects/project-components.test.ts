import { mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { ref } from 'vue';
import { describe, expect, it, vi } from 'vitest';

import ProjectLeaveDialog
  from '../../app/components/projects/ProjectLeaveDialog.vue';
import ProjectReleaseDialog
  from '../../app/components/projects/ProjectReleaseDialog.vue';
import ProjectTechnologies
  from '../../app/components/projects/ProjectTechnologies.vue';
import type { ReleaseState } from '../../app/composables/projectsApi';
import { projectsCopy } from '../../app/i18n/projects';
import { project } from './fixtures';
import {
  requiresSession,
  signedOutRedirect,
} from '../../app/utils/returnPath';
import { literalGuard, parityViolations } from '../support/localizationSource';

async function mountChips(initial: string[] = []) {
  const wrapper = await mountSuspended(ProjectTechnologies, {
    props: {
      'id': 'technologies',
      'modelValue': initial,
      'copy': projectsCopy.en,
      'onUpdate:modelValue': (value: string[]) => {
        void wrapper.setProps({ modelValue: value });
      },
    },
  });
  return wrapper;
}

function chips(wrapper: Awaited<ReturnType<typeof mountChips>>): string[] {
  return wrapper.findAll('[data-technology] span').map((chip) => chip.text());
}

describe('technology chips', () => {
  it('adds with Enter or a comma and removes the last with Backspace',
    async () => {
      const wrapper = await mountChips();
      const input = wrapper.get('[data-testid="project-technology-input"]');

      await input.setValue('Go');
      await input.trigger('keydown', { key: 'Enter' });
      await input.setValue('Vue');
      await input.trigger('keydown', { key: ',' });
      await input.setValue('Caddy, Podman,');
      expect(chips(wrapper)).toEqual(['Go', 'Vue', 'Caddy', 'Podman']);

      await input.setValue('');
      await input.trigger('keydown', { key: 'Backspace' });
      expect(chips(wrapper)).toEqual(['Go', 'Vue', 'Caddy']);
    });

  it('names each remove button after its chip', async () => {
    const wrapper = await mountChips(['Go', 'Vue']);

    const remove = wrapper.get('button[aria-label="Remove Vue"]');
    await remove.trigger('click');

    expect(chips(wrapper)).toEqual(['Go']);
    expect(wrapper.get('ul').attributes('aria-label')).toBe('Technologies');
  });

  it('stops at 20 and refuses a name over 40 characters', async () => {
    const wrapper = await mountChips(
      Array.from({ length: 19 }, (_, index) => `tech-${index}`),
    );
    const input = wrapper.get('[data-testid="project-technology-input"]');

    await input.setValue('x'.repeat(41));
    await input.trigger('keydown', { key: 'Enter' });
    expect(wrapper.text()).toContain(
      'Each technology is at most 40 characters.',
    );
    await input.setValue('last');
    await input.trigger('keydown', { key: 'Enter' });
    await input.setValue('extra');
    await input.trigger('keydown', { key: 'Enter' });

    expect(chips(wrapper)).toHaveLength(20);
    expect(chips(wrapper).at(-1)).toBe('last');
    expect(wrapper.text()).toContain('All 20 technologies are used.');
  });
});

const sources = [
  'pages/app/projects/index.vue',
  'pages/app/projects/[id].vue',
  'components/projects/ProjectList.vue',
  'components/projects/ProjectCreateDialog.vue',
  'components/projects/ProjectReleaseDialog.vue',
  'components/projects/ProjectLanguageDialog.vue',
  'components/projects/ProjectTechnologies.vue',
  'components/projects/ProjectTextFields.vue',
  'components/projects/ProjectLeaveDialog.vue',
  'components/projects/projectIssues.ts',
  'components/projects/projectDocument.ts',
  'components/projects/ProjectResumes.vue',
  'components/projects/projectLinks.ts',
  'components/projects/projectResumes.ts',
  'composables/useProjects.ts',
  'composables/projectsApi.ts',
];
const fixtures: Readonly<Record<string, readonly unknown[]>> = {
  'projects.count': [3, 30],
  'projects.limitReached': [30],
  'projects.updated': ['now'],
  'projects.moreActions': ['Shop'],
  'projects.technologiesLabel': ['Shop'],
  'projects.languagesLabel': ['Shop'],
  'projects.moreTechnologies': [2],
  'projects.languageChip': ['VI', true],
  'projects.defaultTab': ['Tiếng Việt'],
  'projects.languageActions': ['English'],
  'projects.counter': [100, 120],
  'projects.technologiesFull': [20],
  'projects.technologyTooLong': [40],
  'projects.removeTechnology': ['Go'],
  'projects.errors.tooLong': [120],
  'projects.errors.technologies': [20, 40],
  'projects.deleteDescription': ['Shop'],
  'projects.languageName': ['ja'],
  'projects.notLinked': [['Backend', 'Data']],
  'projects.pageUses': ['English'],
  'projects.linkedTo': ['Backend'],
  'projects.cannotTake': ['Backend'],
  'projects.resumeBusy': ['Backend'],
  'projects.deleteLinked': [2],
};

describe('leave confirmation', () => {
  it('focuses Stay and reports either choice', async () => {
    const wrapper = await mountSuspended(ProjectLeaveDialog, {
      props: { open: true, copy: projectsCopy.en },
      attachTo: document.body,
    });
    await new Promise((resolve) => setTimeout(resolve, 0));

    const dialog = document.body.querySelector('[role="alertdialog"]');
    expect(dialog?.textContent).toContain('You have unsaved changes.');
    expect(document.activeElement?.textContent?.trim()).toBe('Stay');
    document.body.querySelector<HTMLElement>(
      '[data-action="project-leave-confirm"]',
    )?.click();
    document.body.querySelector<HTMLElement>(
      '[data-action="project-leave-stay"]',
    )?.click();
    expect(wrapper.emitted('leave')).toHaveLength(1);
    expect(wrapper.emitted('stay')).toHaveLength(1);
    wrapper.unmount();
  });

  it('uses the decided wording in both languages', () => {
    expect([projectsCopy.vi.leaveTitle, projectsCopy.en.leaveTitle])
      .toEqual(['Bạn có thay đổi chưa lưu.', 'You have unsaved changes.']);
    expect([projectsCopy.vi.stay, projectsCopy.vi.leave])
      .toEqual(['Ở lại', 'Rời đi không lưu']);
    expect([projectsCopy.en.stay, projectsCopy.en.leave])
      .toEqual(['Stay', 'Leave without saving']);
  });
});

function releaseAction(initial: ReleaseState) {
  return {
    state: ref<ReleaseState>(initial),
    start: vi.fn(async () => {}),
    reauthPassword: vi.fn(async () => {}),
    startProviderReauth: vi.fn(async () => {}),
    retryAfterProviderReauth: vi.fn(async () => {}),
    continueAfterSecondFactor: vi.fn(async () => {}),
    reset: vi.fn(),
  };
}

async function mountRelease(
  action: ReturnType<typeof releaseAction>,
  locale: 'vi' | 'en' = 'en',
) {
  const wrapper = await mountSuspended(ProjectReleaseDialog, {
    props: {
      open: true,
      kind: 'address',
      project: project(),
      action,
      copy: projectsCopy[locale],
    },
    attachTo: document.body,
  });
  await flushPromises();
  return wrapper;
}

describe('release second verification step', () => {
  it.each([
    [
      'en',
      'Your account has a second verification step. Complete it in the new '
      + 'tab, then come back here.',
      'Open verification step',
      'Continue',
    ],
    [
      'vi',
      'Tài khoản của bạn có bước xác minh thứ hai. Hoàn tất bước đó trong '
      + 'tab mới, rồi quay lại đây.',
      'Mở bước xác minh',
      'Tiếp tục',
    ],
  ] as const)('links the %s step and continues the same action',
    async (locale, intro, linkText, continueText) => {
      const action = releaseAction({ kind: 'idle' });
      const wrapper = await mountRelease(action, locale);
      action.state.value = { kind: 'reauth-second-factor' };
      await flushPromises();

      const dialog = document.body.querySelector('[role="dialog"]')!;
      expect(dialog.textContent).toContain(intro);
      expect(dialog.textContent).toContain(projectsCopy[locale].reauthIntro);
      const link = dialog.querySelector<HTMLAnchorElement>(
        '[data-action="project-second-factor-link"]',
      )!;
      expect(link.textContent?.trim()).toBe(linkText);
      expect(link.getAttribute('href')).toBe('/login/second-factor');
      expect(link.getAttribute('target')).toBe('_blank');
      expect(link.getAttribute('rel')).toBe('noopener');
      expect(dialog.querySelector('[data-action="project-password"]'))
        .toBeNull();
      expect(dialog.querySelector('[data-action="project-address-submit"]'))
        .toBeNull();
      expect(dialog.querySelector<HTMLInputElement>(
        '[data-action="project-new-address"]',
      )?.disabled).toBe(true);
      const next = dialog.querySelector<HTMLElement>(
        '[data-action="project-second-factor-continue"]',
      )!;
      expect(next.textContent?.trim()).toBe(continueText);

      next.click();
      await flushPromises();

      expect(action.continueAfterSecondFactor).toHaveBeenCalledOnce();
      expect(action.reauthPassword).not.toHaveBeenCalled();
      expect(action.start).not.toHaveBeenCalled();
      wrapper.unmount();
    });

  it.each([
    ['en', 'The second verification step is not complete yet.'],
    ['vi', 'Bước xác minh thứ hai chưa hoàn tất.'],
  ] as const)('returns to the %s password form when the factor is missing',
    async (locale, text) => {
      const action = releaseAction({ kind: 'idle' });
      const wrapper = await mountRelease(action, locale);
      action.state.value = {
        kind: 'reauth-second-factor-incomplete', method: 'password',
      };
      await flushPromises();

      const dialog = document.body.querySelector('[role="dialog"]')!;
      expect(dialog.querySelector('[role="alert"]')?.textContent?.trim())
        .toBe(text);
      expect(dialog.querySelector('[data-action="project-password"]'))
        .not.toBeNull();
      expect(dialog.querySelector(
        '[data-action="project-second-factor-continue"]',
      )).toBeNull();
      wrapper.unmount();
    });

  it('drops the action on cancel without continuing it', async () => {
    const action = releaseAction({ kind: 'idle' });
    const wrapper = await mountRelease(action);
    action.state.value = { kind: 'reauth-second-factor' };
    await flushPromises();
    action.reset.mockClear();

    document.body.querySelector<HTMLElement>(
      '[data-action="project-release-cancel"]',
    )!.click();
    await flushPromises();

    expect(action.reset).toHaveBeenCalledOnce();
    expect(action.continueAfterSecondFactor).not.toHaveBeenCalled();
    expect(wrapper.emitted('close')).toHaveLength(1);
    wrapper.unmount();
  });
});

describe('projects navigation', () => {
  it('sends a signed-out visit to sign in and back', () => {
    expect(requiresSession('/app/projects')).toBe(true);
    expect(signedOutRedirect('/app/projects/project-1'))
      .toBe('/login?next=%2Fapp%2Fprojects%2Fproject-1');
  });
});

describe('projects copy', () => {
  it('keeps both catalogs complete and aligned', () => {
    expect(parityViolations(
      projectsCopy.vi, projectsCopy.en, 'projects', fixtures,
    )).toEqual([]);
  });

  it('uses the strings the design quotes', () => {
    expect([projectsCopy.vi.title, projectsCopy.en.title])
      .toEqual(['Dự án', 'Projects']);
    expect([projectsCopy.vi.empty, projectsCopy.en.empty])
      .toEqual(['Chưa có dự án nào.', 'No projects yet.']);
    expect(projectsCopy.vi.count(3, 30)).toBe('3 / 30');
  });

  it('keeps display text out of the project sources', () => {
    const { sourceViolations } = literalGuard({});
    expect(sources.flatMap((path) => sourceViolations(path))).toEqual([]);
  });
});
