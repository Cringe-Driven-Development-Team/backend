package s3client

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"testing"
)

func TestRasterPreview(t *testing.T) {
	var source bytes.Buffer
	if err := png.Encode(&source, image.NewNRGBA(image.Rect(0, 0, 800, 400))); err != nil {
		t.Fatal(err)
	}
	pngData := source.Bytes()
	for _, tc := range []struct {
		name       string
		data       []byte
		format     string
		wantError  bool
		limitError string
	}{
		{name: "PNG", data: pngData, format: "png"},
		{name: "document", data: []byte("example")},
		{name: "broken PNG", data: pngData[:33], wantError: true},
		{name: "too many bytes", data: append(bytes.Clone(pngData), make([]byte, 20<<20)...), wantError: true, limitError: "preview source exceeds 20 MiB"},
		{name: "too wide", data: pngDimensions(pngData, 8193, 1), wantError: true, limitError: "preview source dimensions exceed limits"},
		{name: "too many pixels", data: pngDimensions(pngData, 5000, 4000), wantError: true, limitError: "preview source dimensions exceed limits"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, format, err := rasterPreview(bytes.NewReader(tc.data))
			if (err != nil) != tc.wantError || format != tc.format {
				t.Fatalf("format = %q, error = %v", format, err)
			}
			if tc.limitError != "" && err.Error() != tc.limitError {
				t.Fatalf("expected limit check before decoding, got %v", err)
			}
			if tc.format == "" {
				if len(result) != 0 {
					t.Fatal("unexpected preview")
				}
				return
			}
			img, err := png.Decode(bytes.NewReader(result))
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds().Size() != image.Pt(512, 256) {
				t.Errorf("unexpected bounds %v", img.Bounds())
			}
			if _, _, _, alpha := img.At(0, 0).RGBA(); alpha != 0 {
				t.Error("preview lost PNG transparency")
			}
		})
	}
}

// pngDimensions меняет габариты и контрольную сумму заголовка PNG.
func pngDimensions(data []byte, width, height uint32) []byte {
	header := bytes.Clone(data[:33])
	binary.BigEndian.PutUint32(header[16:20], width)
	binary.BigEndian.PutUint32(header[20:24], height)
	binary.BigEndian.PutUint32(header[29:33], crc32.ChecksumIEEE(header[12:29]))
	return header
}
