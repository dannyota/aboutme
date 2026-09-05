package printrender

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/png"
)

type streamChunk struct {
	data   string
	base64 bool
	eof    bool
}

type pdfStream interface {
	read(context.Context, int64) (streamChunk, error)
	close(context.Context) error
}

func readPDFStream(ctx context.Context, stream pdfStream, limit int) (output []byte, resultErr error) {
	defer func() {
		if err := stream.close(ctx); resultErr == nil && err != nil {
			output, resultErr = nil, ErrRenderFailed
		}
	}()
	for {
		chunk, err := stream.read(ctx, 64*1024)
		if err != nil {
			return nil, ErrRenderFailed
		}
		data := []byte(chunk.data)
		if chunk.base64 {
			if base64.StdEncoding.DecodedLen(len(chunk.data)) > limit-len(output) {
				return nil, ErrOutputTooLarge
			}
			data, err = base64.StdEncoding.DecodeString(chunk.data)
			if err != nil {
				return nil, ErrRenderFailed
			}
		}
		if len(data) > limit-len(output) {
			return nil, ErrOutputTooLarge
		}
		output = append(output, data...)
		if chunk.eof {
			break
		}
		if len(data) == 0 {
			return nil, ErrRenderFailed
		}
	}
	if !bytes.HasPrefix(output, []byte("%PDF-")) {
		return nil, ErrRenderFailed
	}
	return output, nil
}

func validatePNG(data []byte, limit int) error {
	if len(data) > limit {
		return ErrOutputTooLarge
	}
	if len(data) < 8 || !bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return ErrRenderFailed
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width != 1200 || config.Height != 630 {
		return ErrRenderFailed
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return ErrRenderFailed
	}
	bounds := decoded.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := decoded.At(x, y).RGBA()
			if alpha != 0xffff {
				return ErrRenderFailed
			}
		}
	}
	return nil
}
