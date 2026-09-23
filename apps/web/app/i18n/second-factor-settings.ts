import type {
  SecondFactorSettingsErrorKind,
} from '../composables/secondFactorSettings';
import type { TotpSettingsErrorKind } from '../composables/totpSettings';
import type { WorkspaceCopy } from './workspace';

export type SecondFactorSettingsMessage
  = | SecondFactorSettingsErrorKind
    | 'cancelled'
    | 'current-password-required'
    | 'reauth-failed';

export type SecondFactorSettingsCopy = {
  readonly title: string;
  readonly description: string;
  readonly unsupported: string;
  readonly emptyTitle: string;
  readonly emptyDescription: string;
  readonly addPasskey: string;
  readonly adding: string;
  readonly created: string;
  readonly lastUsed: string;
  readonly neverUsed: string;
  readonly removePasskey: string;
  readonly removeTitle: string;
  readonly removeDescription: string;
  readonly removeDescriptionFinal: string;
  readonly removeConfirm: string;
  readonly cancel: string;
  readonly close: string;
  readonly passkeyAdded: string;
  readonly passkeyRemoved: string;
  readonly recoveryTitle: string;
  readonly recoveryRemaining: (count: number) => string;
  readonly regenerate: string;
  readonly regenerateTitle: string;
  readonly regenerateDescription: string;
  readonly regenerateConfirm: string;
  readonly revealEnrolledTitle: string;
  readonly revealRegeneratedTitle: string;
  readonly revealDescription: string;
  readonly copyCodes: string;
  readonly copied: string;
  readonly downloadCodes: string;
  readonly closeReveal: string;
  readonly reauthPasswordDescription: string;
  readonly currentPassword: string;
  readonly continueLabel: string;
  readonly checking: string;
  readonly reauthProviderDescription: string;
  readonly continueWithProvider: (provider: string) => string;
  readonly errors: Readonly<Record<SecondFactorSettingsMessage, string>>;
};

export const secondFactorSettingsCopy: WorkspaceCopy<
  SecondFactorSettingsCopy
