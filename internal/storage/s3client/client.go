// Package s3client создаёт общий AWS-клиент, используемый аватарами.
package s3client

import (
	"context"
	"fmt"
	"net/http"
	"time"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/middleware"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
)

// NewClient берёт регион, ключи и адрес из переменных AWS_*.
func NewClient(ctx context.Context, metrics ...*observability.HTTPClientMetrics) (*awss3.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return awss3.NewFromConfig(cfg, func(o *awss3.Options) {
		o.UsePathStyle = true
		o.APIOptions = append(o.APIOptions, logOperation)
		if len(metrics) > 0 && metrics[0] != nil {
			client := o.HTTPClient
			if client == nil {
				client = awshttp.NewBuildableClient()
			}
			o.HTTPClient = observedClient(func(req *http.Request) (*http.Response, error) {
				return metrics[0].Measure("s3", s3Operation, client.Do, req)
			})
		}
	}), nil
}

// logOperation измеряет вызов SDK целиком, включая повторы.
func logOperation(stack *middleware.Stack) error {
	return stack.Initialize.Add(middleware.InitializeMiddlewareFunc("StorageLog",
		func(ctx context.Context, input middleware.InitializeInput, next middleware.InitializeHandler) (middleware.InitializeOutput, middleware.Metadata, error) {
			started := time.Now()
			output, metadata, err := next.HandleInitialize(ctx, input)
			observability.LogStorage(ctx, "s3."+middleware.GetOperationName(ctx), started, err)
			return output, metadata, err
		}), middleware.Before)
}

// observedClient сохраняет настройки AWS HTTP client и измеряет попытки SDK.
type observedClient func(*http.Request) (*http.Response, error)

// Do отправляет HTTP-запрос SDK.
func (client observedClient) Do(req *http.Request) (*http.Response, error) { return client(req) }

// s3Operation читает только операции SDK аватаров.
func s3Operation(req *http.Request) string {
	switch name := middleware.GetOperationName(req.Context()); name {
	case "PutObject", "DeleteObject", "GetObject", "HeadObject":
		return name
	default:
		return "other"
	}
}
