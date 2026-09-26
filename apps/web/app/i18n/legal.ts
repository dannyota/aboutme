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
  /** The search result summary; restates the intro, no new claims. */
  readonly description: string;
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
    updated: 'Cập nhật lần cuối ngày 26/09/2026',
    privacyLink: 'Chính sách quyền riêng tư',
    termsLink: 'Điều khoản sử dụng',
    agreement: [
      'Khi tạo tài khoản, bạn xác nhận đủ 16 tuổi và đồng ý với ',
      ' và ',
      '.',
    ],
    privacy: {
      title: 'Chính sách quyền riêng tư',
      description:
        'Chính sách quyền riêng tư của aboutme: chúng tôi thu thập gì, dùng '
        + 'để làm gì, và bạn có những lựa chọn nào.',
      intro:
        'aboutme (https://aboutme.vn) là công cụ tạo CV mã nguồn mở. Trang '
        + 'này giải thích chúng tôi thu thập gì, dùng để làm gì, và bạn có '
        + 'những lựa chọn nào.',
      operator: {
        text:
          'aboutme do Danny, một cá nhân, vận hành phi thương mại tại '
          + 'Việt Nam.',
        contactLabel: 'Liên hệ',
      },
      sections: [
        {
          heading: 'Dữ liệu cá nhân chúng tôi thu thập',
          items: [
            'Tài khoản: email, tên, và mật khẩu của bạn. Mật khẩu chỉ được '
            + 'lưu dưới dạng mã băm Argon2id. Nếu bạn đăng nhập bằng Google: '
            + 'ID tài khoản Google, email và tên.',
            'Nội dung: các CV bạn viết, ảnh bạn tải lên, và, khi một CV '
            + 'đang công khai, ảnh xem trước chúng tôi tạo từ CV đó để hiển '
            + 'thị khi đường dẫn được chia sẻ.',
            'Phiên đăng nhập: thông tin trình duyệt (user-agent) và địa chỉ '
            + 'IP của từng phiên, dùng cho bảo mật. Settings → Sessions liệt '
            + 'kê các thiết bị đang đăng nhập. Chúng tôi xoá địa chỉ IP và '
            + 'thông tin trình duyệt ghi nhận cho một lần đăng nhập trong '
            + 'vòng 90 ngày kể từ lần đăng nhập đó. Việc duy trì phiên đăng '
            + 'nhập không kéo dài thời hạn này.',
            'Xác thực hai bước: khoá công khai của passkey (public key), mã '
            + 'bí mật của ứng dụng xác thực (đã mã hoá), và mã băm của các '
            + 'mã khôi phục. Bất thường về bộ đếm passkey được giữ trong '
            + '180 ngày.',
            'Trợ lý AI đã kết nối: tên ứng dụng, các địa chỉ chuyển hướng '
            + '(redirect) của ứng dụng, quyền truy cập bạn đã cấp, và lần '
            + 'dùng gần nhất. Token truy cập chỉ được lưu dưới dạng mã băm.',
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
            'Chúng tôi xử lý dữ liệu cá nhân của bạn để thực hiện thỏa '
            + 'thuận giữa bạn và aboutme theo Điều khoản sử dụng: cung cấp '
            + 'tài khoản và các CV bạn tạo, giữ an toàn cho dịch vụ, và gửi '
            + 'email về tài khoản. Các tính năng tuỳ chọn (đăng CV công '
            + 'khai, cho phép lập chỉ mục, kết nối trợ lý AI, đăng nhập '
            + 'bằng Google) chỉ chạy khi bạn tự bật, và bạn có thể tắt bất '
            + 'cứ lúc nào. Bạn có thể chấm dứt thỏa thuận bằng cách xoá tài '
            + 'khoản.',
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
            + 'Google, giữ trạng thái xác thực hai bước đang chờ trong năm '
            + 'phút, và ghi nhớ giao diện và ngôn ngữ bạn chọn.',
            'Bộ nhớ cục bộ của trình duyệt (localStorage) chỉ dùng để ghi '
            + 'nhớ đường dẫn quay lại sau khi bạn xác minh email (tối đa 24 '
            + 'giờ), và chế độ xem trước (PDF hoặc web) bạn chọn trong '
            + 'trình chỉnh sửa CV.',
          ],
        },
        {
          heading: 'Chỉ công khai khi bạn chọn',
          paragraphs: [
            'CV ở chế độ riêng tư cho đến khi bạn đăng. Mỗi CV có đường dẫn '
            + 'riêng. Việc lập chỉ mục cho công cụ tìm kiếm và AI mặc định '
            + 'tắt cho đến khi bạn bật. Khi bạn ngừng công khai, đường dẫn '
            + 'công khai ngừng hoạt động ngay lập tức. Khi bạn xoá hoặc đổi '
            + 'đường dẫn của một CV, chúng tôi giữ chỗ đường dẫn cũ trong '
            + '180 ngày để không ai khác chiếm được đường dẫn đó. Việc giữ '
            + 'chỗ này không gắn với tài khoản của bạn, và sẽ bị xoá sau '
            + '180 ngày.',
            'Trang công khai được phân phối qua mạng CDN toàn cầu '
            + '(Amazon CloudFront) nhưng không được lưu lại trên CDN. '
            + 'Khi CV đang công khai, bất kỳ ai xem được cũng có thể lưu '
            + 'hoặc chụp lại trang. Theo mặc định, người xem cũng có thể '
            + 'tải về bản PDF của CV; bạn có thể tắt tính năng này. Đừng '
            + 'đưa dữ liệu cá nhân nhạy cảm, như số giấy tờ tuỳ thân, tình '
            + 'trạng sức khoẻ, hoặc tôn giáo, vào một CV công khai.',
            'Khi bất kỳ ai chia sẻ đường dẫn công khai của bạn trong một ứng '
            + 'dụng nhắn tin hoặc mạng xã hội, dịch vụ đó sẽ lấy tiêu đề '
            + 'trang, phần tóm tắt và ảnh xem trước của trang, và có thể giữ '
            + 'bản sao riêng của họ sau khi bạn ngừng công khai.',
          ],
        },
        {
          heading: 'Dữ liệu được lưu ở đâu',
          paragraphs: [
            'Dữ liệu của bạn được lưu tại Amazon Web Services ở Singapore '
            + '(ap-southeast-1): cơ sở dữ liệu, bản sao lưu, kho ảnh, và '
            + 'việc gửi email qua Amazon SES. Amazon CloudFront (mạng máy '
            + 'chủ toàn cầu của Amazon Web Services) phân phối trang web và '
            + 'xử lý địa chỉ IP của bạn. Amazon Route 53 cung cấp dịch vụ '
            + 'DNS. '
            + 'Nếu bạn đăng nhập bằng Google, Google (Hoa Kỳ) xác thực '
            + 'tài khoản của bạn. Email bạn gửi cho chúng tôi được lưu '
            + 'trong hộp thư Google Workspace. Nếu bạn ở Việt Nam, dữ liệu '
            + 'cá nhân của bạn được chuyển ra nước ngoài: chủ yếu đến '
            + 'Singapore, và một phần đến Google.',
            'Kiểm tra mật khẩu dùng dịch vụ Have I Been Pwned.',
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
            + 'khoản. Bản xuất gồm hồ sơ và nội dung CV của bạn; ảnh và '
            + 'lịch sử phiên đăng nhập xin liên hệ qua email.',
            'Khi bạn xoá tài khoản:',
          ],
          items: [
            'Quyền truy cập bị thu hồi ngay lập tức.',
            'Ảnh đã tải lên được xoá, thường trong vòng 24 giờ.',
            'Bản sao lưu cơ sở dữ liệu giữ các bản cũ trong tối đa 30 ngày '
            + 'sau đó, và chỉ dùng để khôi phục sau sự cố.',
            'Bản ghi về việc xoá tài khoản và gỡ liên kết nhà cung cấp (chỉ '
            + 'gồm loại sự kiện và thời điểm) được giữ tối đa 180 ngày.',
          ],
        },
        {
          heading: 'Email',
          paragraphs: [
            'Chúng tôi chỉ gửi email về tài khoản: xác minh email, đặt lại '
            + 'mật khẩu, và thông báo bảo mật khi mật khẩu, xác thực hai '
            + 'bước, passkey, ứng dụng xác thực, hoặc mã khôi phục của bạn '
            + 'thay đổi. Không gửi email tiếp thị.',
          ],
        },
        {
          heading: 'Quyền của bạn',
          paragraphs: [
            'Bạn có quyền được biết, truy cập, chỉnh sửa, xoá dữ liệu cá '
            + 'nhân của mình, phản đối hoặc yêu cầu hạn chế xử lý, khiếu '
            + 'nại, tố cáo, khởi kiện, và yêu cầu bồi thường thiệt hại theo '
            + 'quy định của pháp luật. Phần lớn các quyền này bạn tự thực '
            + 'hiện được trong Settings; các yêu cầu khác xin gửi qua email '
            + 'bên dưới. Chúng tôi phản hồi trong vòng 2 ngày làm việc và '
            + 'hoàn tất yêu cầu trong thời hạn pháp luật quy định.',
          ],
        },
        {
          heading: 'Thay đổi và liên hệ',
          paragraphs: [
            'Khi chính sách này thay đổi, chúng tôi cập nhật trang này và '
            + 'ngày cập nhật. Nếu chúng tôi thay đổi lý do xử lý dữ liệu '
            + 'hoặc dữ liệu chúng tôi thu thập, chúng tôi sẽ gửi email cho '
            + 'bạn trước khi thay đổi có hiệu lực.',
          ],
          contactLabel: 'Gửi câu hỏi và yêu cầu đến',
        },
      ],
    },
    terms: {
      title: 'Điều khoản sử dụng',
      description:
        'Điều khoản sử dụng aboutme, công cụ tạo CV miễn phí và mã nguồn mở.',
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
            'Bạn sở hữu nội dung bạn tạo. Bạn cho phép aboutme lưu trữ, sao '
            + 'lưu, kết xuất thành PDF, gửi đến các trợ lý AI bạn kết nối, '
            + 'và hiển thị nội dung đó theo cách bạn chọn, ví dụ khi bạn '
            + 'đăng CV. Quyền này chấm dứt khi bạn xoá nội dung, trừ các '
            + 'bản sao lưu cho đến khi chúng hết hạn.',
            'Bạn chịu trách nhiệm về nội dung bạn công khai, kể cả tính '
            + 'chính xác và quyền chia sẻ thông tin hoặc hình ảnh của người '
            + 'khác.',
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
            'Bạn chịu trách nhiệm về hành động của các trợ lý AI bạn kết '
            + 'nối với tài khoản.',
          ],
        },
        {
          heading: 'Chấm dứt',
          paragraphs: [
            'Bạn có thể xoá tài khoản bất cứ lúc nào. Nếu chúng tôi gỡ nội '
            + 'dung hoặc tạm ngưng tài khoản do vi phạm, chúng tôi sẽ gửi '
            + 'email và cho bạn thời gian hợp lý để xuất dữ liệu, trừ khi '
            + 'vi phạm nghiêm trọng hoặc pháp luật yêu cầu khác.',
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
          heading: 'Ghi nhận',
          paragraphs: [
            'Biểu tượng LinkedIn lấy từ Font Awesome Free của Fonticons, Inc., '
            + 'theo giấy phép CC BY 4.0 '
            + '(creativecommons.org/licenses/by/4.0). Biểu tượng GitHub và X '
            + 'lấy từ Simple Icons (CC0). Phông chữ dùng giấy phép SIL Open '
            + 'Font License; toàn văn các giấy phép có trong mã nguồn. Các '
            + 'nhãn hiệu thuộc về chủ sở hữu tương ứng.',
          ],
        },
        {
          heading: 'Luật áp dụng',
          paragraphs: [
            'Các điều khoản này tuân theo pháp luật Việt Nam.',
            'Khi có tranh chấp, hai bên sẽ thương lượng trước; nếu không '
            + 'giải quyết được, tranh chấp sẽ được xử lý theo pháp luật '
            + 'Việt Nam.',
          ],
        },
        {
          heading: 'Thay đổi và liên hệ',
          paragraphs: [
            'Khi điều khoản thay đổi, chúng tôi cập nhật trang này và ngày '
            + 'cập nhật. Nếu thay đổi là quan trọng, chúng tôi sẽ gửi email '
            + 'báo trước ít nhất 15 ngày trước khi thay đổi có hiệu lực. '
            + 'Việc bạn tiếp tục sử dụng dịch vụ sau khi thay đổi có hiệu '
            + 'lực đồng nghĩa với việc bạn chấp nhận điều khoản mới.',
          ],
          contactLabel: 'Liên hệ',
        },
      ],
    },
  },
  en: {
    updated: 'Last updated September 26, 2026',
    privacyLink: 'Privacy Policy',
    termsLink: 'Terms of Service',
    agreement: [
      'By creating an account you confirm you are at least 16 and agree '
      + 'to the ',
      ' and the ',
      '.',
    ],
    privacy: {
      title: 'Privacy Policy',
      description:
        'The aboutme privacy policy: what we collect, what we use it for, and '
        + 'the choices you have.',
      intro:
        'aboutme (https://aboutme.vn) is an open-source resume builder. This '
        + 'page explains what we collect, what we use it for, and the '
        + 'choices you have.',
      operator: {
        text:
          'aboutme is operated by Danny, an individual, on a '
          + 'non-commercial basis in Vietnam.',
        contactLabel: 'Contact',
      },
      sections: [
        {
          heading: 'What we collect',
          items: [
            'Account: your email, your name, and your password, stored only '
            + 'as an Argon2id hash. If you sign in with Google: your Google '
            + 'account ID, email, and name.',
            'Content: the resumes you write, the photos you upload, and, '
            + 'while a resume is public, the preview image we make from it '
            + 'for link previews.',
            'Sessions: the browser user-agent and IP address of each '
            + 'session, used for security. Settings → Sessions lists your '
            + 'signed-in devices. We delete the IP address and browser '
            + '(user agent) recorded for a sign-in no later than 90 days '
            + 'after that sign-in. Staying signed in does not extend this.',
            'Second factor: your passkey public keys, your authenticator-'
            + 'app secret (encrypted), and hashes of your recovery codes. '
            + 'Passkey counter anomalies are kept for 180 days.',
            'Connected agents: the app name, its redirect addresses, the '
            + 'access you granted, and when it was last used. Access '
            + 'tokens are stored only as hashes.',
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
            'We process your personal data to perform our agreement with '
            + 'you under the Terms of Service: to provide your account and '
            + 'the resumes you create, keep the service secure, and send '
            + 'account emails. Optional features (publishing, search and '
            + 'AI indexing, connected AI agents, Google sign-in) run only '
            + 'when you turn them on, and you can turn them off at any '
            + 'time. You can end the agreement by deleting your account.',
          ],
        },
        {
          heading: 'What we don\'t do',
          items: [
            'No ads.',
            'No analytics or tracking scripts.',
            'We don\'t sell your data or share it for marketing.',
            'Cookies are used only to keep you signed in, to complete '
            + 'Google sign-in, to hold a pending second-factor sign-in for '
            + 'five minutes, and to remember your theme and language.',
            'Browser local storage is used only to remember the page to '
            + 'return to after you verify your email (for up to 24 hours), '
            + 'and your chosen preview mode (PDF or web) in the resume '
            + 'editor.',
          ],
        },
        {
          heading: 'Public only by your choice',
          paragraphs: [
            'A resume is private until you publish it. Each resume has its '
            + 'own link. Search engine and AI indexing stays off until you '
            + 'turn it on. When you unpublish, the public link stops '
            + 'working right away. When you delete or rename a resume, we '
            + 'keep its old web address reserved for 180 days so no one '
            + 'else can take over your link. The reservation is not linked '
            + 'to your account, and we delete it after the 180 days.',
            'Public pages are delivered through a global CDN '
            + '(Amazon CloudFront), which does not store copies of them. '
            + 'While a resume is public, anyone who can see it can save '
            + 'or screenshot it. By default, viewers can also download '
            + 'the resume\'s PDF; you can turn this off. Avoid putting '
            + 'sensitive personal data, such as ID numbers, health '
            + 'information, or religion, in a public resume.',
            'When anyone shares your public link in a chat app or social '
            + 'network, that service fetches the page\'s title, summary, and '
            + 'preview image, and may keep its own copy after you '
            + 'unpublish.',
          ],
        },
        {
          heading: 'Where your data is stored',
          paragraphs: [
            'Your data is stored with Amazon Web Services in Singapore '
            + '(ap-southeast-1): the database, its backups, photo storage, '
            + 'and email sending through Amazon SES. Amazon CloudFront '
            + '(the global network of Amazon Web Services) delivers the '
            + 'site and processes your IP address. Amazon Route 53 provides '
            + 'DNS. If you sign in with Google, Google (United '
            + 'States) verifies your account. Emails you send us are kept '
            + 'in a Google Workspace mailbox. If you are in Vietnam, your '
            + 'personal data is transferred abroad: mainly to Singapore, '
            + 'and in part to Google.',
            'Password checks use Have I Been Pwned.',
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
            'You can export your account data, edit or delete your '
            + 'resumes, and delete your account. The export holds your '
            + 'profile and resume content; email us for photos or session '
            + 'history.',
            'When you delete your account:',
          ],
          items: [
            'Access is removed immediately.',
            'Uploaded photos are deleted, normally within 24 hours.',
            'Database backups keep earlier copies for up to 30 days after '
            + 'that; they are used only for disaster recovery.',
            'Records of account deletions and provider unlinks, holding only '
            + 'the event type and time, are kept for up to 180 days.',
          ],
        },
        {
          heading: 'Emails',
          paragraphs: [
            'We send only account emails: email verification, password '
            + 'reset, and security notices when your password, second '
            + 'factor, passkeys, authenticator app, or recovery codes '
            + 'change. No marketing.',
          ],
        },
        {
          heading: 'Your rights',
          paragraphs: [
            'You have the right to know about, access, correct, and delete '
            + 'your personal data, to object to or restrict processing, '
            + 'and to complain, report, sue, and claim damages as the law '
            + 'allows. You can do most of this yourself in Settings; send '
            + 'other requests to the address below. We reply within 2 '
            + 'working days and complete requests within the legal '
            + 'deadlines.',
          ],
        },
        {
          heading: 'Changes and contact',
          paragraphs: [
            'When this policy changes, we update this page and its date. '
            + 'If we change why we process data or what we collect, we '
            + 'will email you before the change applies.',
          ],
          contactLabel: 'Send questions and requests to',
        },
      ],
    },
    terms: {
      title: 'Terms of Service',
      description:
        'The terms for using aboutme, a free and open-source resume builder.',
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
            + 'store, back up, render as PDF, send to AI agents you '
            + 'connect, and display your content the way you choose, for '
            + 'example by publishing a resume. This permission ends when '
            + 'you delete the content, except for backup copies until they '
            + 'expire.',
            'You are responsible for what you publish, including its '
            + 'accuracy and your right to share other people\'s '
            + 'information or images.',
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
            'You are responsible for what an AI agent you connect does '
            + 'with your account.',
          ],
        },
        {
          heading: 'Termination',
          paragraphs: [
            'You can delete your account at any time. If we remove content '
            + 'or suspend an account for a breach, we will email you and '
            + 'allow reasonable time to export your data, unless the '
            + 'breach is serious or the law requires otherwise.',
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
          heading: 'Credits',
          paragraphs: [
            'The LinkedIn icon is from Font Awesome Free by Fonticons, Inc., '
            + 'licensed under CC BY 4.0 (creativecommons.org/licenses/by/4.0). '
            + 'The GitHub and X icons are from Simple Icons (CC0). Fonts are '
            + 'licensed under the SIL Open Font License; the full notices are '
            + 'in the source code. Trademarks belong to their owners.',
          ],
        },
        {
          heading: 'Governing law',
          paragraphs: [
            'These terms are governed by the laws of Vietnam.',
            'If a dispute arises, we will first try to resolve it by '
            + 'negotiation; if that fails, it will be resolved under '
            + 'Vietnamese law.',
          ],
        },
        {
          heading: 'Changes and contact',
          paragraphs: [
            'When these terms change, we update this page and its date. If '
            + 'a change is material, we will email you at least 15 days '
            + 'before it applies. If you keep using the service after a '
            + 'change takes effect, you accept the updated terms.',
          ],
          contactLabel: 'Contact',
        },
      ],
    },
  },
};