> = {
  vi: {
    title: 'Passkey',
    description: 'Passkey là lớp xác thực thứ hai gắn với thiết bị của bạn.',
    unsupported: 'Trình duyệt này không hỗ trợ passkey.',
    emptyTitle: 'Chưa có passkey nào.',
    emptyDescription:
      'Thêm passkey để bảo vệ tài khoản bằng một lớp xác thực thứ hai.',
    addPasskey: 'Thêm passkey',
    adding: 'Đang thêm…',
    created: 'Đã tạo ngày',
    lastUsed: 'Dùng lần cuối vào',
    neverUsed: 'Chưa từng dùng',
    removePasskey: 'Xoá',
    removeTitle: 'Xoá passkey',
    removeDescription:
      'Xoá passkey này? Mọi thiết bị và tác nhân đã kết nối khác sẽ bị đăng '
      + 'xuất.',
    removeDescriptionFinal:
      'Đây là passkey cuối cùng của bạn. Xoá nó sẽ tắt xác thực hai lớp và '
      + 'xoá các mã khôi phục. Mọi thiết bị và tác nhân đã kết nối khác sẽ '
      + 'bị đăng xuất.',
    removeConfirm: 'Xoá passkey',
    cancel: 'Hủy',
    close: 'Đóng',
    passkeyAdded: 'Đã thêm passkey.',
    passkeyRemoved: 'Đã xoá passkey.',
    recoveryTitle: 'Mã khôi phục',
    recoveryRemaining: (count) => `Còn lại ${count} mã khôi phục.`,
    regenerate: 'Tạo lại mã khôi phục',
    regenerateTitle: 'Tạo lại mã khôi phục',
    regenerateDescription:
      'Tạo lại sẽ vô hiệu các mã khôi phục cũ và đăng xuất mọi thiết bị, '
      + 'tác nhân đã kết nối khác.',
    regenerateConfirm: 'Tạo lại',
    revealEnrolledTitle: 'Lưu mã khôi phục của bạn',
    revealRegeneratedTitle: 'Mã khôi phục mới của bạn',
    revealDescription:
      'Mỗi mã chỉ dùng được một lần. Đây là lần duy nhất mã được hiển thị; '
      + 'hãy lưu lại trước khi đóng.',
    copyCodes: 'Sao chép',
    copied: 'Đã sao chép.',
    downloadCodes: 'Tải xuống',
    closeReveal: 'Tôi đã lưu các mã này',
    reauthPasswordDescription: 'Nhập mật khẩu hiện tại để xác nhận danh tính.',
    currentPassword: 'Mật khẩu hiện tại',
    continueLabel: 'Tiếp tục',
    checking: 'Đang kiểm tra…',
    reauthProviderDescription:
      'Đăng nhập lại với nhà cung cấp để xác nhận danh tính.',
    continueWithProvider: (provider) => `Tiếp tục với ${provider}`,
    errors: {
      'reauth-required':
        'Đăng nhập lại để xác nhận danh tính trước khi tiếp tục.',
      'enrollment-closed': 'Việc thêm passkey mới hiện đang tắt.',
      'limit-reached': 'Bạn đã đạt số passkey tối đa.',
      'challenge-invalid': 'Yêu cầu đã hết hạn. Hãy thử lại.',
      'verification-failed': 'Không thể xác minh passkey đó. Hãy thử lại.',
      'not-found': 'Không tìm thấy passkey đó.',
      'rate-limited': 'Bạn đã thử quá nhiều lần. Hãy thử lại sau.',
      'unavailable': 'Đã có lỗi. Hãy thử lại.',
      'cancelled': 'Đã hủy.',
      'current-password-required': 'Nhập mật khẩu hiện tại.',
      'reauth-failed': 'Mật khẩu không đúng.',
    },
  },
  en: {
    title: 'Passkeys',
    description: 'Passkeys are a second factor bound to your device.',
    unsupported: 'This browser doesn\'t support passkeys.',
    emptyTitle: 'No passkeys yet.',
    emptyDescription:
      'Add a passkey to protect your account with a second factor.',
    addPasskey: 'Add a passkey',
    adding: 'Adding…',
    created: 'Created',
    lastUsed: 'Last used',
    neverUsed: 'Last used Never',
    removePasskey: 'Remove',
    removeTitle: 'Remove passkey',
    removeDescription:
      'Remove this passkey? Every other device and connected agent will be '
      + 'signed out.',
    removeDescriptionFinal:
      'This is your last passkey. Removing it turns off two-factor sign-in '
      + 'and deletes your recovery codes. Every other device and connected '
      + 'agent will be signed out.',
    removeConfirm: 'Remove passkey',
    cancel: 'Cancel',
    close: 'Close',
    passkeyAdded: 'Passkey added.',
    passkeyRemoved: 'Passkey removed.',
    recoveryTitle: 'Recovery codes',
    recoveryRemaining: (count) => `${count} recovery codes remaining.`,
    regenerate: 'Regenerate recovery codes',
    regenerateTitle: 'Regenerate recovery codes',
    regenerateDescription:
      'Regenerating invalidates your old recovery codes and signs out every '
      + 'other device and connected agent.',
    regenerateConfirm: 'Regenerate',
    revealEnrolledTitle: 'Save your recovery codes',
    revealRegeneratedTitle: 'Your new recovery codes',
    revealDescription:
      'Each code works once. This is the only time they are shown, so save '
      + 'them before closing.',
    copyCodes: 'Copy',
    copied: 'Copied.',
    downloadCodes: 'Download',
    closeReveal: 'I\'ve saved these codes',
    reauthPasswordDescription:
      'Enter your current password to confirm it\'s you.',
    currentPassword: 'Current password',
    continueLabel: 'Continue',
    checking: 'Checking…',
    reauthProviderDescription:
      'Sign in again with your provider to confirm it\'s you.',
    continueWithProvider: (provider) => `Continue with ${provider}`,
    errors: {
      'reauth-required':
        'Sign in again to confirm it\'s you before continuing.',
      'enrollment-closed': 'Adding a new passkey is turned off right now.',
      'limit-reached': 'You have reached the passkey limit.',
      'challenge-invalid': 'That request expired. Try again.',
      'verification-failed': 'That passkey could not be verified. Try again.',
      'not-found': 'That passkey could not be found.',
      'rate-limited': 'Too many attempts. Try again later.',
      'unavailable': 'Something went wrong. Please try again.',
      'cancelled': 'That was cancelled.',
      'current-password-required': 'Enter your current password.',
      'reauth-failed': 'Incorrect password.',
    },
  },
};

export type TotpSettingsCopy = {
  readonly title: string;
  readonly description: string;
  readonly passkeyRecommendation: string;
  readonly statusNotSetUp: string;
  readonly statusEnabled: string;
  readonly setUpButton: string;
  readonly replaceButton: string;
  readonly removeButton: string;
  readonly starting: string;
  readonly setupTitleNew: string;
  readonly setupTitleReplace: string;
  readonly setupDescription: string;
  readonly replaceNotice: string;
  readonly qrAlt: string;
  readonly secretLabel: string;
  readonly copySecret: string;
  readonly secretCopied: string;
  readonly codeLabel: string;
  readonly invalidFormat: string;
  readonly verifyButton: string;
  readonly verifying: string;
  readonly cancel: string;
  readonly close: string;
  readonly addedNotice: string;
  readonly replacedNotice: string;
  readonly removedNotice: string;
  readonly removeTitle: string;
  readonly removeDescription: string;
  readonly removeDescriptionFinal: string;
  readonly removeConfirm: string;
  readonly errors: Readonly<Record<TotpSettingsErrorKind, string>>;
};

