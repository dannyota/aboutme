import type { ResumeApi } from './resumeApi';
import type { AcceptedResume } from './types';
import type { useResumeStore } from '../stores/resumes';

export interface PhotoController {
  sync(accepted: AcceptedResume): Promise<void>;
  clear(): void;
}

export interface PhotoDataCodec {
  toDataURL(
    bytes: Uint8Array,
    mime: 'image/jpeg' | 'image/png',
  ): Promise<string>;
}

export function createPhotoController(deps: {
  api: ResumeApi;
  store: ReturnType<typeof useResumeStore>;
  codec: PhotoDataCodec;
}): PhotoController {
  let activeGeneration = 0;
  let activeResumeId: string | undefined;

  const clear = (): void => {
    activeGeneration += 1;
    if (activeResumeId !== undefined) {
      deps.store.setPhotoRead(activeResumeId, { kind: 'none' });
    }
    activeResumeId = undefined;
  };

  const sync = async (accepted: AcceptedResume): Promise<void> => {
    const resumeId = accepted.metadata.id;
    if (activeResumeId !== undefined && activeResumeId !== resumeId) {
      deps.store.setPhotoRead(activeResumeId, { kind: 'none' });
    }
    activeResumeId = resumeId;
    const binding = accepted.document.personalDetails.photo?.key;
    const generation = ++activeGeneration;
    if (binding === undefined) {
      deps.store.setPhotoRead(resumeId, { kind: 'none' });
      return;
    }

    const prior = deps.store.recordFor(resumeId)?.photoRead;
    const retained = prior?.kind === 'ready' && prior.binding === binding
      ? prior
      : undefined;
    deps.store.setPhotoRead(resumeId, { kind: 'loading', binding, generation });
    const result = await deps.api.readOwnerPhoto(resumeId, retained?.etag);
    if (!isCurrentLoading(deps.store, resumeId, binding, generation)) return;
    if (acceptedPhotoKey(deps.store, resumeId) !== binding) {
      deps.store.setPhotoRead(resumeId, {
        kind: 'suspended',
        binding,
        generation,
        reason: 'binding-mismatch',
      });
      return;
    }

    if (result.kind === 'not-modified') {
      if (retained === undefined || retained.etag !== result.etag) {
        suspend(deps.store, resumeId, binding, generation, 'read-failed');
        return;
      }
      deps.store.setPhotoRead(resumeId, {
        kind: 'ready',
        binding,
        generation,
        etag: retained.etag,
        dataUrl: retained.dataUrl,
      });
      return;
    }
    if (result.kind === 'unavailable') {
      suspend(
        deps.store,
        resumeId,
        binding,
        generation,
        result.reason === 'session-lost' ? 'session-lost' : 'read-failed',
      );
      return;
    }

    let dataUrl: string;
    try {
      dataUrl = await deps.codec.toDataURL(result.bytes, result.mime);
    } catch {
      suspend(deps.store, resumeId, binding, generation, 'read-failed');
      return;
    }
    if (!isCurrentLoading(deps.store, resumeId, binding, generation)) return;
    if (acceptedPhotoKey(deps.store, resumeId) !== binding) {
      deps.store.setPhotoRead(resumeId, {
        kind: 'suspended',
        binding,
        generation,
        reason: 'binding-mismatch',
      });
      return;
    }
    deps.store.setPhotoRead(resumeId, {
      kind: 'ready',
      binding,
      generation,
      etag: result.etag,
      dataUrl,
    });
  };

  return { sync, clear };
}

function isCurrentLoading(
  store: ReturnType<typeof useResumeStore>,
  resumeId: string,
  binding: string,
  generation: number,
): boolean {
  const photoRead = store.recordFor(resumeId)?.photoRead;
  return (
    photoRead?.kind === 'loading'
    && photoRead.binding === binding
    && photoRead.generation === generation
  );
}

function acceptedPhotoKey(
  store: ReturnType<typeof useResumeStore>,
  resumeId: string,
): string | undefined {
  return store.recordFor(resumeId)?.accepted.document.personalDetails.photo
    ?.key;
}

function suspend(
  store: ReturnType<typeof useResumeStore>,
  resumeId: string,
  binding: string,
  generation: number,
  reason: 'read-failed' | 'session-lost',
): void {
  if (!isCurrentLoading(store, resumeId, binding, generation)) return;
  store.setPhotoRead(resumeId, {
    kind: 'suspended',
    binding,
    generation,
    reason,
  });
}
