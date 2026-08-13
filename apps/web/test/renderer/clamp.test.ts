import { describe, expect, it } from 'vitest';

import {
  clampAgainst,
  contrastRatio,
  deriveLevelColors,
} from '../../app/components/resume/clampContrast';

describe('contrast derivation', () => {
  it('returns an already passing input unchanged', () => {
    expect(clampAgainst('#111111', ['#ffffff'], 4.5)).toBe('#111111');
  });

  it('chooses the passing direction for a mid-tone surface', () => {
    const result = clampAgainst('#b7b7b7', ['#b7b7b7'], 4.5);
    expect(result).not.toBeNull();
    expect(contrastRatio(result!, '#b7b7b7')).toBeGreaterThanOrEqual(4.5);
    expect(contrastRatio('#000000', '#b7b7b7')).toBeGreaterThan(
      contrastRatio('#ffffff', '#b7b7b7'),
    );
  });

  it('returns null when neither endpoint can pass every surface', () => {
    expect(clampAgainst('#777777', ['#000000', '#ffffff'], 4.5)).toBeNull();
  });

  it('makes a level fill pass its surface and actual track', () => {
    const result = deriveLevelColors('#959595', '#ffffff');
    expect(contrastRatio(result.solid, '#ffffff')).toBeGreaterThanOrEqual(3);
    expect(contrastRatio(result.solid, result.track)).toBeGreaterThanOrEqual(3);
  });
});
