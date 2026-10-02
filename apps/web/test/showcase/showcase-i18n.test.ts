import { describe, expect, it } from 'vitest';

import { publishCopy } from '../../app/i18n/publish';
import { shellCopy } from '../../app/i18n/shell';
import { showcaseCopy } from '../../app/i18n/showcase';
import { parityViolations } from '../support/localizationSource';

// Every string quotes the Copy tables of docs/design/showcase.md, in both
// languages, with the same shape (AC-SHOW-002, 007).

describe('showcase catalog', () => {
  it('keeps both languages in the same shape and nonempty', () => {
    expect(parityViolations(showcaseCopy.vi, showcaseCopy.en, 'showcase', {
      'showcase.reportLabel': ['ada'],
      'showcase.reportSubject': ['ada'],
      'showcase.pageStatus': [1, 2],
    })).toEqual([]);
    expect(parityViolations(
      publishCopy.vi.showcase,
      publishCopy.en.showcase,
      'publish.showcase',
    )).toEqual([]);
  });

  it('holds the approved page strings in English', () => {
    const copy = showcaseCopy.en;
    expect({
      nav: copy.navLabel,
      title: copy.title,
      description: copy.description,
      lead: copy.lead,
      orderNote: copy.orderNote,
      rolesLabel: copy.rolesLabel,
      allRoles: copy.allRoles,
      other: copy.roles.other,
      languageLabel: copy.languageLabel,
      allLanguages: copy.allLanguages,
      vi: copy.languages.vi,
      en: copy.languages.en,
      otherLanguage: copy.languages.other,
      templateLabel: copy.templateLabel,
      allTemplates: copy.allTemplates,
      customDesign: copy.customDesign,
      report: copy.report,
      reportLabel: copy.reportLabel('ada'),
      reportSubject: copy.reportSubject('ada'),
      empty: copy.empty,
      emptyAction: copy.emptyAction,
      signedIn: copy.emptyActionSignedIn,
      noMatch: copy.noMatch,
      loadFailed: copy.loadFailed,
      retry: copy.retry,
      previous: copy.previous,
      next: copy.next,
      pageStatus: copy.pageStatus(2, 5),
    }).toEqual({
      nav: 'Community',
      title: 'Community resumes',
      description: 'Real resumes that aboutme.vn users chose to share.',
      lead:
        'Real resumes that aboutme.vn users published and chose to show '
        + 'here.',
      orderNote: 'Earliest added first.',
      rolesLabel: 'Filter by role',
      allRoles: 'All roles',
      other: 'Other',
      languageLabel: 'Resume language',
      allLanguages: 'All languages',
      vi: 'Vietnamese',
      en: 'English',
      otherLanguage: 'Other language',
      templateLabel: 'Template',
      allTemplates: 'All templates',
      customDesign: 'Custom design',
      report: 'Report',
      reportLabel: 'Report resume ada by email',
      reportSubject: 'Report showcase: ada',
      empty:
        'No resumes here yet. Publish your resume and turn on Show in the '
        + 'community showcase to be the first.',
      emptyAction: 'Create your resume',
      signedIn: 'Open your resumes',
      noMatch: 'No resume matches these filters.',
      loadFailed: 'Could not load the list. Try again.',
      retry: 'Try again',
      previous: 'Previous',
      next: 'Next',
      pageStatus: 'Page 2 of 5',
    });
  });

  it('holds the approved page strings in Vietnamese', () => {
    const copy = showcaseCopy.vi;
    expect({
      nav: copy.navLabel,
      title: copy.title,
      description: copy.description,
      lead: copy.lead,
      orderNote: copy.orderNote,
      rolesLabel: copy.rolesLabel,
      allRoles: copy.allRoles,
      other: copy.roles.other,
      languageLabel: copy.languageLabel,
      allLanguages: copy.allLanguages,
      vi: copy.languages.vi,
      en: copy.languages.en,
      otherLanguage: copy.languages.other,
      templateLabel: copy.templateLabel,
      allTemplates: copy.allTemplates,
      customDesign: copy.customDesign,
      report: copy.report,
      reportLabel: copy.reportLabel('ada'),
      reportSubject: copy.reportSubject('ada'),
      empty: copy.empty,
      emptyAction: copy.emptyAction,
      signedIn: copy.emptyActionSignedIn,
      noMatch: copy.noMatch,
      loadFailed: copy.loadFailed,
      retry: copy.retry,
      previous: copy.previous,
      next: copy.next,
      pageStatus: copy.pageStatus(2, 5),
    }).toEqual({
      nav: 'Cộng đồng',
      title: 'CV từ cộng đồng',
      description: 'CV thật do người dùng aboutme.vn chọn chia sẻ.',
      lead:
        'CV thật do người dùng aboutme.vn xuất bản và chọn hiện ở đây.',
      orderNote: 'CV thêm sớm nhất hiện trước.',
      rolesLabel: 'Lọc theo vị trí',
      allRoles: 'Mọi vị trí',
      other: 'Khác',
      languageLabel: 'Ngôn ngữ của CV',
      allLanguages: 'Mọi ngôn ngữ',
      vi: 'Tiếng Việt',
      en: 'Tiếng Anh',
      otherLanguage: 'Ngôn ngữ khác',
      templateLabel: 'Mẫu',
      allTemplates: 'Mọi mẫu',
      customDesign: 'Thiết kế riêng',
      report: 'Báo cáo',
      reportLabel: 'Báo cáo CV ada qua email',
      reportSubject: 'Báo cáo trang Cộng đồng: ada',
      empty:
        'Chưa có CV nào ở đây. Xuất bản CV của bạn và bật Hiện trong trang '
        + 'Cộng đồng để là người đầu tiên.',
      emptyAction: 'Tạo CV',
      signedIn: 'Mở CV của bạn',
      noMatch: 'Không có CV nào khớp với bộ lọc này.',
      loadFailed: 'Không tải được danh sách. Hãy thử lại.',
      retry: 'Thử lại',
      previous: 'Trang trước',
      next: 'Trang sau',
      pageStatus: 'Trang 2/5',
    });
  });

  it('uses the Library role labels for the nine roles', () => {
    const { roles } = showcaseCopy.en;
    expect(roles.backend).toBe('Backend');
    expect(roles.devops).toBe('DevOps/SRE');
    expect(roles['data-ai']).toBe('Data/AI');
    expect(roles.fresher).toBe('Fresher/Intern');
    expect(showcaseCopy.vi.roles.security).toBe('Bảo mật');
  });

  it('holds the approved publish dialog strings in English', () => {
    const copy = publishCopy.en.showcase;
    expect(copy.label).toBe('Show in the community showcase');
    expect(copy.help).toBe(
      'Shows this resume\'s preview image, template, language, and role at '
      + 'aboutme.vn/showcase. Anyone can see that page.',
    );
    expect(copy.roleLabel).toBe('Role shown');
    expect(copy.roleHint).toBe('Optional. Lets visitors filter by role.');
    expect(copy.noRole).toBe('None');
    expect(copy.roles.other).toBe('Other');
    expect(copy.needsPublic).toBe(
      'Turn on Public resume to show it in the community showcase.',
    );
    expect(copy.needsOpenView).toBe(
      'Not available while Require sign-in to view is on.',
    );
    expect(copy.listed).toBe('Shown in the community showcase.');
    expect(copy.listedLink).toBe('View the showcase');
    expect(copy.issueOpenView).toBe(
      'This option needs Require sign-in to view off.',
    );
  });

  it('holds the approved publish dialog strings in Vietnamese', () => {
    const copy = publishCopy.vi.showcase;
    expect(copy.label).toBe('Hiện trong trang Cộng đồng');
    expect(copy.help).toBe(
      'Hiện ảnh xem trước, mẫu, ngôn ngữ và vị trí của CV này tại '
      + 'aboutme.vn/showcase. Ai cũng xem được trang đó.',
    );
    expect(copy.roleLabel).toBe('Vị trí hiển thị');
    expect(copy.roleHint).toBe('Tùy chọn. Giúp người xem lọc theo vị trí.');
    expect(copy.noRole).toBe('Không chọn');
    expect(copy.roles.other).toBe('Khác');
    expect(copy.needsPublic).toBe(
      'Bật CV công khai để hiện trong trang Cộng đồng.',
    );
    expect(copy.needsOpenView).toBe(
      'Không dùng được khi bật Yêu cầu đăng nhập để xem.',
    );
    expect(copy.listed).toBe('Đang hiện trong trang Cộng đồng.');
    expect(copy.listedLink).toBe('Xem trang Cộng đồng');
    expect(copy.issueOpenView).toBe(
      'Tùy chọn này cần tắt Yêu cầu đăng nhập để xem.',
    );
  });

  it('names the shell link in both languages', () => {
    expect(shellCopy.vi.community).toBe('Cộng đồng');
    expect(shellCopy.en.community).toBe('Community');
  });
});
