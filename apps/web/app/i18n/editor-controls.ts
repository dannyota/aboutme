import type {
  PageFormat,
} from '../components/editor/customization/pageSettings';
import type { CustomizationSetPath } from '../editor/commands';
import type { Locale } from './locale';
import type { WorkspaceCopy } from './workspace';

type EditorControlsCopy = {
  readonly controls: EditorControlCopy;
  readonly customization: Record<CustomizationSetPath, string>;
  readonly groups: Record<CustomizationGroupId, string>;
  readonly enums: Record<EditorControlEnum, string>;
  readonly page: {
    readonly description: string;
    readonly pageSize: string;
    readonly margins: string;
    readonly marginLabels: Readonly<
      Record<'narrow' | 'normal' | 'wide' | 'custom', string>
    >;
    readonly custom: string;
    readonly horizontal: (unit: string) => string;
    readonly vertical: (unit: string) => string;
    readonly edge: (length: string) => string;
    readonly invalid: (maximum: string) => string;
    readonly preset: (label: string, length: string) => string;
  };
  readonly richText: Record<RichTextControlId, string>;
};

export type EditorControlId
  = | 'addSection' | 'addSectionTitle' | 'apply' | 'chooseOption'
    | 'colorInvalid'
    | 'colorRemove' | 'cropHint' | 'cropInvalid' | 'cropPosition'
    | 'customSectionInvalidId' | 'customization' | 'deletePhoto'
    | 'deletePhotoDescription' | 'deletePhotoTitle' | 'deleteSection'
    | 'deleteSectionDescription' | 'deleteSectionTitle' | 'entryOrder'
    | 'header' | 'keepObservedPhoto' | 'noChanges' | 'noPhoto'
    | 'opaquePhoto' | 'photoChanged' | 'photoChangedCrop' | 'photoPreview'
    | 'photoPreviewLoading' | 'photoPreviewUnavailable' | 'photoRequestUnknown'
    | 'photoPositionHint'
    | 'photoRequestAttention' | 'photoRetry' | 'photoSessionEnded'
    | 'photoStatusBusy' | 'photoStatusRateLimited'
    | 'photoUploadHint' | 'photoUploading' | 'replacePhoto' | 'retry'
    | 'sectionChanged' | 'sectionCreateDuplicate'
    | 'sectionIssue' | 'sectionMissing' | 'sectionPlacementChanged'
    | 'sectionTypeProfile' | 'sectionTypeWork' | 'sectionTypeEducation'
    | 'sectionTypeSkill' | 'sectionTypeLanguage' | 'sectionTypeCertificate'
    | 'sectionTypeProject' | 'sectionTypeCustom' | 'templateApplied'
    | 'templateChangeProgress'
    | 'templateChangesReady'
    | 'templateChangesReview'
    | 'templateCurrentChanged'
    | 'templateFormatWarning'
    | 'templateMarginWarning'
    | 'templateNeedsAttention'
    | 'templateResultReview' | 'templateSaving' | 'templateSizeWarning'
    | 'templateSaved' | 'templateUndoUnavailable' | 'tryAgainLater'
    | 'uploadPhoto' | 'validationAttention' | 'validationFormat'
    | 'validationRange' | 'warningCustomization' | 'warningPlacement'
    | 'warningAccepted' | 'warningRemains'
    | 'sections' | 'entryOrderChanged' | 'reopenPlacement' | 'reopenOrder'
    | 'reopenCrop' | 'retryRemaining' | 'restorePreApply' | 'keepPartial'
    | 'selectPhoto' | 'imageType' | 'imageLarge' | 'imageInvalid'
    | 'photoPrecondition' | 'observedUnchanged' | 'observedChanged'
    | 'observedUnavailable' | 'replaceObservedPhoto' | 'column'
    | 'cancel' | 'width' | 'height'
    | 'main' | 'sidebar' | 'create' | 'sectionType' | 'sectionName'
    | 'headingIcon' | 'placement' | 'moveUp' | 'moveDown' | 'moveMain'
    | 'moveSidebar' | 'moveStart' | 'moveEntryUp' | 'moveEntryDown'
    | 'removeSurfaceTarget'
    | 'photo' | 'cropPhoto' | 'saveCrop' | 'clearCrop' | 'zoom'
    | 'exactValues' | 'onResume' | 'templates' | 'undoTemplate'
    | 'templatePresets' | 'templateWarnings' | 'templateReadRequired';

