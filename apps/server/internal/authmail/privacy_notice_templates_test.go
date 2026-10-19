package authmail

import (
	"html"
	"strings"
	"testing"
)

func TestPrivacyNoticeFixedBilingualMessage(t *testing.T) {
	payload, err := decodePayloadStrict([]byte(noticeFixtureJSON))
	if err != nil {
		t.Fatal("notice fixture rejected")
	}
	message := buildMessage(Kind("privacy_notice"), payload, FooterNoteForMode("smtp"))
	if message.Subject != "Cập nhật quyền riêng tư aboutme.vn / aboutme.vn privacy update" {
		t.Fatal("notice subject differs")
	}
	vi := "Chúng tôi đã cập nhật chính sách quyền riêng tư của aboutme.vn. Trước khi áp dụng thay đổi, chúng tôi gửi email này để thông báo: mẫu cảnh báo tấn công có thể chứa đường dẫn đích của tuyến không được nhận diện trả về mã 400, 404 hoặc 405, tối đa 512 byte và không có phần truy vấn. Đường dẫn của tuyến đã biết, gồm tài khoản, API và CV công khai hợp lệ, không được ghi. Dữ liệu CrowdSec sẽ được lưu trong PostgreSQL tại Việt Nam; bản ghi đang hoạt động được giữ tối đa 24 giờ. Bản sao lưu được mã hóa tại Việt Nam có thể giữ bản ghi cũ trong khoảng năm tuần sau khi xóa. Vui lòng đọc chính sách đã cập nhật để biết đầy đủ phạm vi thu thập và cách lưu giữ. Bạn không cần thực hiện thao tác nào. Bạn có thể trả lời email này nếu có câu hỏi."
	en := "We have updated the aboutme.vn privacy policy. Before this change takes effect, we are emailing you to explain it: attack alert samples may include target paths for unrecognized routes returning 400, 404, or 405, up to 512 bytes and without the query. Paths for known routes, including account, API, and valid public resume routes, are not recorded. CrowdSec data will be stored in PostgreSQL in Vietnam; live records are kept for at most 24 hours. Encrypted backups in Vietnam may retain earlier records for about five weeks after deletion. Please read the updated policy for the full collection and retention rules. You do not need to take any action. You can reply to this email with questions."
	for _, body := range []string{vi, en} {
		if strings.Count(message.TextBody, body) != 1 || strings.Count(message.HTMLBody, html.EscapeString(body)) != 1 {
			t.Fatal("notice body differs")
		}
	}
	if strings.Index(message.TextBody, vi) >= strings.Index(message.TextBody, en) || strings.Index(message.HTMLBody, html.EscapeString(vi)) >= strings.Index(message.HTMLBody, html.EscapeString(en)) {
		t.Fatal("notice language order differs")
	}
	for _, body := range []string{message.TextBody, message.HTMLBody} {
		for _, required := range []string{"Thông báo trước về thay đổi chống tấn công", "Advance notice of an attack-protection change", "https://aboutme.vn/privacy", "Bizfly", "Google Workspace"} {
			if !strings.Contains(body, required) {
				t.Fatal("fixed notice component missing")
			}
		}
		for _, forbidden := range []string{payload.To, "token=", "unsubscribe", "<img", "<script"} {
			if strings.Contains(body, forbidden) {
				t.Fatal("notice contains forbidden material")
			}
		}
	}
	if strings.Count(message.HTMLBody, "Đọc chính sách quyền riêng tư · Read the privacy policy") != 1 {
		t.Fatal("notice action differs")
	}
	if !strings.Contains(message.HTMLBody, "Chính sách quyền riêng tư đã được cập nhật. The privacy policy has been updated.") {
		t.Fatal("notice preheader differs")
	}
}
