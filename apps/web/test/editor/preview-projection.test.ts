import { describe, expect, it } from 'vitest';

import {
  photoStateFor,
  previewProjection,
} from '../../app/components/editor/previewProjection';
import { acceptedFixture } from './fixture';

describe('previewProjection', () => {
  it('drops photo metadata when no authorized URL exists', () => {
    const { document } = acceptedFixture();
    document.personalDetails.photo = { key: 'resumes/r/photo-x.jpg' };
    const projected = previewProjection(document, undefined);
    expect(projected.personalDetails.photo).toBeUndefined();
    expect(projected.personalDetails.fullName).toBe(
      document.personalDetails.fullName,
    );
    expect(document.personalDetails.photo).toBeDefined();
  });

  it('keeps the document identical when a URL exists', () => {
    const { document } = acceptedFixture();
    document.personalDetails.photo = { key: 'resumes/r/photo-x.jpg' };
    expect(previewProjection(document, 'data:image/png;base64,AA==')).toBe(
      document,
    );
  });
});

describe('photoStateFor', () => {
  it.each([
    [undefined, false, 'none'],
    [{ kind: 'none' }, true, 'unavailable'],
    [{ kind: 'loading', binding: 'k', generation: 1 }, true, 'loading'],
    [
      {
        kind: 'suspended',
        binding: 'k',
        generation: 1,
        reason: 'read-failed',
      },
      true,
      'unavailable',
    ],
    [
      {
        kind: 'ready',
        binding: 'k',
        generation: 1,
        etag: '"e"',
        dataUrl: 'd',
      },
      true,
      'ready',
    ],
  ] as const)('maps %o with photo=%s to %s', (read, hasPhoto, expected) => {
    expect(photoStateFor(read, hasPhoto)).toBe(expected);
  });
});
