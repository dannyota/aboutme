import { flushPromises } from '@vue/test-utils';
import { computed, effectScope, ref } from 'vue';
import { describe, expect, it, vi } from 'vitest';

import {
  useProjectEvidence,
} from '../../app/components/projects/projectEvidenceState';
import { useProjectEditor } from '../../app/composables/useProjects';
import {
  EVIDENCE_IDS,
  evidenceFailed,
  evidenceItem,
  fakeApi,
  fakeAuth,
  fakeEvidenceApi,
  ok,
  project,
  sequence,
  stale,
  written,
} from './fixtures';

const [E1, E2, E3] = EVIDENCE_IDS;
const NOW = Date.parse('2026-11-18T09:00:00Z');

const three = () => [
  evidenceItem({ id: E1, title: 'First' }),
  evidenceItem({ id: E2, title: 'Second', kind: 'repository' }),
  evidenceItem({ id: E3, title: 'Third', kind: 'release' }),
];

async function setup(loaded = project({ evidence: three() })) {
  const api = fakeApi();
  const fakes = fakeAuth();
  const evidenceApi = fakeEvidenceApi();
  const onSessionEnded = vi.fn();
  api.read.mockResolvedValue(ok(loaded));
  const drafts = ref(0);
  const { editor, evidence } = effectScope().run(() => {
    const editor = useProjectEditor(loaded.id, {
      api, auth: fakes.auth, uuid: sequence('s'), navigate: vi.fn(),
    });
    const changed = computed(() => editor.dirty.value || drafts.value > 0);
    const evidence = useProjectEvidence(editor, changed, {
      api: evidenceApi,
      projects: api,
      auth: fakes.auth,
      uuid: sequence('e'),
      now: () => NOW,
      onSessionEnded,
    });
    return { editor, evidence };
  })!;
  await flushPromises();
  api.read.mockClear();
  return { api, evidenceApi, editor, evidence, drafts, onSessionEnded,
    ...fakes };
}

function held<T>() {
  let answer: (value: T) => void = () => {};
  const promise = new Promise<T>((resolve) => {
    answer = resolve;
  });
  return { promise, answer: (value: T) => answer(value) };
}

