import type {
  FaviconEmojiIssue,
  PublicTitleIssue,
} from '../editor/publicPageMeta';
import type { WorkspaceCopy } from './workspace';

type PublishBlockedReason
  = | 'not-loaded'
    | 'saving'
    | 'conflict'
    | 'session-lost'
    | 'issue'
    | 'partial-template'
    | 'opaque-photo'
    | 'read-required';

type PublishIssueCode
  = | 'required_for_live'
    | 'requires_live'
    | 'invalid_format'
    | 'reserved'
    | 'required'
    | 'visible_entry_required';

type PublishFailureCode
  = | 'provider_disabled'
    | 'provider_unavailable'
    | 'csrf_rejected'
    | 'save_failed';

export type PublishPageCopy = {
  readonly legend: string;
  readonly title: string;
  readonly titleHint: (count: number, maximum: number) => string;
  readonly emoji: string;
  readonly emojiHint: string;
  readonly suggestedIcons: string;
  readonly useIcon: (icon: string) => string;
  readonly removeIcon: string;
  readonly tabPreview: string;
  readonly titleError: Readonly<Record<PublicTitleIssue, string>> & {
    readonly generic: string;
  };
  readonly emojiError: Readonly<Record<FaviconEmojiIssue, string>> & {
    readonly generic: string;
  };
};

export type PublishPreviewCopy = {
  readonly heading: string;
  readonly caption: string;
};

export type PublishCopy = {
  readonly title: string;
  readonly description: string;
  readonly slug: string;
  readonly slugError: string;
  readonly options: string;
  readonly live: string;
  readonly liveHelp: string;
  readonly download: string;
  readonly downloadHelp: string;
  readonly discovery: string;
  readonly discoveryHelp: string;
  readonly publish: string;
  readonly update: string;
  readonly unpublish: string;
  readonly cancel: string;
  readonly retry: string;
  readonly publishing: string;
  readonly currentPassword: string;
  readonly passwordAction: string;
  readonly providerContinue: string;
  readonly providerStart: string;
  readonly providerRetryAction: string;
  readonly providerLink: string;
  readonly providerReturn: string;
  readonly providerRetry: string;
  readonly wrongPassword: string;
  readonly reauthRateLimited: string;
  readonly reauthUnavailable: string;
  readonly blocked: Readonly<Record<PublishBlockedReason, string>>;
  readonly issue: Readonly<Record<PublishIssueCode, string>>;
  readonly invalid: string;
  readonly slugTaken: string;
  readonly stale: string;
  readonly rateLimited: string;
  readonly unknown: string;
  readonly sessionLost: string;
  readonly failed: Readonly<Record<PublishFailureCode, string>> & {
    readonly generic: string;
  };
  readonly published: string;
  readonly private: string;
  readonly copyLink: string;
  readonly copied: string;
  readonly copyFailed: string;
  readonly page: PublishPageCopy;
  readonly preview: PublishPreviewCopy;
};

