package s3client

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPrivateFilesS3(t *testing.T) {
	if os.Getenv("S3_PRIVATE_FILES_SMOKE") != "1" {
		t.Skip("set S3_PRIVATE_FILES_SMOKE=1 to check a real S3 bucket")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	bucket := strings.TrimSpace(os.Getenv("S3_PRIVATE_FILES_BUCKET"))
	endpoint, err := url.Parse(os.Getenv("AWS_ENDPOINT_URL_S3"))
	if err != nil || endpoint.Host == "" ||
		(endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil {
		t.Fatal("AWS_ENDPOINT_URL_S3 must be an HTTP(S) URL without credentials")
	}
	client, err := NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	files, err := NewPrivateFiles(client, bucket)
	if err != nil {
		t.Fatal(err)
	}

	suffix := make([]byte, 12)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	key := "smoke/private-files/" + hex.EncodeToString(suffix) + ".txt"
	want := []byte("Cellestial private S3 storage check\n")
	if err := files.Put(ctx, key, bytes.NewReader(want)); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Logf("uploaded %s", key)

	body, err := files.Open(ctx, key)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	defer body.Close()
	got, err := io.ReadAll(io.LimitReader(body, int64(len(want)+1)))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("read-back differs from the uploaded file: %v", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.JoinPath(bucket, key).String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	anonymous := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	response, err := anonymous.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Errorf("anonymous GET returned %d, want 403", response.StatusCode)
	}
}
