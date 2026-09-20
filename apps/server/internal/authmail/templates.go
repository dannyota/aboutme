package authmail

import (
	"html"
	"strconv"
	"strings"
)

// Message copy is fixed code. Each message is bilingual, Vietnamese first
// because the initial community is Vietnamese (docs/design/product.md), then
// English. Only the canonical-origin link is interpolated, HTML-escaped. The
// HTML has no image, script, stylesheet, web font, tracking pixel, or any
// external resource: every style is inline and the wordmark is text.

// Brand colors sampled from docs/brand/aboutme-icon.png, plus the paper desk.
const (
	colorInk    = "#192024"
	colorGreen  = "#34C871"
	colorDesk   = "#EDEFEB"
	colorMuted  = "#5B6470"
	colorRule   = "#DADDD6"
	fontStack   = "-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif"
	footerVI    = "aboutme, công cụ tạo CV mã nguồn mở."
	footerEN    = "aboutme, the open-source resume builder."
	fallbackVI  = "Nút không hoạt động? Sao chép và dán liên kết này vào trình duyệt:"
	fallbackEN  = "Button not working? Paste this link into your browser:"
	fallbackTxt = "Mở liên kết / Open the link:"
)

// section is one language's copy for a message.
type section struct {
	lang    string
	heading string
	body    string
	note    string
}

// template is the fixed copy for one message kind. action is empty for a
// notice that carries no link.
type template struct {
	subject   string
	preheader string
	action    string
	vi, en    section
	security  bool
}

var templates = map[Kind]template{
	KindVerify: {
		subject:   "Xác minh email aboutme / Verify your aboutme email",
		preheader: "Xác nhận email để hoàn tất đăng ký. Confirm your email to finish your account.",
		action:    "Xác minh email · Verify email",
		vi: section{
			lang:    "vi",
			heading: "Xác minh email của bạn",
			body:    "Cảm ơn bạn đã đăng ký aboutme. Hãy xác nhận địa chỉ email này để hoàn tất việc tạo tài khoản.",
			note:    "Liên kết có hiệu lực trong 24 giờ. Nếu bạn không đăng ký, hãy bỏ qua email này; sẽ không có tài khoản nào được tạo.",
		},
		en: section{
			lang:    "en",
			heading: "Verify your email",
			body:    "Thanks for signing up for aboutme. Confirm this email address to finish creating your account.",
			note:    "The link works for 24 hours. If you did not sign up, ignore this email and no account is created.",
		},
	},
	KindReset: {
		subject:   "Đặt lại mật khẩu aboutme / Reset your aboutme password",
		preheader: "Liên kết đặt lại mật khẩu, hiệu lực 30 phút. Your password reset link, valid for 30 minutes.",
		action:    "Đặt lại mật khẩu · Reset password",
		vi: section{
			lang:    "vi",
			heading: "Đặt lại mật khẩu",
			body:    "Có người đã yêu cầu đặt lại mật khẩu cho tài khoản aboutme dùng email này.",
			note:    "Liên kết có hiệu lực trong 30 phút và chỉ dùng được một lần. Nếu bạn không yêu cầu, hãy bỏ qua email này; mật khẩu của bạn sẽ không thay đổi.",
		},
		en: section{
			lang:    "en",
			heading: "Reset your password",
			body:    "Someone asked to reset the password for the aboutme account that uses this email.",
			note:    "The link works once, for 30 minutes. If you did not ask, ignore this email and your password stays the same.",
		},
	},
	KindPasswordChanged: {
		subject:   "Mật khẩu aboutme đã thay đổi / Your aboutme password was changed",
		preheader: "Mật khẩu tài khoản của bạn vừa được thay đổi. Your account password was just changed.",
		vi: section{
			lang:    "vi",
			heading: "Mật khẩu đã được thay đổi",
			body:    "Mật khẩu tài khoản aboutme của bạn vừa được thay đổi.",
			note:    "Nếu bạn không thực hiện thay đổi này, hãy trả lời email này ngay để chúng tôi hỗ trợ.",
		},
		en: section{
			lang:    "en",
			heading: "Your password was changed",
			body:    "The password for your aboutme account was just changed.",
			note:    "If you did not make this change, reply to this email right away and we will help.",
		},
	},
	KindSecondFactorEnabled:           securityTemplate("Xác thực hai bước đã bật / Second factor enabled", "Xác thực hai bước đã được bật", "Second-factor authentication was enabled"),
	KindPasskeyAdded:                  securityTemplate("Passkey đã được thêm / Passkey added", "Một passkey đã được thêm", "A passkey was added"),
	KindPasskeyRemoved:                securityTemplate("Passkey đã được xóa / Passkey removed", "Một passkey đã được xóa", "A passkey was removed"),
	KindSecondFactorDisabled:          securityTemplate("Xác thực hai bước đã tắt / Second factor disabled", "Xác thực hai bước đã được tắt", "Second-factor authentication was disabled"),
	KindRecoveryCodesRegenerated:      securityTemplate("Mã khôi phục đã tạo lại / Recovery codes regenerated", "Mã khôi phục đã được tạo lại", "Recovery codes were regenerated"),
	KindRecoveryCodeUsed:              securityTemplate("Mã khôi phục đã được dùng / Recovery code used", "Một mã khôi phục đã được dùng", "A recovery code was used"),
	KindSecondFactorAttemptsExhausted: securityTemplate("Đã hết lượt xác thực hai bước / Second-factor attempts exhausted", "Đã hết lượt thử xác thực hai bước", "Second-factor verification attempts were exhausted"),
}