export const publishCopy: WorkspaceCopy<PublishCopy> = {
  vi: {
    title: 'Xuất bản CV',
    description: 'Chọn cách chia sẻ CV này công khai.',
    slug: 'Đường dẫn',
    slugError:
      'Dùng 4–30 chữ cái hoặc số ASCII viết thường, '
      + 'ngăn cách bằng một dấu gạch nối.',
    options: 'Tùy chọn xuất bản',
    live: 'CV công khai',
    liveHelp:
      'CV công khai có thể được phân phối qua mạng phân phối '
      + 'nội dung toàn cầu.',
    download: 'Tải PDF',
    downloadHelp:
      'Khách truy cập có thể tải PDF. Bạn luôn có thể xuất bản PDF của mình.',
    discovery: 'SEO và GEO',
    discoveryHelp:
      'SEO và GEO cho phép công cụ tìm kiếm và công cụ trả lời AI '
      + 'tìm thấy và dùng lại nội dung CV công khai.',
    publish: 'Xuất bản',
    update: 'Cập nhật xuất bản',
    unpublish: 'Hủy xuất bản',
    cancel: 'Hủy',
    retry: 'Thử xuất bản lại',
    publishing: 'Đang xuất bản…',
    currentPassword: 'Mật khẩu hiện tại',
    passwordAction: 'Xác thực lại và xuất bản',
    providerContinue: 'Tiếp tục với nhà cung cấp đã liên kết để xác thực lại.',
    providerStart: 'Bắt đầu xác thực lại với nhà cung cấp',
    providerRetryAction: 'Thử xác thực lại với nhà cung cấp',
    providerLink: 'Tiếp tục xác thực lại trong thẻ mới',
    providerReturn:
      'Hoàn tất xác thực lại trong thẻ mới, quay lại trình chỉnh sửa '
      + 'rồi chọn Thử xuất bản lại.',
    providerRetry: 'Thử xuất bản lại',
    wrongPassword: 'Mật khẩu không được chấp nhận. Hãy thử lại.',
    reauthRateLimited: 'Xác thực lại đang bị giới hạn. Hãy thử lại sau.',
    reauthUnavailable: 'Không thể xác thực lại. Hãy thử lại sau.',
    blocked: {
      'not-loaded': 'CV vẫn đang tải. Chưa thể bắt đầu xuất bản.',
      'saving': 'Lưu các thay đổi CV mới nhất trước khi xuất bản.',
      'conflict': 'Giải quyết xung đột CV trước khi xuất bản.',
      'session-lost':
        'Phiên của bạn đã kết thúc. Đăng nhập lại trước khi xuất bản.',
      'issue': 'Giải quyết các vấn đề CV hiện tại trước khi xuất bản.',
      'partial-template': 'Hoàn tất khôi phục thay đổi mẫu trước khi xuất bản.',
      'opaque-photo': 'Giải quyết thay đổi ảnh trước khi xuất bản.',
      'read-required': 'Làm mới CV hoàn chỉnh trước khi xuất bản.',
    },
    issue: {
      required_for_live: 'Thiếu trường bắt buộc để xuất bản.',
      requires_live: 'Tùy chọn này cần bật CV công khai.',
      invalid_format: 'Định dạng đường dẫn không hợp lệ.',
      reserved: 'Đường dẫn này đã được dành riêng.',
      required: 'Thiếu trường bắt buộc.',
      visible_entry_required: 'Thêm một mục CV hiển thị trước khi xuất bản.',
    },
    invalid: 'Không thể xuất bản CV.',
    slugTaken: 'Đường dẫn công khai này đã được dùng. Chọn đường dẫn khác.',
    stale:
      'CV đã thay đổi ở nơi khác. Xem phiên bản mới nhất trước khi '
      + 'xuất bản lại.',
    rateLimited: 'Tạm thời không thể xuất bản. Hãy thử lại sau.',
    unknown:
      'Không thể xác nhận việc xuất bản. Thử xuất bản lại để kiểm tra an toàn.',
    sessionLost: 'Phiên của bạn đã kết thúc. Đăng nhập lại trước khi xuất bản.',
    failed: {
      provider_disabled:
        'Không có phương thức xác thực lại được hỗ trợ. Không thể xuất bản.',
      provider_unavailable:
        'Không có phương thức xác thực lại được hỗ trợ. Không thể xuất bản.',
      csrf_rejected:
        'Không thể xác minh thao tác này. Làm mới phiên rồi thử lại.',
      save_failed: 'Không thể lưu CV trước khi xuất bản. Hãy thử lại.',
      generic: 'Xuất bản không thành công. Hãy thử lại.',
    },
    published: 'Đã xuất bản thành công.',
    private: 'CV đang ở chế độ riêng tư.',
    copyLink: 'Sao chép liên kết',
    copied: 'Đã sao chép',
    copyFailed: 'Sao chép không thành công. Chọn liên kết để sao chép.',
    page: {
      legend: 'Thẻ trình duyệt',
      title: 'Tiêu đề trang',
      titleHint: (count, maximum) => `Tùy chọn. ${count}/${maximum} ký tự.`,
      emoji: 'Biểu tượng thẻ',
      emojiHint:
        'Tùy chọn. Một biểu tượng cảm xúc, để trống để dùng biểu tượng '
        + 'aboutme.',
      suggestedIcons: 'Biểu tượng thẻ gợi ý',
      useIcon: (icon) => `Dùng ${icon}`,
      removeIcon: 'Xóa biểu tượng',
      tabPreview: 'Thẻ trình duyệt sẽ hiển thị như sau',
      titleError: {
        invalid_characters: 'Xóa ký tự ẩn hoặc ký tự điều khiển.',
        too_long: 'Dùng tối đa 70 ký tự.',
        generic: 'Kiểm tra tiêu đề trang.',
      },
      emojiError: {
        invalid_emoji: 'Nhập đúng một biểu tượng cảm xúc.',
        generic: 'Kiểm tra biểu tượng thẻ.',
      },
    },
    preview: {
      heading: 'Xem trước khi chia sẻ CV',
      caption:
        'Ứng dụng chat và mạng xã hội hiển thị thẻ như thế này. '
        + 'Mỗi ứng dụng có thể cắt ảnh khác nhau.',
    },
  },
  en: {
    title: 'Publish resume',
    description: 'Choose how this resume is shared publicly.',
    slug: 'Slug',
    slugError:
      'Use 4–30 lowercase ASCII letters or numbers, '
      + 'separated by single hyphens.',
    options: 'Publish options',
    live: 'Public resume',
    liveHelp:
      'Public resumes may be delivered through a global '
      + 'content-delivery network.',
    download: 'PDF download',
    downloadHelp:
      'Whether visitors can download the PDF. You can always export your own.',
    discovery: 'SEO and GEO',
    discoveryHelp:
      'SEO and GEO allow search crawlers and AI answer engines to '
      + 'discover and reuse public resume content.',
    publish: 'Publish',
    update: 'Update publication',
    unpublish: 'Unpublish',
    cancel: 'Cancel',
    retry: 'Retry publish',
    publishing: 'Publishing…',
    currentPassword: 'Current password',
    passwordAction: 'Reauthenticate and publish',
    providerContinue: 'Continue with your linked provider to reauthenticate.',
    providerStart: 'Start provider reauthentication',
    providerRetryAction: 'Try provider reauthentication again',
    providerLink: 'Continue reauthentication in a new tab',
    providerReturn:
      'Finish reauthentication in the new tab, return to the editor, '
      + 'and choose Retry publish.',
    providerRetry: 'Retry publish',
    wrongPassword: 'That password was not accepted. Try again.',
    reauthRateLimited:
      'Reauthentication is temporarily rate limited. Try again shortly.',
    reauthUnavailable: 'Reauthentication is unavailable. Try again later.',
    blocked: {
      'not-loaded': 'The resume is still loading. Publishing cannot start yet.',
      'saving': 'Save the latest resume changes before publishing.',
      'conflict': 'Resolve the resume conflict before publishing.',
      'session-lost': 'Your session ended. Sign in again before publishing.',
      'issue': 'Resolve the current resume issues before publishing.',
      'partial-template':
        'Finish recovering the template changes before publishing.',
      'opaque-photo': 'Resolve the photo change before publishing.',
      'read-required': 'Refresh the complete resume before publishing.',
    },
    issue: {
      required_for_live: 'A required field is missing for publication.',
      requires_live: 'This option requires Public resume.',
      invalid_format: 'The slug format is invalid.',
      reserved: 'That slug is reserved.',
      required: 'A required field is missing.',
      visible_entry_required: 'Add a visible resume entry before publishing.',
    },
    invalid: 'The resume cannot be published.',
    slugTaken: 'That public slug is already in use. Choose another slug.',
    stale:
      'The resume changed elsewhere. Review the latest version before '
      + 'publishing again.',
    rateLimited: 'Publishing is temporarily unavailable. Try again shortly.',
    unknown: 'We could not confirm publication. Retry publish to check safely.',
    sessionLost: 'Your session ended. Sign in again before publishing.',
    failed: {
      provider_disabled:
        'No supported reauthentication method is available. '
        + 'Publishing is unavailable.',
      provider_unavailable:
        'No supported reauthentication method is available. '
        + 'Publishing is unavailable.',
      csrf_rejected:
        'We could not verify this action. Refresh your session and try again.',
      save_failed: 'We could not save the resume before publishing. Try again.',
      generic: 'Publishing failed. Try again.',
    },
    published: 'Published successfully.',
    private: 'Resume is private.',
    copyLink: 'Copy link',
    copied: 'Copied',
    copyFailed: 'Copy failed. Select the link to copy it.',
    page: {
      legend: 'Browser tab',
      title: 'Page title',
      titleHint: (count, maximum) =>
        `Optional. ${count}/${maximum} characters.`,
      emoji: 'Tab icon',
      emojiHint: 'Optional. One emoji; leave empty for the aboutme icon.',
      suggestedIcons: 'Suggested tab icons',
      useIcon: (icon) => `Use ${icon}`,
      removeIcon: 'Remove icon',
      tabPreview: 'How the browser tab will look',
      titleError: {
        invalid_characters: 'Remove hidden or control characters.',
        too_long: 'Use 70 characters or fewer.',
        generic: 'Check the page title.',
      },
      emojiError: {
        invalid_emoji: 'Enter exactly one emoji.',
        generic: 'Check the tab icon.',
      },
    },
    preview: {
      heading: 'Preview when your resume is shared',
      caption:
        'Chat apps and social networks show a card like this. '
        + 'Each app may crop it.',
    },
  },
};
