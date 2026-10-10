package authmail

import (
	"html"
	"strings"
	"testing"
)

func TestAuthMailFooterByMode(t *testing.T) {
	const vi = "Email này được gửi qua máy chủ thư của Bizfly. Nếu bạn trả lời, thư sẽ đến thẳng hộp thư Google Workspace của Danny."
	const en = "This email was sent through Bizfly's mail server. If you reply, your email goes straight to Danny's Google Workspace mailbox."
	for _, mode := range []string{"smtp", "ses", "capture"} {
		t.Run(mode, func(t *testing.T) {
			for kind := range templates {
				t.Run(string(kind), func(t *testing.T) {
					payload := Payload{To: "alice@example.com", Link: verifyLinkPrefix + "token", OccurredAt: "2026-10-11T09:00:00Z"}
					message := buildMessage(kind, payload, FooterNoteForMode(mode))
					withoutNote := buildMessage(kind, payload, "")
					if mode != "smtp" {
						if message != withoutNote {
							t.Fatal("non-SMTP mode changes the message without a footer note")
						}
						for _, body := range []string{message.TextBody, message.HTMLBody} {
							if strings.Contains(body, "Bizfly") {
								t.Fatal("non-SMTP body contains Bizfly")
							}
						}
						return
					}
					textFooter := "\n-- \n" + vi + "\n" + en + "\n" + footerVI + "\n" + footerEN + "\n"
					if !strings.HasSuffix(message.TextBody, textFooter) {
						t.Error("text footer is missing the exact bilingual note above the wordmark lines")
					}
					htmlFooter := `<tr><td style="padding:24px 32px 32px;font-size:12px;line-height:18px;color:` + colorMuted + `;">` +
						`<span lang="vi">` + html.EscapeString(vi) + `</span><br><span lang="en">` + html.EscapeString(en) + `</span><br>` +
						`<span lang="vi">` + html.EscapeString(footerVI) + `</span><br><span lang="en">` + html.EscapeString(footerEN) + `</span></td></tr>`
					if !strings.Contains(message.HTMLBody, htmlFooter) {
						t.Error("HTML footer is missing the escaped bilingual note in the muted 12px footer")
					}
					if strings.Contains(message.HTMLBody, "Bizfly's") || strings.Contains(message.HTMLBody, "Danny's") {
						t.Error("HTML footer contains an unescaped apostrophe")
					}
					if strings.Replace(message.TextBody, vi+"\n"+en+"\n", "", 1) != withoutNote.TextBody {
						t.Error("SMTP changes text outside the footer note")
					}
					encodedNote := `<span lang="vi">` + html.EscapeString(vi) + `</span><br><span lang="en">` + html.EscapeString(en) + `</span><br>`
					if strings.Replace(message.HTMLBody, encodedNote, "", 1) != withoutNote.HTMLBody {
						t.Error("SMTP changes HTML outside the footer note")
					}
				})
			}
		})
	}
}
