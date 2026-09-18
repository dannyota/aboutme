// Privacy Policy and Terms of Service in both site languages. Every statement
// here must describe shipped behavior or an owner decision; do not add claims.
import type { Locale } from './locale';

export const legalContact = 'danny@aboutme.vn';
export const repositoryUrl = 'https://github.com/dannyota/aboutme';

export type LegalSection = {
  readonly heading: string;
  readonly paragraphs?: readonly string[];
  readonly items?: readonly string[];
  /** Text after the list, in the same section. */
  readonly after?: readonly string[];
  /** Ends the section with a link to the source repository. */
  readonly repositoryLink?: string;
  /** Ends the section with this label and the contact address. */
  readonly contactLabel?: string;
};

export type LegalDocument = {
  readonly title: string;
  readonly intro: string;
  /** Who runs the service, followed by the contact address. */
  readonly operator?: { readonly text: string; readonly contactLabel: string };
  readonly sections: readonly LegalSection[];
};

export type LegalCopy = {
  readonly updated: string;
  readonly privacy: LegalDocument;
  readonly terms: LegalDocument;
  /** Short link labels for the homepage footer and registration. */
  readonly privacyLink: string;
  readonly termsLink: string;
  /** "By creating an account you agree to the [Terms] and the [Privacy]." */
  readonly agreement: readonly [string, string, string];
};

