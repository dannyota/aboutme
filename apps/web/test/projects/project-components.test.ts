import { mountSuspended } from '@nuxt/test-utils/runtime';
import { describe, expect, it } from 'vitest';

import ProjectTechnologies
  from '../../app/components/projects/ProjectTechnologies.vue';
import { projectsCopy } from '../../app/i18n/projects';
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
  'components/projects/projectIssues.ts',
  'components/projects/projectDocument.ts',
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
};

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
