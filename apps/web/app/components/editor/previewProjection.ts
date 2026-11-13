import type { Resume } from '@aboutme/schema';

import type { PhotoReadState } from '../../stores/resumes';

export type PhotoState = 'ready' | 'loading' | 'unavailable' | 'none';

export function previewProjection(
  document: Resume,
  photoUrl: string | undefined,
): Resume {
  if (
    document.personalDetails.photo === undefined
    || photoUrl !== undefined
  ) {
    return document;
  }
  const { photo: _photo, ...personalDetails } = document.personalDetails;
  return { ...document, personalDetails };
}

export function photoStateFor(
  read: PhotoReadState | undefined,
  hasPhoto: boolean,
): PhotoState {
  if (!hasPhoto) return 'none';
  switch (read?.kind) {
    case 'ready':
      return 'ready';
    case 'loading':
      return 'loading';
    default:
      return 'unavailable';
  }
}
