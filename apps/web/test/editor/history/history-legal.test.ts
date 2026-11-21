import { describe, expect, it } from 'vitest';

import { legalCopy } from '../../../app/i18n/legal';

function sections(locale: 'en' | 'vi', page: 'privacy' | 'terms') {
  return legalCopy[locale][page].sections.flatMap(
    (section: { paragraphs?: readonly string[] }) => section.paragraphs ?? [],
  );
}

describe('history legal copy', () => {
  it('sets both update dates to the release date', () => {
    expect(legalCopy.en.privacyUpdated).toBe('Last updated November 22, 2026');
    expect(legalCopy.en.termsUpdated).toBe('Last updated November 22, 2026');
    const vi = 'Cập nhật lần cuối ngày 22/11/2026';
    expect(legalCopy.vi.privacyUpdated).toBe(vi);
    expect(legalCopy.vi.termsUpdated).toBe(vi);
  });

  it('states the 30 day history in the privacy notice', () => {
    expect(sections('en', 'privacy').join('\n')).toContain(
      'We keep your account, resumes, and photos until you delete them or '
      + 'your account. Each resume also keeps earlier versions of its text, '
      + 'contact details, layout, title, and language for up to 30 days, so '
      + 'you can restore them. Text you remove from a resume stays in those '
      + 'versions until they are 30 days old or you delete the resume\'s '
      + 'history in the editor. Deleting a resume or your account deletes '
      + 'its history. The export includes your profile, your resumes with '
      + 'their history, and resume photos.',
    );
    expect(sections('vi', 'privacy').join('\n')).toContain(
      'Chúng tôi giữ tài khoản, CV và ảnh của bạn cho đến khi bạn xóa chúng '
      + 'hoặc xóa tài khoản. Mỗi CV còn giữ các phiên bản trước của nội '
      + 'dung, thông tin liên hệ, bố cục, tiêu đề và ngôn ngữ trong tối đa '
      + '30 ngày để bạn có thể khôi phục. Nội dung bạn xóa khỏi CV vẫn nằm '
      + 'trong các phiên bản đó cho đến khi chúng đủ 30 ngày hoặc bạn xóa '
      + 'lịch sử của CV trong trình chỉnh sửa. Xóa CV hoặc tài khoản sẽ xóa '
      + 'lịch sử của CV. Bản xuất gồm hồ sơ, các CV cùng lịch sử của chúng, '
      + 'và ảnh trong CV của bạn.',
    );
  });

  it('keeps history out of the content permission end', () => {
    expect(sections('en', 'terms').join('\n')).toContain(
      'This permission ends when you delete the content, except for earlier '
      + 'versions in a resume\'s history until they are 30 days old or you '
      + 'delete that history, and backup copies until they expire.',
    );
    expect(sections('vi', 'terms').join('\n')).toContain(
      'Quyền này chấm dứt khi bạn xóa nội dung, trừ các phiên bản trước '
      + 'trong lịch sử CV cho đến khi chúng đủ 30 ngày hoặc bạn xóa lịch sử '
      + 'đó, và các bản sao lưu cho đến khi chúng hết hạn.',
    );
  });
});
