import type { WorkspaceCopy } from './workspace';

export type ConflictControlKind
  = | 'apply-field'
    | 'select-entry'
    | 'recreate-entry'
    | 'reopen-entry-order'
    | 'reopen-placement'
    | 'reopen-crop'
    | 'review-template'
    | 'confirm-deletion'
    | 'review-photo';

export type EditorShellCopy = {
  readonly localeLabel: string;
  readonly loadingEditor: string;
  readonly resumeUnavailable: string;
  readonly resumeUnavailableDescription: string;
  readonly backToResumes: string;
  readonly editorUnavailable: string;
  readonly editorUnavailableDescription: string;
  readonly tryAgain: string;
  readonly publish: string;
  readonly editorTools: string;
  readonly document: string;
  readonly personalDetails: string;
  readonly structure: string;
  readonly design: string;
  readonly templates: string;
  readonly photo: string;
  readonly resume: string;
  readonly toggleOutline: string;
  readonly addSection: string;
  readonly resumeOutline: string;
  readonly sections: string;
  readonly resumeSections: string;
  readonly edit: string;
  readonly preview: string;
  readonly editorView: string;
  readonly sessionLostTitle: string;
  readonly sessionLostDescription: string;
  readonly discardAndSignIn: string;
  readonly openSignIn: string;
  readonly resumeAfterSignIn: string;
  readonly checkFields: string;
  readonly issueRequired: string;
  readonly issueLimit: string;
  readonly issueFormat: string;
  readonly issueGeneric: string;
  readonly conflictTitle: string;
  readonly conflictDescription: string;
  readonly acceptLatest: string;
  readonly conflictControl: (control: ConflictControlKind) => string;
  readonly previewLabel: string;
  readonly previewUnavailable: string;
  readonly estimatedPages: string;
  readonly estimatedPageCount: string;
  readonly pageCount: (count: number | null) => string;
  readonly photoLoading: string;
  readonly photoUnavailable: string;
  readonly openPhotoPanel: string;
  readonly previewZoom: string;
  readonly fit: string;
  readonly fullZoom: string;
  readonly issueFor: (code: string) => string;
};

