package s3client

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

const privateFileLinkTTL = 2 * time.Minute

// NewPrivateFileKey выделяет случайный ключ для одной загрузки.
func NewPrivateFileKey() string {
	var id [16]byte
	_, _ = rand.Read(id[:])
	return "private-files/" + hex.EncodeToString(id[:])
}

// PresignPut подписывает PUT для указанного ключа на две минуты.
// Вызывающий код передаёт клиенту URL и обязательные заголовки запроса.
func (s *PrivateFiles) PresignPut(ctx context.Context, key string) (*v4.PresignedHTTPRequest, error) {
	return awss3.NewPresignClient(s.client).PresignPutObject(ctx, &awss3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s.presignOptions)
}

// PresignDownload подписывает GET для скачивания объекта как вложения на две минуты.
func (s *PrivateFiles) PresignDownload(ctx context.Context, key string) (*v4.PresignedHTTPRequest, error) {
	return awss3.NewPresignClient(s.client).PresignGetObject(ctx, &awss3.GetObjectInput{
		Bucket:                     aws.String(s.bucket),
		Key:                        aws.String(key),
		ResponseContentDisposition: aws.String("attachment"),
	}, s.presignOptions)
}

// presignOptions выбирает адресацию до того, как SDK подпишет URL.
func (s *PrivateFiles) presignOptions(options *awss3.PresignOptions) {
	options.Expires = privateFileLinkTTL
	options.ClientOptions = append(options.ClientOptions, func(client *awss3.Options) {
		client.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		client.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		endpoint, err := url.Parse(aws.ToString(client.BaseEndpoint))
		if err == nil && endpoint.Scheme == "https" &&
			strings.HasSuffix(strings.ToLower(endpoint.Hostname()), ".storage.selcloud.ru") {
			client.UsePathStyle = false
		}
	})
}

// Size возвращает фактический размер объекта в байтах по данным S3.
func (s *PrivateFiles) Size(ctx context.Context, key string) (int64, error) {
	object, err := s.client.HeadObject(ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, err
	}
	if object.ContentLength == nil || *object.ContentLength < 0 {
		return 0, errors.New("S3 response has no valid object size")
	}
	return *object.ContentLength, nil
}
