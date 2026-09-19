// Account-page copy: sign in, registration, password recovery and reset,
// and email verification. Failures are closed client-side messages mapped
// from server codes, so every message lives here in both languages.
import type { Locale } from './locale';

export type AuthMessage
  = | 'enterEmailAndPassword'
    | 'invalidCredentials'
    | 'checkEmailAndPassword'
    | 'fillAllFields'
    | 'checkDetails'
    | 'passwordsDoNotMatch'
    | 'passwordLength'
    | 'passwordCommon'
    | 'passwordBreached'
    | 'passwordInvalid'
    | 'rateLimited'
    | 'unavailable'
    | 'enterEmail'
    | 'resetRequested'
    | 'enterNewPassword'
    | 'resetLinkIncomplete'
    | 'resetLinkExpired'
    | 'verifyLinkIncomplete'
    | 'verifyLinkExpired'
    | 'providerFailed'
    | 'providerEmailNotVerified'
    | 'providerCancelled'
    | 'providerEmailRegistered';

type Provider = 'google' | 'github' | 'linkedin';

export type AuthCopy = {
  readonly messages: Readonly<Record<AuthMessage, string>>;
  readonly email: string;
  readonly password: string;
  readonly newPassword: string;
  readonly confirmPassword: string;
  readonly name: string;
  readonly showPassword: (label: string) => string;
  readonly hidePassword: (label: string) => string;
  readonly signIn: string;
  readonly createAccount: string;
  readonly or: string;
  readonly providers: Readonly<Record<Provider, string>>;
  readonly login: {
    readonly lead: string;
    readonly pending: string;
    readonly forgotPassword: string;
  };
  readonly register: {
    readonly lead: string;
    readonly pending: string;
    readonly success: string;
    readonly afterVerify: string;
    readonly haveAccount: string;
    readonly noEmail: string;
    readonly noEmailGoogle: string;
    readonly closed: string;
  };
  readonly forgot: {
    readonly title: string;
    readonly lead: string;
    readonly submit: string;
    readonly pending: string;
    readonly backToSignIn: string;
  };
  readonly reset: {
    readonly title: string;
    readonly lead: string;
    readonly submit: string;
    readonly pending: string;
    readonly success: string;
  };
  readonly verify: {
    readonly title: string;
    readonly lead: string;
    readonly pending: string;
    readonly success: string;
    readonly useGoogle: string;
  };
};

