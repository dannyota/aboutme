package viewcount

import "strings"

// Platform names an app whose link-preview fetcher requested a public page.
// The set is closed and matches the resume_share_signal_days check.
type Platform string

// Link-preview platforms (docs/design/link-previews.md, "Platforms").
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

// Platforms lists every platform in a fixed order.
var Platforms = []Platform{
	PlatformFacebook, PlatformLinkedIn, PlatformX, PlatformTelegram, PlatformWhatsApp,
	PlatformSlack, PlatformDiscord, PlatformSkype, PlatformViber, PlatformZalo,
}

// AgentKind is what the user agent of an HTML request declares itself to be.
type AgentKind uint8

// Agent kinds (docs/design/viewer-analytics/counting.md, "Layer 1").
const (
	// AgentBrowser is anything not on the list. Its page script decides.
	AgentBrowser AgentKind = iota
	// AgentPreview is a link-preview fetcher: a share signal, never a view.
	AgentPreview
	// AgentCrawler is a search engine, HTTP library, or other declared bot.
	AgentCrawler
)

type previewToken struct {
	token    string
	platform Platform
	// appOnly matches only a user agent without "mozilla/": the same app
	// token also appears in that app's in-app browser, which is a person.
	appOnly bool
}

// previewTokens is checked in order. Facebook and Telegram come before X
// because iMessage sends a user agent holding both facebookexternalhit and
// Twitterbot, and counts as Facebook, and Telegram's says "like TwitterBot". The Zalo token is confirmed with the
// Zalo sharing debugger during the live checks.
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

// crawlerTokens are search engines and generic automation. "bot" is matched
// only before "/", ";", or ")" so a phone model such as "CUBOT_X19" is not a
// crawler. "+http" is the contact link bots put in their user agent.
var crawlerTokens = []string{
	"googlebot", "bingbot", "coccocbot", "applebot", "duckduckbot",
	"bot/", "bot;", "bot)", "+http", "spider", "crawl",
	"curl/", "wget/", "python-requests", "go-http-client", "headlesschrome",
}

// ClassifyUserAgent maps a user agent to its kind and, for a preview
// fetcher, its platform. An empty user agent is a crawler.
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
