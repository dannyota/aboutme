// @vitest-environment node

import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';

import { describe, expect, it } from 'vitest';

import { CARD_LAYOUT_VERSION } from '../../app/components/preview/cardLayout';

// Every live card's URL carries the layout version, and platforms refetch an
// image only when its URL changes
// (docs/adr/0014-public-page-head-and-link-preview.md).
// A change to any file that draws the card must raise CARD_LAYOUT_VERSION
// here and LayoutVersion in apps/server/internal/previewcard/card.go, then
// pin the new hash.
const LAYOUT_FILES = [
  'app/components/preview/PreviewCard.vue',
  'app/components/preview/preview-card.css',
  'app/components/preview/cardFit.ts',
  'app/components/app/AppLogo.vue',
];

const PINNED: Record<number, string> = {
  1: 'f2c881e2aa99b538f000099b19845cc00badae743c96cffb69d4ee736cfb2991',
  // ADR 0020: the footer mark became the seal.
  // Re-pinned without a version bump: only an ADR citation in a comment
  // changed, so the rendered card is identical.
  2: 'a28b2c244fe4c56a049ab5013ad1016cd4c2755b575502d31fefddb2d310e3a4',
};

describe('card layout pin', () => {
  it('pins the card markup and CSS to the layout version', () => {
    const hash = createHash('sha256');
    for (const file of LAYOUT_FILES) {
      hash.update(file).update('\0').update(readFileSync(file)).update('\0');
    }
    expect(hash.digest('hex')).toBe(PINNED[CARD_LAYOUT_VERSION]);
  });
});
