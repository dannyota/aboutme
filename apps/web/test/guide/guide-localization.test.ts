import { describe, expect, it } from 'vitest';
import { guideCopy } from '../../app/i18n/guide';
import { literalGuard, parityViolations } from '../support/localizationSource';

const { sourceViolations } = literalGuard({});

const guideSources = [
  'pages/guide/mcp.vue',
  'components/guide/GuideCanCannot.vue',
  'components/guide/GuideClaudeSteps.vue',
  'components/guide/GuideCopyBlock.vue',
  'components/guide/GuideRichText.vue',
  'components/guide/GuideTroubleshooting.vue',
];

describe('guide copy parity', () => {
  it('keeps guideCopy typed, complete, and nonempty', () => {
    expect(Object.keys(guideCopy).sort()).toEqual(['en', 'vi']);
    expect(parityViolations(guideCopy.vi, guideCopy.en, 'guideCopy'))
      .toEqual([]);
  });
});

describe('guide source literal guard', () => {
  it('has no display literal outside the guide copy catalog', () => {
    const violations = guideSources.flatMap((path) => sourceViolations(path));
    expect(violations).toEqual([]);
  });
});
