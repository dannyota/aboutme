import type { WorkspaceCopy } from './workspace';

export type ResumeCreateCopy = {
  readonly invalidTitle: string;
  readonly invalidDescription: string;
  readonly browseTemplates: string;
  readonly loading: string;
  readonly blankHeading: string;
  readonly sampleHeading: string;
  readonly blankSummary: (template: string) => string;
  readonly sampleSummary:
  (template: string, role: string, language: string) => string;
  readonly blankDescription: string;
  readonly sampleDescription: string;
  readonly cap: (cap: number) => string;
  readonly returnToResumes: string;
  readonly title: string;
  readonly count: (count: number, cap: number) => string;
  readonly titleRequired: string;
  readonly createFailed: string;
  readonly retryLater: string;
  readonly sessionLost: string;
  readonly back: string;
  readonly creating: string;
  readonly createAndOpen: string;
  readonly dialogTitle: string;
  readonly dialogBlankDescription: string;
  readonly dialogSampleDescription: string;
  readonly create: string;
  readonly createFromSample: string;
  readonly uncertain: string;
  readonly startFrom: string;
  readonly blank: string;
  readonly fromSample: string;
  readonly samples: string;
  readonly resumeLanguage: string;
  readonly sampleOtherLanguageHint: string;
  readonly sampleLanguageHint: string;
  readonly blankLanguageHint: string;
  readonly languageCode: string;
  readonly refreshList: string;
  readonly abandon: string;
  readonly browseAll: (count: number) => string;
  readonly cancel: string;
  readonly close: string;
  readonly languageName: (language: 'vi' | 'en') => string;
  readonly language: {
    readonly other: string;
    readonly unset: string;
    readonly hint: string;
    readonly error: string;
  };
};

export const resumeCreateCopy: WorkspaceCopy<ResumeCreateCopy> = {
  vi: {
    invalidTitle: 'Không tìm thấy mẫu',
    invalidDescription: 'Liên kết này không nêu mẫu hoặc CV mẫu có sẵn.',
    browseTemplates: 'Xem mẫu CV',
    loading: 'Đang tải',
    blankHeading: 'Tạo CV trống với mẫu này',
    sampleHeading: 'Tạo CV từ CV mẫu này',
    blankSummary: (template) => `${template} · CV trống`,
    sampleSummary: (template, role, language) => [template, role, language]
      .filter(Boolean).join(' · '),
    blankDescription: 'CV mới của bạn bắt đầu trống với mẫu này.',
    sampleDescription:
      'Bạn nhận một bản riêng để thay nội dung bằng của mình.',
    cap: (cap) => `Bạn đã có ${cap} CV. Hãy xóa một CV để tạo CV khác.`,
    returnToResumes: 'Đi đến CV của bạn',
    title: 'Tên CV',
    count: (count, cap) => `Bạn có ${count} trong ${cap} CV.`,
    titleRequired: 'Nhập tên CV.',
    createFailed: 'Không thể tạo CV. Hãy thử lại.',
    retryLater: 'Hãy chờ rồi thử lại.',
    sessionLost: 'Phiên đăng nhập đã kết thúc. Hãy đăng nhập lại.',
    back: 'Quay lại mẫu CV',
    creating: 'Đang tạo…',
    createAndOpen: 'Tạo và mở trình chỉnh sửa',
    dialogTitle: 'Tạo CV',
    dialogBlankDescription: 'Tạo CV riêng tư mới.',
    dialogSampleDescription:
      'Bắt đầu từ CV mẫu rồi thay nội dung bằng của bạn.',
    create: 'Tạo',
    createFromSample: 'Tạo từ CV mẫu',
    uncertain:
      'Không thể xác nhận CV đã được tạo. Hãy kiểm tra danh sách CV '
      + 'trước khi thử lại.',
    startFrom: 'Bắt đầu từ',
    blank: 'Trống',
    fromSample: 'Từ CV mẫu',
    samples: 'CV mẫu',
    resumeLanguage: 'Ngôn ngữ CV',
    sampleOtherLanguageHint:
      'CV mẫu có tiếng Việt và tiếng Anh. Ngôn ngữ CV vẫn là mã bạn nhập.',
    sampleLanguageHint: 'CV mẫu có tiếng Việt và tiếng Anh.',
    blankLanguageHint: 'Ngôn ngữ CV được viết bằng.',
    languageCode: 'Mã ngôn ngữ',
    refreshList: 'Tải lại danh sách',
    abandon: 'Bỏ qua',
    browseAll: (count) => `Xem cả ${count} mẫu CV`,
    cancel: 'Hủy',
    close: 'Đóng',
    languageName: (language) =>
      language === 'vi' ? 'Tiếng Việt' : 'Tiếng Anh',
    language: {
      other: 'Ngôn ngữ khác…',
      unset: 'Chưa đặt',
      hint: 'Thẻ BCP 47, như fr hoặc zh-Hant.',
      error: 'Nhập mã ngôn ngữ, như fr hoặc zh-Hant.',
    },
  },
  en: {
    invalidTitle: 'Template not found',
    invalidDescription: 'This link does not name a template or sample we have.',
    browseTemplates: 'Browse templates',
    loading: 'Loading',
    blankHeading: 'Create a blank resume with this template',
    sampleHeading: 'Create a resume from this sample',
    blankSummary: (template) => `${template} · Blank resume`,
    sampleSummary: (template, role, language) => [template, role, language]
      .filter(Boolean).join(' · '),
    blankDescription: 'Your new resume starts empty and wears this template.',
    sampleDescription:
      'You get a private copy to replace with your own content.',
    cap: (cap) => `You have ${cap} resumes. Delete one to create another.`,
    returnToResumes: 'Go to your resumes',
    title: 'Title',
    count: (count, cap) => `You have ${count} of ${cap} resumes.`,
    titleRequired: 'Enter a title.',
    createFailed: 'Could not create the resume. Try again.',
    retryLater: 'Please wait, then try again.',
    sessionLost: 'Your session ended. Sign in again.',
    back: 'Back to templates',
    creating: 'Creating…',
    createAndOpen: 'Create and open editor',
    dialogTitle: 'Create resume',
    dialogBlankDescription: 'Create a new private resume.',
    dialogSampleDescription:
      'Start from a sample resume and replace its content with yours.',
    create: 'Create',
    createFromSample: 'Create from sample',
    uncertain:
      'We could not confirm whether this resume was created. '
      + 'Check your resumes before trying again.',
    startFrom: 'Start from',
    blank: 'Blank',
    fromSample: 'From a sample',
    samples: 'Samples',
    resumeLanguage: 'Resume language',
    sampleOtherLanguageHint:
      'Samples are in Vietnamese or English. Your resume language '
      + 'stays the code you enter.',
    sampleLanguageHint: 'Samples come in Vietnamese and English.',
    blankLanguageHint: 'The language your resume is written in.',
    languageCode: 'Language code',
    refreshList: 'Refresh list',
    abandon: 'Abandon',
    browseAll: (count) => `Browse all ${count} templates`,
    cancel: 'Cancel',
    close: 'Close',
    languageName: (language) =>
      language === 'vi' ? 'Vietnamese' : 'English',
    language: {
      other: 'Other…',
      unset: 'Not set',
      hint: 'A BCP 47 tag, such as fr or zh-Hant.',
      error: 'Enter a language code, such as fr or zh-Hant.',
    },
  },
};
