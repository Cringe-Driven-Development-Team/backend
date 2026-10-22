package s3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
)

func TestS3LogsOperationWithoutObjectData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/missing") {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, "<Error><Code>NoSuchKey</Code><Message>SECRET-error</Message></Error>")
			return
		}
		if strings.Contains(r.URL.Path, "/failed") {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "<Error><Code>ServiceUnavailable</Code><Message>SECRET-error</Message></Error>")
			return
		}
		if strings.Contains(r.URL.Path, "/truncated") {
			w.Header().Set("Content-Length", "20")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "SECRET")
			return
		}
		switch r.Method {
		case http.MethodPut:
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			_, _ = io.WriteString(w, "SECRET-body")
		}
	}))
	defer server.Close()
	client := awss3.NewFromConfig(aws.Config{Region: "us-east-1", RetryMaxAttempts: 1, Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: "key", SecretAccessKey: "SECRET-key"}, nil
	}), HTTPClient: server.Client(), BaseEndpoint: aws.String(server.URL)}, func(o *awss3.Options) { o.UsePathStyle = true })
	storage := NewFileStorage(client, "SECRET-bucket")
	var out bytes.Buffer
	var level slog.LevelVar
	level.Set(slog.LevelDebug)
	old := slog.Default()
	slog.SetDefault(slog.New(observability.NewHandler(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: &level}))))
	t.Cleanup(func() { slog.SetDefault(old) })
	ctx := observability.WithRequestID(context.Background(), "req-s3")
	if err := storage.Put(ctx, "SECRET-path", []byte("SECRET-body")); err != nil {
		t.Fatal(err)
	}
	data, err := storage.Get(ctx, "SECRET-path")
	if err != nil || string(data) != "SECRET-body" {
		t.Fatalf("Get data=%q err=%v", data, err)
	}
	log := out.String()
	for _, operation := range []string{"notebook.put_object", "notebook.get_object"} {
		if strings.Count(log, `"operation":"`+operation+`"`) != 1 {
			t.Fatalf("operation %s: %s", operation, log)
		}
	}
	if strings.Count(log, `"request_id":"req-s3"`) != 2 || strings.Contains(log, "SECRET") {
		t.Fatalf("unsafe logs: %s", log)
	}
	for _, tc := range []struct {
		name, operation string
		call            func() error
		wantNotFound    bool
		wantBodyRead    bool
	}{
		{"missing", "notebook.get_object", func() error { _, err := storage.Get(ctx, "missing"); return err }, true, false},
		{"sdk_error", "notebook.put_object", func() error { return storage.Put(ctx, "failed", []byte("SECRET-body")) }, false, false},
		{"truncated_body", "notebook.get_object", func() error { _, err := storage.Get(ctx, "truncated"); return err }, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out.Reset()
			err := tc.call()
			if err == nil || tc.wantNotFound && !errors.Is(err, usecase.ErrFileNotFound) || tc.wantBodyRead && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("expected storage error, got %v", err)
			}
			log := out.String()
			if strings.Count(log, `"operation":"`+tc.operation+`"`) != 1 || !strings.Contains(log, `"outcome":"error"`) || !strings.Contains(log, `"request_id":"req-s3"`) || strings.Contains(log, "SECRET") {
				t.Fatalf("unsafe failure log: %s", log)
			}
		})
	}
}
