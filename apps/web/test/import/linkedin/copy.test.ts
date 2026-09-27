// noticeCut formats its character count per locale, not as a raw digit
// string (docs/design/linkedin-import-ui.md, "Messages").
import { describe, expect, it } from 'vitest';

import { importCopy } from '../../../app/i18n/import';

describe('importCopy noticeCut', () => {
  it('groups thousands the English way', () => {
    expect(importCopy.en.noticeCut('Description', 16384))
      .toBe('Description was shortened to 16,384 characters.');
  });

  it('groups thousands the Vietnamese way', () => {
    expect(importCopy.vi.noticeCut('Mô tả', 16384))
      .toBe('Mô tả đã được rút gọn còn 16.384 ký tự.');
  });
});