type EditorControlCopy = Record<
  Exclude<
    EditorControlId,
    | 'deleteSectionDescription'
    | 'movedToMain'
    | 'movedToSidebar'
    | 'photoStatusBusy'
    | 'photoStatusRateLimited'
    | 'tryAgainLater'
  >,
  string
> & {
  readonly deleteSectionDescription: (key: string) => string;
  readonly movedToMain: (names: string) => string;
  readonly movedToSidebar: (names: string) => string;
  readonly photoStatusBusy: (wait: string) => string;
  readonly photoStatusRateLimited: (wait: string) => string;
  readonly tryAgainLater: (seconds?: number) => string;
};

export type CustomizationGroupId
  = 'page' | 'type' | 'spacing' | 'headings' | 'layout' | 'colors';

export type RichTextControlId
  = | 'toolbar'
    | 'editorLabel'
    | 'paragraph'
    | 'lineBreak'
    | 'bold'
    | 'italic'
    | 'underline'
    | 'orderedList'
    | 'bulletList'
    | 'link'
    | 'linkUrl'
    | 'unlink';

export type EditorControlEnum
  = | 'a4'
    | 'bar'
    | 'center'
    | 'dots'
    | 'header'
    | 'inline'
    | 'justify'
    | 'left'
    | 'letter'
    | 'none'
    | 'normal'
    | 'outline'
    | 'right'
    | 'sidebar'
    | 'stacked'
    | 'tag'
    | 'text'
    | 'titlecase'
    | 'top'
    | 'uppercase';

