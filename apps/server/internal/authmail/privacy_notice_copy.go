package authmail

// PrivacyNoticeURL and the message copy are compiled, never payload-derived.
const (
	PrivacyNoticeURL       = "https://aboutme.vn/privacy"
	PrivacyNoticeSubject   = "Cập nhật quyền riêng tư aboutme.vn / aboutme.vn privacy update"
	PrivacyNoticeAction    = "Đọc chính sách quyền riêng tư · Read the privacy policy"
	PrivacyNoticePreheader = "Chính sách quyền riêng tư đã được cập nhật. The privacy policy has been updated."
	PrivacyNoticeHeadingVI = "Thông báo trước về thay đổi chống tấn công"
	PrivacyNoticeHeadingEN = "Advance notice of an attack-protection change"
	PrivacyNoticeBodyVI    = "Chúng tôi đã cập nhật chính sách quyền riêng tư của aboutme.vn. Trước khi áp dụng thay đổi, chúng tôi gửi email này để thông báo: mẫu cảnh báo tấn công có thể chứa đường dẫn đích của tuyến không được nhận diện trả về mã 400, 404 hoặc 405, tối đa 512 byte và không có phần truy vấn. Đường dẫn của tuyến đã biết, gồm tài khoản, API và CV công khai hợp lệ, không được ghi. Dữ liệu CrowdSec sẽ được lưu trong PostgreSQL tại Việt Nam; bản ghi đang hoạt động được giữ tối đa 24 giờ. Bản sao lưu được mã hóa tại Việt Nam có thể giữ bản ghi cũ trong khoảng năm tuần sau khi xóa. Vui lòng đọc chính sách đã cập nhật để biết đầy đủ phạm vi thu thập và cách lưu giữ. Bạn không cần thực hiện thao tác nào. Bạn có thể trả lời email này nếu có câu hỏi."
	PrivacyNoticeBodyEN    = "We have updated the aboutme.vn privacy policy. Before this change takes effect, we are emailing you to explain it: attack alert samples may include target paths for unrecognized routes returning 400, 404, or 405, up to 512 bytes and without the query. Paths for known routes, including account, API, and valid public resume routes, are not recorded. CrowdSec data will be stored in PostgreSQL in Vietnam; live records are kept for at most 24 hours. Encrypted backups in Vietnam may retain earlier records for about five weeks after deletion. Please read the updated policy for the full collection and retention rules. You do not need to take any action. You can reply to this email with questions."
)

func privacyNoticeTemplate() template {
	return template{
		subject:   PrivacyNoticeSubject,
		preheader: PrivacyNoticePreheader,
		action:    PrivacyNoticeAction,
		vi:        section{lang: "vi", heading: PrivacyNoticeHeadingVI, body: PrivacyNoticeBodyVI},
		en:        section{lang: "en", heading: PrivacyNoticeHeadingEN, body: PrivacyNoticeBodyEN},
	}
}
