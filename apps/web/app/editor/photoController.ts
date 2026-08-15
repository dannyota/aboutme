import type { ResumeApi } from './resumeApi';
import type { AcceptedResume } from './types';
import type { PhotoReadState, useResumeStore } from '../stores/resumes';

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
    if (retained === undefined) {
      deps.store.setPhotoRead(resumeId, {
        kind: 'loading',
        binding,
        generation,
      });
    }
    const result = await deps.api.readOwnerPhoto(resumeId, retained?.etag);
    if (!isCurrentSync(resumeId, binding, generation, retained)) return;
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
        deps.store.setPhotoRead(resumeId, {
          kind: 'suspended',
          binding,
          generation,
          reason: 'read-failed',
        });
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
      deps.store.setPhotoRead(resumeId, {
        kind: 'suspended',
        binding,
        generation,
        reason:
          result.reason === 'session-lost' ? 'session-lost' : 'read-failed',
      });
      return;
    }

    let dataUrl: string;
    try {
      dataUrl = await deps.codec.toDataURL(result.bytes, result.mime);
    } catch {
      if (isCurrentSync(resumeId, binding, generation, retained)) {
        deps.store.setPhotoRead(resumeId, {
          kind: 'suspended',
          binding,
          generation,
          reason: 'read-failed',
        });
      }
      return;
    }
    if (!isCurrentSync(resumeId, binding, generation, retained)) return;
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

  const isCurrentSync = (
    resumeId: string,
    binding: string,
    generation: number,
    retained: Extract<PhotoReadState, { kind: 'ready' }> | undefined,
  ): boolean => {
    if (generation !== activeGeneration || activeResumeId !== resumeId) {
      return false;
    }
    const current = deps.store.recordFor(resumeId)?.photoRead;
    if (retained === undefined) {
      return current?.kind === 'loading'
        && current.binding === binding
        && current.generation === generation;
    }
    return current?.kind === 'ready'
      && current.binding === retained.binding
      && current.generation === retained.generation
      && current.etag === retained.etag
      && current.dataUrl === retained.dataUrl;
  };

  return { sync, clear };
}

function acceptedPhotoKey(
  store: ReturnType<typeof useResumeStore>,
  resumeId: string,
): string | undefined {
  return store.recordFor(resumeId)?.accepted.document.personalDetails.photo
    ?.key;
}
