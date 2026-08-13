//go:build ignore

// This program deterministically regenerates the locally-authored JPEG and PNG
// normalization-budget fixtures. Run it from this directory with `go run
// generate.go`; the benchmark manifest pins the resulting hashes.
package main

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
)

func mustCreate(path string) *os.File {
	file, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	return file
}

func main() {
	opaque := image.NewNRGBA(image.Rect(0, 0, 4096, 4096))
	for y := 0; y < 4096; y++ {
		for x := 0; x < 4096; x++ {
			opaque.SetNRGBA(x, y, color.NRGBA{
				R: uint8((x / 64) % 256),
				G: uint8((y / 64) % 256),
				B: uint8(((x + y) / 128) % 256),
				A: 255,
			})
		}
	}
	jpegFile := mustCreate("opaque-max-pixels.jpg")
	if err := jpeg.Encode(jpegFile, opaque, &jpeg.Options{Quality: 88}); err != nil {
		panic(err)
	}
	if err := jpegFile.Close(); err != nil {
		panic(err)
	}

	alpha := image.NewNRGBA(image.Rect(0, 0, 4096, 4096))
	for y := 0; y < 4096; y++ {
		for x := 0; x < 4096; x++ {
			alpha.SetNRGBA(x, y, color.NRGBA{
				R: uint8((x / 128) % 256),
				G: uint8((y / 128) % 256),
				B: 96,
				A: uint8(64 + ((x+y)/64)%192),
			})
		}
	}
	alphaFile := mustCreate("alpha-max-pixels.png")
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(alphaFile, alpha); err != nil {
		panic(err)
	}
	if err := alphaFile.Close(); err != nil {
		panic(err)
	}
	alphaBytes, err := os.ReadFile("alpha-max-pixels.png")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile("alpha-max-pixels-oriented.png", insertPNGChunk(alphaBytes, "eXIf", tiffOrientation(6), "IDAT"), 0o644); err != nil {
		panic(err)
	}

	rgba16 := image.NewNRGBA64(image.Rect(0, 0, 4096, 4096))
	for y := 0; y < 4096; y++ {
		for x := 0; x < 4096; x++ {
			rgba16.SetNRGBA64(x, y, color.NRGBA64{
				R: 0x1357, G: 0x2468, B: 0x369c, A: 0xffff,
			})
		}
	}
	var rgba16Bytes bytes.Buffer
	if err := encoder.Encode(&rgba16Bytes, rgba16); err != nil {
		panic(err)
	}
	if err := os.WriteFile("rgba16-max-pixels-oriented.png", insertPNGChunk(rgba16Bytes.Bytes(), "eXIf", tiffOrientation(6), "IDAT"), 0o644); err != nil {
		panic(err)
	}

	gray16 := image.NewGray16(image.Rect(0, 0, 2048, 2048))
	for y := 0; y < 2048; y++ {
		for x := 0; x < 2048; x++ {
			gray16.SetGray16(x, y, color.Gray16{Y: uint16((x + y) % 65536)})
		}
	}
	grayFile := mustCreate("gray16-boundary.png")
	if err := encoder.Encode(grayFile, gray16); err != nil {
		panic(err)
	}
	if err := grayFile.Close(); err != nil {
		panic(err)
	}

	edge := image.NewNRGBA(image.Rect(0, 0, 8192, 1))
	for x := 0; x < 8192; x++ {
		edge.SetNRGBA(x, 0, color.NRGBA{R: uint8(x % 256), G: 64, B: 192, A: 255})
	}
	edgeFile := mustCreate("max-edge.jpg")
	if err := jpeg.Encode(edgeFile, edge, &jpeg.Options{Quality: 88}); err != nil {
		panic(err)
	}
	if err := edgeFile.Close(); err != nil {
		panic(err)
	}
}

func insertPNGChunk(source []byte, kind string, data []byte, before string) []byte {
	output := append([]byte(nil), source[:8]...)
	inserted := false
	for offset := 8; offset < len(source); {
		length := int(binary.BigEndian.Uint32(source[offset : offset+4]))
		end := offset + 12 + length
		chunkType := string(source[offset+4 : offset+8])
		if chunkType == before && !inserted {
			chunk := make([]byte, 12+len(data))
			binary.BigEndian.PutUint32(chunk[:4], uint32(len(data)))
			copy(chunk[4:8], kind)
			copy(chunk[8:8+len(data)], data)
			binary.BigEndian.PutUint32(chunk[8+len(data):], crc32.ChecksumIEEE(chunk[4:8+len(data)]))
			output = append(output, chunk...)
			inserted = true
		}
		output = append(output, source[offset:end]...)
		offset = end
	}
	return output
}

func tiffOrientation(orientation uint16) []byte {
	payload := make([]byte, 26)
	copy(payload, []byte{'I', 'I'})
	binary.LittleEndian.PutUint16(payload[2:4], 42)
	binary.LittleEndian.PutUint32(payload[4:8], 8)
	binary.LittleEndian.PutUint16(payload[8:10], 1)
	binary.LittleEndian.PutUint16(payload[10:12], 0x0112)
	binary.LittleEndian.PutUint16(payload[12:14], 3)
	binary.LittleEndian.PutUint32(payload[14:18], 1)
	binary.LittleEndian.PutUint16(payload[18:20], orientation)
	return payload
}
