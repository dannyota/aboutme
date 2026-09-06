package printrender

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"strings"
	"testing"
)

type fakePDFStream struct {
	chunks   []streamChunk
	closed   int
	closeErr error
}

func (s *fakePDFStream) read(context.Context, int64) (streamChunk, error) {
	if len(s.chunks) == 0 {
		return streamChunk{eof: true}, nil
	}
	chunk := s.chunks[0]
	s.chunks = s.chunks[1:]
	return chunk, nil
}

func (s *fakePDFStream) close(context.Context) error {
	s.closed++
	return s.closeErr
}

func TestReadPDFStreamIsBoundedAndAlwaysClosed(t *testing.T) {
	valid := &fakePDFStream{chunks: []streamChunk{{data: "%PDF-1.7\n", eof: true}}}
	got, err := readPDFStream(context.Background(), valid, 64)
	if err != nil || string(got) != "%PDF-1.7\n" || valid.closed != 1 {
		t.Fatalf("valid = %q, %v, closes %d", got, err, valid.closed)
	}

	encoded := &fakePDFStream{chunks: []streamChunk{{data: base64.StdEncoding.EncodeToString([]byte("%PDF-1.7\n")), base64: true, eof: true}}}
	got, err = readPDFStream(context.Background(), encoded, 64)
	if err != nil || string(got) != "%PDF-1.7\n" || encoded.closed != 1 {
		t.Fatalf("base64 = %q, %v, closes %d", got, err, encoded.closed)
	}

	tooLarge := &fakePDFStream{chunks: []streamChunk{{data: "%PDF-123456789", eof: true}}}
	if _, err := readPDFStream(context.Background(), tooLarge, 8); !errors.Is(err, ErrOutputTooLarge) || tooLarge.closed != 1 {
		t.Fatalf("large error = %v, closes %d", err, tooLarge.closed)
	}

	bad := &fakePDFStream{chunks: []streamChunk{{data: "not pdf", eof: true}}}
	if _, err := readPDFStream(context.Background(), bad, 64); !errors.Is(err, ErrRenderFailed) || bad.closed != 1 {
		t.Fatalf("bad signature error = %v, closes %d", err, bad.closed)
	}

	closeFailure := &fakePDFStream{chunks: []streamChunk{{data: "%PDF-1.7\n", eof: true}}, closeErr: errors.New("close detail")}
	if _, err := readPDFStream(context.Background(), closeFailure, 64); !errors.Is(err, ErrRenderFailed) || strings.Contains(err.Error(), "close detail") {
		t.Fatalf("close error = %v", err)
	}
}

func TestCanonicalizePDFMetadataResolvesInfoAndPreservesEveryOtherByte(t *testing.T) {
	dynamic := "D:20260905174051+00'00'"
	canonical := "D:19700101000000+00'00'"
	input := classicPDF(t, dynamic, dynamic, dynamic)
	want := classicPDF(t, dynamic, canonical, canonical)

	if err := canonicalizePDFMetadata(input); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(input, want) {
		t.Fatalf("canonical PDF differs outside Info dates: %s", describeByteDifferences(input, want))
	}
}

