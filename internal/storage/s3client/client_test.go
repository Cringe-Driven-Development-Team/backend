package s3client

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/prometheus/client_golang/prometheus"
)

type httpClientFunc func(*http.Request) (*http.Response, error)

func (f httpClientFunc) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestNewClientLogsS3OperationsWithoutObjectData(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "SECRET-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "SECRET-key")
	var output bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(observability.NewHandler(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))))
	t.Cleanup(func() { slog.SetDefault(old) })

	ctx := observability.WithRequestID(context.Background(), "req-s3")
	client, err := NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String("test-bucket"),
		Key:    aws.String("SECRET-object"),
	}, func(options *awss3.Options) {
		options.HTTPClient = httpClientFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader(""))}, nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	log := output.String()
	if strings.Count(log, `"operation":"s3.DeleteObject"`) != 1 ||
		!strings.Contains(log, `"request_id":"req-s3"`) || strings.Contains(log, "SECRET") {
		t.Fatalf("missing correlation or unsafe data: %s", log)
	}
}

// TestNewClientMeasuresFailedHTTPAttempt проверяет labels реального запроса SDK при 403.
func TestNewClientMeasuresFailedHTTPAttempt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	t.Setenv("AWS_ENDPOINT_URL_S3", server.URL)
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "secret-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret-key")
	registry := prometheus.NewRegistry()
	ctx := t.Context()
	client, err := NewClient(ctx, observability.NewHTTPClientMetrics(registry))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: aws.String("private-bucket"), Key: aws.String("private/object")})
	if err == nil {
		t.Fatal("forbidden S3 response succeeded")
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != "cellestial_http_client_requests_total" {
			continue
		}
		if len(family.Metric) != 1 || family.Metric[0].Counter.GetValue() != 1 {
			t.Fatal("S3 request was measured more than once")
		}
		for _, label := range family.Metric[0].Label {
			switch label.GetName() {
			case "dependency":
				if label.GetValue() != "s3" {
					t.Fatal(label)
				}
			case "operation":
				if label.GetValue() != "DeleteObject" {
					t.Fatal(label)
				}
			case "status_code":
				if label.GetValue() != "403" {
					t.Fatal(label)
				}
			case "method":
				if label.GetValue() != http.MethodDelete {
					t.Fatal(label)
				}
			default:
				t.Fatal("unexpected label", label.GetName())
			}
		}
		return
	}
	t.Fatal("S3 attempt metric missing")
}
