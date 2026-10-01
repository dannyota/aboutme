import { describe, expect, it } from 'vitest';

import { type LegalSection, legalCopy } from '../../app/i18n/legal';

// The Privacy Policy and Terms carry exactly the approved showcase text
// (docs/design/showcase.md, Privacy Policy and Terms of Service; AC-SHOW-011).

function section(
  document: 'privacy' | 'terms',
  locale: 'vi' | 'en',
  heading: string,
): LegalSection {
  const found = legalCopy[locale][document].sections
    .find((candidate) => candidate.heading === heading);
  if (found === undefined) throw new Error(`no section ${heading}`);
  return found;
}

const text = {
  vi: {
    collect:
      'Trang Cộng đồng: nếu bạn bật tùy chọn này cho một CV, chúng tôi lưu '
      + 'thời điểm bạn bật và vị trí bạn chọn.',
    features:
      'Các tính năng tùy chọn (xuất bản CV công khai, cho phép lập chỉ mục, '
      + 'hiện CV trong trang Cộng đồng, kết nối trợ lý AI, đăng nhập bằng '
      + 'Google hoặc LinkedIn) chỉ chạy khi bạn tự bật',
    public:
      'Trang Cộng đồng (aboutme.vn/showcase) chỉ hiện những CV mà chủ CV '
      + 'bật Hiện trong trang Cộng đồng. Trang này hiện ảnh xem trước của CV '
      + '(họ tên, tiêu đề và ảnh của bạn), mẫu, ngôn ngữ và vị trí bạn chọn, '
      + 'kèm đường dẫn đến CV. Khi bạn tắt tùy chọn này, hủy xuất bản, hoặc '
      + 'bật Yêu cầu đăng nhập để xem, CV rời khỏi trang Cộng đồng ngay lập '
      + 'tức. Trang Cộng đồng không cho công cụ tìm kiếm lập chỉ mục, nhưng bất kỳ ai '
      + 'truy cập đều có thể xem và sao chép những gì trang hiển thị.',
    content:
      'Nếu bạn bật Hiện trong trang Cộng đồng cho một CV, bạn cũng cho phép '
      + 'aboutme.vn hiện ảnh xem trước, mẫu, ngôn ngữ và vị trí bạn chọn của '
      + 'CV đó trên trang Cộng đồng, cho đến khi bạn tắt tùy chọn này hoặc '
      + 'hủy xuất bản.',
  },
  en: {
    collect:
      'Community showcase: if you turn it on for a resume, we store when you '
      + 'turned it on and the role you picked.',
    features:
      'Optional features (publishing, search and AI indexing, the community '
      + 'showcase, connected AI agents, Google or LinkedIn sign-in) run only '
      + 'when you turn them on',
    public:
      'The community showcase (aboutme.vn/showcase) lists only resumes whose '
      + 'owners turn on Show in the community showcase. It shows the '
      + 'resume\'s preview image (your name, headline, and photo), its '
      + 'template, language, and the role you picked, with a link to the '
      + 'resume. When you turn the option '
      + 'off, unpublish, or turn on Require sign-in to view, the '
      + 'resume leaves the showcase right away. Search engines are asked not '
      + 'to index the showcase, but anyone who visits it can see and copy '
      + 'what it shows.',
    content:
      'If you turn on Show in the community showcase for a resume, you also '
      + 'let aboutme.vn show its preview image, template, language, and the '
      + 'role you picked on the showcase page, until you turn it off or '
      + 'unpublish.',
  },
} as const;

const headings = {
  vi: {
    collect: 'Dữ liệu cá nhân chúng tôi thu thập',
    why: 'Mục đích và cơ sở xử lý',
    public: 'Chỉ công khai khi bạn chọn',
    content: 'Nội dung của bạn',
    use: 'Quy tắc sử dụng',
    contentNext: 'Bạn chịu trách nhiệm về nội dung bạn xuất bản',
  },
  en: {
    collect: 'What we collect',
    why: 'Why we use your data',
    public: 'Public only by your choice',
    content: 'Your content',
    use: 'Acceptable use',
    contentNext: 'You are responsible for what you publish',
  },
} as const;

describe.each(['vi', 'en'] as const)('showcase legal text (%s)', (lng) => {
  it('adds the collected-data item right after Content', () => {
    const items = section('privacy', lng, headings[lng].collect).items!;
    const at = items.indexOf(text[lng].collect);
    expect(at).toBeGreaterThan(0);
    expect(items[at - 1]).toMatch(/^(Nội dung|Content):/u);
    expect(items[at + 1]).toMatch(/^(Phiên đăng nhập|Sessions):/u);
  });

  it('names the showcase in the optional features list', () => {
    const [paragraph] = section('privacy', lng, headings[lng].why).paragraphs!;
    expect(paragraph).toContain(text[lng].features);
  });

  it('ends Public only by your choice with the showcase paragraph', () => {
    const paragraphs = section('privacy', lng, headings[lng].public)
      .paragraphs!;
    expect(paragraphs.at(-1)).toBe(text[lng].public);
    expect(paragraphs).toHaveLength(4);
  });

  it('adds the Terms paragraph after the first of Your content', () => {
    const paragraphs = section('terms', lng, headings[lng].content)
      .paragraphs!;
    expect(paragraphs).toHaveLength(3);
    expect(paragraphs[1]).toBe(text[lng].content);
    expect(paragraphs[2]).toContain(headings[lng].contentNext);
  });

  it('adds no showcase line under Acceptable use', () => {
    const after = section('terms', lng, headings[lng].use).after!;
    expect(after).toHaveLength(1);
    expect(after[0]).toMatch(/^(Chúng tôi có thể gỡ|We may remove)/u);
  });
});

describe('legal catalog split', () => {
  it('keeps both languages complete under the same shape', () => {
    expect(Object.keys(legalCopy).sort()).toEqual(['en', 'vi']);
    for (const document of ['privacy', 'terms'] as const) {
      expect(legalCopy.vi[document].sections.length)
        .toBe(legalCopy.en[document].sections.length);
    }
  });
});