func securityTemplate(subject, viAction, enAction string) template {
	return template{
		subject:   subject,
		preheader: "Thông báo bảo mật aboutme. aboutme security notice.",
		vi: section{
			lang: "vi", heading: "Thông báo bảo mật", body: viAction,
			note: "Nếu bạn không thực hiện việc này, hãy đổi mật khẩu và thu hồi các phiên đăng nhập ngay.",
		},
		en: section{
			lang: "en", heading: "Security notice", body: enAction,
			note: "If you did not do this, change your password and revoke your sessions right away.",
		},
		security: true,
	}
}

// buildMessage renders the fixed template for a decrypted payload. An unknown
// kind yields a message with no subject or body, which SES rejects.
func buildMessage(kind Kind, p Payload) Message {
	t, ok := templates[kind]
	if !ok {
		return Message{Kind: kind, To: p.To}
	}
	if t.security {
		t = withSecurityDetails(t, p)
	}
	link := ""
	if t.action != "" {
		link = p.Link
	}
	return Message{
		Kind:     kind,
		To:       p.To,
		Subject:  t.subject,
		TextBody: renderText(t, link),
		HTMLBody: renderHTML(t, link),
	}
}

func withSecurityDetails(t template, p Payload) template {
	t.vi.body += " vào lúc " + p.OccurredAt + "."
	t.en.body += " at " + p.OccurredAt + "."
	if p.RemainingRecoveryCodes != nil {
		count := strconv.Itoa(*p.RemainingRecoveryCodes)
		t.vi.body += " Còn lại " + count + " mã khôi phục."
		t.en.body += " " + count + " recovery codes remain."
	}
	return t
}

func renderText(t template, link string) string {
	var b strings.Builder
	for i, s := range []section{t.vi, t.en} {
		if i > 0 {
			b.WriteString("\n---\n\n")
		}
		b.WriteString(s.heading + "\n\n" + s.body + "\n\n")
		if link != "" && i == 0 {
			b.WriteString(fallbackTxt + "\n" + link + "\n\n")
		}
		b.WriteString(s.note + "\n")
	}
	b.WriteString("\n-- \n" + footerVI + "\n" + footerEN + "\n")
	return b.String()
}

func renderHTML(t template, link string) string {
	esc := html.EscapeString
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html lang="vi"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<meta name="color-scheme" content="light"><title>` + esc(t.subject) + `</title></head>`)
	b.WriteString(`<body style="margin:0;padding:0;background:` + colorDesk + `;">`)
	// Inbox preview text; hidden in the opened message.
	b.WriteString(`<div style="display:none;max-height:0;overflow:hidden;">` + esc(t.preheader) + `</div>`)
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:` +
		colorDesk + `;"><tr><td align="center" style="padding:32px 16px;">`)
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:520px;` +
		`background:#FFFFFF;border:1px solid ` + colorRule + `;border-radius:8px;font-family:` + fontStack +
		`;color:` + colorInk + `;"><tr><td style="padding:32px 32px 8px;">`)
	b.WriteString(`<div style="font-size:22px;font-weight:800;letter-spacing:-0.02em;">about<span style="color:` +
		colorGreen + `;">/</span>me</div>`)
	b.WriteString(`</td></tr>`)

	writeSection(&b, t.vi, `padding:16px 32px 0;`, colorInk)
	if link != "" {
		b.WriteString(`<tr><td style="padding:24px 32px 8px;">` +
			`<a href="` + esc(link) + `" style="display:inline-block;background:` + colorInk +
			`;color:#FFFFFF;text-decoration:none;font-weight:600;font-size:15px;padding:12px 20px;border-radius:6px;">` +
			esc(t.action) + `</a></td></tr>`)
	}
	writeNote(&b, t.vi)
	b.WriteString(`<tr><td style="padding:24px 32px 0;"><div style="border-top:1px solid ` + colorRule + `;"></div></td></tr>`)
	writeSection(&b, t.en, `padding:24px 32px 0;`, colorInk)
	writeNote(&b, t.en)

	if link != "" {
		b.WriteString(`<tr><td style="padding:24px 32px 0;font-size:13px;line-height:20px;color:` + colorMuted + `;">` +
			`<span lang="vi">` + esc(fallbackVI) + `</span><br><span lang="en">` + esc(fallbackEN) + `</span><br>` +
			`<a href="` + esc(link) + `" style="color:` + colorInk + `;word-break:break-all;">` + esc(link) + `</a></td></tr>`)
	}
	b.WriteString(`<tr><td style="padding:24px 32px 32px;font-size:12px;line-height:18px;color:` + colorMuted + `;">` +
		`<span lang="vi">` + esc(footerVI) + `</span><br><span lang="en">` + esc(footerEN) + `</span></td></tr>`)
	b.WriteString(`</table></td></tr></table></body></html>`)
	return b.String()
}

func writeSection(b *strings.Builder, s section, padding, color string) {
	b.WriteString(`<tr><td lang="` + s.lang + `" style="` + padding + `">` +
		`<h1 style="margin:0 0 12px;font-size:20px;line-height:28px;font-weight:700;color:` + color + `;">` +
		html.EscapeString(s.heading) + `</h1>` +
		`<p style="margin:0;font-size:15px;line-height:24px;">` + html.EscapeString(s.body) + `</p></td></tr>`)
}

func writeNote(b *strings.Builder, s section) {
	b.WriteString(`<tr><td lang="` + s.lang + `" style="padding:12px 32px 0;font-size:13px;line-height:20px;color:` +
		colorMuted + `;">` + html.EscapeString(s.note) + `</td></tr>`)
}