describe('project evidence writes', () => {
  it('is available only when the read carries evidence', async () => {
    const without = await setup(project());
    expect(without.evidence.available.value).toBe(false);
    const empty = await setup(project({ evidence: [] }));
    expect(empty.evidence.available.value).toBe(true);
    expect(empty.evidence.items.value).toEqual([]);
  });

  it('adopts each answer and sends the next write and Save with its revision',
    async () => {
      const { evidenceApi, api, editor, evidence } = await setup();
      const generation = editor.generation.value;
      const rechecked = three().map((item) => item.id === E1
        ? { ...item, confirmed: false, flagReason: 'gone' as const }
        : item);
      evidenceApi.recheck.mockResolvedValue(written(rechecked, '4'));

      await evidence.recheck(E1);

      expect(evidence.items.value[0]?.flagReason).toBe('gone');
      expect(editor.loaded.value?.revision).toBe('4');
      expect(editor.generation.value).toBe(generation);
      expect(editor.dirty.value).toBe(false);
      evidenceApi.remove.mockResolvedValue(written(rechecked.slice(0, 2), '5'));
      await evidence.remove(E3);
      expect(evidenceApi.recheck.mock.calls)
        .toEqual([['project-1', E1, '3', 'e1', 'csrf-1']]);
      expect(evidenceApi.remove.mock.calls)
        .toEqual([['project-1', E3, '4', 'e2', 'csrf-1']]);
      expect(evidence.items.value.map((item) => item.id)).toEqual([E1, E2]);

      editor.draft.value!.text.en!.name = 'Shop';
      api.replace.mockResolvedValue(ok(project({ revision: '6' })));
      await editor.save();
      expect(api.replace.mock.calls[0]?.[1]).toBe('5');
    });

  it('keeps text typed while a write runs', async () => {
    const { evidenceApi, editor, evidence } = await setup();
    const answer = held<Awaited<ReturnType<typeof evidenceApi.recheck>>>();
    evidenceApi.recheck.mockReturnValue(answer.promise);

    const running = evidence.recheck(E1);
    await flushPromises();
    expect(evidence.pending.value).toBe(E1);
    editor.draft.value!.text.en!.name = 'Typed during the write';
    answer.answer(written(three(), '4'));
    await running;

    expect(evidence.pending.value).toBeNull();
    expect(editor.draft.value?.text.en?.name).toBe('Typed during the write');
    expect(editor.loaded.value?.revision).toBe('4');
    expect(editor.dirty.value).toBe(true);
  });

  it('keeps one key across retryable failures and renews it after an answer',
    async () => {
      const { evidenceApi, evidence } = await setup();
      evidenceApi.recheck
        .mockResolvedValueOnce(evidenceFailed('upstream'))
        .mockResolvedValueOnce(evidenceFailed('rate-limited', 30))
        .mockResolvedValueOnce(evidenceFailed('busy'))
        .mockResolvedValueOnce(evidenceFailed('unavailable'))
        .mockResolvedValueOnce(evidenceFailed('csrf-rejected'))
        .mockResolvedValueOnce(evidenceFailed('csrf-rejected'))
        .mockResolvedValueOnce(evidenceFailed('session-lost'))
        .mockResolvedValueOnce(written(three(), '4'))
        .mockResolvedValueOnce(written(three(), '5'));

      for (let attempt = 0; attempt < 8; attempt += 1) {
        await evidence.recheck(E1);
      }

      expect(evidenceApi.recheck.mock.calls.map((call) => [call[2], call[3]]))
        .toEqual([
          ['3', 'e1'], ['3', 'e1'], ['3', 'e1'], ['3', 'e1'], ['3', 'e1'],
          ['3', 'e1'], ['3', 'e1'], ['3', 'e1'], ['4', 'e2'],
        ]);
    });

  it('renews the key after a definitive failure or a revision change',
    async () => {
      const { evidenceApi, editor, evidence } = await setup();
      evidenceApi.attach
        .mockResolvedValueOnce(evidenceFailed('not-confirmed'))
        .mockResolvedValueOnce(evidenceFailed('upstream'))
        .mockResolvedValueOnce(evidenceFailed('upstream'));
      const body = { kind: 'repository', candidateId: 'c'.repeat(22) } as const;

      await evidence.attach(body);
      await evidence.attach(body);
      editor.adoptEvidence(three(), '4');
      await evidence.attach(body);

      expect(evidenceApi.attach.mock.calls.map((call) => [call[1], call[3]]))
        .toEqual([['3', 'e1'], ['3', 'e2'], ['4', 'e3']]);
    });

  it('sends one write at a time', async () => {
    const { evidenceApi, evidence } = await setup();
    const answer = held<Awaited<ReturnType<typeof evidenceApi.recheck>>>();
    evidenceApi.recheck.mockReturnValue(answer.promise);

    const running = evidence.recheck(E1);
    await flushPromises();
    await evidence.remove(E2);
    await evidence.move(E2, -1);
    expect(await evidence.attach({
      kind: 'pullRequest', link: 'https://github.com/a/b/pull/1',
    })).toEqual({ kind: 'refused' });
    answer.answer(written(three(), '4'));
    await running;

    expect(evidenceApi.remove).not.toHaveBeenCalled();
    expect(evidenceApi.reorder).not.toHaveBeenCalled();
    expect(evidenceApi.attach).not.toHaveBeenCalled();
  });

  it.each([
    ['the draft changed', (context: Awaited<ReturnType<typeof setup>>) => {
      context.editor.draft.value!.text.en!.name = 'Typed';
    }],
    ['a field draft is held', (context: Awaited<ReturnType<typeof setup>>) => {
      context.drafts.value = 1;
    }],
    ['a save runs', (context: Awaited<ReturnType<typeof setup>>) => {
      context.api.replace.mockReturnValue(new Promise(() => {}));
      void context.editor.save();
    }],
    ['the project changed elsewhere',
      (context: Awaited<ReturnType<typeof setup>>) => {
        context.editor.markStale(project({ revision: '9' }));
      }],
  ])('refuses every write while %s', async (_name, arrange) => {
    const context = await setup();
    arrange(context);
    await flushPromises();

    expect(context.evidence.locked.value).toBe(true);
    expect(await context.evidence.attach({
      kind: 'repository', link: 'https://github.com/a/b',
    })).toEqual({ kind: 'refused' });
    await context.evidence.move(E2, 1);
    await context.evidence.recheck(E1);
    await context.evidence.remove(E1);

    const { evidenceApi } = context;
    expect(evidenceApi.attach).not.toHaveBeenCalled();
    expect(evidenceApi.reorder).not.toHaveBeenCalled();
    expect(evidenceApi.recheck).not.toHaveBeenCalled();
    expect(evidenceApi.remove).not.toHaveBeenCalled();
  });

  it('moves an item up or down by one and never past the ends', async () => {
    const { evidenceApi, evidence } = await setup();
    evidenceApi.reorder
      .mockResolvedValueOnce(written(
        [three()[1]!, three()[0]!, three()[2]!], '4',
      ))
      .mockResolvedValueOnce(written(
        [three()[1]!, three()[2]!, three()[0]!], '5',
      ));

    await evidence.move(E2, -1);
    await evidence.move(E1, 1);
    await evidence.move(E2, -1);
    await evidence.move(E1, 1);

    expect(evidenceApi.reorder.mock.calls.map((call) => [call[1], call[2]]))
      .toEqual([['3', [E2, E1, E3]], ['4', [E2, E3, E1]]]);
    expect(evidence.items.value.map((item) => item.id)).toEqual([E2, E3, E1]);
  });

  it('shows a check again result and changes nothing on a transient failure',
    async () => {
      const { evidenceApi, editor, evidence } = await setup();
      const before = editor.loaded.value;
      evidenceApi.recheck
        .mockResolvedValueOnce(evidenceFailed('upstream'))
        .mockResolvedValueOnce(evidenceFailed('rate-limited', 120))
        .mockResolvedValueOnce(evidenceFailed('rate-limited'));

      await evidence.recheck(E1);
      expect(evidence.status.value).toEqual({ kind: 'upstream' });
      await evidence.recheck(E1);
      expect(evidence.status.value)
        .toEqual({ kind: 'rate-limited', until: NOW + 120_000 });
      await evidence.recheck(E1);
      expect(evidence.status.value)
        .toEqual({ kind: 'rate-limited', until: null });

      expect(editor.loaded.value).toBe(before);
      expect(editor.notice.value).toBeNull();
    });

  it('reads the project again on evidence_not_found and adopts an unchanged '
    + 'document', async () => {
    const { api, evidenceApi, editor, evidence } = await setup();
    evidenceApi.remove.mockResolvedValue(evidenceFailed('evidence-not-found'));
    api.read.mockResolvedValue(ok(project({
      revision: '6', evidence: [three()[1]!],
    })));

    await evidence.remove(E1);

    expect(api.read).toHaveBeenCalledWith('project-1');
    expect(evidence.items.value.map((item) => item.id)).toEqual([E2]);
    expect(editor.loaded.value?.revision).toBe('6');
    expect(editor.notice.value).toBeNull();
    expect(evidence.status.value).toEqual({ kind: 'not-found' });
  });

  it('reads the project again on an order the server refuses', async () => {
    const { api, evidenceApi, evidence } = await setup();
    evidenceApi.reorder.mockResolvedValue(evidenceFailed('invalid'));
    api.read.mockResolvedValue(ok(project({
      revision: '6', evidence: [...three(), evidenceItem({ id: 'added' })],
    })));

    await evidence.move(E1, 1);

    expect(evidence.items.value).toHaveLength(4);
    expect(evidence.status.value).toEqual({ kind: 'try-again' });
  });

  it('marks the editor stale when the re-read document changed', async () => {
    const { api, evidenceApi, editor, evidence } = await setup();
    evidenceApi.attach.mockResolvedValue(evidenceFailed('duplicate'));
    const winner = project({
      revision: '7', text: { en: { name: 'Elsewhere' } }, evidence: three(),
    });
    api.read.mockResolvedValue(ok(winner));

    expect(await evidence.attach({
      kind: 'repository', link: 'https://github.com/a/b',
    })).toEqual({ kind: 'closed' });

    expect(editor.notice.value).toBe('changed-elsewhere');
    expect(editor.loaded.value?.revision).toBe('3');
    await editor.reload();
    expect(editor.loaded.value).toEqual(winner);
  });

  it('answers a duplicate after adopting an unchanged re-read', async () => {
    const { api, evidenceApi, evidence } = await setup();
    evidenceApi.attach.mockResolvedValue(evidenceFailed('duplicate'));
    api.read.mockResolvedValue(ok(project({
      revision: '4', evidence: three(),
    })));

    expect(await evidence.attach({
      kind: 'repository', link: 'https://github.com/a/b',
    })).toEqual({ kind: 'failed', notice: { kind: 'duplicate' } });
  });

  it('marks the editor stale on a revision conflict with no retry',
    async () => {
      const { evidenceApi, editor, evidence } = await setup();
      const winner = project({ revision: '9', evidence: [three()[0]!] });
      evidenceApi.remove.mockResolvedValue(stale(winner));

      await evidence.remove(E2);

      expect(evidenceApi.remove).toHaveBeenCalledTimes(1);
      expect(editor.notice.value).toBe('changed-elsewhere');
      expect(evidence.locked.value).toBe(true);
      await editor.reload();
      expect(evidence.items.value).toEqual([three()[0]]);
      expect(evidence.locked.value).toBe(false);
    });

  it('turns the editor not-found when the project is gone', async () => {
    const { evidenceApi, editor, evidence } = await setup();
    evidenceApi.recheck.mockResolvedValue(evidenceFailed('project-not-found'));

    await evidence.recheck(E1);

    expect(editor.view.value).toBe('not-found');
  });

  it('shows the editor session notice when the session is lost', async () => {
    const { evidenceApi, editor, evidence, csrfToken } = await setup();
    csrfToken.value = null;

    await evidence.remove(E1);

    expect(evidenceApi.remove).not.toHaveBeenCalled();
    expect(editor.notice.value).toBe('session-lost');
  });

  it('opens the dialog session-ended when Check again has no session',
    async () => {
      const { evidenceApi, editor, evidence, onSessionEnded } = await setup();
      const before = editor.loaded.value;
      evidenceApi.recheck.mockResolvedValue(evidenceFailed('session-required'));

      await evidence.recheck(E1);

      expect(onSessionEnded).toHaveBeenCalledTimes(1);
      expect(editor.loaded.value).toBe(before);
      expect(evidence.status.value).toBeNull();
    });

  it('reports each attach outcome', async () => {
    const { evidenceApi, evidence } = await setup(project({ evidence: [] }));
    const link = { kind: 'repository', link: 'https://github.com/a/b' } as const;
    evidenceApi.attach
      .mockResolvedValueOnce(evidenceFailed('session-required'))
      .mockResolvedValueOnce(evidenceFailed('limit'))
      .mockResolvedValueOnce(evidenceFailed('unsupported-link'))
      .mockResolvedValueOnce(written([evidenceItem()], '4'));

    expect(await evidence.attach(link)).toEqual({ kind: 'session-ended' });
    expect(await evidence.attach(link))
      .toEqual({ kind: 'failed', notice: { kind: 'limit' } });
    expect(await evidence.attach(link))
      .toEqual({ kind: 'failed', notice: { kind: 'unsupported-link' } });
    expect(await evidence.attach(link)).toEqual({ kind: 'attached' });
    expect(evidence.items.value).toEqual([evidenceItem()]);
    expect(evidence.status.value).toBeNull();
  });

  it('reaches the limit at ten items', async () => {
    const ten = Array.from({ length: 10 }, (_, index) =>
      evidenceItem({ id: `item-${index}` }));
    const { evidence } = await setup(project({ evidence: ten }));

    expect(evidence.atLimit.value).toBe(true);
  });
});
