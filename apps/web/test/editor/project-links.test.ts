import { describe, expect, it } from 'vitest';

import { revealsAcrossResumes } from '../../app/editor/projectLinks';

const resumes = [
  { id: 'public-1', live: true },
  { id: 'public-2', live: true },
  { id: 'draft', live: false },
];

describe('a project shown on two published resumes', () => {
  it('is revealed by linking a second published resume', () => {
    expect(revealsAcrossResumes('public-2', ['public-1'], resumes)).toBe(true);
  });

  it('is not revealed by linking a draft', () => {
    expect(revealsAcrossResumes('draft', ['public-1'], resumes)).toBe(false);
  });

  it('is not revealed by the first published resume', () => {
    expect(revealsAcrossResumes('public-2', [], resumes)).toBe(false);
    expect(revealsAcrossResumes('public-2', ['draft'], resumes)).toBe(false);
    expect(revealsAcrossResumes('public-2', ['public-2'], resumes))
      .toBe(false);
  });

  it('is assumed when a reread fails, except for a draft', () => {
    expect(revealsAcrossResumes('public-2', [], null)).toBe(true);
    expect(revealsAcrossResumes('public-2', null, resumes)).toBe(true);
    expect(revealsAcrossResumes('draft', null, resumes)).toBe(false);
  });
});
