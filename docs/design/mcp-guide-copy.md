# MCP guide copy

The Vietnamese and English text of the [MCP guide](mcp-guide.md), in page order.
Vietnamese is the default. **Bold** marks a UI name set in weight 600; backticks
mark inline code. Claude's UI names stay in English in both languages. `{url}`
is `https://aboutme.vn/mcp`, built from the site origin. A slash separates a
troubleshooting question from its answer.

## Head

- Title
  - vi: Kết nối trợ lý AI qua MCP · aboutme.vn
  - en: Connect your AI assistant with MCP · aboutme.vn
- Description
  - vi: Kết nối Claude hoặc trợ lý AI khác với aboutme.vn qua MCP để đọc và sửa
    CV. Bạn duyệt quyền, thu hồi bất cứ lúc nào và tự quyết định xuất bản.
  - en: Connect Claude or another AI assistant to aboutme.vn through MCP to read
    and edit your resumes. You approve access, revoke it any time, and decide
    what gets published.
- Sitemap and `llms.txt` title (English only, like the other site pages):
  Connect an AI assistant with MCP

## Links elsewhere

- Header and landing footer
  - vi: Kết nối AI
  - en: Connect AI
- Landing “Bring your own AI” card
  - vi: Xem cách kết nối
  - en: See how to connect
- Settings, Connected agents
  - vi: Xem cách kết nối trợ lý AI
  - en: See how to connect an AI assistant

## Header block

- `h1`
  - vi: Kết nối trợ lý AI với aboutme.vn
  - en: Connect your AI assistant to aboutme.vn
- Lead
  - vi: Dùng Claude hoặc một trợ lý AI khác hỗ trợ MCP để đọc và chỉnh sửa CV
    của bạn. Bạn duyệt từng kết nối, còn việc xuất bản vẫn do bạn quyết định.
  - en: Use Claude or another MCP-capable AI assistant to read and edit your
    resumes. You approve each connection, and publishing stays your decision.
- Explainer
  - vi: MCP (Model Context Protocol) là một chuẩn mở giúp trợ lý AI dùng công cụ
    của dịch vụ khác. aboutme.vn không chạy mô hình AI nào; trợ lý bạn chọn làm
    việc với CV qua đúng những bước kiểm tra mà trình chỉnh sửa dùng.
  - en: MCP (Model Context Protocol) is an open standard that lets an AI
    assistant use another service's tools. aboutme.vn runs no AI model; the
    assistant you choose works on your resumes through the same checks as the
    editor.
- URL label
  - vi: Địa chỉ máy chủ MCP
  - en: MCP server URL
- URL block: `{url}`
- Copy button name
  - vi: Sao chép địa chỉ
  - en: Copy URL
- Copied announcement
  - vi: Đã sao chép địa chỉ máy chủ MCP.
  - en: Copied the MCP server URL.
- Copy failed announcement
  - vi: Không sao chép được. Hãy chọn địa chỉ và tự sao chép.
  - en: Could not copy. Select the URL and copy it yourself.
- Account line, with two links
  - vi: Bạn cần một tài khoản aboutme.vn. [Tạo tài khoản] hoặc [đăng nhập] trước
    để kết nối nhanh hơn.
  - en: You need an aboutme.vn account. [Create an account] or [sign in] first
    to connect faster.

## What your assistant can do

- `h2`
  - vi: Trợ lý AI làm được gì
  - en: What your assistant can do
- Can heading
  - vi: Được phép
  - en: Can
- Can items
  1. - vi: Xem danh sách và đọc CV của bạn, kể cả ảnh chân dung.
     - en: List and read your resumes, including the photo.
  2. - vi: Tạo CV mới, trong giới hạn ba CV mỗi tài khoản.
     - en: Create a resume, within the limit of three per account.
  3. - vi: Sửa nội dung, các mục, bố cục, thông tin cá nhân và giao diện.
     - en: Edit content, sections, layout, personal details, and styling.
  4. - vi: Tải lên, cắt hoặc xóa ảnh chân dung.
     - en: Upload, crop, or remove the photo.
  5. - vi: Xóa CV. Xóa một CV đã xuất bản cũng gỡ đường dẫn công khai của CV đó.
     - en: Delete a resume. Deleting a published resume also takes its public
       link down.
- Cannot heading
  - vi: Không được phép
  - en: Cannot
- Cannot items
  1. - vi: Xuất bản, hủy xuất bản, hay đổi cài đặt PDF và lập chỉ mục. Bạn làm
       việc này trong trình chỉnh sửa.
     - en: Publish, unpublish, or change PDF and indexing settings. You do that
       in the editor.
  2. - vi: Đọc CV của người khác.
     - en: Read anyone else's resumes.
  3. - vi: Xem hay đổi mật khẩu, email, phiên đăng nhập hoặc cài đặt tài khoản.
     - en: See or change your password, email, sessions, or account settings.
  4. - vi: Xem lượt xem, xuất PDF hoặc nhập hồ sơ LinkedIn.
     - en: See view counts, export a PDF, or import from LinkedIn.
  5. - vi: Lưu thứ mà trình chỉnh sửa sẽ từ chối. Mọi thay đổi đều qua cùng các
       bước kiểm tra.
     - en: Save anything the editor would reject. Every change passes the same
       checks.
