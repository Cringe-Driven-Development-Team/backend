package s3client

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"golang.org/x/image/draw"
)

const (
	maxPreviewSourceBytes  = 20 << 20
	maxPreviewSourceSide   = 8192
	maxPreviewSourcePixels = 16_000_000
	maxPreviewSide         = 512
)

// CreatePreview читает оригинал и сохраняет JPEG или PNG под отдельным ключом.
// Для документов возвращает пустой ключ, для повреждённых и слишком больших изображений — ошибку.
func (s *PrivateFiles) CreatePreview(ctx context.Context, originalKey string) (string, error) {
	source, err := s.Open(ctx, originalKey)
	if err != nil {
		return "", err
	}
	defer source.Close()

	data, format, err := rasterPreview(source)
	if err != nil || format == "" {
		return "", err
	}
	key := originalKey + ".preview." + format
	if err := s.put(ctx, key, bytes.NewReader(data), aws.String("image/"+format)); err != nil {
		return "", err
	}
	return key, nil
}

// rasterPreview проверяет формат, объём и габариты до декодирования пикселей.
func rasterPreview(source io.Reader) ([]byte, string, error) {
	reader := bufio.NewReader(source)
	header, err := reader.Peek(8)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, "", err
	}
	var decodeConfig func(io.Reader) (image.Config, error)
	var decode func(io.Reader) (image.Image, error)
	var format string
	switch {
	case bytes.HasPrefix(header, []byte("\xff\xd8")):
		decodeConfig, decode, format = jpeg.DecodeConfig, jpeg.Decode, "jpeg"
	case bytes.HasPrefix(header, []byte("\x89PNG\r\n\x1a\n")):
		decodeConfig, decode, format = png.DecodeConfig, png.Decode, "png"
	default:
		return nil, "", nil
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxPreviewSourceBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxPreviewSourceBytes {
		return nil, "", errors.New("preview source exceeds 20 MiB")
	}
	config, err := decodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	width, height := config.Width, config.Height
	if width <= 0 || height <= 0 || width > maxPreviewSourceSide ||
		height > maxPreviewSourceSide || width*height > maxPreviewSourcePixels {
		return nil, "", errors.New("preview source dimensions exceed limits")
	}
	original, err := decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	longSide := max(width, height)
	if longSide > maxPreviewSide {
		width = max(1, (width*maxPreviewSide+longSide/2)/longSide)
		height = max(1, (height*maxPreviewSide+longSide/2)/longSide)
	}
	preview := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.BiLinear.Scale(preview, preview.Bounds(), original, original.Bounds(), draw.Src, nil)

	var output bytes.Buffer
	if format == "jpeg" {
		err = jpeg.Encode(&output, preview, nil)
	} else {
		err = png.Encode(&output, preview)
	}
	if err != nil {
		return nil, "", err
	}
	return output.Bytes(), format, nil
}
