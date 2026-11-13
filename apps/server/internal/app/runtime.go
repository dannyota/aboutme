package app

import (
	"encoding/base64"
	"errors"

	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

type PublicRuntime struct {
	PublicOrigin   publicresume.PublicOrigin
	RenderOrigin   directrender.RenderOrigin
	AppDigest      string
	RendererDigest string
}

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

func DecodeViewPassKey(cfg config.Config) ([]byte, error) {
	return base64.RawURLEncoding.Strict().DecodeString(cfg.ViewPassKey)
}
