// @vitest-environment node

import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';

import { describe, expect, it } from 'vitest';

import { CARD_LAYOUT_VERSION } from '../../app/components/preview/cardLayout';

const LAYOUT_FILES = [
  'app/components/preview/PreviewCard.vue',
  'app/components/preview/preview-card.css',
  'app/components/preview/cardFit.ts',
  'app/components/app/AppLogo.vue',
  'app/components/app/sealMark.ts',
];

const PINNED: Record<number, string> = {
  1: 'f2c881e2aa99b538f000099b19845cc00badae743c96cffb69d4ee736cfb2991',
  2: 'c514fd4f26c163997bdbaf2c33f2c01a1c03254caca69621ee77fe07b26a0890',
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
