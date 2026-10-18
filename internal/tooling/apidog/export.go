// Package apidog скачивает спецификацию OpenAPI из Apidog.
//
// Токен берётся из APIDOG_TOKEN, sprint-ветка из APIDOG_BRANCH_ID, без неё
// выгружается main. Переменных нет в окружении, они читаются из .env,
// поэтому make generate работает без export.
package apidog

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	exportURL      = "https://api.apidog.com/v1/projects/1382426/export-openapi"
	apiVersion     = "2024-03-28"
	requestTimeout = time.Minute
	maxErrorBody   = 512
)

type exportRequest struct {
	Scope        exportScope   `json:"scope"`
	Options      exportOptions `json:"options"`
	OASVersion   string        `json:"oasVersion"`
	ExportFormat string        `json:"exportFormat"`
	BranchID     int64         `json:"branchId,omitempty"`
}

type exportScope struct {
	Type string `json:"type"`
}

type exportOptions struct {
	IncludeApidogExtensionProperties bool `json:"includeApidogExtensionProperties"`
	AddFoldersToTags                 bool `json:"addFoldersToTags"`
}

// HTTPClient выполняет экспорт спецификации.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// Options задаёт файлы и зависимости экспорта. Нулевые зависимости используют окружение процесса.
type Options struct {
	Out, EnvFile string
	Client       HTTPClient
	LookupEnv    func(string) (string, bool)
	Log          io.Writer
}

// Run экспортирует спецификацию, записывая файл только после успешной проверки ответа.
func Run(ctx context.Context, opts Options) error {
	if opts.Client == nil {
		opts.Client = http.DefaultClient
	}
	if opts.LookupEnv == nil {
		opts.LookupEnv = os.LookupEnv
	}
	if opts.Log == nil {
		opts.Log = os.Stderr
	}
	out, envFile := opts.Out, opts.EnvFile
	fileEnv, err := readEnvFile(envFile)
	if err != nil {
		return err
	}
	lookup := func(key string) string {
		if value, ok := opts.LookupEnv(key); ok {
			return value
		}
		return fileEnv[key]
	}

	token := lookup("APIDOG_TOKEN")
	if token == "" {
		return errors.New("APIDOG_TOKEN is not set: create a personal token in Apidog " +
			"(Account Settings → API Access Token) and put it into .env, " +
			"see «Как менять контракт» in README.md")
	}

	req := exportRequest{
		Scope:        exportScope{Type: "ALL"},
		OASVersion:   "3.1",
		ExportFormat: "YAML",
	}
	branch := "main"
	if raw := lookup("APIDOG_BRANCH_ID"); raw != "" {
		id, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || id <= 0 {
			return fmt.Errorf("APIDOG_BRANCH_ID must be a positive number, got %q", raw)
		}
		req.BranchID = id
		branch = "sprint branch " + raw
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	spec, err := export(ctx, opts.Client, token, req)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, spec, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}

	fmt.Fprintf(opts.Log, "apidog: exported %s to %s\n", branch, out)
	return nil
}

func export(ctx context.Context, client HTTPClient, token string, body exportRequest) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exportURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Apidog-Api-Version", apiVersion)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request Apidog: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("apidog rejected the token (%s): check APIDOG_TOKEN", resp.Status)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("apidog responded %s: %s", resp.Status, truncate(data))
	case !bytes.HasPrefix(data, []byte("openapi:")):
		return nil, fmt.Errorf("apidog returned something other than an OpenAPI spec: %s", truncate(data))
	}
	return data, nil
}

// readEnvFile читает пары KEY=VALUE. Если файла нет, это не ошибка.
func readEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	env := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		env[strings.TrimSpace(key)] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return env, nil
}

func truncate(data []byte) string {
	if len(data) > maxErrorBody {
		data = data[:maxErrorBody]
	}
	return strings.TrimSpace(string(data))
}
