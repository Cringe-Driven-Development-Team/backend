package s3client

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

// PrivateFiles читает и записывает объекты в отдельном приватном бакете.
type PrivateFiles struct {
	client *awss3.Client
	bucket string
}

func NewPrivateFiles(client *awss3.Client, bucket string) (*PrivateFiles, error) {
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return nil, errors.New("S3_PRIVATE_FILES_BUCKET is not set")
	}
	return &PrivateFiles{client: client, bucket: bucket}, nil
}

func (s *PrivateFiles) Put(ctx context.Context, key string, body io.ReadSeeker) error {
	_, err := s.client.PutObject(ctx, &awss3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   body,
	}, func(options *awss3.Options) {
		options.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	})
	return err
}

// Open возвращает поток объекта. Вызывающий код закрывает его после чтения.
func (s *PrivateFiles) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	object, err := s.client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	return object.Body, nil
}
