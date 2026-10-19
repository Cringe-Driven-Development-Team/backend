package apidog

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type clientFunc func(*http.Request) (*http.Response, error)

func (f clientFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }
func TestExportFromEnvFileAndEnvironment(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, "env")
	out := filepath.Join(dir, "spec.yaml")
	if err := os.WriteFile(env, []byte("APIDOG_TOKEN=file-token\nAPIDOG_BRANCH_ID=12\n"), 0600); err != nil {
		t.Fatal(err)
	}
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "preserved")
	client := clientFunc(func(r *http.Request) (*http.Response, error) {
		if r.Context().Value(key{}) != "preserved" || r.Header.Get("Authorization") != "Bearer environment-token" || r.Method != "POST" {
			t.Error("request parameters or context changed")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["branchId"] != float64(12) || body["oasVersion"] != "3.1" {
			t.Fatalf("body: %v", body)
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader("openapi: 3.1.0\n"))}, nil
	})
	err := Run(ctx, Options{Out: out, EnvFile: env, Client: client, LookupEnv: func(k string) (string, bool) { return "environment-token", k == "APIDOG_TOKEN" }, Log: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil || string(data) != "openapi: 3.1.0\n" {
		t.Fatalf("output: %q, %v", data, err)
	}
}
func TestExportRejectsInvalidConfigurationAndResponses(t *testing.T) {
	for _, tt := range []struct {
		name, branch, body string
		status             int
	}{
		{"bad branch", "zero", "", 200}, {"unauthorized", "", "denied", 401}, {"bad format", "", "<html>", 200}, {"upstream failure", "", "oops", 500},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "spec.yaml")
			lookup := func(k string) (string, bool) {
				if k == "APIDOG_TOKEN" {
					return "token", true
				}
				if k == "APIDOG_BRANCH_ID" {
					return tt.branch, tt.branch != ""
				}
				return "", false
			}
			c := clientFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tt.status, Status: http.StatusText(tt.status), Body: io.NopCloser(strings.NewReader(tt.body))}, nil
			})
			if err := Run(context.Background(), Options{Out: out, Client: c, LookupEnv: lookup, Log: io.Discard}); err == nil {
				t.Fatal("invalid export accepted")
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatal("failed export wrote output")
			}
		})
	}
}
