import { createPinia, setActivePinia } from 'pinia';
import { toRaw } from 'vue';
import { describe, expect, it, vi } from 'vitest';

import type { ObjectETag } from '../../app/editor/attempt';
import {
  createPhotoController,
  type PhotoDataCodec,
} from '../../app/editor/photoController';
import type {
  OwnerPhotoReadResult,
  ResumeApi,
} from '../../app/editor/resumeApi';
import { parseRevision } from '../../app/editor/revision';
import type { AcceptedResume } from '../../app/editor/types';
import { useResumeStore } from '../../app/stores/resumes';
import { acceptedFixture } from './fixture';

const withPhoto = (value: AcceptedResume, key: string): AcceptedResume => ({
  ...value,
  document: {
    ...value.document,
    personalDetails: { ...value.document.personalDetails, photo: { key } },
  },
});

const bytes = (value: string): OwnerPhotoReadResult => ({
  kind: 'bytes',
  mime: 'image/png',
  etag: '"photo-1"' as ObjectETag,
  bytes: new TextEncoder().encode(value),
});

const deferred = <T>() => {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
};

describe('private photo controller', () => {
  it('clears retained data without reading when accepted metadata has no photo',
    async () => {
      const { accepted, api, controller, store } = controllerFor();
      const withReady = withPhoto(accepted, 'photo-a');
      store.initialize(withReady);
      store.setPhotoRead(withReady.metadata.id, ready('photo-a'));

      await controller.sync(accepted);

      expect(api.readOwnerPhoto).not.toHaveBeenCalled();
      expect(store.recordFor(accepted.metadata.id)!.photoRead).toEqual({
        kind: 'none',
      });
    },
  );

  it('retains the exact authorized data URL on a conditional 304', async () => {
    const result = deferred<OwnerPhotoReadResult>();
    const { accepted, api, controller, store } = controllerFor(result.promise);
    const current = withPhoto(accepted, 'photo-a');
    store.initialize(current);
    const retained = ready('photo-a');
    store.setPhotoRead(current.metadata.id, retained);

    const syncing = controller.sync(current);

    expect(store.recordFor(current.metadata.id)!.photoRead).toEqual(retained);
    result.resolve({
      kind: 'not-modified', etag: '"photo-a"' as ObjectETag,
    });
    await syncing;

    expect(api.readOwnerPhoto).toHaveBeenCalledWith(
      current.metadata.id,
      '"photo-a"',
    );
    expect(store.recordFor(current.metadata.id)!.photoRead).toMatchObject({
      kind: 'ready',
      binding: 'photo-a',
      dataUrl: 'data:image/png;base64,ready',
    });
  });

  it.each([
    ['invalid', 'read-failed'],
    ['network', 'read-failed'],
    ['session-lost', 'session-lost'],
  ] as const)('suspends an unavailable %s owner read', async (reason, want) => {
    const { accepted, controller, store } = controllerFor({
      kind: 'unavailable', reason,
    });
    const current = withPhoto(accepted, 'photo-a');
    store.initialize(current);

    await controller.sync(current);

    expect(store.recordFor(current.metadata.id)!.photoRead).toMatchObject({
      kind: 'suspended', binding: 'photo-a', reason: want,
    });
  });

  it('suspends a read when the accepted photo binding changes', async () => {
    const gate = deferred<OwnerPhotoReadResult>();
    const { accepted, controller, store } = controllerFor(gate.promise);
    const old = withPhoto(accepted, 'photo-a');
    store.initialize(old);

    const syncing = controller.sync(old);
    const replacement = withPhoto(
      { ...accepted, revision: parseRevision('2') },
      'photo-b',
    );
    store.adoptComplete(replacement.metadata.id, replacement);
    gate.resolve(bytes('a'));
    await syncing;

    expect(store.recordFor(old.metadata.id)!.photoRead).toMatchObject({
      kind: 'suspended', binding: 'photo-a', reason: 'binding-mismatch',
    });
  });

  it('clears data on delete and prevents a late read from restoring it',
    async () => {
      const gate = deferred<OwnerPhotoReadResult>();
      const { accepted, controller, store } = controllerFor(gate.promise);
      const current = withPhoto(accepted, 'photo-a');
      store.initialize(current);

      const syncing = controller.sync(current);
      await controller.sync(accepted);
      const before = structuredClone(
        toRaw(store.recordFor(accepted.metadata.id)!.photoRead),
      );
      gate.resolve(bytes('late'));
      await syncing;

      expect(store.recordFor(accepted.metadata.id)!.photoRead).toEqual(before);
    },
  );

  it('does not mutate byte-for-byte after a stale generation resolves',
    async () => {
      const first = deferred<OwnerPhotoReadResult>();
      const { accepted, api, controller, store } = controllerFor();
      api.readOwnerPhoto
        .mockReturnValueOnce(first.promise)
        .mockResolvedValueOnce(bytes('new'));
      const oldAccepted = withPhoto(accepted, 'old');
      const newAccepted = withPhoto(
        { ...accepted, revision: parseRevision('2') },
        'new',
      );
      store.initialize(oldAccepted);

      const oldSync = controller.sync(oldAccepted);
      store.adoptComplete(newAccepted.metadata.id, newAccepted);
      await controller.sync(newAccepted);
      expect(store.recordFor(accepted.metadata.id)!.photoRead).toMatchObject({
        kind: 'ready',
        binding: 'new',
        dataUrl: 'data:image/png;base64,3',
      });
      const afterNew = structuredClone(
        toRaw(store.recordFor(accepted.metadata.id)!.photoRead),
      );
      first.resolve(bytes('old'));
      await oldSync;

      expect(
        store.recordFor(accepted.metadata.id)!.photoRead,
      ).toEqual(afterNew);
    },
  );

  it('clears retained state and invalidates a late generation', async () => {
    const gate = deferred<OwnerPhotoReadResult>();
    const { accepted, controller, store } = controllerFor(gate.promise);
    const current = withPhoto(accepted, 'photo-a');
    store.initialize(current);

    const syncing = controller.sync(current);
    controller.clear();
    const before = structuredClone(
      toRaw(store.recordFor(current.metadata.id)!.photoRead),
    );
    gate.resolve(bytes('late'));
    await syncing;

    expect(store.recordFor(current.metadata.id)!.photoRead).toEqual(before);
  });
});

function controllerFor(
  result: OwnerPhotoReadResult | Promise<OwnerPhotoReadResult> = bytes('ok'),
) {
  setActivePinia(createPinia());
  const api = {
    readOwnerPhoto: vi.fn().mockResolvedValue(result),
  } as unknown as {
    readOwnerPhoto: ReturnType<typeof vi.fn>;
  };
  const store = useResumeStore();
  const codec: PhotoDataCodec = {
    toDataURL: async (value, mime) => `data:${mime};base64,${value.length}`,
  };
  return {
    accepted: acceptedFixture(),
    api,
    controller: createPhotoController({
      api: api as unknown as ResumeApi,
      store,
      codec,
    }),
    store,
  };
}

function ready(binding: string) {
  return {
    kind: 'ready' as const,
    binding,
    generation: 1,
    etag: '"photo-a"' as ObjectETag,
    dataUrl: 'data:image/png;base64,ready',
  };
}
