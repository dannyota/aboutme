import { readFileSync } from 'node:fs';

import { TEMPLATES } from '@aboutme/schema/templates';
import { createPinia, setActivePinia } from 'pinia';
import { computed } from 'vue';
import { describe, expect, it } from 'vitest';

import { applyTemplate } from '../../app/components/resume/applyTemplate';
// eslint-disable-next-line max-len
import { createResumeEditorActions } from '../../app/composables/useResumeEditor';
import { replayCommand } from '../../app/editor/commands';
import { createMutationCoordinator } from '../../app/editor/coordinator';
import { parseCurrentDocument } from '../../app/editor/documentValidation';
import { parseRevision } from '../../app/editor/revision';
import {
  advanceTemplateGroup,
  captureTemplateGroup,
  nextTemplateChild,
} from '../../app/editor/templateGroup';
import type {
  TemplateChildCommand,
  TemplateGroupCommand,
  TemplateGroupState,
} from '../../app/editor/templateGroup';
import type { AcceptedResume } from '../../app/editor/types';
import { useResumeStore } from '../../app/stores/resumes';
import { acceptedFixture } from './fixture';

const fullDocument = parseCurrentDocument(
  JSON.parse(
    readFileSync('../../packages/schema/fixtures/full.json', 'utf8'),
  ) as unknown,
);

type KeyOrder = 'server' | 'fixture';

function byteOrder(left: string, right: string): number {
  return left < right ? -1 : left > right ? 1 : 0;
}

/**
 * The document as the server encodes it: the Go schema.Resume holds content
 * in a map, which encoding/json writes in byte order of its keys.
 */
function serverEncoded(document: AcceptedResume['document']) {
  const content = Object.fromEntries(
    Object.keys(document.content)
      .sort(byteOrder)
      .map((key) => [key, document.content[key]!]),
  );
  return parseCurrentDocument(
    JSON.parse(JSON.stringify({ ...document, content })),
  );
}

/**
 * The server's 200 response to one template child, following
 * apps/server/internal/resumeapi/customization.go and persist.go: a set
 * creates a missing parent and replaces the leaf, an unset deletes the leaf,
 * and the response re-encodes the whole stored document.
 */
function serverAccept(
  before: AcceptedResume,
  child: TemplateChildCommand,
  revision: number,
): AcceptedResume {
  const document = structuredClone(before.document);
  if (child.kind === 'customization') {
    for (const delta of child.deltas) {
      const segments = delta.path.split('.');
      let parent = document.customization as unknown as Record<
        string,
        unknown
      >;
      for (const segment of segments.slice(0, -1)) {
        parent[segment] ??= {};
        parent = parent[segment] as Record<string, unknown>;
      }
      const leaf = segments.at(-1)!;
      if (delta.op === 'set') parent[leaf] = structuredClone(delta.value);
      else Reflect.deleteProperty(parent, leaf);
    }
  } else {
    document.customization = replayCommand(
      { document, metadata: before.metadata },
      child,
    ).document.customization;
  }
  return {
    ...before,
    document: serverEncoded(document),
    revision: parseRevision(String(revision)),
    metadataFreshness: 'complete',
  };
}

function runtime() {
  let next = 0;
  return {
    nowEpochMs: () => 0,
    uuid: () => `id-${++next}`,
    delay: async () => {},
  };
}

function runGroup(group: TemplateGroupCommand, start: AcceptedResume) {
  let state: TemplateGroupState = { kind: 'queued', nextChild: 0 };
  let accepted = start;
  let revision = 2;
  while (state.kind === 'queued' || state.kind === 'running') {
    const child = nextTemplateChild(group, state);
    if (child === null) break;
    accepted = serverAccept(accepted, child, revision++);
    state = advanceTemplateGroup(group, state, accepted);
  }
  return state;
}

/** An owner-like resume: two columns with a sidebar, a custom date format,
 * a non-default font size, body justify, and a photo position. */
function ownerLike(): AcceptedResume {
  const document = structuredClone(fullDocument);
  document.customization.dateFormat = 'MM/YYYY';
  document.customization.font = {
    ...document.customization.font,
    baseSizePx: 16,
    textAlign: 'justify',
  };
  document.customization.header = {
    ...document.customization.header!,
    photoPosition: 'left',
  };
  return acceptedFixture({ document });
}

/**
 * Every starting resume: three bases, and each base after every preset.
 * With `server`, the editor holds content in the order the server writes it;
 * with `fixture`, it holds the fixture's own order, which differs.
 */
function startingPoints(order: KeyOrder) {
  const bases = [
    {
      name: 'full',
      accepted: acceptedFixture({ document: structuredClone(fullDocument) }),
    },
    { name: 'owner-like', accepted: ownerLike() },
    { name: 'minimal', accepted: acceptedFixture() },
  ];
  const fromPresets = bases.flatMap(({ name, accepted }) =>
    TEMPLATES.map((preset) => ({
      name: `${name} after ${preset.id}`,
      accepted: {
        ...accepted,
        document: {
          ...accepted.document,
          customization: applyTemplate(
            accepted.document.customization,
            preset,
            accepted.document.content,
          ),
        },
      },
    })),
  );
  return [...bases, ...fromPresets].map(({ name, accepted }) => ({
    name,
    accepted: order === 'server'
      ? { ...accepted, document: serverEncoded(accepted.document) }
      : accepted,
  }));
}

describe.each(['server', 'fixture'] as const)(
  'template apply when the editor holds content in %s key order',
  (order) => {
    it('ends complete for every preset from every starting resume', () => {
      const failures: string[] = [];
      for (const start of startingPoints(order)) {
        for (const preset of TEMPLATES) {
          const group = captureTemplateGroup({
            resumeId: start.accepted.metadata.id,
            ownerId: 'owner-1',
            sequence: 1,
            current: start.accepted,
            preset,
            dependencyIds: [],
            runtime: runtime(),
          });
          if (group === null) continue;
          const state = runGroup(group, start.accepted);
          if (state.kind !== 'complete') {
            failures.push(`${start.name} -> ${preset.id}: ${state.kind}`);
          }
        }
      }
      expect(failures).toEqual([]);
    });

    it('ends complete through the editor store and coordinator', async () => {
      const failures: string[] = [];
      for (const start of startingPoints(order)) {
        for (const preset of TEMPLATES) {
          setActivePinia(createPinia());
          const store = useResumeStore();
          store.initialize(start.accepted);
          const id = start.accepted.metadata.id;
          let server = start.accepted;
          let revision = 2;
          const auth = {
            user: computed(() => ({ id: 'owner-1' })),
            csrfToken: computed(() => 'csrf-1'),
            authState: computed(() => 'authenticated'),
          } as never;
          const api = {
            dispatch: async () => {
              const command = store.recordFor(id)!.attempt!.command;
              server = serverAccept(
                server,
                command as TemplateChildCommand,
                revision++,
              );
              return { kind: 'complete', status: 200, accepted: server };
            },
            read: async () => ({ kind: 'complete', accepted: server }),
          } as never;
          const shared = runtime();
          const coordinator = createMutationCoordinator({
            api,
            store,
            auth,
            runtime: shared,
          });
          const actions = createResumeEditorActions({
            resumeId: id,
            store,
            coordinator,
            auth,
            runtime: shared,
          });
          if (actions.applyTemplate(preset).kind !== 'enqueued') continue;
          await coordinator.flush(id);
          const state = store.recordFor(id)!.templateState;
          if (state?.kind !== 'complete') {
            failures.push(`${start.name} -> ${preset.id}: ${state?.kind}`);
          }
        }
      }
      expect(failures).toEqual([]);
    });
  },
);
