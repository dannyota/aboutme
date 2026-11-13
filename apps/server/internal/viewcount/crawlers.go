package viewcount

import "strings"

type Platform string

const (
	PlatformFacebook Platform = "facebook"
	PlatformLinkedIn Platform = "linkedin"
	PlatformX        Platform = "x"
	PlatformTelegram Platform = "telegram"
	PlatformWhatsApp Platform = "whatsapp"
	PlatformSlack    Platform = "slack"
	PlatformDiscord  Platform = "discord"
	PlatformSkype    Platform = "skype"
	PlatformViber    Platform = "viber"
	PlatformZalo     Platform = "zalo"
)

var Platforms = []Platform{
	PlatformFacebook, PlatformLinkedIn, PlatformX, PlatformTelegram, PlatformWhatsApp,
	PlatformSlack, PlatformDiscord, PlatformSkype, PlatformViber, PlatformZalo,
}

type AgentKind uint8

const (
	AgentBrowser AgentKind = iota
	AgentPreview
	AgentCrawler
)

type previewToken struct {
	token    string
	platform Platform
	// An app's in-app browser sends the same app token with "mozilla/", and is a person.
	appOnly bool
}

// iMessage's user agent holds both facebookexternalhit and Twitterbot, and Telegram's says "like TwitterBot", so order matters.
var previewTokens = []previewToken{
	{token: "facebookexternalhit", platform: PlatformFacebook},
	{token: "facebot", platform: PlatformFacebook},
	{token: "linkedinbot", platform: PlatformLinkedIn},
	{token: "telegrambot", platform: PlatformTelegram},
	{token: "twitterbot", platform: PlatformX},
	{token: "whatsapp", platform: PlatformWhatsApp, appOnly: true},
	{token: "slackbot-linkexpanding", platform: PlatformSlack},
	{token: "slack-imgproxy", platform: PlatformSlack},
	{token: "discordbot", platform: PlatformDiscord},
	{token: "skypeuripreview", platform: PlatformSkype},
	{token: "viber", platform: PlatformViber, appOnly: true},
	{token: "zalo", platform: PlatformZalo, appOnly: true},
}

var crawlerTokens = []string{
	"googlebot", "bingbot", "coccocbot", "applebot", "duckduckbot",
	"bot/", "bot;", "bot)", "+http", "spider", "crawl",
	"curl/", "wget/", "python-requests", "go-http-client", "headlesschrome",
}

func ClassifyUserAgent(userAgent string) (AgentKind, Platform) {
	ua := strings.ToLower(strings.TrimSpace(userAgent))
	if ua == "" {
		return AgentCrawler, ""
	}
	browserLike := strings.Contains(ua, "mozilla/")
	for _, t := range previewTokens {
		if t.appOnly && browserLike {
			continue
		}
		if strings.Contains(ua, t.token) {
			return AgentPreview, t.platform
		}
	}
	for _, t := range crawlerTokens {
		if strings.Contains(ua, t) {
			return AgentCrawler, ""
		}
	}
	return AgentBrowser, ""
}