export const editorShellCopy: WorkspaceCopy<EditorShellCopy> = {
  vi: {
    localeLabel: 'Ngôn ngữ',
    loadingEditor: 'Đang tải trình chỉnh sửa',
    resumeUnavailable: 'Không có CV',
    resumeUnavailableDescription: 'Không thể mở CV này.',
    backToResumes: 'Quay lại CV',
    editorUnavailable: 'Không thể mở trình chỉnh sửa',
    editorUnavailableDescription: 'Không thể mở CV này. Hãy thử lại.',
    tryAgain: 'Thử lại',
    publish: 'Xuất bản',
    editorTools: 'Công cụ chỉnh sửa',
    document: 'Tài liệu',
    personalDetails: 'Thông tin cá nhân',
    structure: 'Cấu trúc',
    design: 'Thiết kế',
    templates: 'Mẫu',
    photo: 'Ảnh',
    resume: 'CV',
    toggleOutline: 'Bật tắt dàn ý CV',
    addSection: 'Thêm mục',
    resumeOutline: 'Dàn ý CV',
    sections: 'Mục',
    resumeSections: 'Các mục trong CV',
    edit: 'Chỉnh sửa',
    preview: 'Xem trước',
    editorView: 'Chế độ chỉnh sửa',
    sessionLostTitle: 'Đăng nhập để tiếp tục chỉnh sửa',
    sessionLostDescription: 'Bản nháp chưa lưu vẫn mở trong thẻ này.',
    discardAndSignIn: 'Bỏ bản nháp và đăng nhập',
    openSignIn: 'Mở trang đăng nhập trong thẻ khác',
    resumeAfterSignIn: 'Tiếp tục sau khi đăng nhập',
    checkFields: 'Kiểm tra các trường này',
    issueRequired: 'Thêm giá trị bắt buộc.',
    issueLimit: 'Giá trị này vượt quá giới hạn cho phép.',
    issueFormat: 'Kiểm tra giá trị này rồi thử lại.',
    issueGeneric: 'Giá trị này cần được kiểm tra.',
    conflictTitle: 'Xem lại thay đổi',
    conflictDescription:
      'Phần này đã thay đổi ở nơi khác. Hãy xem bản mới nhất trước khi lưu.',
    acceptLatest: 'Chấp nhận bản mới nhất',
    conflictControl: (control) => ({
      'apply-field': 'Áp dụng giá trị của tôi',
      'select-entry': 'Chọn mục khác',
      'recreate-entry': 'Tạo lại mục',
      'reopen-entry-order': 'Mở lại thứ tự mục',
      'reopen-placement': 'Mở lại vị trí',
      'reopen-crop': 'Mở lại cắt ảnh',
      'review-template': 'Xem lại thay đổi mẫu',
      'confirm-deletion': 'Xác nhận xóa lại',
      'review-photo': 'Xem lại thay đổi ảnh',
    })[control],
    previewLabel: 'Bản xem trước CV',
    previewUnavailable: [
      'Bản xem trước tạm thời không khả dụng.',
      'Bản chỉnh sửa của bạn vẫn an toàn.',
    ].join(' '),
    estimatedPages: 'Số trang ước tính',
    estimatedPageCount: 'Số trang ước tính',
    pageCount: (count) => count === 1 ? '1 trang' : `${count ?? '—'} trang`,
    photoLoading: 'Đang tải ảnh. Bản xem trước hiển thị không có ảnh.',
    photoUnavailable:
      'Không thể tải ảnh. Bản xem trước hiển thị không có ảnh.',
    openPhotoPanel: 'Mở bảng ảnh',
    previewZoom: 'Thu phóng bản xem trước',
    fit: 'Vừa khung',
    fullZoom: '100%',
    issueFor: (code) => {
      if (code === 'required') return 'Thêm giá trị bắt buộc.';
      if (code === 'maxLength' || code === 'maxItems' || code === 'maximum') {
        return 'Giá trị này vượt quá giới hạn cho phép.';
      }
      if (
        code === 'format'
        || code === 'pattern'
        || code === 'date-range-order'
      ) {
        return 'Kiểm tra giá trị này rồi thử lại.';
      }
      return 'Giá trị này cần được kiểm tra.';
    },
  },
  en: {
    localeLabel: 'Language',
    loadingEditor: 'Loading editor',
    resumeUnavailable: 'Resume unavailable',
    resumeUnavailableDescription: 'This resume is not available.',
    backToResumes: 'Back to resumes',
    editorUnavailable: 'Editor unavailable',
    editorUnavailableDescription: 'We could not open this resume. Try again.',
    tryAgain: 'Try again',
    publish: 'Publish',
    editorTools: 'Editor tools',
    document: 'Document',
    personalDetails: 'Personal details',
    structure: 'Structure',
    design: 'Design',
    templates: 'Templates',
    photo: 'Photo',
    resume: 'Resume',
    toggleOutline: 'Toggle resume outline',
    addSection: 'Add section',
    resumeOutline: 'Resume outline',
    sections: 'Sections',
    resumeSections: 'Resume sections',
    edit: 'Edit',
    preview: 'Preview',
    editorView: 'Editor view',
    sessionLostTitle: 'Sign in to continue editing',
    sessionLostDescription: 'Your unsaved work is still open in this tab.',
    discardAndSignIn: 'Discard and sign in',
    openSignIn: 'Open sign-in in another tab',
    resumeAfterSignIn: 'Resume after sign-in',
    checkFields: 'Check these fields',
    issueRequired: 'Add the required value.',
    issueLimit: 'This value is over the allowed limit.',
    issueFormat: 'Check this value and try again.',
    issueGeneric: 'This value needs attention.',
    conflictTitle: 'Review changes',
    conflictDescription:
      'This part changed elsewhere. Review the latest version before saving.',
    acceptLatest: 'Accept latest',
    conflictControl: (control) => ({
      'apply-field': 'Apply my value',
      'select-entry': 'Select another entry',
      'recreate-entry': 'Recreate entry',
      'reopen-entry-order': 'Reopen entry order',
      'reopen-placement': 'Reopen placement',
      'reopen-crop': 'Reopen crop',
      'review-template': 'Review template changes',
      'confirm-deletion': 'Confirm deletion again',
      'review-photo': 'Review photo changes',
    })[control],
    previewLabel: 'Resume preview',
    previewUnavailable:
      'Preview is temporarily unavailable. Your edits are still safe.',
    estimatedPages: 'Estimated pages',
    estimatedPageCount: 'Estimated page count',
    pageCount: (count) => count === 1 ? '1 page' : `${count ?? '—'} pages`,
    photoLoading: 'Photo is loading. The preview is shown without it.',
    photoUnavailable: 'Photo unavailable. The preview is shown without it.',
    openPhotoPanel: 'Open photo panel',
    previewZoom: 'Preview zoom',
    fit: 'Fit',
    fullZoom: '100%',
    issueFor: (code) => {
      if (code === 'required') return 'Add the required value.';
      if (code === 'maxLength' || code === 'maxItems' || code === 'maximum') {
        return 'This value is over the allowed limit.';
      }
      if (
        code === 'format'
        || code === 'pattern'
        || code === 'date-range-order'
      ) {
        return 'Check this value and try again.';
      }
      return 'This value needs attention.';
    },
  },
};
