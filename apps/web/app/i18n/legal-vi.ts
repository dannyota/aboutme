// Vietnamese text of the Privacy Policy and Terms of Service. Every statement
// here must describe shipped behavior or an owner decision; do not add claims.
import type { LegalCopy } from './legal';

export const legalVi: LegalCopy = {
  updated: 'Cập nhật lần cuối ngày 01/10/2026',
  privacyLink: 'Chính sách quyền riêng tư',
  termsLink: 'Điều khoản sử dụng',
  verifyLink: 'Kiểm chứng',
  guideLink: 'Kết nối AI',
  agreement: [
    'Khi tạo tài khoản, bạn xác nhận đủ 16 tuổi và đồng ý với ',
    ' và ',
    '.',
  ],
  privacy: {
    title: 'Chính sách quyền riêng tư',
    description:
      'Chính sách quyền riêng tư của aboutme.vn: chúng tôi thu thập gì, dùng '
      + 'để làm gì, và bạn có những lựa chọn nào.',
    intro:
      'aboutme.vn là công cụ tạo CV mã nguồn mở. Trang này giải thích '
      + 'chúng tôi thu thập gì, dùng để làm gì, và bạn có những lựa chọn '
      + 'nào.',
    operator: {
      text:
        'aboutme.vn do Danny, một cá nhân, vận hành phi thương mại tại '
        + 'Việt Nam.',
      contactLabel: 'Liên hệ',
    },
    sections: [
      {
        heading: 'Dữ liệu cá nhân chúng tôi thu thập',
        items: [
          'Tài khoản: email, tên, và mật khẩu của bạn. Mật khẩu chỉ được '
          + 'lưu dưới dạng mã băm Argon2id. Nếu bạn đăng nhập bằng Google '
          + 'hoặc LinkedIn: ID tài khoản của bạn tại nhà cung cấp đó, '
          + 'email và tên.',
          'Nhập từ LinkedIn: trình duyệt của bạn đọc tệp PDF hồ sơ '
          + 'LinkedIn bạn chọn. Chúng tôi không nhận tệp này; chúng tôi '
          + 'chỉ lưu CV bạn tạo từ nó.',
          'Nội dung: các CV bạn viết, ảnh bạn tải lên, và, khi một CV '
          + 'đang công khai, ảnh xem trước chúng tôi tạo từ CV đó để hiển '
          + 'thị khi đường dẫn được chia sẻ.',
          'Trang Cộng đồng: nếu bạn bật tùy chọn này cho một CV, chúng tôi '
          + 'lưu thời điểm bạn bật, vị trí bạn chọn, và kết quả duyệt.',
          'Phiên đăng nhập: thông tin trình duyệt (user-agent) và địa chỉ '
          + 'IP của từng phiên, dùng cho bảo mật. Mục Cài đặt → Thiết bị đã '
          + 'đăng nhập liệt kê các thiết bị đang đăng nhập. Chúng tôi xóa địa chỉ IP và '
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
          'Lượt xem CV công khai: chúng tôi đếm lượt xem và chỉ lưu tổng '
          + 'số theo ngày. Để phân biệt người với bot, địa chỉ IP và '
          + 'thông tin trình duyệt của bạn chỉ được dùng tạm trong bộ nhớ '
          + 'máy chủ và bị xóa trong ngày; trình duyệt của bạn giải một '
          + 'phép tính nhỏ. Việc đếm lượt xem không dùng cookie.',
          'Nếu chủ CV yêu cầu đăng nhập để xem: Google hoặc LinkedIn xác '
          + 'minh tài khoản của bạn. Chúng tôi chỉ yêu cầu xác minh đăng '
          + 'nhập, không yêu cầu tên hay email, nên chỉ nhận một mã định '
          + 'danh tài khoản do nhà cung cấp cấp, và xóa mã đó ngay trong '
          + 'lần đăng nhập. Chủ CV không được cho biết bạn là ai.',
        ],
      },
      {
        heading: 'Mục đích và cơ sở xử lý',
        paragraphs: [
          'Chúng tôi xử lý dữ liệu cá nhân của bạn để thực hiện thỏa '
          + 'thuận giữa bạn và aboutme.vn theo Điều khoản sử dụng: cung cấp '
          + 'tài khoản và các CV bạn tạo, giữ an toàn cho dịch vụ, và gửi '
          + 'email về tài khoản. Các tính năng tùy chọn (xuất bản CV công '
          + 'khai, cho phép lập chỉ mục, hiện CV trong trang Cộng đồng, kết '
          + 'nối trợ lý AI, đăng nhập bằng Google hoặc LinkedIn) chỉ chạy '
          + 'khi bạn tự bật, và bạn có thể tắt bất cứ lúc nào. Bạn có thể '
          + 'chấm dứt thỏa thuận bằng cách xóa tài khoản.',
          'Người xem CV công khai không cần tài khoản. Khi bạn mở một CV '
          + 'công khai, chúng tôi dùng địa chỉ IP và thông tin trình '
          + 'duyệt của bạn để phục vụ trang, chặn lạm dụng, và lọc bot '
          + 'khỏi số lượt xem. Kết quả chỉ là tổng số theo ngày, không mô '
          + 'tả bất kỳ ai. Nếu chủ CV yêu cầu đăng nhập để xem, bạn tự '
          + 'chọn đăng nhập để yêu cầu xem CV đó; chúng tôi chỉ xử lý mã '
          + 'định danh tài khoản để đáp ứng yêu cầu này.',
        ],
      },
      {
        heading: 'Chúng tôi không làm gì',
        items: [
          'Không quảng cáo.',
          'Không dùng công cụ phân tích hay mã theo dõi quảng cáo của bên '
          + 'thứ ba. Chúng tôi chỉ đếm lượt xem CV công khai như mô tả ở '
          + 'trên.',
          'Không bán dữ liệu của bạn và không chia sẻ dữ liệu cho mục đích '
          + 'tiếp thị.',
          'Cookie chỉ dùng để giữ phiên đăng nhập, hoàn tất đăng nhập bằng '
          + 'Google hoặc LinkedIn, giữ trạng thái xác thực hai bước đang '
          + 'chờ trong năm phút, ghi nhớ giao diện và ngôn ngữ bạn chọn, '
          + 'và, khi chủ CV yêu cầu đăng nhập để xem, cho phép bạn xem CV '
          + 'đó trong 7 ngày.',
          'Bộ nhớ của trình duyệt chỉ dùng để ghi nhớ: đường dẫn quay '
          + 'lại sau khi bạn xác minh email (tối đa 24 giờ); chế độ xem '
          + 'trước (PDF hoặc web), mức thu phóng và độ rộng khung chỉnh '
          + 'sửa bạn chọn trong trình chỉnh sửa CV; việc bạn đã đóng lời '
          + 'mời tạo CV miễn phí (tối đa 90 ngày); và, chỉ trong phiên '
          + 'duyệt hiện tại (sessionStorage), thời điểm một trang CV công '
          + 'khai tự tải lại lần cuối, để tránh tải lại liên tục. Không có '
          + 'dữ liệu nào trong số này được gửi đến máy chủ.',
        ],
      },
      {
        heading: 'Chỉ công khai khi bạn chọn',
        paragraphs: [
          'CV ở chế độ riêng tư cho đến khi bạn xuất bản. Mỗi CV có đường '
          + 'dẫn riêng. Việc lập chỉ mục cho công cụ tìm kiếm và AI mặc '
          + 'định tắt cho đến khi bạn bật. Khi bạn hủy xuất bản, đường '
          + 'dẫn công khai ngừng hoạt động ngay lập tức. Khi bạn xóa hoặc '
          + 'đổi đường dẫn của một CV, chúng tôi giữ chỗ đường dẫn cũ '
          + 'trong 180 ngày để không ai khác chiếm được đường dẫn đó. '
          + 'Việc giữ chỗ này không gắn với tài khoản của bạn, và sẽ bị '
          + 'xóa sau 180 ngày.',
          'Trang công khai được phân phối qua mạng CDN toàn cầu '
          + '(Amazon CloudFront) nhưng không được lưu lại trên CDN. '
          + 'Khi CV đang công khai, bất kỳ ai xem được cũng có thể lưu '
          + 'hoặc chụp lại trang. Theo mặc định, người xem cũng có thể '
          + 'tải về bản PDF của CV; bạn có thể tắt tính năng này. Đừng '
          + 'đưa dữ liệu cá nhân nhạy cảm, như số hoặc ảnh giấy tờ tùy '
          + 'thân, tình trạng sức khoẻ, tôn giáo, hoặc quan điểm chính '
          + 'trị, vào một CV công khai.',
          'Khi bất kỳ ai chia sẻ đường dẫn công khai của bạn trong một ứng '
          + 'dụng nhắn tin hoặc mạng xã hội, dịch vụ đó sẽ lấy tiêu đề '
          + 'trang, phần tóm tắt và ảnh xem trước của trang (họ tên, tiêu '
          + 'đề và ảnh của bạn), và có thể giữ bản sao riêng của họ sau khi '
          + 'bạn hủy xuất bản.',
          'Trang Cộng đồng (aboutme.vn/showcase) chỉ hiện những CV mà chủ '
          + 'CV bật Hiện trong trang Cộng đồng. Trang này hiện ảnh xem trước '
          + 'của CV (họ tên, tiêu đề và ảnh của bạn), mẫu, ngôn ngữ và vị '
          + 'trí bạn chọn, kèm đường dẫn đến CV. Chúng tôi duyệt từng CV '
          + 'trước khi hiện, và duyệt lại khi họ tên, tiêu đề, ảnh, đường '
          + 'dẫn hoặc ngôn ngữ của CV thay đổi. Khi bạn tắt tùy chọn này, '
          + 'hủy xuất bản, hoặc bật Yêu cầu đăng nhập để xem, CV rời khỏi '
          + 'trang Cộng đồng ngay lập tức. Trang Cộng đồng không cho công cụ '
          + 'tìm kiếm lập chỉ mục, nhưng bất kỳ ai truy cập đều có thể xem '
          + 'và sao chép những gì trang hiển thị.',
        ],
      },
      {
        heading: 'Dữ liệu được lưu ở đâu',
        paragraphs: [
          'Dữ liệu của bạn được lưu tại Amazon Web Services ở Singapore '
          + '(ap-southeast-1): cơ sở dữ liệu, bản sao lưu, kho ảnh, và '
          + 'việc gửi email qua Amazon SES. Amazon CloudFront (mạng máy '
          + 'chủ toàn cầu của Amazon Web Services) phân phối trang web; '
          + 'CloudFront và AWS WAF xử lý địa chỉ IP và thông tin trình '
          + 'duyệt của bạn để chặn tấn công và nhận diện bot. Amazon Route 53 cung cấp dịch vụ '
          + 'DNS. '
          + 'Nếu bạn đăng nhập bằng Google, Google (Hoa Kỳ) xác thực '
          + 'tài khoản của bạn. Nếu bạn đăng nhập bằng LinkedIn, LinkedIn '
          + '(Hoa Kỳ) xác thực tài khoản của bạn. Email bạn gửi cho '
          + 'chúng tôi được lưu trong hộp thư Google Workspace. Nếu bạn '
          + 'ở Việt Nam, dữ liệu cá nhân của bạn được chuyển ra nước '
          + 'ngoài: chủ yếu đến Singapore, và một phần đến Google và '
          + 'LinkedIn.',
          'Kiểm tra mật khẩu dùng dịch vụ Have I Been Pwned.',
        ],
      },
      {
        heading: 'Trợ lý AI được kết nối',
        paragraphs: [
          'Trợ lý AI bạn kết nối chỉ làm được những gì bạn cho phép: đọc '
          + 'CV, và nếu bạn cấp quyền ghi, tạo, sửa hoặc xóa CV và ảnh. '
          + 'Trợ lý không thể xuất bản hay hủy xuất bản CV, nhưng nếu '
          + 'xóa một CV đang công khai thì đường dẫn của CV đó ngừng hoạt '
          + 'động. Nội dung trợ lý đọc được sẽ đến dịch vụ AI mà bạn chọn, có '
          + 'thể ở ngoài Việt Nam. Bạn là người quyết định việc chuyển '
          + 'dữ liệu này. Bạn có thể thu hồi quyền bất cứ lúc nào trong '
          + 'Cài đặt.',
        ],
      },
      {
        heading: 'Quyền kiểm soát của bạn',
        paragraphs: [
          'Bạn có thể xuất dữ liệu tài khoản, sửa hoặc xóa CV, và xóa tài '
          + 'khoản. Chúng tôi giữ tài khoản, CV và ảnh của bạn cho đến khi '
          + 'bạn xóa chúng hoặc xóa tài khoản. Bản xuất gồm hồ sơ và nội dung CV của bạn; ảnh và '
          + 'lịch sử phiên đăng nhập xin liên hệ qua email.',
          'Khi bạn xóa tài khoản:',
        ],
        items: [
          'Quyền truy cập bị thu hồi ngay lập tức.',
          'Ảnh đã tải lên được xóa, thường trong vòng 24 giờ.',
          'Bản sao lưu cơ sở dữ liệu giữ các bản cũ trong tối đa 30 ngày '
          + 'sau đó, và chỉ dùng để khôi phục sau sự cố.',
          'Bản ghi về việc xóa tài khoản và gỡ liên kết nhà cung cấp (chỉ '
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
          'Bạn có quyền được biết về việc xử lý dữ liệu cá nhân của mình; '
          + 'đồng ý, không đồng ý, hoặc rút lại sự đồng ý; xem, chỉnh '
          + 'sửa, hoặc yêu cầu chỉnh sửa; yêu cầu cung cấp, xóa, hoặc hạn '
          + 'chế xử lý, và phản đối việc xử lý; yêu cầu chúng tôi áp dụng '
          + 'biện pháp bảo vệ dữ liệu của bạn; và khiếu nại, tố cáo, khởi '
          + 'kiện, yêu cầu bồi thường thiệt hại theo quy định của pháp '
          + 'luật. Bạn tự xuất dữ liệu, sửa hoặc xóa CV, và xóa tài khoản '
          + 'trong Cài đặt. Các yêu cầu khác xin gửi qua email bên dưới; '
          + 'nếu yêu cầu liên quan đến một tài khoản, xin gửi từ địa chỉ '
          + 'email của tài khoản đó để chúng tôi xác minh. Chúng tôi phản '
          + 'hồi trong vòng 2 ngày làm việc. Chúng tôi cho bạn xem, sửa, '
          + 'hoặc nhận bản sao dữ liệu trong vòng 10 ngày; xóa dữ liệu '
          + 'trong vòng 20 ngày; và xử lý yêu cầu hạn chế, phản đối, rút '
          + 'lại sự đồng ý, hoặc áp dụng biện pháp bảo vệ trong vòng 15 '
          + 'ngày. Nếu một yêu cầu cần thêm thời gian, chúng tôi báo cho '
          + 'bạn lý do và chỉ gia hạn một lần, trong giới hạn pháp luật '
          + 'cho phép.',
          'Nếu xảy ra sự cố làm lộ hoặc mất dữ liệu cá nhân của bạn, '
          + 'chúng tôi báo cho cơ quan chuyên trách bảo vệ dữ liệu cá nhân '
          + 'thuộc Bộ Công an theo quy định của pháp luật và gửi email '
          + 'cho bạn.',
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
      'Điều khoản sử dụng aboutme.vn, công cụ tạo CV miễn phí và mã nguồn '
      + 'mở.',
    intro:
      'Các điều khoản này áp dụng khi bạn sử dụng aboutme.vn.',
    sections: [
      {
        heading: 'Dịch vụ',
        paragraphs: [
          'aboutme.vn miễn phí. Mã nguồn được công bố theo giấy phép '
          + 'AGPL-3.0.',
        ],
        repositoryLink: 'Mã nguồn trên GitHub',
      },
      {
        heading: 'Nội dung của bạn',
        paragraphs: [
          'Bạn sở hữu nội dung bạn tạo. Bạn cho phép aboutme.vn lưu trữ, sao '
          + 'lưu, kết xuất thành PDF, gửi đến các trợ lý AI bạn kết nối, '
          + 'và hiển thị nội dung đó theo cách bạn chọn, ví dụ khi bạn '
          + 'xuất bản CV. Quyền này chấm dứt khi bạn xóa nội dung, trừ các '
          + 'bản sao lưu cho đến khi chúng hết hạn.',
          'Nếu bạn bật Hiện trong trang Cộng đồng cho một CV, bạn cũng cho '
          + 'phép aboutme.vn hiện ảnh xem trước, mẫu, ngôn ngữ và vị trí '
          + 'bạn chọn của CV đó trên trang Cộng đồng, cho đến khi bạn tắt '
          + 'tùy chọn này hoặc hủy xuất bản.',
          'Bạn chịu trách nhiệm về nội dung bạn xuất bản, kể cả tính '
          + 'chính xác và quyền chia sẻ thông tin hoặc hình ảnh của người '
          + 'khác.',
        ],
      },
      {
        heading: 'Quy tắc sử dụng',
        paragraphs: [
          'Không dùng aboutme.vn để:',
        ],
        items: [
          'đăng nội dung vi phạm pháp luật;',
          'mạo danh người khác;',
          'đăng dữ liệu cá nhân của người khác khi chưa được họ cho '
          + 'phép;',
          'gửi thư rác, phát tán phần mềm độc hại, hoặc lạm dụng dịch vụ '
          + 'hay người khác.',
        ],
        after: [
          'Chúng tôi duyệt mọi CV trước khi hiện trong trang Cộng đồng và '
          + 'có thể từ chối hoặc gỡ CV khỏi trang này.',
          'Chúng tôi có thể gỡ nội dung hoặc xóa tài khoản vi phạm các quy '
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
          + 'sẽ xóa tài khoản đó.',
          'Bạn chịu trách nhiệm về hành động của các trợ lý AI bạn kết '
          + 'nối với tài khoản.',
        ],
      },
      {
        heading: 'Chấm dứt',
        paragraphs: [
          'Bạn có thể xóa tài khoản bất cứ lúc nào. Nếu chúng tôi gỡ nội '
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
          + 'Nếu không đồng ý, bạn có thể xuất dữ liệu và xóa tài khoản '
          + 'trước ngày đó. Việc bạn tiếp tục sử dụng dịch vụ sau khi '
          + 'thay đổi có hiệu lực đồng nghĩa với việc bạn chấp nhận điều '
          + 'khoản mới. Nếu một thay đổi cần sự đồng ý của bạn theo pháp '
          + 'luật, chúng tôi sẽ xin riêng; việc tiếp tục sử dụng không '
          + 'được coi là đồng ý.',
        ],
        contactLabel: 'Liên hệ',
      },
    ],
  },
};