export const totpSettingsCopy: WorkspaceCopy<TotpSettingsCopy> = {
  vi: {
    title: 'Ứng dụng xác thực',
    description:
      'Mã từ ứng dụng xác thực là lớp bảo vệ thứ hai cho tài khoản. Đây '
      + 'không phải phương thức chống lừa đảo; nếu trình duyệt hỗ trợ, hãy '
      + 'ưu tiên dùng passkey.',
    passkeyRecommendation:
      'Trình duyệt này hỗ trợ passkey, một lựa chọn chống lừa đảo tốt hơn.',
    statusNotSetUp: 'Chưa thiết lập.',
    statusEnabled: 'Đã bật.',
    setUpButton: 'Thiết lập ứng dụng xác thực',
    replaceButton: 'Thay ứng dụng xác thực',
    removeButton: 'Xoá',
    starting: 'Đang bắt đầu…',
    setupTitleNew: 'Thiết lập ứng dụng xác thực',
    setupTitleReplace: 'Thay ứng dụng xác thực',
    setupDescription:
      'Quét mã QR bằng ứng dụng xác thực, hoặc nhập mã bí mật theo cách '
      + 'thủ công.',
    replaceNotice:
      'Ứng dụng xác thực hiện tại vẫn hoạt động cho đến khi bạn hoàn tất '
      + 'bước này.',
    qrAlt: 'Mã QR thiết lập ứng dụng xác thực',
    secretLabel: 'Mã bí mật',
    copySecret: 'Sao chép mã bí mật',
    secretCopied: 'Đã sao chép mã bí mật.',
    codeLabel: 'Mã 6 chữ số',
    invalidFormat: 'Nhập đủ 6 chữ số.',
    verifyButton: 'Xác minh',
    verifying: 'Đang xác minh…',
    cancel: 'Hủy',
    close: 'Đóng',
    addedNotice: 'Đã thêm ứng dụng xác thực.',
    replacedNotice: 'Đã thay ứng dụng xác thực.',
    removedNotice: 'Đã xoá ứng dụng xác thực.',
    removeTitle: 'Xoá ứng dụng xác thực',
    removeDescription:
      'Xoá ứng dụng xác thực này? Mọi thiết bị và tác nhân đã kết nối khác '
      + 'sẽ bị đăng xuất.',
    removeDescriptionFinal:
      'Đây là phương thức xác thực hai lớp cuối cùng của bạn. Xoá nó sẽ '
      + 'tắt xác thực hai lớp và xoá các mã khôi phục. Mọi thiết bị và tác '
      + 'nhân đã kết nối khác sẽ bị đăng xuất.',
    removeConfirm: 'Xoá',
    errors: {
      'reauth-required':
        'Đăng nhập lại để xác nhận danh tính trước khi tiếp tục.',
      'closed': 'Việc thiết lập ứng dụng xác thực hiện đang tắt.',
      'expired': 'Yêu cầu thiết lập đã hết hạn. Hãy bắt đầu lại.',
      'invalid-code': 'Mã không đúng. Hãy thử lại.',
      'not-found': 'Không tìm thấy ứng dụng xác thực nào.',
      'rate-limited': 'Bạn đã thử quá nhiều lần. Hãy thử lại sau.',
      'unavailable': 'Đã có lỗi. Hãy thử lại.',
    },
  },
  en: {
    title: 'Authenticator app',
    description:
      'An authenticator app code is a second layer of protection. It is '
      + 'not phishing-resistant; use a passkey instead when your browser '
      + 'supports one.',
    passkeyRecommendation:
      'This browser supports passkeys, a more phishing-resistant choice.',
    statusNotSetUp: 'Not set up.',
    statusEnabled: 'Enabled.',
    setUpButton: 'Set up authenticator app',
    replaceButton: 'Replace authenticator app',
    removeButton: 'Remove',
    starting: 'Starting…',
    setupTitleNew: 'Set up authenticator app',
    setupTitleReplace: 'Replace authenticator app',
    setupDescription:
      'Scan the QR code with your authenticator app, or enter the secret '
      + 'key by hand.',
    replaceNotice:
      'Your current authenticator app code keeps working until you finish '
      + 'this step.',
    qrAlt: 'Authenticator app setup QR code',
    secretLabel: 'Secret key',
    copySecret: 'Copy secret key',
    secretCopied: 'Secret key copied.',
    codeLabel: '6-digit code',
    invalidFormat: 'Enter all 6 digits.',
    verifyButton: 'Verify',
    verifying: 'Verifying…',
    cancel: 'Cancel',
    close: 'Close',
    addedNotice: 'Authenticator app added.',
    replacedNotice: 'Authenticator app replaced.',
    removedNotice: 'Authenticator app removed.',
    removeTitle: 'Remove authenticator app',
    removeDescription:
      'Remove this authenticator app? Every other device and connected '
      + 'agent will be signed out.',
    removeDescriptionFinal:
      'This is your last second factor. Removing it turns off two-factor '
      + 'sign-in and deletes your recovery codes. Every other device and '
      + 'connected agent will be signed out.',
    removeConfirm: 'Remove',
    errors: {
      'reauth-required':
        'Sign in again to confirm it\'s you before continuing.',
      'closed': 'Setting up an authenticator app is turned off right now.',
      'expired': 'That setup request expired. Start again.',
      'invalid-code': 'That code could not be verified. Try again.',
      'not-found': 'No authenticator app was found.',
      'rate-limited': 'Too many attempts. Try again later.',
      'unavailable': 'Something went wrong. Please try again.',
    },
  },
};
