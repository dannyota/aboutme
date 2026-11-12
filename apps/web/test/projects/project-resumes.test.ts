import { flushPromises } from '@vue/test-utils';
import { effectScope, ref } from 'vue';
import { describe, expect, it, vi } from 'vitest';

import {
  takeUnlinked,
  type LinkResult,
} from '../../app/components/projects/projectLinks';
import {
  useProjectResumes,
} from '../../app/components/projects/projectResumes';
import type { ResumeListResult } from '../../app/editor/resumeApi';
import { useProjectList } from '../../app/composables/useProjects';
import {
  fakeApi,
  fakeAuth,
  ok,
  project,
  resumeSummary,
  sequence,
} from './fixtures';

function inScope<T>(run: () => T): T {
  return effectScope().run(run)!;
}

const linkOk: LinkResult = { kind: 'ok' };
const linkFailed = (failure: string) =>
  ({ kind: 'failed', failure }) as LinkResult;

const three = () => [
  resumeSummary('resume-1', '1', 'Backend'),
  resumeSummary('resume-2', '1', 'Frontend'),
  resumeSummary('resume-3', '1', 'Data'),
];

function fakeLinker(items = three()) {
  return {
    resumes: vi.fn<() => Promise<ResumeListResult>>()
      .mockResolvedValue({ kind: 'ready', items }),
    link: vi.fn<(owner: string, resume: string, project: string) =>
    Promise<LinkResult>>().mockResolvedValue(linkOk),
  };
}

function createSetup() {
  const api = fakeApi();
  const { auth } = fakeAuth();
  const navigate = vi.fn();
  const linker = fakeLinker();
  api.list.mockResolvedValue(ok([]));
  api.create.mockResolvedValue(ok(project({ id: 'created' })));
  const list = inScope(() => useProjectList(ref('en'), {
    api, auth, uuid: sequence(), navigate, linker,
  }));
  return { api, navigate, linker, list };
}

describe('new project shown on resumes', () => {
  it('offers the account resumes when the dialog asks', async () => {
    const { list } = createSetup();
    await flushPromises();

    await list.loadResumes();

    expect(list.resumes.value.map((item) => item.title))
      .toEqual(['Backend', 'Frontend', 'Data']);
  });

  it('creates without links when no resume is checked', async () => {
    const { list, linker, navigate } = createSetup();
    await flushPromises();
    await list.loadResumes();

    await list.create('Shop', []);

    expect(linker.link).not.toHaveBeenCalled();
    expect(navigate).toHaveBeenCalledWith('/app/projects/created');
    expect(takeUnlinked('created')).toEqual([]);
  });

  it('links the one checked resume before opening the editor', async () => {
    const { list, linker, navigate } = createSetup();
    await flushPromises();
    await list.loadResumes();
    linker.link.mockImplementation(async () => {
      expect(navigate).not.toHaveBeenCalled();
      return linkOk;
    });

    await list.create('Shop', ['resume-2']);

    expect(linker.link.mock.calls)
      .toEqual([['owner-1', 'resume-2', 'created']]);
    expect(navigate).toHaveBeenCalledWith('/app/projects/created');
    expect(takeUnlinked('created')).toEqual([]);
  });

  it('links all three and names the ones that failed for the editor',
    async () => {
      const { list, linker, navigate } = createSetup();
      await flushPromises();
      await list.loadResumes();
      linker.link
        .mockResolvedValueOnce(linkFailed('busy'))
        .mockResolvedValueOnce(linkFailed('already-linked'))
        .mockResolvedValueOnce(linkFailed('unavailable'));

      await list.create('Shop', ['resume-1', 'resume-2', 'resume-3']);

      expect(linker.link).toHaveBeenCalledTimes(3);
      expect(navigate).toHaveBeenCalledWith('/app/projects/created');
      expect(takeUnlinked('created')).toEqual(['Backend', 'Data']);
    });

  it('links nothing when the create fails', async () => {
    const { api, list, linker } = createSetup();
    await flushPromises();
    await list.loadResumes();
    api.create.mockResolvedValue({ kind: 'failed', failure: 'limit' });

    await list.create('Shop', ['resume-1']);

    expect(linker.link).not.toHaveBeenCalled();
    expect(list.createState.value).toBe('limit');
  });
});

function resumesSetup(
  options: { dirty?: boolean; linker?: ReturnType<typeof fakeLinker> } = {},
) {
  const api = fakeApi();
  const { auth } = fakeAuth();
  const linker = options.linker ?? fakeLinker();
  const loaded = ref(project({ linkedResumeIds: ['resume-1'] }));
  const dirty = ref(options.dirty ?? false);
  const resumes = inScope(() => useProjectResumes(loaded, dirty, {
    api, auth, linker,
  }));
  return { api, linker, loaded, dirty, resumes };
}

