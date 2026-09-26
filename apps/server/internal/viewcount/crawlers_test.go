package viewcount

import "testing"

// The closed crawler and link-preview list (docs/design/viewer-analytics/
// counting.md, "Layer 1").
func TestClassifyUserAgent(t *testing.T) {
	tests := []struct {
		ua       string
		kind     AgentKind
		platform Platform
	}{
		{"facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)", AgentPreview, PlatformFacebook},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_11_1) AppleWebKit/601.2.4 (KHTML, like Gecko) Version/9.0.1 Safari/601.2.4 facebookexternalhit/1.1 Facebot Twitterbot/1.0", AgentPreview, PlatformFacebook},
		{"LinkedInBot/1.0 (compatible; Mozilla/5.0; Apache-HttpClient +http://www.linkedin.com)", AgentPreview, PlatformLinkedIn},
		{"Twitterbot/1.0", AgentPreview, PlatformX},
		{"TelegramBot (like TwitterBot)", AgentPreview, PlatformTelegram},
		{"WhatsApp/2.23.20.0 A", AgentPreview, PlatformWhatsApp},
		{"Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)", AgentPreview, PlatformSlack},
		{"Slack-ImgProxy (+https://api.slack.com/robots)", AgentPreview, PlatformSlack},
		{"Mozilla/5.0 (compatible; Discordbot/2.0; +https://discordapp.com)", AgentPreview, PlatformDiscord},
		{"Mozilla/5.0 (Windows NT 6.1; WOW64) SkypeUriPreview Preview/0.5", AgentPreview, PlatformSkype},
		{"Viber/20.0", AgentPreview, PlatformViber},
		{"Zalo/195.1 CFNetwork/897.15 Darwin/17.5.0", AgentPreview, PlatformZalo},
		// In-app browsers are people.
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 Zalo iOS/475", AgentBrowser, ""},
		{"Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Mobile Safari/537.36 Viber/20.0", AgentBrowser, ""},
		{"Mozilla/5.0 (Linux; Android 13; CUBOT_X19) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Mobile Safari/537.36", AgentBrowser, ""},
		{"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", AgentCrawler, ""},
		{"Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)", AgentCrawler, ""},
		{"Mozilla/5.0 (compatible; coccocbot-web/1.0; +http://help.coccoc.com/searchengine)", AgentCrawler, ""},
		{"curl/8.9.1", AgentCrawler, ""},
		{"python-requests/2.32.3", AgentCrawler, ""},
		{"Go-http-client/2.0", AgentCrawler, ""},
		{"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/140.0 Safari/537.36", AgentCrawler, ""},
		{"", AgentCrawler, ""},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36", AgentBrowser, ""},
	}
	for _, test := range tests {
		kind, platform := ClassifyUserAgent(test.ua)
		if kind != test.kind || platform != test.platform {
			t.Errorf("ClassifyUserAgent(%q) = %d, %q; want %d, %q", test.ua, kind, platform, test.kind, test.platform)
		}
	}
}
