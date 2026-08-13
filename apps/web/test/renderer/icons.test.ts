import { describe, expect, it } from 'vitest';

import { iconFor } from '../../app/components/resume/icons';

describe('renderer icon registry', () => {
  it('returns components only for known stable keys', () => {
    expect(iconFor('briefcase')).toBeTruthy();
    expect(iconFor('graduation-cap')).toBeTruthy();
    expect(iconFor('not-in-the-registry')).toBeNull();
  });
});