describe('project resumes section', () => {
  it('lists every resume and marks the linked ones', async () => {
    const { resumes } = resumesSetup();
    await flushPromises();

    expect(resumes.view.value).toMatchObject({ kind: 'ready' });
    expect([...resumes.linked.value]).toEqual(['resume-1']);
  });

  it('does nothing while the project has unsaved changes', async () => {
    const { resumes, linker, dirty } = resumesSetup({ dirty: true });
    await flushPromises();

    await resumes.add('resume-2');
    expect(linker.link).not.toHaveBeenCalled();
    dirty.value = false;
    await resumes.add('resume-2');
    expect(linker.link).toHaveBeenCalledOnce();
  });

  it('Linked: the row turns to Shown and the status names the resume',
    async () => {
      const { resumes, linker } = resumesSetup();
      await flushPromises();

      await resumes.add('resume-2');

      expect(linker.link).toHaveBeenCalledWith(
        'owner-1', 'resume-2', 'project-1',
      );
      expect(resumes.linked.value.has('resume-2')).toBe(true);
      expect(resumes.status.value).toMatchObject({
        kind: 'linked', resume: { title: 'Frontend' },
      });
    });

  it('project_already_linked: reloads the list and the row shows Shown',
    async () => {
      const { resumes, linker, api } = resumesSetup();
      await flushPromises();
      linker.link.mockResolvedValue(linkFailed('already-linked'));
      api.read.mockResolvedValue(ok(project({
        linkedResumeIds: ['resume-1', 'resume-2'],
      })));

      await resumes.add('resume-2');

      expect(api.read).toHaveBeenCalledWith('project-1');
      expect(linker.resumes).toHaveBeenCalledTimes(2);
      expect(resumes.linked.value.has('resume-2')).toBe(true);
      expect(resumes.status.value).toBeNull();
    });

  it.each([
    ['document-invalid', 'document-invalid'],
    ['busy', 'busy'],
  ] as const)('%s: says so and names the resume', async (failure, kind) => {
    const { resumes, linker } = resumesSetup();
    await flushPromises();
    linker.link.mockResolvedValue(linkFailed(failure));

    await resumes.add('resume-3');

    expect(resumes.status.value).toMatchObject({
      kind, resume: { id: 'resume-3', title: 'Data' },
    });
    expect(resumes.linked.value.has('resume-3')).toBe(false);
  });

  it.each([['rate-limited'], ['unavailable'], ['stale']] as const)(
    '%s: says to try again and retries nothing', async (failure) => {
      const { resumes, linker } = resumesSetup();
      await flushPromises();
      linker.link.mockResolvedValue(linkFailed(failure));

      await resumes.add('resume-2');
      await flushPromises();

      expect(resumes.status.value).toEqual({ kind: 'try-again' });
      expect(linker.link).toHaveBeenCalledOnce();
    },
  );

  it('shows an unavailable list with Retry', async () => {
    const linker = fakeLinker();
    linker.resumes.mockResolvedValueOnce({ kind: 'failed', reason: 'network' });
    const { resumes } = resumesSetup({ linker });
    await flushPromises();
    expect(resumes.view.value).toEqual({ kind: 'unavailable' });

    await resumes.retry();

    expect(resumes.view.value).toMatchObject({ kind: 'ready' });
  });
});

describe('adding to a second published resume', () => {
  const published = () => [
    resumeSummary('resume-1', '1', 'Backend', { live: true, slug: 'ada-1' }),
    resumeSummary('resume-2', '1', 'Frontend', { live: true, slug: 'ada-2' }),
    resumeSummary('resume-3', '1', 'Data'),
  ];

  function privacySetup(linkedResumeIds: string[]) {
    const api = fakeApi();
    const { auth } = fakeAuth();
    const linker = fakeLinker(published());
    const loaded = ref(project({ linkedResumeIds }));
    const resumes = inScope(() => useProjectResumes(loaded, ref(false), {
      api, auth, linker,
    }));
    return { linker, resumes };
  }

  it('asks first when another published resume already shows it',
    async () => {
      const { resumes, linker } = privacySetup(['resume-1']);
      await flushPromises();

      await resumes.add('resume-2');

      expect(resumes.asking.value).toBe('resume-2');
      expect(linker.link).not.toHaveBeenCalled();
    });

  it('does not ask for a draft', async () => {
    const { resumes, linker } = privacySetup(['resume-1']);
    await flushPromises();

    await resumes.add('resume-3');

    expect(resumes.asking.value).toBeNull();
    expect(linker.link).toHaveBeenCalledOnce();
  });

  it('does not ask for the first published resume', async () => {
    const { resumes, linker } = privacySetup(['resume-3']);
    await flushPromises();

    await resumes.add('resume-2');

    expect(resumes.asking.value).toBeNull();
    expect(linker.link).toHaveBeenCalledOnce();
  });

  it('Cancel sends nothing', async () => {
    const { resumes, linker } = privacySetup(['resume-1']);
    await flushPromises();
    await resumes.add('resume-2');

    resumes.cancelAsk();
    await flushPromises();

    expect(resumes.asking.value).toBeNull();
    expect(linker.link).not.toHaveBeenCalled();
  });

  it('Add anyway sends once', async () => {
    const { resumes, linker } = privacySetup(['resume-1']);
    await flushPromises();
    await resumes.add('resume-2');

    await resumes.addAnyway();
    await resumes.addAnyway();

    expect(linker.link.mock.calls)
      .toEqual([['owner-1', 'resume-2', 'project-1']]);
    expect(resumes.linked.value.has('resume-2')).toBe(true);
  });
});
