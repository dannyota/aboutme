package privacynotice

import (
	"crypto/sha256"
	"strings"

	"github.com/dannyota/aboutme/apps/server/internal/authmail"
)

// PublicationDate is the approved notice publication date.
const PublicationDate = "2026-10-19"

const publicEN = "Attack protection uses your IP address, request time, method, response status, broad route class, and a fixed probe category. Only for unrecognized routes returning 400, 404, or 405, attack alert samples may include the exact escaped target path, without the query, up to 512 bytes. We do not record target paths for known routes, including account, API, and valid public resume routes. Invalid or oversized targets use a fixed category and no path. Queries, headers, and bodies are never included. The request feed stays in RAM; CrowdSec stores attack alert samples and bans, not a full access log. Live records are kept for at most 24 hours. Encrypted database backups in Vietnam may retain earlier records for about five weeks after deletion, under the backup policy below. Backups are used only for disaster recovery. Restored CrowdSec data is discarded before attack protection resumes. None of this data is shared with the CrowdSec community."
const publicVI = "Hệ thống chống tấn công dùng địa chỉ IP, thời điểm yêu cầu, phương thức, mã phản hồi, nhóm tuyến truy cập và nhóm dấu hiệu thăm dò cố định. Chỉ với tuyến không được nhận diện trả về mã 400, 404 hoặc 405, mẫu cảnh báo tấn công mới có thể chứa nguyên đường dẫn đích đã mã hóa ký tự, bỏ phần truy vấn, tối đa 512 byte. Chúng tôi không ghi đường dẫn đích của các tuyến đã biết, gồm tuyến tài khoản, API và CV công khai hợp lệ. Đích không hợp lệ hoặc quá dài chỉ được gán nhóm cố định, không ghi đường dẫn. Tham số truy vấn, tiêu đề và nội dung yêu cầu không được ghi. Dữ liệu yêu cầu nằm trong RAM; CrowdSec lưu mẫu cảnh báo tấn công và lệnh chặn, không lưu nhật ký đầy đủ mọi lượt truy cập. Bản ghi đang hoạt động được giữ tối đa 24 giờ. Bản sao lưu cơ sở dữ liệu được mã hóa tại Việt Nam có thể giữ bản ghi cũ trong khoảng năm tuần sau khi xóa, theo chính sách lưu bản sao bên dưới. Bản sao lưu chỉ dùng để khôi phục sau sự cố. Dữ liệu CrowdSec được khôi phục sẽ bị loại bỏ trước khi hệ thống chống tấn công hoạt động trở lại. Không dữ liệu nào được chia sẻ với cộng đồng CrowdSec."

// ManifestBytes returns the literal UTF-8 manifest, including its final LF.
func ManifestBytes() []byte {
	fields := []string{
		"aboutme-crowdsec-privacy-content-v1",
		"campaign_id=" + authmail.PrivacyNoticeID,
		"publication_date=" + PublicationDate,
		"canonical_link=" + authmail.PrivacyNoticeURL,
		"public_en=" + publicEN,
		"public_vi=" + publicVI,
		"email_subject=" + authmail.PrivacyNoticeSubject,
		"email_button=" + authmail.PrivacyNoticeAction,
		"email_heading_vi=" + authmail.PrivacyNoticeHeadingVI,
		"email_heading_en=" + authmail.PrivacyNoticeHeadingEN,
		"email_preheader=" + authmail.PrivacyNoticePreheader,
		"email_body_vi=" + authmail.PrivacyNoticeBodyVI,
		"email_body_en=" + authmail.PrivacyNoticeBodyEN,
	}
	return []byte(strings.Join(fields, "\n") + "\n")
}

// ContentDigest returns fresh raw digest bytes for the compiled manifest.
func ContentDigest() []byte { return contentDigest() }

func contentDigest() []byte {
	digest := sha256.Sum256(ManifestBytes())
	return digest[:]
}

// ContentDigest implements the worker's fixed notice authority.
func (*Service) ContentDigest() []byte { return contentDigest() }