export const editorControlsCopy: WorkspaceCopy<EditorControlsCopy> = {
  vi: {
    controls: {
      addSection: 'Thêm phần',
      addSectionTitle: 'Thêm phần',
      chooseOption: 'Chọn một trong các tùy chọn có sẵn.',
      colorInvalid: 'Nhập mã màu hex gồm sáu chữ số.',
      colorRemove: 'Xóa',
      removeSurfaceTarget: 'Xóa mục tiêu bề mặt',
      cropHint:
        'Kéo hình vuông để chọn phần hiển thị trên hồ sơ. '
        + 'Bạn cũng có thể dùng phím mũi tên và + hoặc − để thu phóng.',
      cropInvalid: 'Nhập vùng cắt trong phạm vi ảnh.',
      cropPosition: 'Vị trí cắt',
      customSectionInvalidId:
        'Không thể tạo phần tùy chỉnh vì mã được tạo không hợp lệ.',
      customization: 'Tùy chỉnh',
      deletePhotoDescription: 'Xóa ảnh hiện tại?',
      deletePhotoTitle: 'Xóa ảnh',
      deleteSectionDescription: (key: string) =>
        `Thao tác này xóa vĩnh viễn ${key} và các mục trong phần.`,
      movedToMain: (names) => `Đã chuyển vào cột chính: ${names}.`,
      movedToSidebar: (names) => `Đã chuyển vào cột bên: ${names}.`,
      deleteSectionTitle: 'Xóa phần',
      entryOrderChanged: 'Thứ tự mục đã thay đổi. Mở lại thứ tự.',
      header: 'Đầu trang',
      imageInvalid: 'Không thể dùng ảnh này.',
      imageLarge: 'Ảnh này vượt quá kích thước cho phép.',
      imageType: 'Chọn ảnh JPEG hoặc PNG.',
      keepObservedPhoto: 'Giữ ảnh đã thấy',
      keepPartial: 'Giữ phần đã áp dụng',
      noChanges: 'Không có thay đổi',
      noPhoto: 'Chưa có ảnh.',
      observedChanged: 'Ảnh đã thấy đã thay đổi.',
      observedUnavailable: 'Không có ảnh đã thấy.',
      observedUnchanged: 'Ảnh đã thấy không thay đổi.',
      opaquePhoto:
        'Không thể xác nhận việc tải ảnh có thay đổi ảnh của bạn hay không.',
      photoChanged: 'Ảnh đã thay đổi. Mở lại thao tác xóa và xác nhận lại.',
      photoChangedCrop: 'Ảnh đã thay đổi. Mở lại vùng cắt theo ảnh hiện tại.',
      photoPrecondition: 'Ảnh đã thay đổi. Làm mới và thử lại.',
      photoPreview: 'Bản xem trước ảnh được cho phép.',
      photoPreviewLoading: 'Đang tải bản xem trước ảnh.',
      photoPreviewUnavailable: 'Không có bản xem trước ảnh.',
      photoPositionHint: 'Có hiệu lực khi bạn thêm ảnh.',
      photoRequestAttention: 'Yêu cầu ảnh cần được xem lại.',
      photoRequestUnknown: 'Không thể xác nhận yêu cầu ảnh.',
      photoRetry: 'Thử lại yêu cầu ảnh',
      photoSessionEnded: 'Phiên của bạn đã kết thúc. Đăng nhập để tiếp tục.',
      photoStatusBusy: (wait: string) => `Đang xử lý ảnh. ${wait}`,
      photoStatusRateLimited: (wait: string) =>
        `Có quá nhiều yêu cầu ảnh. ${wait}`,
      photoUploadHint: 'JPEG hoặc PNG, tối đa 2 MB.',
      photoUploading: 'Đang tải ảnh lên.',
      reopenCrop: 'Mở lại vùng cắt',
      reopenOrder: 'Mở lại thứ tự',
      reopenPlacement: 'Mở lại vị trí',
      replaceObservedPhoto: 'Thay ảnh',
      restorePreApply: 'Khôi phục trước khi áp dụng',
      retry: 'Thử lại',
      retryRemaining: 'Thử lại phần còn lại',
      sectionChanged:
        'Phần này đã thay đổi. Mở lại thao tác xóa và xác nhận lại.',
      sectionCreateDuplicate: 'Phần này đã tồn tại. Chọn loại phần khác.',
      sectionIssue: 'Xem lại các điều khiển phần được đánh dấu.',
      sectionMissing:
        'Phần này không còn tồn tại. Tạo phần mới hoặc chọn phần khác.',
      sectionPlacementChanged: 'Vị trí phần đã thay đổi. Mở lại vị trí.',
      sectionTypeProfile: 'Hồ sơ',
      sectionTypeWork: 'Kinh nghiệm làm việc',
      sectionTypeEducation: 'Học vấn',
      sectionTypeSkill: 'Kỹ năng',
      sectionTypeLanguage: 'Ngôn ngữ',
      sectionTypeCertificate: 'Chứng chỉ',
      sectionTypeProject: 'Dự án',
      sectionTypeCustom: 'Tùy chỉnh',
      selectPhoto: 'Chọn ảnh thay thế',
      templateApplied: 'Đã áp dụng mẫu',
      templateChangeProgress: 'Tiến độ thay đổi mẫu',
      templateChangesReady: 'Các thay đổi mẫu đã sẵn sàng để lưu.',
      templateChangesReview: 'Các thay đổi mẫu cần được xem lại',
      templateCurrentChanged:
        'Ngữ cảnh hồ sơ đã thay đổi. Xem lại hồ sơ hiện tại trước khi thử lại.',
      templateFormatWarning: 'Định dạng ngày sẽ thay đổi.',
      templateMarginWarning: 'Mẫu này đặt lề dưới 5 mm.',
      templateNeedsAttention: 'Mẫu cần được xem lại',
      templateResultReview: 'Kết quả mẫu cần được xem lại.',
      templateSaving: 'Đang lưu mẫu',
      templateSaved: 'Đã lưu mẫu',
      templateSizeWarning: 'Mẫu này dùng cỡ chữ cơ bản 10 pt.',
      templateUndoUnavailable:
        'Không thể hoàn tác thay đổi mẫu trên hồ sơ hiện tại.',
      tryAgainLater: (seconds?: number) => seconds === undefined
        ? 'Vui lòng thử lại sau.'
        : `Thử lại sau ${seconds} giây.`,
      warningPlacement: 'Thay đổi vị trí',
      warningCustomization: 'Thay đổi tùy chỉnh',
      warningAccepted: 'đã chấp nhận',
      warningRemains: 'còn lại',
      sections: 'Phần',
      entryOrder: 'Thứ tự mục',
      moveEntryUp: 'Di chuyển mục lên',
      moveEntryDown: 'Di chuyển mục xuống',
      sectionName: 'Tên phần',
      headingIcon: 'Biểu tượng tiêu đề',
      placement: 'Điều khiển vị trí phần',
      moveUp: 'Di chuyển lên',
      moveDown: 'Di chuyển xuống',
      moveMain: 'Chuyển vào cột chính',
      moveSidebar: 'Chuyển vào cột bên',
      moveStart: 'Chuyển lên đầu',
      deleteSection: 'Xóa phần',
      sectionType: 'Loại phần',
      column: 'Cột',
      cancel: 'Hủy',
      width: 'Chiều rộng',
      height: 'Chiều cao',
      main: 'Chính',
      sidebar: 'Cột bên',
      create: 'Tạo',
      deletePhoto: 'Xóa ảnh',
      uploadPhoto: 'Tải ảnh lên',
      validationAttention: 'Giá trị này cần được xem lại.',
      validationFormat: 'Nhập giá trị theo định dạng bắt buộc.',
      validationRange: 'Nhập giá trị trong phạm vi cho phép.',
      replacePhoto: 'Thay ảnh',
      photo: 'Ảnh',
      cropPhoto: 'Cắt ảnh',
      saveCrop: 'Lưu ảnh cắt',
      clearCrop: 'Xóa ảnh cắt',
      zoom: 'Thu phóng',
      exactValues: 'Giá trị chính xác',
      onResume: 'Trên hồ sơ của bạn',
      templates: 'Mẫu',
      apply: 'Áp dụng',
      undoTemplate: 'Hoàn tác thay đổi mẫu',
      templatePresets: 'Mẫu có sẵn',
      templateWarnings: 'Cảnh báo mẫu',
      templateReadRequired: 'Tải hồ sơ hiện tại trước khi thử lại.',
    },
    customization: {
      'font.family': 'Phông chữ',
      'font.baseSizePx': 'Cỡ chữ cơ bản (px)',
      'font.textAlign': 'Căn chỉnh văn bản',
      'colors.primary': 'Màu chính',
      'colors.text': 'Màu chữ',
      'colors.background': 'Nền',
      'colors.accent': 'Điểm nhấn',
      'colors.surface': 'Bề mặt',
      'spacing.sectionGap': 'Khoảng cách giữa phần',
      'spacing.entryGap': 'Khoảng cách giữa mục',
      'spacing.lineHeight': 'Giãn dòng',
      'spacing.pageMargin.x': 'Lề ngang',
      'spacing.pageMargin.y': 'Lề dọc',
      'heading.style': 'Kiểu tiêu đề',
      'heading.showRule': 'Đường kẻ tiêu đề',
      'header.align': 'Căn chỉnh đầu trang',
      'header.detailsLayout': 'Bố cục liên hệ',
      'header.iconStyle': 'Kiểu biểu tượng',
      'header.photoPosition': 'Vị trí ảnh',
      'layout.columns': 'Cột',
      'layout.surfaceTarget': 'Nền bề mặt',
      'sectionDisplay.skill.style': 'Cách hiển thị kỹ năng',
      'sectionDisplay.language.style': 'Cách hiển thị ngôn ngữ',
      'pageFormat': 'Khổ giấy',
      'dateFormat': 'Định dạng ngày',
    },
    groups: {
      page: 'Trang và PDF',
      type: 'Chữ',
      spacing: 'Khoảng cách',
      headings: 'Tiêu đề',
      layout: 'Bố cục',
      colors: 'Màu sắc',
    },
    enums: {
      a4: 'A4',
      bar: 'Thanh',
      center: 'Giữa',
      dots: 'Chấm',
      header: 'Đầu trang',
      inline: 'Ngang',
      justify: 'Căn đều',
      left: 'Trái',
      letter: 'Letter',
      none: 'Không',
      normal: 'Chuẩn',
      outline: 'Viền',
      right: 'Phải',
      sidebar: 'Cột bên',
      stacked: 'Xếp chồng',
      tag: 'Thẻ',
      text: 'Văn bản',
      titlecase: 'Viết hoa đầu từ',
      top: 'Trên',
      uppercase: 'VIẾT HOA',
    },
    page: {
      description:
        'Dùng cho PDF và khi in. Trang web tự điều chỉnh theo màn hình.',
      pageSize: 'Khổ giấy',
      margins: 'Lề',
      marginLabels: {
        narrow: 'Hẹp', normal: 'Chuẩn', wide: 'Rộng', custom: 'Tùy chỉnh',
      },
      custom: 'Tùy chỉnh',
      horizontal: (unit) => `Trái và phải (${unit})`,
      vertical: (unit) => `Trên và dưới (${unit})`,
      edge: (length) =>
        `Hầu hết máy in không thể in trong phạm vi ${length} tính từ mép giấy.`,
      invalid: (maximum) => `Nhập giá trị từ 0 đến ${maximum}.`,
      preset: (label, length) => `${label} · ${length}`,
    },
    richText: {
      toolbar: 'Điều khiển văn bản có định dạng',
      editorLabel: 'Văn bản có định dạng',
      paragraph: 'Đoạn văn',
      lineBreak: 'Ngắt dòng',
      bold: 'Đậm',
      italic: 'Nghiêng',
      underline: 'Gạch chân',
      orderedList: 'Danh sách có thứ tự',
      bulletList: 'Danh sách dấu đầu dòng',
      link: 'Liên kết',
      linkUrl: 'URL liên kết',
      unlink: 'Bỏ liên kết',
    },
  },
  en: {
    controls: {
      addSection: 'Add section',
      addSectionTitle: 'Add a section',
      chooseOption: 'Choose one of the available options.',
      colorInvalid: 'Enter a six-digit hex color.',
      colorRemove: 'Remove',
      removeSurfaceTarget: 'Remove surface target',
      cropHint:
        'Drag the square to choose what your resume shows. '
        + 'You can also use the arrow keys and + or − to zoom.',
      cropInvalid: 'Enter a crop within the image bounds.',
      cropPosition: 'Crop position',
      customSectionInvalidId:
        'Cannot create a custom section because its generated ID is invalid.',
      customization: 'Customization',
      deletePhotoDescription: 'Delete the current photo?',
      deletePhotoTitle: 'Delete photo',
      deleteSectionDescription: (key: string) =>
        `This permanently deletes ${key} and its entries.`,
      movedToMain: (names) => `Moved to the main column: ${names}.`,
      movedToSidebar: (names) => `Moved to the sidebar: ${names}.`,
      deleteSectionTitle: 'Delete section',
      entryOrderChanged: 'Entry order changed. Reopen order.',
      header: 'Header',
      imageInvalid: 'This image could not be used.',
      imageLarge: 'This image exceeds the allowed size.',
      imageType: 'Choose a JPEG or PNG image.',
      keepObservedPhoto: 'Keep observed photo',
      keepPartial: 'Keep partial',
      noChanges: 'No changes',
      noPhoto: 'No photo has been added.',
      observedChanged: 'The observed photo changed.',
      observedUnavailable: 'The observed photo is unavailable.',
      observedUnchanged: 'The observed photo is unchanged.',
      opaquePhoto:
        'We could not confirm whether the upload changed your photo.',
      photoChanged:
        'This photo changed. Reopen deletion and confirm again.',
      photoChangedCrop:
        'The photo changed. Reopen crop against the current photo.',
      photoPrecondition: 'The photo changed. Refresh and try again.',
      photoPreview: 'Authorized photo preview.',
      photoPreviewLoading: 'Photo preview is loading.',
      photoPreviewUnavailable: 'Photo preview is unavailable.',
      photoPositionHint: 'Takes effect when you add a photo.',
      photoRequestAttention: 'The photo request needs attention.',
      photoRequestUnknown: 'We could not confirm the photo request.',
      photoRetry: 'Retry photo request',
      photoSessionEnded: 'Your session ended. Sign in to continue.',
      photoStatusBusy: (wait: string) =>
        `Photo processing is busy. ${wait}`,
      photoStatusRateLimited: (wait: string) =>
        `Too many photo requests. ${wait}`,
      photoUploadHint: 'JPEG or PNG, up to 2 MB.',
      photoUploading: 'Uploading photo.',
      reopenCrop: 'Reopen crop',
      reopenOrder: 'Reopen order',
      reopenPlacement: 'Reopen placement',
      replaceObservedPhoto: 'Replace photo',
      restorePreApply: 'Restore pre-apply',
      retry: 'Retry',
      retryRemaining: 'Retry remaining',
      sectionChanged:
        'This section changed. Reopen deletion and confirm again.',
      sectionCreateDuplicate:
        'This section already exists. Choose another section type.',
      sectionIssue: 'Review the highlighted section controls.',
      sectionMissing:
        'This section is no longer available. '
        + 'Create a new section or select another section.',
      sectionPlacementChanged: 'Section placement changed. Reopen placement.',
      sectionTypeProfile: 'Profile',
      sectionTypeWork: 'Work',
      sectionTypeEducation: 'Education',
      sectionTypeSkill: 'Skill',
      sectionTypeLanguage: 'Language',
      sectionTypeCertificate: 'Certificate',
      sectionTypeProject: 'Project',
      sectionTypeCustom: 'Custom',
      selectPhoto: 'Select a replacement photo',
      templateApplied: 'Template applied',
      templateChangeProgress: 'Template change progress',
      templateChangesReady: 'Template changes are ready to save.',
      templateChangesReview: 'Template changes need review',
      templateCurrentChanged:
        'The resume context changed. '
        + 'Review the current resume before trying again.',
      templateFormatWarning: 'Date format will change.',
      templateMarginWarning: 'This template sets margins below 5 mm.',
      templateNeedsAttention: 'Template needs attention',
      templateResultReview: 'The template result needs review.',
      templateSaving: 'Saving template',
      templateSaved: 'Template saved',
      templateSizeWarning: 'This template uses a 10 pt base size.',
      templateUndoUnavailable:
        'Template changes cannot be undone on the current resume.',
      tryAgainLater: (seconds?: number) => seconds === undefined
        ? 'Please try again later.'
        : `Try again in ${seconds} seconds.`,
      warningPlacement: 'Placement change',
      warningCustomization: 'Customization change',
      warningAccepted: 'accepted',
      warningRemains: 'remains',
      sections: 'Sections',
      entryOrder: 'Entry order',
      moveEntryUp: 'Move entry up',
      moveEntryDown: 'Move entry down',
      sectionName: 'Section name',
      headingIcon: 'Heading icon',
      placement: 'Section placement controls',
      moveUp: 'Move up',
      moveDown: 'Move down',
      moveMain: 'Move to main',
      moveSidebar: 'Move to sidebar',
      moveStart: 'Move to start',
      deleteSection: 'Delete section',
      sectionType: 'Section type',
      column: 'Column',
      cancel: 'Cancel',
      width: 'Width',
      height: 'Height',
      main: 'Main',
      sidebar: 'Sidebar',
      create: 'Create',
      deletePhoto: 'Delete photo',
      uploadPhoto: 'Upload photo',
      validationAttention: 'This value needs attention.',
      validationFormat: 'Enter a value in the required format.',
      validationRange: 'Enter a value within the allowed range.',
      replacePhoto: 'Replace photo',
      photo: 'Photo',
      cropPhoto: 'Crop photo',
      saveCrop: 'Save crop',
      clearCrop: 'Clear crop',
      zoom: 'Zoom',
      exactValues: 'Exact values',
      onResume: 'On your resume',
      templates: 'Templates',
      apply: 'Apply',
      undoTemplate: 'Undo template changes',
      templatePresets: 'Template presets',
      templateWarnings: 'Template warnings',
      templateReadRequired: 'Load the current resume before trying again.',
    },
    customization: {
      'font.family': 'Font',
      'font.baseSizePx': 'Base size (px)',
      'font.textAlign': 'Text alignment',
      'colors.primary': 'Primary',
      'colors.text': 'Text',
      'colors.background': 'Background',
      'colors.accent': 'Accent',
      'colors.surface': 'Surface',
      'spacing.sectionGap': 'Section gap',
      'spacing.entryGap': 'Entry gap',
      'spacing.lineHeight': 'Line height',
      'spacing.pageMargin.x': 'Horizontal margin',
      'spacing.pageMargin.y': 'Vertical margin',
      'heading.style': 'Heading style',
      'heading.showRule': 'Heading rule',
      'header.align': 'Header alignment',
      'header.detailsLayout': 'Contact layout',
      'header.iconStyle': 'Icon style',
      'header.photoPosition': 'Photo position',
      'layout.columns': 'Columns',
      'layout.surfaceTarget': 'Surface target',
      'sectionDisplay.skill.style': 'Skill display',
      'sectionDisplay.language.style': 'Language display',
      'pageFormat': 'Page size',
      'dateFormat': 'Date format',
    },
    groups: {
      page: 'Page & PDF',
      type: 'Type',
      spacing: 'Spacing',
      headings: 'Headings',
      layout: 'Layout',
      colors: 'Colors',
    },
    enums: {
      a4: 'A4',
      bar: 'Bar',
      center: 'Center',
      dots: 'Dots',
      header: 'Header',
      inline: 'Inline',
      justify: 'Justify',
      left: 'Left',
      letter: 'Letter',
      none: 'None',
      normal: 'Normal',
      outline: 'Outline',
      right: 'Right',
      sidebar: 'Sidebar',
      stacked: 'Stacked',
      tag: 'Tag',
      text: 'Text',
      titlecase: 'Titlecase',
      top: 'Top',
      uppercase: 'Uppercase',
    },
    page: {
      description:
        'Used for the PDF and printing. The web page adapts to the screen.',
      pageSize: 'Page size',
      margins: 'Margins',
      marginLabels: {
        narrow: 'Narrow', normal: 'Normal', wide: 'Wide', custom: 'Custom',
      },
      custom: 'Custom',
      horizontal: (unit) => `Left and right (${unit})`,
      vertical: (unit) => `Top and bottom (${unit})`,
      edge: (length) =>
        `Most printers cannot print within ${length} of the paper edge.`,
      invalid: (maximum) => `Enter a value from 0 to ${maximum}.`,
      preset: (label, length) => `${label} · ${length}`,
    },
    richText: {
      toolbar: 'Rich-text controls',
      editorLabel: 'Rich text',
      paragraph: 'Paragraph',
      lineBreak: 'Line break',
      bold: 'Bold',
      italic: 'Italic',
      underline: 'Underline',
      orderedList: 'Ordered list',
      bulletList: 'Bullet list',
      link: 'Link',
      linkUrl: 'Link URL',
      unlink: 'Unlink',
    },
  },
};

const pageSizeNames: Record<PageFormat, string> = {
  a4: 'A4 · 210 × 297 mm',
  letter: 'Letter · 8.5 × 11 in',
};

export function pageSizeLabel(_locale: Locale, format: PageFormat): string {
  return pageSizeNames[format];
}

export function pageSizeHelp(locale: Locale, format: PageFormat): string {
  const label = pageSizeLabel(locale, format);
  return locale === 'vi' ? `Khổ giấy PDF: ${label}` : `PDF page size: ${label}`;
}
