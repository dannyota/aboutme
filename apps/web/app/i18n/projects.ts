// Copy for the owner's Projects pages (/app/projects and /app/projects/{id}),
// specified in docs/design/ui/projects.md.
import type { WorkspaceCopy } from './workspace';

export type ProjectReauthCopy = {
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
  readonly secondFactorIntro: string;
  readonly secondFactorLink: string;
  readonly secondFactorContinue: string;
  readonly secondFactorIncomplete: string;
};

export type ProjectsCopy = {
  readonly title: string;
  readonly lead: string;
  readonly count: (count: number, limit: number) => string;
  readonly newProject: string;
  readonly limitReached: (limit: number) => string;
  readonly empty: string;
  readonly loading: string;
  readonly unavailable: string;
  readonly retry: string;
  readonly listLabel: string;
  readonly updated: (time: string) => string;
  readonly moreActions: (name: string) => string;
  readonly technologiesLabel: (name: string) => string;
  readonly languagesLabel: (name: string) => string;
  readonly moreTechnologies: (count: number) => string;
  readonly languageChip: (code: string, isDefault: boolean) => string;
  readonly defaultTab: (name: string) => string;
  readonly untitled: string;
  readonly changeAddress: string;
  readonly delete: string;
  readonly cancel: string;
  readonly close: string;
  readonly createTitle: string;
  readonly nameLabel: string;
  readonly create: string;
  readonly createInvalid: string;
  readonly tryAgain: string;
  readonly sessionLost: string;
  readonly breadcrumb: string;
  readonly loadingProject: string;
  readonly notFoundTitle: string;
  readonly notFoundDescription: string;
  readonly backToProjects: string;
  readonly projectUnavailable: string;
  readonly textSection: string;
  readonly languageTabs: string;
  readonly addLanguage: string;
  readonly languageActions: (language: string) => string;
  readonly makeDefault: string;
  readonly removeLanguage: string;
  readonly fields: {
    readonly name: string;
    readonly role: string;
    readonly summary: string;
    readonly outcome: string;
    readonly description: string;
  };
  readonly counter: (count: number, limit: number) => string;
  readonly detailsSection: string;
  readonly repositoryUrl: string;
  readonly demoUrl: string;
  readonly linkHint: string;
  readonly technologies: string;
  readonly technologiesHint: string;
  readonly technologiesFull: (limit: number) => string;
  readonly technologyTooLong: (limit: number) => string;
  readonly removeTechnology: (name: string) => string;
  readonly addressSection: string;
  readonly addressLabel: string;
  readonly deleteSection: string;
  readonly deleteHelp: string;
  readonly deleteProject: string;
  readonly saveBar: string;
  readonly save: string;
  readonly errors: {
    readonly nameRequired: string;
    readonly tooLong: (limit: number) => string;
    readonly descriptionTooLong: string;
    readonly https: string;
    readonly technologies: (count: number, length: number) => string;
  };
  readonly changedElsewhere: string;
  readonly reload: string;
  readonly saveInvalid: string;
  readonly addressTitle: string;
  readonly newAddress: string;
  readonly addressHint: string;
  readonly addressNote: string;
  readonly addressInvalid: string;
  readonly slugTaken: string;
  readonly slugHeld: string;
  readonly deleteTitle: string;
  readonly deleteDescription: (name: string) => string;
  readonly notFound: string;
  readonly reauthIntro: string;
  readonly noReauthMethod: string;
  readonly reauth: ProjectReauthCopy;
  readonly working: string;
  readonly addLanguageTitle: string;
  readonly languageLabel: string;
  readonly otherLanguage: string;
  readonly languageCode: string;
  readonly languageCodeHint: string;
  readonly languageCodeError: string;
  readonly add: string;
  readonly languageName: (key: string) => string;
  readonly leaveTitle: string;
  readonly stay: string;
  readonly leave: string;
};

const NAMES: Readonly<Record<string, string>> = {
  vi: 'Tiếng Việt',
  en: 'English',
};