- Scope line
  - vi: Quyền áp dụng cho mọi CV trong tài khoản; không giới hạn được cho riêng
    một CV.
  - en: Access covers every resume in your account; you cannot limit it to one
    resume.

## Connect Claude

- `h2`
  - vi: Kết nối Claude
  - en: Connect Claude
- Intro
  - vi: Claude hỗ trợ máy chủ MCP từ xa có đăng nhập. Tên nút và menu bên dưới
    được giữ nguyên như Claude hiển thị.
  - en: Claude supports remote MCP servers with sign-in. Button and menu names
    below are as Claude shows them.
- `h3`
  - vi: Claude trên web, máy tính và điện thoại
  - en: Claude on the web, desktop, and mobile
- Steps
  1. - vi: Trong Claude, mở **Customize**, rồi **Connectors**.
     - en: In Claude, open **Customize**, then **Connectors**.
  2. - vi: Chọn **+**, rồi **Add custom connector**.
     - en: Choose **+**, then **Add custom connector**.
  3. - vi: Đặt tên `aboutme.vn` và dán `{url}` vào ô URL. Để trống **Advanced
       settings**.
     - en: Name it `aboutme.vn` and paste `{url}` as the URL. Leave **Advanced
       settings** empty.
  4. - vi: Chọn **Add**. Nếu Claude hiện **Connect**, hãy chọn nút đó.
     - en: Choose **Add**. If Claude shows **Connect**, choose it.
  5. - vi: Trang aboutme.vn mở ra. Đăng nhập nếu được hỏi, kiểm tra yêu cầu ghi
       tên Claude và quay lại claude.ai, rồi chọn **Cho phép**.
     - en: An aboutme.vn page opens. Sign in if asked, check that the request
       names Claude and returns to claude.ai, then choose **Approve**.
  6. - vi: Trong một cuộc trò chuyện, chọn **+**, rồi **Connectors**, và bật
       aboutme.vn.
     - en: In a chat, choose **+**, then **Connectors**, and turn on aboutme.vn.