func TestCanonicalizePDFMetadataRejectsMalformedOrAmbiguousStructure(t *testing.T) {
	date := "D:20260905174051+00'00'"
	valid := classicPDF(t, date, date, date)
	duplicateInfo := fmt.Sprintf("<</CreationDate (%s) /ModDate (%s) /ModDate (%s)>>", date, date, date)
	badObjectBoundary := append([]byte(nil), valid...)
	boundary := bytes.LastIndex(badObjectBoundary, []byte("endobj\nxref"))
	if boundary < 0 {
		t.Fatal("test PDF lacks final object boundary")
	}
	badObjectBoundary[boundary+len("endobj")] = 'X'
	for _, test := range []struct {
		name string
		pdf  []byte
	}{
		{name: "missing startxref", pdf: bytes.Replace(valid, []byte("startxref"), []byte("startxBAD"), 1)},
		{name: "ambiguous startxref", pdf: append(append([]byte(nil), valid...), valid...)},
		{name: "wrong xref offset", pdf: replaceStartXRef(t, valid, 1)},
		{name: "missing info", pdf: bytes.Replace(valid, []byte("/Info 3 0 R"), []byte("/Infx 3 0 R"), 1)},
		{name: "duplicate info", pdf: bytes.Replace(valid, []byte("/Info 3 0 R"), []byte("/Info 3 0 R /Info 3 0 R"), 1)},
		{name: "wrong info object offset", pdf: corruptInfoXRef(t, valid)},
		{name: "info object exceeds bounds", pdf: badObjectBoundary},
		{name: "missing creation date", pdf: bytes.Replace(valid, []byte("/CreationDate ("+date+")\n/ModDate"), []byte("/CreationDatz ("+date+")\n/ModDate"), 1)},
		{name: "duplicate modification date", pdf: classicPDFWithInfo(date, duplicateInfo)},
		{name: "xref stream", pdf: []byte("%PDF-1.5\n1 0 obj\n<</Type /XRef>>\nendobj\nstartxref\n9\n%%EOF\n")},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := append([]byte(nil), test.pdf...)
			if err := canonicalizePDFMetadata(test.pdf); err == nil {
				t.Fatal("canonicalizePDFMetadata() accepted malformed PDF")
			}
			if !bytes.Equal(test.pdf, before) {
				t.Fatal("rejected PDF was partially mutated")
			}
		})
	}
}

func classicPDF(t *testing.T, contentDate, creationDate, modificationDate string) []byte {
	t.Helper()
	info := fmt.Sprintf("<</Title (Resume)\n/CreationDate (%s)\n/ModDate (%s)>>", creationDate, modificationDate)
	return classicPDFWithInfo(contentDate, info)
}

func classicPDFWithInfo(contentDate, info string) []byte {
	stream := "/CreationDate (" + contentDate + ")\n"
	objects := []string{
		"<</Type /Catalog>>",
		fmt.Sprintf("<</Length %d>>\nstream\n%sendstream", len(stream), stream),
		info,
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xrefOffset := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<</Size %d /Root 1 0 R /Info 3 0 R>>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xrefOffset)
	return pdf.Bytes()
}

func replaceStartXRef(t *testing.T, pdf []byte, offset int) []byte {
	t.Helper()
	marker := []byte("startxref\n")
	start := bytes.LastIndex(pdf, marker)
	if start < 0 {
		t.Fatal("test PDF lacks startxref")
	}
	start += len(marker)
	end := start + bytes.IndexByte(pdf[start:], '\n')
	result := append([]byte(nil), pdf[:start]...)
	result = append(result, strconv.Itoa(offset)...)
	result = append(result, pdf[end:]...)
	return result
}

func corruptInfoXRef(t *testing.T, pdf []byte) []byte {
	t.Helper()
	offset := bytes.Index(pdf, []byte("3 0 obj\n"))
	if offset < 0 {
		t.Fatal("test PDF lacks Info object")
	}
	entry := []byte(fmt.Sprintf("%010d 00000 n", offset))
	if !bytes.Contains(pdf, entry) {
		t.Fatal("test PDF lacks Info xref entry")
	}
	return bytes.Replace(pdf, entry, []byte("0000000001 00000 n"), 1)
}

func TestValidatePNGRequiresFixedBoundedImage(t *testing.T) {
	valid := encodePNG(t, 1200, 630)
	if err := validatePNG(valid, len(valid)); err != nil {
		t.Fatalf("valid PNG: %v", err)
	}
	for _, test := range []struct {
		name  string
		value []byte
		limit int
		want  error
	}{
		{"too large", valid, len(valid) - 1, ErrOutputTooLarge},
		{"wrong dimensions", encodePNG(t, 1199, 630), 4_194_304, ErrRenderFailed},
		{"transparent", encodeTransparentPNG(t, 1200, 630), 4_194_304, ErrRenderFailed},
		{"bad signature", []byte("not png"), 4_194_304, ErrRenderFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validatePNG(test.value, test.limit); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func encodeTransparentPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func encodePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	value := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			value.Set(x, y, color.White)
		}
	}
	if err := png.Encode(&buffer, value); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
