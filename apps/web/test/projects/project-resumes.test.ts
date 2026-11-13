import { flushPromises } from '@vue/test-utils';
import { effectScope, ref } from 'vue';
import { describe, expect, it, vi } from 'vitest';

import {
  takeUnlinked,
  type LinkResult,
} from '../../app/components/projects/projectLinks';
import {
  useProjectResumes,
} from '../../app/components/projects/projectResumesState';
import type {
  ResumeListResult,
  ResumeSummary,
} from '../../app/editor/resumeApi';
import { useProjectList } from '../../app/composables/useProjects';
import {
  failed,
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

  it('reads the resumes again on submit and stops to show the note when '
    + 'two checked resumes became published', async () => {
    const { api, list, linker } = createSetup();
    await flushPromises();
    await list.loadResumes();
    linker.resumes.mockResolvedValue({
      kind: 'ready',
      items: three().map((item) => item.id === 'resume-3'
        ? item
        : { ...item, live: true, slug: item.id }),
    });

    await list.create('Shop', ['resume-1', 'resume-2']);

    expect(api.create).not.toHaveBeenCalled();
    expect(list.createState.value).toBe('idle');
    expect(list.resumes.value.filter((item) => item.live)
      .map((item) => item.id)).toEqual(['resume-1', 'resume-2']);
    await list.create('Shop', ['resume-1', 'resume-2']);
    expect(api.create).toHaveBeenCalledOnce();
  });

  it('shows the note when the read at submit fails for two checked '
    + 'resumes, then creates on the next submit', async () => {
    const { api, list, linker } = createSetup();
    await flushPromises();
    await list.loadResumes();
    linker.resumes.mockResolvedValue({ kind: 'failed', reason: 'network' });

    await list.create('Shop', ['resume-1', 'resume-2']);

    expect(api.create).not.toHaveBeenCalled();
    expect(list.privacyNote.value).toBe(true);
    await list.create('Shop', ['resume-1', 'resume-2']);
    expect(api.create).toHaveBeenCalledOnce();
  });

  it('creates at once when the read at submit fails for one checked resume',
    async () => {
      const { api, list, linker } = createSetup();
      await flushPromises();
      await list.loadResumes();
      linker.resumes.mockResolvedValue({ kind: 'failed', reason: 'network' });

      await list.create('Shop', ['resume-1']);

      expect(list.privacyNote.value).toBe(false);
      expect(api.create).toHaveBeenCalledOnce();
    });

  it('decides each submit anew and clears the note when the dialog opens '
    + 'again', async () => {
    const { list, linker } = createSetup();
    await flushPromises();
    await list.loadResumes();
    linker.resumes.mockResolvedValue({ kind: 'failed', reason: 'network' });
    await list.create('Shop', ['resume-1', 'resume-2']);
    const reads = linker.resumes.mock.calls.length;

    list.resetCreate();

    expect(list.privacyNote.value).toBe(false);
    await list.create('Shop', ['resume-1', 'resume-2']);
    expect(linker.resumes.mock.calls.length).toBe(reads + 1);
    expect(list.privacyNote.value).toBe(true);
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

      expect(linker.link.mock.calls[0]?.slice(0, 3))
        .toEqual(['owner-1', 'resume-2', 'project-1']);
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
      expect(linker.resumes).toHaveBeenCalledTimes(3);
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
  const live = (id: string, title: string) =>
    resumeSummary(id, '1', title, { live: true, slug: id });
  const published = () => [
    live('resume-1', 'Backend'),
    live('resume-2', 'Frontend'),
    resumeSummary('resume-3', '1', 'Data'),
  ];

  interface Account {
    items: ResumeSummary[] | null;
    linked: string[] | null;
  }

  function privacySetup(
    loadedLinks: string[],
    account: Partial<Account> = {},
  ) {
    const api = fakeApi();
    const { auth } = fakeAuth();
    const now: Account = {
      items: published(), linked: loadedLinks, ...account,
    };
    const sent: string[] = [];
    const linker = {
      resumes: vi.fn(async (): Promise<ResumeListResult> => (now.items === null
        ? { kind: 'failed', reason: 'network' }
        : { kind: 'ready', items: now.items })),
      link: vi.fn(async (_owner: string, resumeId: string) => {
        sent.push(resumeId);
        return linkOk;
      }),
    };
    api.read.mockImplementation(async () => (now.linked === null
      ? failed('unavailable')
      : ok(project({ linkedResumeIds: now.linked }))));
    const loaded = ref(project({ linkedResumeIds: loadedLinks }));
    const resumes = inScope(() => useProjectResumes(loaded, ref(false), {
      api, auth, linker,
    }));
    return { api, now, linker, sent, resumes };
  }

  it('asks first when another published resume already shows it',
    async () => {
      const { resumes, sent } = privacySetup(['resume-1']);
      await flushPromises();

      await resumes.add('resume-2');

      expect(resumes.asking.value).toBe('resume-2');
      expect(sent).toEqual([]);
    });

  it('does not ask for a draft', async () => {
    const { resumes, sent } = privacySetup(['resume-1']);
    await flushPromises();

    await resumes.add('resume-3');

    expect(resumes.asking.value).toBeNull();
    expect(sent).toEqual(['resume-3']);
  });

  it('does not ask for the first published resume', async () => {
    const { resumes, sent } = privacySetup(['resume-3']);
    await flushPromises();

    await resumes.add('resume-2');

    expect(resumes.asking.value).toBeNull();
    expect(sent).toEqual(['resume-2']);
  });

  it('Cancel sends nothing', async () => {
    const { resumes, sent } = privacySetup(['resume-1']);
    await flushPromises();
    await resumes.add('resume-2');

    resumes.cancelAsk();
    await flushPromises();

    expect(resumes.asking.value).toBeNull();
    expect(sent).toEqual([]);
  });

  it('Add anyway sends once, deciding nothing again', async () => {
    const { resumes, linker, api, sent } = privacySetup(['resume-1']);
    await flushPromises();
    await resumes.add('resume-2');
    const reads = [
      linker.resumes.mock.calls.length, api.read.mock.calls.length,
    ];

    await resumes.addAnyway();
    await resumes.addAnyway();

    expect(sent).toEqual(['resume-2']);
    expect([linker.resumes.mock.calls.length, api.read.mock.calls.length])
      .toEqual(reads);
    expect(resumes.linked.value.has('resume-2')).toBe(true);
  });

  it('decides once per click, from reads taken when the person clicks',
    async () => {
      const { resumes, linker, api } = privacySetup(['resume-3']);
      await flushPromises();
      const loadReads = linker.resumes.mock.calls.length;

      await resumes.add('resume-2');

      expect(linker.resumes.mock.calls.length - loadReads).toBe(1);
      expect(api.read).toHaveBeenCalledOnce();
      expect(api.read).toHaveBeenCalledWith('project-1');
    });

  it('asks when the resume was published elsewhere after the list loaded',
    async () => {
      const { resumes, sent, now } = privacySetup(['resume-1'], {
        items: [live('resume-1', 'Backend'), resumeSummary('resume-2')],
      });
      await flushPromises();
      now.items = published();

      await resumes.add('resume-2');

      expect(resumes.asking.value).toBe('resume-2');
      expect(sent).toEqual([]);
    });

  it('asks when another resume linked it after the page loaded', async () => {
    const { resumes, sent, now } = privacySetup([]);
    await flushPromises();
    now.linked = ['resume-1'];

    await resumes.add('resume-2');

    expect(resumes.asking.value).toBe('resume-2');
    expect(sent).toEqual([]);
  });

  it.each([
    ['the project', { linked: null }],
    ['the account resumes', { items: null }],
  ] as const)('asks when %s cannot be read again', async (_name, failure) => {
    const { resumes, sent, now } = privacySetup([]);
    await flushPromises();
    Object.assign(now, failure);

    await resumes.add('resume-2');

    expect(resumes.asking.value).toBe('resume-2');
    expect(sent).toEqual([]);
  });

  it('does not ask for a draft even when the project cannot be read again',
    async () => {
      const { resumes, sent } = privacySetup([], { linked: null });
      await flushPromises();

      await resumes.add('resume-3');

      expect(sent).toEqual(['resume-3']);
    });

  it('keeps no answer: the next resume and the next click ask again',
    async () => {
      const resumes4 = [...published(), live('resume-4', 'Mobile')];
      const { resumes, sent } = privacySetup(['resume-1'], {
        items: resumes4,
      });
      await flushPromises();
      await resumes.add('resume-2');
      await resumes.addAnyway();
      expect(sent).toEqual(['resume-2']);

      await resumes.add('resume-4');
      expect(resumes.asking.value).toBe('resume-4');
      resumes.cancelAsk();
      await resumes.add('resume-4');
      expect(resumes.asking.value).toBe('resume-4');
      expect(sent).toEqual(['resume-2']);
    });

  it('does not decide again while the link is out, even if another tab '
    + 'publishes meanwhile', async () => {
    const { resumes, sent, linker, now } = privacySetup(['resume-3']);
    await flushPromises();
    linker.link.mockImplementationOnce(async (_owner, resumeId) => {
      now.items = [...published()];
      now.linked = ['resume-1'];
      sent.push(resumeId);
      return linkOk;
    });

    await resumes.add('resume-2');

    expect(resumes.asking.value).toBeNull();
    expect(sent).toEqual(['resume-2']);
  });
});