export const legalCopy: Record<Locale, LegalCopy> = {
  vi: {
    updated: 'Cập nhật lần cuối ngày 18/09/2026',
    privacyLink: 'Chính sách quyền riêng tư',
    termsLink: 'Điều khoản sử dụng',
    agreement: ['Khi tạo tài khoản, bạn đồng ý với ', ' và ', '.'],
    privacy: {
      title: 'Chính sách quyền riêng tư',
      intro:
        'aboutme (https://aboutme.vn) là công cụ tạo CV mã nguồn mở. Trang '
        + 'này giải thích chúng tôi thu thập gì, dùng để làm gì, và bạn có '
        + 'những lựa chọn nào.',
      operator: {
        text: 'aboutme do Danny vận hành.',
        contactLabel: 'Liên hệ',
      },
      sections: [
        {
          heading: 'Dữ liệu cá nhân chúng tôi thu thập',
          items: [
            'Tài khoản: email, tên, và mật khẩu của bạn. Mật khẩu chỉ được '
            + 'lưu dưới dạng mã băm Argon2id. Nếu bạn đăng nhập bằng Google: '
            + 'ID tài khoản Google, email và tên.',
            'Nội dung: các CV bạn viết và ảnh bạn tải lên.',
            'Phiên đăng nhập: thông tin trình duyệt (user-agent) và địa chỉ '
            + 'IP của từng phiên, dùng cho bảo mật. Settings → Sessions liệt '
            + 'kê các thiết bị đang đăng nhập. Thông tin trình duyệt và IP '
            + 'được xoá 90 ngày sau khi đăng nhập.',
            'Nhật ký yêu cầu của máy chủ (không ghi địa chỉ IP), lưu tối đa '
            + '180 ngày.',
            'Sự kiện gửi email: đã gửi, đã nhận, bị trả lại, hoặc bị đánh '
            + 'dấu là thư rác. Nếu email gửi đến bạn bị trả lại hoặc bị đánh '
            + 'dấu là thư rác, địa chỉ đó được đưa vào danh sách chặn gửi '
            + 'của Amazon SES cho đến khi được gỡ ra.',
            'Kiểm tra mật khẩu: khi bạn đặt mật khẩu, chúng tôi kiểm tra xem '
            + 'mật khẩu đã từng bị lộ hay chưa qua dịch vụ Have I Been '
            + 'Pwned. Chỉ 5 ký tự đầu của mã băm SHA-1 được gửi đi, không '
            + 'bao giờ gửi mật khẩu.',
          ],
        },
        {
          heading: 'Mục đích và cơ sở xử lý',
          paragraphs: [
            'Chúng tôi xử lý dữ liệu cá nhân của bạn để cung cấp tài khoản '
            + 'và các CV bạn tạo, giữ an toàn cho dịch vụ, và gửi email về '
            + 'tài khoản. Cơ sở xử lý là sự đồng ý của bạn khi tạo tài '
            + 'khoản. Bạn có thể rút lại sự đồng ý bằng cách xoá tài khoản.',
          ],
        },
        {
          heading: 'Chúng tôi không làm gì',
          items: [
            'Không quảng cáo.',
            'Không dùng công cụ phân tích hay mã theo dõi.',
            'Không bán dữ liệu của bạn và không chia sẻ dữ liệu cho mục đích '
            + 'tiếp thị.',
            'Cookie chỉ dùng để giữ phiên đăng nhập, hoàn tất đăng nhập bằng '
            + 'Google, và ghi nhớ giao diện và ngôn ngữ bạn chọn.',
          ],
        },
        {
          heading: 'Chỉ công khai khi bạn chọn',
          paragraphs: [
            'CV ở chế độ riêng tư cho đến khi bạn đăng. Mỗi CV có đường dẫn '
            + 'riêng. Việc lập chỉ mục cho công cụ tìm kiếm và AI mặc định '
            + 'tắt cho đến khi bạn bật. Khi bạn ngừng công khai, đường dẫn '
            + 'công khai ngừng hoạt động ngay lập tức.',
            'Trang công khai được phân phối qua mạng CDN toàn cầu '
            + '(Cloudflare) nhưng không được lưu lại trên CDN. Khi CV đang '
            + 'công khai, bất kỳ ai xem được cũng có thể lưu hoặc chụp lại '
            + 'trang.',
          ],
        },
        {
          heading: 'Dữ liệu được lưu ở đâu',
          paragraphs: [
            'Dữ liệu của bạn được lưu tại Amazon Web Services ở Singapore '
            + '(ap-southeast-1): cơ sở dữ liệu, kho lưu ảnh, và việc gửi '
            + 'email qua Amazon SES. Cloudflare phân phối trang web. Thư trả '
            + 'lời email của chúng tôi được gửi đến một hộp thư Google '
            + 'Workspace. Nếu bạn ở Việt Nam, điều này có nghĩa là dữ liệu '
            + 'cá nhân của bạn được chuyển ra nước ngoài (Singapore).',
            'Kiểm tra mật khẩu dùng Have I Been Pwned. Nếu bật đăng nhập '
            + 'bằng Google, Google xác thực tài khoản của bạn.',
          ],
        },
        {
          heading: 'Trợ lý AI được kết nối',
          paragraphs: [
            'Trợ lý AI bạn kết nối chỉ làm được những gì bạn cho phép: đọc '
            + 'CV, và nếu bạn cấp quyền ghi, tạo, sửa hoặc xoá CV và ảnh. '
            + 'Trợ lý không thể đăng hay ngừng công khai CV, nhưng nếu xoá '
            + 'một CV đang công khai thì đường dẫn của CV đó ngừng hoạt '
            + 'động. Nội dung trợ lý đọc được sẽ đến dịch vụ AI mà bạn chọn. '
            + 'Bạn có thể thu hồi quyền bất cứ lúc nào trong Settings.',
          ],
        },
        {
          heading: 'Quyền kiểm soát của bạn',
          paragraphs: [
            'Bạn có thể xuất dữ liệu tài khoản, sửa hoặc xoá CV, và xoá tài '
            + 'khoản. Khi bạn xoá tài khoản:',
          ],
          items: [
            'Quyền truy cập bị thu hồi ngay lập tức.',
            'Ảnh đã tải lên được xoá, thường trong vòng 24 giờ.',
            'Bản sao lưu tự động của cơ sở dữ liệu hết hạn sau 30 ngày. Bản '
            + 'sao lưu tạo khi phát hành phiên bản mới được xoá ở lần phát '
            + 'hành sau, khi đã quá 30 ngày.',
            'Bản ghi về việc xoá (chỉ gồm loại sự kiện và thời điểm) được '
            + 'giữ tối đa 180 ngày.',
          ],
        },
        {
          heading: 'Email',
          paragraphs: [
            'Chúng tôi chỉ gửi email về tài khoản: xác minh email, đặt lại '
            + 'mật khẩu, và thông báo khi mật khẩu thay đổi. Không gửi email '
            + 'tiếp thị.',
          ],
        },
        {
          heading: 'Quyền của bạn',
          paragraphs: [
            'Bạn có quyền được biết, truy cập, chỉnh sửa, xoá dữ liệu cá '
            + 'nhân của mình, rút lại sự đồng ý, phản đối hoặc yêu cầu hạn '
            + 'chế xử lý, và khiếu nại với cơ quan có thẩm quyền. Phần lớn '
            + 'các quyền này bạn tự thực hiện được trong Settings; các yêu '
            + 'cầu khác xin gửi qua email bên dưới.',
          ],
        },
        {
          heading: 'Thay đổi và liên hệ',
          paragraphs: [
            'Khi chính sách này thay đổi, chúng tôi cập nhật trang này và '
            + 'ngày cập nhật.',
          ],
          contactLabel: 'Gửi câu hỏi và yêu cầu đến',
        },
      ],
    },
    terms: {
      title: 'Điều khoản sử dụng',
      intro:
        'Các điều khoản này áp dụng khi bạn sử dụng aboutme '
        + '(https://aboutme.vn).',
      sections: [
        {
          heading: 'Dịch vụ',
          paragraphs: [
            'aboutme miễn phí. Mã nguồn được công bố theo giấy phép AGPL-3.0.',
          ],
          repositoryLink: 'Mã nguồn trên GitHub',
        },
        {
          heading: 'Nội dung của bạn',
          paragraphs: [
            'Bạn sở hữu nội dung bạn tạo. Bạn cho phép aboutme lưu trữ nội '
            + 'dung đó và hiển thị theo cách bạn chọn, ví dụ khi bạn đăng '
            + 'CV. Quyền này chấm dứt khi bạn xoá nội dung, trừ các bản sao '
            + 'lưu cho đến khi chúng hết hạn.',
          ],
        },
        {
          heading: 'Quy tắc sử dụng',
          paragraphs: [
            'Không dùng aboutme để:',
          ],
          items: [
            'đăng nội dung vi phạm pháp luật;',
            'mạo danh người khác;',
            'đăng dữ liệu cá nhân của người khác khi chưa được họ cho phép;',
            'gửi thư rác, phát tán phần mềm độc hại, hoặc lạm dụng dịch vụ '
            + 'hay người khác.',
          ],
          after: [
            'Chúng tôi có thể gỡ nội dung hoặc xoá tài khoản vi phạm các quy '
            + 'tắc này.',
          ],
        },
        {
          heading: 'Tài khoản',
          items: [
            'Giữ mật khẩu của bạn an toàn.',
            'Mỗi tài khoản dành cho một người.',
            'Mỗi tài khoản có tối đa ba CV.',
            'Bạn phải từ 16 tuổi trở lên.',
            'Nếu biết một tài khoản thuộc về người dưới 16 tuổi, chúng tôi '
            + 'sẽ xoá tài khoản đó.',
          ],
        },
        {
          heading: 'Không bảo đảm',
          paragraphs: [
            'Dịch vụ được cung cấp theo hiện trạng. Dịch vụ có thể thay đổi '
            + 'và đôi khi không truy cập được. Nếu dự định ngừng dịch vụ, '
            + 'chúng tôi sẽ cố gắng báo trước để bạn kịp xuất dữ liệu.',
          ],
        },
        {
          heading: 'Giới hạn trách nhiệm',
          paragraphs: [
            'Trách nhiệm của chúng tôi được giới hạn trong phạm vi pháp luật '
            + 'cho phép.',
          ],
        },
        {
          heading: 'Luật áp dụng',
          paragraphs: [
            'Các điều khoản này tuân theo pháp luật Việt Nam.',
          ],
        },
        {
          heading: 'Thay đổi và liên hệ',
          paragraphs: [
            'Khi điều khoản thay đổi, chúng tôi cập nhật trang này và ngày '
            + 'cập nhật. Việc bạn tiếp tục sử dụng dịch vụ sau khi thay đổi '
            + 'có hiệu lực đồng nghĩa với việc bạn chấp nhận điều khoản mới.',
          ],
          contactLabel: 'Liên hệ',
        },
      ],
    },
  },
  en: {
    updated: 'Last updated September 18, 2026',
    privacyLink: 'Privacy Policy',
    termsLink: 'Terms of Service',
    agreement: ['By creating an account you agree to the ', ' and the ', '.'],
    privacy: {
      title: 'Privacy Policy',
      intro:
        'aboutme (https://aboutme.vn) is an open-source resume builder. This '
        + 'page explains what we collect, what we use it for, and the '
        + 'choices you have.',
      operator: {
        text: 'aboutme is operated by Danny.',
        contactLabel: 'Contact',
      },
      sections: [
        {
          heading: 'What we collect',
          items: [
            'Account: your email, your name, and your password, stored only '
            + 'as an Argon2id hash. If you sign in with Google: your Google '
            + 'account ID, email, and name.',
            'Content: the resumes you write and the photos you upload.',
            'Sessions: the browser user-agent and IP address of each '
            + 'session, used for security. Settings → Sessions lists your '
            + 'signed-in devices. We delete the user-agent and IP 90 days '
            + 'after sign-in.',
            'Server request logs, which do not record IP addresses, kept for '
            + 'up to 180 days.',
            'Email delivery events: sent, delivered, bounced, or marked as '
            + 'spam. If an email to you bounces or is marked as spam, Amazon '
            + 'SES keeps that address on its suppression list until it is '
            + 'removed.',
            'Password check: when you set a password, we check whether it '
            + 'has appeared in a known breach using Have I Been Pwned. Only '
            + 'the first 5 characters of its SHA-1 hash are sent, never the '
            + 'password.',
          ],
        },
        {
          heading: 'Why we use your data',
          paragraphs: [
            'We process your personal data to provide your account and the '
            + 'resumes you create, to keep the service secure, and to send '
            + 'account emails. We do this with the consent you give when you '
            + 'create an account. You can withdraw consent by deleting your '
            + 'account.',
          ],
        },
        {
          heading: 'What we don\'t do',
          items: [
            'No ads.',
            'No analytics or tracking scripts.',
            'We don\'t sell your data or share it for marketing.',
            'Cookies are used only to keep you signed in, to complete Google '
            + 'sign-in, and to remember your theme and language.',
          ],
        },
        {
          heading: 'Public only by your choice',
          paragraphs: [
            'A resume is private until you publish it. Each resume has its '
            + 'own link. Search engine and AI indexing stays off until you '
            + 'turn it on. When you unpublish, the public link stops working '
            + 'right away.',
            'Public pages are delivered through a global CDN (Cloudflare), '
            + 'which does not store copies of them. While a resume is '
            + 'public, anyone who can see it can save or screenshot it.',
          ],
        },
        {
          heading: 'Where your data is stored',
          paragraphs: [
            'Your data is stored with Amazon Web Services in Singapore '
            + '(ap-southeast-1): the database, photo storage, and email '
            + 'sending through Amazon SES. Cloudflare delivers the site. '
            + 'Replies to our emails reach a Google Workspace mailbox. If '
            + 'you are in Vietnam, this means your personal data is '
            + 'transferred abroad, to Singapore.',
            'Password checks use Have I Been Pwned. If Google sign-in is on, '
            + 'Google verifies your account.',
          ],
        },
        {
          heading: 'Connected AI agents',
          paragraphs: [
            'An AI agent you connect can do only what you allow: read your '
            + 'resumes and, with write access, create, edit, or delete '
            + 'resumes and photos. It cannot publish or unpublish, but '
            + 'deleting a published resume takes its link down. Content the '
            + 'agent reads goes to the AI service you chose. You can revoke '
            + 'access at any time in Settings.',
          ],
        },
        {
          heading: 'Your controls',
          paragraphs: [
            'You can export your account data, edit or delete your resumes, '
            + 'and delete your account. When you delete your account:',
          ],
          items: [
            'Access is removed immediately.',
            'Uploaded photos are deleted, normally within 24 hours.',
            'Automatic database backups expire after 30 days. A backup taken '
            + 'for a new release is deleted at a later release once it is more '
            + 'than 30 days old.',
            'Records of the deletion, holding only the event type and time, '
            + 'are kept for up to 180 days.',
          ],
        },
        {
          heading: 'Emails',
          paragraphs: [
            'We send only account emails: email verification, password '
            + 'reset, and a notice when your password changes. No marketing.',
          ],
        },
        {
          heading: 'Your rights',
          paragraphs: [
            'You have the right to know about, access, correct, and delete '
            + 'your personal data, to withdraw consent, to object to or '
            + 'restrict processing, and to complain to the authorities. You '
            + 'can do most of this yourself in Settings; send other requests '
            + 'to the address below.',
          ],
        },
        {
          heading: 'Changes and contact',
          paragraphs: [
            'When this policy changes, we update this page and its date.',
          ],
          contactLabel: 'Send questions and requests to',
        },
      ],
    },
    terms: {
      title: 'Terms of Service',
      intro: 'These terms apply when you use aboutme (https://aboutme.vn).',
      sections: [
        {
          heading: 'The service',
          paragraphs: [
            'aboutme is free. Its code is open source under the AGPL-3.0 '
            + 'license.',
          ],
          repositoryLink: 'Source code on GitHub',
        },
        {
          heading: 'Your content',
          paragraphs: [
            'You own the content you create. You give aboutme permission to '
            + 'store it and to display it the way you choose, for example by '
            + 'publishing a resume. This permission ends when you delete the '
            + 'content, except for backup copies until they expire.',
          ],
        },
        {
          heading: 'Acceptable use',
          paragraphs: [
            'Do not use aboutme to:',
          ],
          items: [
            'post illegal content;',
            'impersonate anyone;',
            'post other people\'s personal data without their permission;',
            'send spam, spread malware, or abuse the service or other people.',
          ],
          after: [
            'We may remove content or delete accounts that break these rules.',
          ],
        },
        {
          heading: 'Your account',
          items: [
            'Keep your password safe.',
            'One person per account.',
            'Up to three resumes per account.',
            'You must be at least 16 years old.',
            'If we learn an account belongs to someone under 16, we will '
            + 'delete it.',
          ],
        },
        {
          heading: 'No warranty',
          paragraphs: [
            'The service is provided as is. It may change and may '
            + 'occasionally be unavailable. If we plan to shut it down, we '
            + 'will try to give notice first so you can export your data.',
          ],
        },
        {
          heading: 'Limitation of liability',
          paragraphs: [
            'Our liability is limited to the extent the law allows.',
          ],
        },
        {
          heading: 'Governing law',
          paragraphs: [
            'These terms are governed by the laws of Vietnam.',
          ],
        },
        {
          heading: 'Changes and contact',
          paragraphs: [
            'When these terms change, we update this page and its date. If '
            + 'you keep using the service after a change, you accept the '
            + 'updated terms.',
          ],
          contactLabel: 'Contact',
        },
      ],
    },
  },
};