- Plan note
  - vi: Gói Free cho phép một connector tùy chỉnh. Với gói Team hoặc Enterprise,
    chủ sở hữu tổ chức thêm connector trong **Organization settings** >
    **Connectors**, rồi mỗi thành viên chọn **Connect**. [Trang trợ giúp của
    Claude]
  - en: The Free plan allows one custom connector. On Team and Enterprise plans,
    an organization owner adds it in **Organization settings** > **Connectors**,
    then each member chooses **Connect**. [Claude's help page]
- `h3`: Claude Code
- Steps
  1. - vi: Thêm máy chủ cho mọi dự án của bạn:
     - en: Add the server for all your projects:
     - Command block:
       `claude mcp add --transport http --scope user aboutme {url}`
  2. - vi: Đăng nhập. Trình duyệt mở aboutme.vn; chọn **Cho phép**. Trong Claude
       Code, bạn cũng có thể gõ `/mcp` rồi chọn aboutme.
     - en: Sign in. Your browser opens aboutme.vn; choose **Approve**. Inside
       Claude Code you can also run `/mcp` and pick aboutme.
     - Command block: `claude mcp login aboutme`
  3. - vi: Để gỡ, chạy `claude mcp remove aboutme`, rồi thu hồi quyền trong Cài
       đặt của aboutme.vn.
     - en: To remove it, run `claude mcp remove aboutme`, then revoke access in
       aboutme.vn Settings.
- Command copy button name
  - vi: Sao chép lệnh
  - en: Copy command
- Command copied announcement
  - vi: Đã sao chép lệnh.
  - en: Copied the command.

## Try a request

- `h2`
  - vi: Thử một yêu cầu
  - en: Try a request
- Examples
  1. - vi: Đọc CV của tôi và đề xuất một phần tóm tắt ngắn gọn hơn.
     - en: Read my resume and suggest a shorter summary.
  2. - vi: Tạo một bản CV tiếng Việt từ CV tiếng Anh của tôi. Giữ nguyên tên
       công ty, ngày tháng và số liệu.
     - en: Make a Vietnamese copy of my English resume. Keep company names,
       dates, and numbers exactly.
  3. - vi: Viết lại các gạch đầu dòng ở công việc gần nhất để nêu kết quả trước,
       nhưng không thêm điều tôi chưa làm.
     - en: Rewrite the bullets in my latest job to lead with results, without
       adding anything I did not do.
- After
  - vi: Mở CV trong trình chỉnh sửa để xem lại thay đổi, rồi tự xuất bản khi bạn
    sẵn sàng.
  - en: Open the resume in the editor to review the changes, then publish it
    yourself when you are ready.

## Privacy and control

- `h2`
  - vi: Quyền riêng tư và quyền kiểm soát
  - en: Privacy and control
- Points
  1. - vi: Bạn duyệt từng kết nối trên aboutme.vn. Trang cấp quyền ghi tên ứng
       dụng, nơi bạn sẽ quay lại, và hai quyền: Đọc CV và Chỉnh sửa CV.
     - en: You approve each connection on aboutme.vn. The approval page names
       the app, where you return, and two permissions: Read resumes and Write
       resumes.
  2. - vi: Chỉ cho phép yêu cầu do chính bạn vừa bắt đầu. Nếu trang cấp quyền mở
       ra khi bạn không kết nối gì, hãy chọn **Từ chối**.
     - en: Approve only a request you just started. If an approval page opens
       when you did not connect anything, choose **Deny**.
  3. - vi: Trợ lý không bao giờ nhận mật khẩu hay phiên đăng nhập của bạn, chỉ
       nhận một mã truy cập riêng mà bạn thu hồi được.
     - en: The assistant never gets your password or sign-in session, only its
       own access token, which you can revoke.
  4. - vi: Nội dung trợ lý đọc được sẽ đến dịch vụ AI bạn chọn và theo chính
       sách của dịch vụ đó. Xem [Chính sách quyền riêng tư].
     - en: Content the assistant reads goes to the AI service you chose, under
       that service's policy. See the [Privacy Policy].
  5. - vi: Thu hồi bất cứ lúc nào trong [Cài đặt] > Tác nhân đã kết nối. Yêu cầu
       tiếp theo của trợ lý sẽ bị từ chối; các thay đổi đã lưu vẫn còn.
     - en: Revoke any time in [Settings] > Connected agents. The assistant's
       next request is refused; changes it already saved stay.

## Other apps

- `h2`
  - vi: Ứng dụng khác
  - en: Other apps
- Requirements
  - vi: Ứng dụng hỗ trợ máy chủ MCP từ xa (Streamable HTTP) với đăng nhập OAuth
    và tự đăng ký ứng dụng có thể dùng cùng địa chỉ `{url}`.
  - en: An app that supports remote MCP servers (Streamable HTTP) with OAuth
    sign-in and automatic client registration can use the same URL, `{url}`.
- Limits
  - vi: Ứng dụng chỉ chạy máy chủ MCP cục bộ, chỉ nhận khóa API, hoặc quay về
    một địa chỉ riêng của ứng dụng thay vì https hay localhost thì chưa kết nối
    được.
  - en: Apps that only run local MCP servers, only accept API keys, or return to
    an app-specific address instead of https or localhost cannot connect yet.
- Visual Studio Code, shown only after its proof passes
  - vi: Visual Studio Code: chạy lệnh **MCP: Add Server**, chọn **HTTP**, dán
    địa chỉ, rồi đăng nhập khi được hỏi.
  - en: Visual Studio Code: run **MCP: Add Server**, choose **HTTP**, paste the
    URL, and sign in when asked.

## Troubleshooting

- `h2`
  - vi: Khắc phục sự cố
  - en: Troubleshooting
- Pairs
  1. - vi: Claude báo không kết nối được. / Kiểm tra địa chỉ đúng là `{url}`,
       không có dấu cách hay dấu gạch chéo ở cuối. Gỡ connector rồi thêm lại.
     - en: Claude says it could not connect. / Check that the URL is exactly
       `{url}`, with no space or trailing slash. Remove the connector and add it
       again.
  2. - vi: Trang cấp quyền báo yêu cầu không hợp lệ. / Yêu cầu đã hết hạn hoặc
       đã được dùng. Bắt đầu kết nối lại từ trợ lý.
     - en: The approval page says the request is invalid. / The request expired
       or was already used. Start the connection again from your assistant.
  3. - vi: Trang báo bạn đã có 10 trợ lý được kết nối. / Thu hồi một trợ lý cũ
       trong [Cài đặt], rồi kết nối lại.
     - en: The page says you already have 10 connected agents. / Revoke an old
       one in [Settings], then connect again.
  4. - vi: Báo quá nhiều lần thử. / aboutme.vn giới hạn số lần ứng dụng đăng ký
       và đăng nhập. Đợi đến một giờ rồi thử lại.
     - en: It says there were too many attempts. / aboutme.vn limits how often
       apps register and sign in. Wait up to an hour and try again.
  5. - vi: Trợ lý không tạo được CV mới. / Mỗi tài khoản có tối đa ba CV. Xóa
       một CV hoặc để trợ lý sửa CV có sẵn.
     - en: The assistant cannot create a resume. / Each account holds at most
       three resumes. Delete one, or have the assistant edit an existing one.
  6. - vi: Trợ lý báo CV đã thay đổi hoặc không xuất bản được. / CV đã được sửa
       ở nơi khác, ví dụ trong trình chỉnh sửa: bảo trợ lý đọc lại rồi thử lại.
       Việc xuất bản thì luôn do bạn làm trong trình chỉnh sửa.
     - en: The assistant says the resume changed, or it cannot publish. / The
       resume was edited somewhere else, such as the editor: ask the assistant
       to read it again and retry. Publishing is always yours to do in the
       editor.