export const projectsCopy: WorkspaceCopy<ProjectsCopy> = {
  vi: {
    title: 'Dự án',
    lead: 'Viết mỗi dự án một lần, rồi chọn CV nào hiển thị nó.',
    count: (count, limit) => `${count} / ${limit}`,
    newProject: 'Dự án mới',
    limitReached: (limit) => `Bạn đã đạt giới hạn ${limit} dự án.`,
    empty: 'Chưa có dự án nào.',
    loading: 'Đang tải dự án.',
    unavailable: 'Dự án hiện không khả dụng.',
    retry: 'Thử lại',
    listLabel: 'Dự án của bạn',
    updated: (time) => `Cập nhật ${time}`,
    moreActions: (name) => `Thao tác khác cho ${name}`,
    technologiesLabel: (name) => `Công nghệ của ${name}`,
    languagesLabel: (name) => `Ngôn ngữ của ${name}`,
    moreTechnologies: (count) => `+${count}`,
    languageChip: (code, isDefault) =>
      (isDefault ? `${code} · mặc định` : code),
    defaultTab: (name) => `${name} · mặc định`,
    untitled: 'Dự án chưa có tên',
    changeAddress: 'Đổi địa chỉ',
    delete: 'Xóa',
    cancel: 'Hủy',
    close: 'Đóng',
    createTitle: 'Dự án mới',
    nameLabel: 'Tên',
    create: 'Tạo',
    createInvalid: 'Hãy nhập tên dự án, tối đa 120 ký tự.',
    tryAgain: 'Không thể hoàn tất lúc này. Hãy thử lại.',
    sessionLost: 'Phiên đăng nhập đã kết thúc. Hãy đăng nhập lại.',
    breadcrumb: 'Đường dẫn',
    loadingProject: 'Đang tải dự án.',
    notFoundTitle: 'Không tìm thấy dự án',
    notFoundDescription: 'Dự án này không tồn tại hoặc đã bị xóa.',
    backToProjects: 'Quay lại Dự án',
    projectUnavailable: 'Không thể mở dự án này. Hãy thử lại.',
    textSection: 'Nội dung',
    languageTabs: 'Ngôn ngữ của dự án',
    addLanguage: 'Thêm ngôn ngữ',
    languageActions: (language) => `Thao tác cho ${language}`,
    makeDefault: 'Đặt làm mặc định',
    removeLanguage: 'Xóa ngôn ngữ',
    fields: {
      name: 'Tên',
      role: 'Vai trò',
      summary: 'Tóm tắt',
      outcome: 'Kết quả',
      description: 'Mô tả',
    },
    counter: (count, limit) => `${count} / ${limit} ký tự`,
    detailsSection: 'Chi tiết',
    repositoryUrl: 'Liên kết mã nguồn',
    demoUrl: 'Liên kết demo',
    linkHint: 'Địa chỉ bắt đầu bằng https://',
    technologies: 'Công nghệ',
    technologiesHint: 'Nhấn Enter hoặc dấu phẩy để thêm.',
    technologiesFull: (limit) => `Đã đủ ${limit} công nghệ.`,
    technologyTooLong: (limit) => `Mỗi công nghệ tối đa ${limit} ký tự.`,
    removeTechnology: (name) => `Xóa ${name}`,
    addressSection: 'Địa chỉ',
    addressLabel: 'Địa chỉ dự án',
    deleteSection: 'Xóa',
    deleteHelp: 'Xóa dự án khỏi thư viện của bạn.',
    deleteProject: 'Xóa dự án',
    saveBar: 'Lưu dự án',
    save: 'Lưu',
    errors: {
      nameRequired: 'Hãy nhập tên cho ngôn ngữ mặc định.',
      tooLong: (limit) => `Tối đa ${limit} ký tự.`,
      descriptionTooLong: 'Mô tả quá dài.',
      https: 'Dùng địa chỉ bắt đầu bằng https://.',
      technologies: (count, length) =>
        `Tối đa ${count} công nghệ, mỗi công nghệ tối đa ${length} ký tự.`,
    },
    changedElsewhere:
      'Dự án đã thay đổi ở nơi khác. Nội dung bạn nhập vẫn được giữ.',
    reload: 'Tải lại',
    saveInvalid:
      'Không thể lưu dự án này. Hãy kiểm tra các trường rồi thử lại.',
    addressTitle: 'Đổi địa chỉ',
    newAddress: 'Địa chỉ mới',
    addressHint:
      'Từ 2 đến 40 ký tự: chữ thường không dấu, số và dấu gạch ngang đơn.',
    addressNote: 'Liên kết tới địa chỉ cũ sẽ ngừng hoạt động.',
    addressInvalid: 'Địa chỉ này không đúng định dạng.',
    slugTaken: 'Địa chỉ này đang được dùng.',
    slugHeld:
      'Tài khoản này đã dùng địa chỉ này gần đây. Hãy chọn địa chỉ khác.',
    deleteTitle: 'Xóa dự án',
    deleteDescription: (name) =>
      `Xóa “${name}”? Thao tác này không thể hoàn tác.`,
    notFound: 'Dự án này không còn tồn tại.',
    reauthIntro: 'Thao tác này cần xác thực lại.',
    noReauthMethod:
      'Không có cách xác thực lại phù hợp. Hãy đặt mật khẩu trong Cài đặt.',
    reauth: {
      currentPassword: 'Mật khẩu hiện tại',
      passwordAction: 'Xác thực lại và tiếp tục',
      providerContinue:
        'Tiếp tục với nhà cung cấp đã liên kết để xác thực lại.',
      providerStart: 'Bắt đầu xác thực lại với nhà cung cấp',
      providerRetryAction: 'Thử xác thực lại với nhà cung cấp',
      providerLink: 'Tiếp tục xác thực lại trong thẻ mới',
      providerReturn:
        'Hoàn tất xác thực lại trong thẻ mới, quay lại đây rồi chọn Thử lại.',
      providerRetry: 'Thử lại',
      wrongPassword: 'Mật khẩu không được chấp nhận. Hãy thử lại.',
      reauthRateLimited: 'Xác thực lại đang bị giới hạn. Hãy thử lại sau.',
      reauthUnavailable: 'Không thể xác thực lại. Hãy thử lại sau.',
      secondFactorIntro:
        'Tài khoản của bạn có bước xác minh thứ hai. Hoàn tất bước đó '
        + 'trong tab mới, rồi quay lại đây.',
      secondFactorLink: 'Mở bước xác minh',
      secondFactorContinue: 'Tiếp tục',
      secondFactorIncomplete: 'Bước xác minh thứ hai chưa hoàn tất.',
    },
    working: 'Đang xử lý…',
    addLanguageTitle: 'Thêm ngôn ngữ',
    languageLabel: 'Ngôn ngữ',
    otherLanguage: 'Ngôn ngữ khác',
    languageCode: 'Mã ngôn ngữ',
    languageCodeHint: 'Mã hai đến tám chữ cái thường, như ja hoặc ko.',
    languageCodeError: 'Hãy nhập mã ngôn ngữ chưa có trong dự án.',
    add: 'Thêm',
    languageName: (key) => NAMES[key] ?? key,
    leaveTitle: 'Bạn có thay đổi chưa lưu.',
    stay: 'Ở lại',
    leave: 'Rời đi không lưu',
  },
  en: {
    title: 'Projects',
    lead: 'Write each project once, then choose which resumes show it.',
    count: (count, limit) => `${count} / ${limit}`,
    newProject: 'New project',
    limitReached: (limit) => `You have reached the limit of ${limit} projects.`,
    empty: 'No projects yet.',
    loading: 'Loading projects.',
    unavailable: 'Projects are unavailable right now.',
    retry: 'Retry',
    listLabel: 'Your projects',
    updated: (time) => `Updated ${time}`,
    moreActions: (name) => `More actions for ${name}`,
    technologiesLabel: (name) => `Technologies of ${name}`,
    languagesLabel: (name) => `Languages of ${name}`,
    moreTechnologies: (count) => `+${count}`,
    languageChip: (code, isDefault) =>
      (isDefault ? `${code} · default` : code),
    defaultTab: (name) => `${name} · default`,
    untitled: 'Untitled project',
    changeAddress: 'Change address',
    delete: 'Delete',
    cancel: 'Cancel',
    close: 'Close',
    createTitle: 'New project',
    nameLabel: 'Name',
    create: 'Create',
    createInvalid: 'Enter a project name of at most 120 characters.',
    tryAgain: 'That could not finish right now. Try again.',
    sessionLost: 'Your session has ended. Sign in again.',
    breadcrumb: 'Breadcrumb',
    loadingProject: 'Loading the project.',
    notFoundTitle: 'Project not found',
    notFoundDescription: 'This project does not exist or was deleted.',
    backToProjects: 'Back to Projects',
    projectUnavailable: 'This project could not be opened. Try again.',
    textSection: 'Text',
    languageTabs: 'Project languages',
    addLanguage: 'Add language',
    languageActions: (language) => `Actions for ${language}`,
    makeDefault: 'Make default',
    removeLanguage: 'Remove language',
    fields: {
      name: 'Name',
      role: 'Role',
      summary: 'Summary',
      outcome: 'Outcome',
      description: 'Description',
    },
    counter: (count, limit) => `${count} / ${limit} characters`,
    detailsSection: 'Details',
    repositoryUrl: 'Repository link',
    demoUrl: 'Demo link',
    linkHint: 'An address that starts with https://',
    technologies: 'Technologies',
    technologiesHint: 'Press Enter or a comma to add one.',
    technologiesFull: (limit) => `All ${limit} technologies are used.`,
    technologyTooLong: (limit) =>
      `Each technology is at most ${limit} characters.`,
    removeTechnology: (name) => `Remove ${name}`,
    addressSection: 'Address',
    addressLabel: 'Project address',
    deleteSection: 'Delete',
    deleteHelp: 'Delete the project from your library.',
    deleteProject: 'Delete project',
    saveBar: 'Save the project',
    save: 'Save',
    errors: {
      nameRequired: 'Enter a name in the default language.',
      tooLong: (limit) => `Use at most ${limit} characters.`,
      descriptionTooLong: 'The description is too long.',
      https: 'Use an address that starts with https://.',
      technologies: (count, length) =>
        `Use at most ${count} technologies of at most ${length} characters.`,
    },
    changedElsewhere:
      'This project changed elsewhere. Your typed text is kept.',
    reload: 'Reload',
    saveInvalid:
      'This project could not be saved. Check the fields and try again.',
    addressTitle: 'Change address',
    newAddress: 'New address',
    addressHint:
      '2 to 40 characters: lowercase letters, digits and single hyphens.',
    addressNote: 'Links to the old address stop working.',
    addressInvalid: 'This address does not match the format.',
    slugTaken: 'This address is in use.',
    slugHeld: 'This account used this address recently. Choose another.',
    deleteTitle: 'Delete project',
    deleteDescription: (name) => `Delete “${name}”? This cannot be undone.`,
    notFound: 'This project no longer exists.',
    reauthIntro: 'This action needs you to reauthenticate.',
    noReauthMethod:
      'No supported reauthentication method is available. '
      + 'Set a password in Settings.',
    reauth: {
      currentPassword: 'Current password',
      passwordAction: 'Reauthenticate and continue',
      providerContinue: 'Continue with your linked provider to reauthenticate.',
      providerStart: 'Start provider reauthentication',
      providerRetryAction: 'Try provider reauthentication again',
      providerLink: 'Continue reauthentication in a new tab',
      providerReturn:
        'Finish reauthentication in the new tab, return here, '
        + 'then choose Retry.',
      providerRetry: 'Retry',
      wrongPassword: 'That password was not accepted. Try again.',
      reauthRateLimited:
        'Reauthentication is rate limited. Try again later.',
      reauthUnavailable: 'Reauthentication is unavailable. Try again later.',
      secondFactorIntro:
        'Your account has a second verification step. Complete it in the '
        + 'new tab, then come back here.',
      secondFactorLink: 'Open verification step',
      secondFactorContinue: 'Continue',
      secondFactorIncomplete:
        'The second verification step is not complete yet.',
    },
    working: 'Working…',
    addLanguageTitle: 'Add language',
    languageLabel: 'Language',
    otherLanguage: 'Another language',
    languageCode: 'Language code',
    languageCodeHint: 'Two to eight lowercase letters, such as ja or ko.',
    languageCodeError: 'Enter a language code the project does not have yet.',
    add: 'Add',
    languageName: (key) => NAMES[key] ?? key,
    leaveTitle: 'You have unsaved changes.',
    stay: 'Stay',
    leave: 'Leave without saving',
  },
};
