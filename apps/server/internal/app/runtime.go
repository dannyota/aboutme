package app

import (
	"encoding/base64"
	"errors"

	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

// PublicRuntime is the validated public serving identity: the canonical and
// render origins plus the build digests public responses report.
type PublicRuntime struct {
	// PublicOrigin is the parsed canonical PUBLIC_ORIGIN.
	PublicOrigin publicresume.PublicOrigin
	// RenderOrigin is the parsed PUBLIC_RENDER_ORIGIN of the direct render
	// worker.
	RenderOrigin directrender.RenderOrigin
	// AppDigest is APP_BUILD_DIGEST: non-empty printable ASCII, no spaces.
	AppDigest string
	// RendererDigest is PUBLIC_RENDERER_BUILD_DIGEST: non-empty printable
	// ASCII, no spaces.
	RendererDigest string
}

// ParsePublicRuntime validates the public and render origins for environment
// and both build digests. Each error names only the rejected setting.
func ParsePublicRuntime(publicOrigin, renderOrigin, environment, appDigest, rendererDigest string) (PublicRuntime, error) {
	parsedPublicOrigin, err := publicresume.ParsePublicOrigin(publicOrigin, environment)
	if err != nil {
		return PublicRuntime{}, errors.New("PUBLIC_ORIGIN is invalid")
	}
	parsedRenderOrigin, err := directrender.ParseRenderOrigin(renderOrigin, environment)
	if err != nil {
		return PublicRuntime{}, errors.New("PUBLIC_RENDER_ORIGIN is invalid")
	}
	if !printableDigest(appDigest) {
		return PublicRuntime{}, errors.New("APP_BUILD_DIGEST is invalid")
	}
	if !printableDigest(rendererDigest) {
		return PublicRuntime{}, errors.New("PUBLIC_RENDERER_BUILD_DIGEST is invalid")
	}
	return PublicRuntime{PublicOrigin: parsedPublicOrigin, RenderOrigin: parsedRenderOrigin, AppDigest: appDigest, RendererDigest: rendererDigest}, nil
}

func printableDigest(value string) bool {
	if value == "" {
		return false
	}
	for i := range value {
		if value[i] < 0x21 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

// DecodeViewPassKey returns the 32-byte view pass key. VIEW_PASS_KEY is
// validated at config load as the canonical 43-character unpadded base64url
// encoding of 32 bytes (docs/design/viewer-analytics/sign-in-to-view.md
// "Pass cookie").
func DecodeViewPassKey(cfg config.Config) ([]byte, error) {
	return base64.RawURLEncoding.Strict().DecodeString(cfg.ViewPassKey)
}