export const authCopy: Record<Locale, AuthCopy> = {
  vi: {
    messages: {
      enterEmailAndPassword: 'Nhập email và mật khẩu.',
      invalidCredentials: 'Email hoặc mật khẩu không đúng.',
      checkEmailAndPassword: 'Kiểm tra email và mật khẩu rồi thử lại.',
      fillAllFields: 'Hãy điền đủ các trường.',
      checkDetails: 'Kiểm tra thông tin rồi thử lại.',
      passwordsDoNotMatch: 'Mật khẩu không khớp.',
      passwordLength: 'Mật khẩu phải có ít nhất 12 ký tự.',
      passwordCommon: 'Mật khẩu này quá phổ biến. Hãy chọn mật khẩu khác.',
      passwordBreached:
        'Mật khẩu này đã bị lộ trong một vụ rò rỉ dữ liệu. Hãy chọn mật khẩu '
        + 'khác.',
      passwordInvalid: 'Mật khẩu chưa đáp ứng yêu cầu.',
      rateLimited: 'Bạn đã thử quá nhiều lần. Hãy thử lại sau.',
      unavailable: 'Đã có lỗi. Hãy thử lại.',
      enterEmail: 'Nhập địa chỉ email.',
      resetRequested:
        'Nếu email này có tài khoản, chúng tôi đã gửi đường dẫn đặt lại mật '
        + 'khẩu.',
      enterNewPassword: 'Nhập mật khẩu mới.',
      resetLinkIncomplete:
        'Đường dẫn đặt lại này không hợp lệ hoặc không đầy đủ.',
      resetLinkExpired: 'Đường dẫn đặt lại này không hợp lệ hoặc đã hết hạn.',
      verifyLinkIncomplete:
        'Đường dẫn xác minh này không hợp lệ hoặc không đầy đủ.',
      verifyLinkExpired:
        'Đường dẫn xác minh này không hợp lệ hoặc đã hết hạn.',
      providerFailed: 'Đã có lỗi khi đăng nhập. Hãy thử lại.',
      providerEmailNotVerified:
        'Bạn cần xác minh địa chỉ email với nhà cung cấp trước khi đăng nhập.',
      providerCancelled: 'Đã hủy đăng nhập.',
      providerEmailRegistered:
        'Email này đã có tài khoản. Hãy đăng nhập bằng nhà cung cấp bạn đã '
        + 'dùng trước đây.',
    },
    email: 'Email',
    password: 'Mật khẩu',
    newPassword: 'Mật khẩu mới',
    confirmPassword: 'Nhập lại mật khẩu',
    name: 'Họ và tên',
    showPassword: (label) => `Hiện ${label.toLowerCase()}`,
    hidePassword: (label) => `Ẩn ${label.toLowerCase()}`,
    signIn: 'Đăng nhập',
    createAccount: 'Tạo tài khoản',
    or: 'hoặc',
    providers: {
      google: 'Tiếp tục với Google',
      github: 'Tiếp tục với GitHub',
      linkedin: 'Tiếp tục với LinkedIn',
    },
    login: {
      lead: 'Dùng email và mật khẩu của tài khoản.',
      pending: 'Đang đăng nhập…',
      forgotPassword: 'Quên mật khẩu?',
    },
    register: {
      lead: 'Tạo tài khoản để viết và đăng CV của bạn.',
      pending: 'Đang tạo tài khoản…',
      success: 'Hãy kiểm tra email để xác minh địa chỉ của bạn.',
      afterVerify: 'sau khi bạn xác minh email.',
      haveAccount: 'Đã có tài khoản?',
      noEmail:
        'Nếu sau vài phút vẫn chưa thấy email, hãy kiểm tra thư mục thư rác.',
      noEmailGoogle: 'Hoặc đăng nhập bằng tài khoản Google của bạn:',
      closed: 'Đăng ký bằng email đang tạm đóng. Hãy tiếp tục với Google.',
    },
    forgot: {
      title: 'Quên mật khẩu',
      lead: 'Nhập email để nhận đường dẫn đặt lại mật khẩu nếu có tài khoản.',
      submit: 'Gửi đường dẫn đặt lại',
      pending: 'Đang gửi…',
      backToSignIn: 'Quay lại đăng nhập',
    },
    reset: {
      title: 'Đặt lại mật khẩu',
      lead: 'Chọn mật khẩu mới cho tài khoản.',
      submit: 'Đặt lại mật khẩu',
      pending: 'Đang đặt lại…',
      success: 'Đã đặt lại mật khẩu. Hãy đăng nhập.',
    },
    verify: {
      title: 'Xác minh email',
      lead: 'Mở đường dẫn trong email để xác minh địa chỉ của bạn.',
      pending: 'Đang xác minh địa chỉ email…',
      success: 'Đã xác minh email. Hãy đăng nhập.',
      useGoogle: 'Hoặc đăng nhập bằng tài khoản Google của bạn:',
    },
  },
  en: {
    messages: {
      enterEmailAndPassword: 'Enter your email and password.',
      invalidCredentials: 'Invalid email or password.',
      checkEmailAndPassword: 'Check your email and password and try again.',
      fillAllFields: 'Please fill in all fields.',
      checkDetails: 'Check your details and try again.',
      passwordsDoNotMatch: 'Passwords do not match.',
      passwordLength: 'Password must be at least 12 characters.',
      passwordCommon: 'That password is too common. Choose a different one.',
      passwordBreached:
        'That password was exposed in a data breach. Choose a different one.',
      passwordInvalid: 'Password does not meet our requirements.',
      rateLimited: 'Too many attempts. Try again later.',
      unavailable: 'Something went wrong. Please try again.',
      enterEmail: 'Enter your email address.',
      resetRequested:
        'If an account exists for this email, we\'ve sent a password reset '
        + 'link.',
      enterNewPassword: 'Enter a new password.',
      resetLinkIncomplete: 'This reset link is invalid or incomplete.',
      resetLinkExpired: 'This reset link is invalid or has expired.',
      verifyLinkIncomplete: 'This verification link is invalid or incomplete.',
      verifyLinkExpired: 'This verification link is invalid or has expired.',
      providerFailed:
        'Something went wrong while signing you in. Please try again.',
      providerEmailNotVerified:
        'Your email address must be verified with your provider before you '
        + 'can sign in.',
      providerCancelled: 'Sign-in was cancelled.',
      // Deliberately does not name the existing provider: naming it hands an
      // attacker a targeted-phishing hint (OAuth email-collision rule).
      providerEmailRegistered:
        'An account with this email already exists. Sign in with the provider '
        + 'you used originally.',
    },
    email: 'Email',
    password: 'Password',
    newPassword: 'New password',
    confirmPassword: 'Confirm password',
    name: 'Name',
    showPassword: (label) => `Show ${label.toLowerCase()}`,
    hidePassword: (label) => `Hide ${label.toLowerCase()}`,
    signIn: 'Sign in',
    createAccount: 'Create account',
    or: 'or',
    providers: {
      google: 'Continue with Google',
      github: 'Continue with GitHub',
      linkedin: 'Continue with LinkedIn',
    },
    login: {
      lead: 'Use the email and password for your account.',
      pending: 'Signing in…',
      forgotPassword: 'Forgot password?',
    },
    register: {
      lead: 'Create an account to build and publish your resumes.',
      pending: 'Creating account…',
      success: 'Check your email to verify your address.',
      afterVerify: 'after you verify your email.',
      haveAccount: 'Already have an account?',
      noEmail:
        'If the email has not arrived within a few minutes, check your spam '
        + 'folder.',
      noEmailGoogle: 'Or sign in with your Google account instead:',
      closed:
        'Email sign-up is temporarily closed. Continue with Google instead.',
    },
    forgot: {
      title: 'Forgot password',
      lead: 'Enter your email to receive a reset link if an account exists.',
      submit: 'Send reset link',
      pending: 'Sending…',
      backToSignIn: 'Back to sign in',
    },
    reset: {
      title: 'Reset password',
      lead: 'Choose a new password for your account.',
      submit: 'Reset password',
      pending: 'Resetting…',
      success: 'Password reset. Sign in.',
    },
    verify: {
      title: 'Verify email',
      lead: 'Follow the link in your email to verify your address.',
      pending: 'Verifying your email address…',
      success: 'Email verified. Sign in.',
      useGoogle: 'Or sign in with your Google account instead:',
    },
  },
};
