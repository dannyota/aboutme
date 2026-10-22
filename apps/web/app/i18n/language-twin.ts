import type { TwinLanguage } from '../editor/languageTwin';
import type { WorkspaceCopy } from './workspace';

export type LanguageTwinCopy = {
  readonly action: Readonly<Record<TwinLanguage, string>>;
  readonly languageName: Readonly<Record<TwinLanguage, string>>;
  readonly title: string;
  readonly sentences: Readonly<Record<TwinLanguage, string>>;
  readonly numbers: Readonly<Record<TwinLanguage, string>>;
  readonly createdUnpublished: string;
  readonly photoRemoved: Readonly<Record<TwinLanguage, string>>;
  readonly birthRemoved: string;
  readonly zaloSuggested: string;
  readonly zaloRemoved: string;
  readonly headingsLeft: string;
  readonly agentReady: string;
  readonly agentAbsent: string;
  readonly copyInstructions: string;
  readonly copied: string;
  readonly dismiss: string;
  readonly settings: string;
  readonly agentPrompt: (title: string, language: string) => string;
};

export const languageTwinCopy: WorkspaceCopy<LanguageTwinCopy> = {
  vi: {
    action: {
      vi: 'Tạo bản tiếng Việt',
      en: 'Tạo bản tiếng Anh',
    },
    languageName: { vi: 'Tiếng Việt', en: 'English' },
    title: 'Xem lại bản ngôn ngữ kia',
    sentences: {
      vi: 'Viết lại câu bằng tiếng Việt. Giữ nguyên chức danh '
        + 'và tên công nghệ bằng tiếng Anh.',
      en: 'Viết lại câu bằng tiếng Anh. Đừng dịch từng chữ '
        + 'chức danh và tên công nghệ.',
    },
    numbers: {
      vi: 'Số liệu tiếng Việt dùng 10.000 và 1,9.',
      en: 'Số liệu tiếng Anh dùng 10,000 và 1.9.',
    },
    createdUnpublished: 'CV này được tạo dưới dạng bản sao chưa công khai.',
    photoRemoved: {
      vi: 'Ảnh chưa được sao chép. Thêm lại nếu nhà tuyển dụng '
        + 'trong nước cần ảnh.',
      en: 'Ảnh được bỏ ra. CV tiếng Anh cho công ty sản phẩm '
        + 'hoặc nhà tuyển dụng nước ngoài không dùng ảnh.',
    },
    birthRemoved: 'Ngày sinh đã được bỏ khỏi bản tiếng Anh.',
    zaloSuggested: 'Thêm Zalo nếu bạn dùng và muốn nhà tuyển dụng '
      + 'liên hệ qua đó.',
    zaloRemoved: 'Gợi ý Zalo đã được bỏ khỏi bản tiếng Anh.',
    headingsLeft: 'Một số tiêu đề mục không nằm trong danh sách chuẩn. '
      + 'Hãy đọc lại.',
    agentReady: 'Tác nhân đã kết nối có thể viết lại câu. '
      + 'Tác nhân không thể đăng CV này.',
    agentAbsent: 'Sửa câu tại đây, hoặc kết nối một tác nhân '
      + 'rồi dán hướng dẫn.',
    copyInstructions: 'Sao chép hướng dẫn',
    copied: 'Đã sao chép',
    dismiss: 'Đã xem xong',
    settings: 'Tác nhân đã kết nối',
    agentPrompt: (title, language) =>
      `Viết lại CV "${title}" sang ${language}. `
      + 'Giữ chức danh và tên công nghệ bằng tiếng Anh. '
      + 'Dùng cách viết số của ngôn ngữ đó. '
      + 'Không đăng CV.',
  },
  en: {
    action: {
      vi: 'Create the Vietnamese version',
      en: 'Create the English version',
    },
    languageName: { vi: 'Tiếng Việt', en: 'English' },
    title: 'Review the other language',
    sentences: {
      vi: 'Rewrite the sentences in Vietnamese. '
        + 'Keep English job titles and technology names.',
      en: 'Rewrite the sentences in English. Do not translate '
        + 'job titles and technology names word for word.',
    },
    numbers: {
      vi: 'Vietnamese numbers use 10.000 and 1,9.',
      en: 'English numbers use 10,000 and 1.9.',
    },
    createdUnpublished: 'This resume was created as an unpublished copy.',
    photoRemoved: {
      vi: 'The photo was not copied. Add it again if a local '
        + 'employer expects one.',
      en: 'The photo was left off. English resumes for product '
        + 'and foreign employers leave it out.',
    },
    birthRemoved: 'The birth date was left off the English copy.',
    zaloSuggested: 'Add Zalo if you use it and want recruiters '
      + 'to contact you there.',
    zaloRemoved: 'The Zalo hint was left off the English copy.',
    headingsLeft: 'Some section headings are outside the standard list. '
      + 'Read them again.',
    agentReady: 'A connected agent can rewrite the sentences. '
      + 'It cannot publish this copy.',
    agentAbsent: 'Edit the sentences here, or connect an agent '
      + 'and paste the instructions.',
    copyInstructions: 'Copy instructions',
    copied: 'Copied',
    dismiss: 'Done reviewing',
    settings: 'Connected agents',
    agentPrompt: (title, language) =>
      `Rewrite the resume "${title}" into ${language}. `
      + 'Keep English job titles and technology names. '
      + 'Use that language\'s number format. '
      + 'Do not publish the resume.',
  },
};
