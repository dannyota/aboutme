// @vitest-environment node

import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';

import { describe, expect, it } from 'vitest';

import { CARD_LAYOUT_VERSION } from '../../app/components/preview/cardLayout';

// Every live card's URL carries the layout version, and platforms refetch an
// image only when its URL changes (docs/adr/0055-stored-link-preview-card.md).
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
  1: '7d0a2cfbd4151109e4a3f7c8d92f4e5171ade6de9d8405b2c3930f8116e1c42f',
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
