package s3client

import (
	"bytes"
	"context"
	"image"
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
	if endpoint.Path == "" {
		endpoint.Path = "/"
	}
	client, err := NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	files, err := NewPrivateFiles(client, bucket)
	if err != nil {
		t.Fatal(err)
	}

	httpClient := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	for _, example := range []string{"example.jpg", "example.txt"} {
		t.Run(example, func(t *testing.T) {
			want, err := os.ReadFile("testdata/" + example)
			if err != nil {
				t.Fatal(err)
			}
			key := "smoke/" + NewPrivateFileKey()
			put, err := files.PresignPut(ctx, key)
			if err != nil {
				t.Fatal("PUT signing failed")
			}
			assertPathStyle(t, put.URL, endpoint, bucket, key)
			request, err := http.NewRequestWithContext(ctx, put.Method, put.URL, bytes.NewReader(want))
			if err != nil {
				t.Fatal("invalid signed PUT request")
			}
			request.Header = put.SignedHeader.Clone()
			request.Header.Set("Content-Type", "application/octet-stream")
			response := doPrivateFilesRequest(t, httpClient, request)
			response.Body.Close()
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				t.Fatalf("PUT returned %d", response.StatusCode)
			}
			t.Logf("uploaded %s", key)

			size, err := files.Size(ctx, key)
			if err != nil || size != int64(len(want)) {
				t.Fatalf("HEAD size = %d, error = %v", size, err)
			}
			get, err := files.PresignDownload(ctx, key)
			if err != nil {
				t.Fatal("GET signing failed")
			}
			assertPathStyle(t, get.URL, endpoint, bucket, key)
			request, err = http.NewRequestWithContext(ctx, get.Method, get.URL, nil)
			if err != nil {
				t.Fatal("invalid signed GET request")
			}
			request.Header = get.SignedHeader.Clone()
			response = doPrivateFilesRequest(t, httpClient, request)
			got, readErr := io.ReadAll(io.LimitReader(response.Body, int64(len(want)+1)))
			response.Body.Close()
			if response.StatusCode != http.StatusOK || response.Header.Get("Content-Disposition") != "attachment" ||
				readErr != nil || !bytes.Equal(got, want) {
				t.Fatalf("unexpected signed GET response, status %d, disposition %q",
					response.StatusCode, response.Header.Get("Content-Disposition"))
			}

			request, err = http.NewRequestWithContext(ctx, http.MethodGet, endpoint.JoinPath(bucket, key).String(), nil)
			if err != nil {
				t.Fatal("invalid anonymous GET request")
			}
			response = doPrivateFilesRequest(t, httpClient, request)
			response.Body.Close()
			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("anonymous GET returned %d, want 403", response.StatusCode)
			}

			preview, err := files.CreatePreview(ctx, key)
			if err != nil {
				t.Fatal(err)
			}
			if example == "example.txt" {
				if preview != "" {
					t.Fatal("unexpected preview for text")
				}
				return
			}
			if preview != key+".preview.jpeg" {
				t.Fatalf("unexpected preview key %q", preview)
			}
			t.Logf("uploaded %s", preview)
			signedPreview, err := files.PresignDownload(ctx, preview)
			if err != nil {
				t.Fatal("preview signing failed")
			}
			request, err = http.NewRequestWithContext(ctx, signedPreview.Method, signedPreview.URL, nil)
			if err != nil {
				t.Fatal("invalid preview GET request")
			}
			request.Header = signedPreview.SignedHeader.Clone()
			response = doPrivateFilesRequest(t, httpClient, request)
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "image/jpeg" {
				t.Fatalf("unexpected preview response, status %d, type %q",
					response.StatusCode, response.Header.Get("Content-Type"))
			}
			img, format, err := image.Decode(response.Body)
			if err != nil || format != "jpeg" || img.Bounds().Size() != image.Pt(512, 256) {
				t.Fatalf("unexpected JPEG preview, format %q, error %v", format, err)
			}
		})
	}
}

// assertPathStyle проверяет адрес запроса и скрывает параметры подписи при ошибке.
func assertPathStyle(t *testing.T, signedURL string, endpoint *url.URL, bucket, key string) {
	t.Helper()
	u, err := url.Parse(signedURL)
	want := endpoint.JoinPath(bucket, key)
	if err != nil {
		t.Fatal("invalid signed URL")
	}
	if u.Scheme != want.Scheme || u.Host != want.Host || u.Path != want.Path {
		t.Fatalf("signed address %s://%s%s, want %s://%s%s", u.Scheme, u.Host, u.Path, want.Scheme, want.Host, want.Path)
	}
}

// doPrivateFilesRequest отправляет запрос без вывода подписанного URL при ошибке.
func doPrivateFilesRequest(t *testing.T, client *http.Client, request *http.Request) *http.Response {
	t.Helper()
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s request failed", request.Method)
	}
	return response
}

func TestPrivateFilesSelectelPresignPut(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "test-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_REGION", "ru-7")
	t.Setenv("AWS_ENDPOINT_URL_S3", "https://s3.ru-7.storage.selcloud.ru")
	client, err := NewClient(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	files, err := NewPrivateFiles(client, "cellestial-private-files-0")
	if err != nil {
		t.Fatal(err)
	}
	const key = "private-files/example"
	signed, err := files.PresignPut(t.Context(), key)
	if err != nil {
		t.Fatal("PUT signing failed")
	}
	u, err := url.Parse(signed.URL)
	if err != nil {
		t.Fatal("invalid signed URL")
	}
	if origin := u.Scheme + "://" + u.Host; origin != "https://cellestial-private-files-0.s3.ru-7.storage.selcloud.ru" {
		t.Errorf("unexpected origin %s", origin)
	}
	if signed.Method != http.MethodPut || u.Path != "/"+key || u.Query().Get("X-Amz-Expires") != "120" {
		t.Error("unexpected signed method, path or expiration")
	}
}
