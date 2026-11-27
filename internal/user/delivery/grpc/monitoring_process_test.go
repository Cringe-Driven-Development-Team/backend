package grpcdelivery_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// checkPrometheus запускает локальный Prometheus по явному опциональному пути.
func checkPrometheus(ctx context.Context, t *testing.T, addrs map[string]string, monitoring map[string]string) func() {
	t.Helper()
	binary := os.Getenv("MONITORING_PROMETHEUS_BIN")
	if binary == "" {
		return nil
	}
	dir := t.TempDir()
	rules := filepath.Join(dir, "rules.yml")
	ruleTests := filepath.Join(dir, "rules-test.yml")
	config := filepath.Join(dir, "prometheus.yml")
	contents := `groups:
- name: smoke
  rules:
  - record: cellestial:http_server_rps:sum
    expr: sum by (environment, service) (rate(cellestial_http_requests_total{route!~"/livez|/readyz|/metrics"}[20s]))
`
	if err := os.WriteFile(rules, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	ruleCases := `rule_files: [rules.yml]
evaluation_interval: 5s
fuzzy_compare: true
tests:
- interval: 5s
  input_series:
  - series: 'cellestial_http_requests_total{environment="local",service="identity",route="/login",instance="a"}'
    values: '40 45 50 0 5'
  - series: 'cellestial_http_requests_total{environment="local",service="identity",route="/login",instance="b"}'
    values: '0+0x4'
  promql_expr_test:
  - expr: cellestial:http_server_rps:sum
    eval_time: 20s
    exp_samples:
    - labels: 'cellestial:http_server_rps:sum{environment="local",service="identity"}'
      value: 0.6666666666666667
  - expr: resets(cellestial_http_requests_total{instance="a"}[20s])
    eval_time: 20s
    exp_samples:
    - labels: '{environment="local",service="identity",route="/login",instance="a"}'
      value: 1
- interval: 5s
  input_series:
  - series: 'cellestial_http_request_duration_seconds_bucket{service="workspace",instance="a",le="0.1"}'
    values: '0+5x4'
  - series: 'cellestial_http_request_duration_seconds_bucket{service="workspace",instance="a",le="0.5"}'
    values: '0+10x4'
  - series: 'cellestial_http_request_duration_seconds_bucket{service="workspace",instance="a",le="+Inf"}'
    values: '0+10x4'
  - series: 'cellestial_http_request_duration_seconds_bucket{service="workspace",instance="b",le="0.1"}'
    values: '0+5x4'
  - series: 'cellestial_http_request_duration_seconds_bucket{service="workspace",instance="b",le="0.5"}'
    values: '0+10x4'
  - series: 'cellestial_http_request_duration_seconds_bucket{service="workspace",instance="b",le="+Inf"}'
    values: '0+10x4'
  promql_expr_test:
  - expr: round(histogram_quantile(0.95, sum by (service,le) (rate(cellestial_http_request_duration_seconds_bucket[20s]))), 0.01)
    eval_time: 20s
    exp_samples:
    - labels: '{service="workspace"}'
      value: 0.46
`
	if writeErr := os.WriteFile(ruleTests, []byte(ruleCases), 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	var jobs []map[string]any
	for service, address := range addrs {
		job := map[string]any{
			"job_name": service,
			"static_configs": []map[string]any{{
				"targets": []string{address},
				"labels":  map[string]string{"service": service, "instance": service + "-smoke", "environment": "local"},
			}},
		}
		if service == "runtime-agent" {
			job["scheme"] = "https"
			job["tls_config"] = map[string]string{
				"ca_file": monitoring["TLS_CA_FILE"], "cert_file": monitoring["TLS_CERT_FILE"],
				"key_file": monitoring["TLS_KEY_FILE"], "server_name": "agent-metrics.internal.cellestial.ru",
			}
		}
		jobs = append(jobs, job)
	}
	data, err := json.Marshal(map[string]any{
		"global":     map[string]string{"scrape_interval": "3s", "scrape_timeout": "2s"},
		"rule_files": []string{rules}, "scrape_configs": jobs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(config, data, 0600); err != nil {
		t.Fatal(err)
	}
	promtool := filepath.Join(filepath.Dir(binary), "promtool")
	for _, check := range [][]string{{"check", "config", config}, {"check", "rules", rules}, {"test", "rules", ruleTests}} {
		if output, checkErr := exec.CommandContext(ctx, promtool, check...).CombinedOutput(); checkErr != nil {
			t.Fatalf("promtool %v: %v\n%s", check, checkErr, output)
		}
	}
	address := loopback(t)
	startPrometheus := func() *runningService {
		return startProcess(t, exec.Command(binary, "--config.file="+config,
			"--storage.tsdb.path="+dir+"/tsdb", "--web.listen-address="+address), nil)
	}
	process := startPrometheus()
	for ctx.Err() == nil {
		up, queryErr := queryPrometheus(ctx, address, `count(up{environment="local"} == 1)`)
		if queryErr == nil && up == float64(len(addrs)) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if ctx.Err() != nil {
		t.Fatalf("targets did not become healthy: %s", process.logs.String())
	}
	for _, check := range []struct {
		expression string
		minimum    float64
	}{
		{`sum(cellestial_http_requests_total{service="identity",route="/api/v1/auth/register",status_code="201"})`, 1},
		{`sum(cellestial_grpc_calls_total{service="runtime-agent",rpc_side="server",grpc_method="Assign",grpc_code="OK"})`, 1},
		{`sum(cellestial_runtime_target_info{service="environments",state="active"})`, 1},
	} {
		got, queryErr := queryPrometheus(ctx, address, check.expression)
		if queryErr != nil || got < check.minimum {
			t.Fatalf("Prometheus query %s = %v: %v", check.expression, got, queryErr)
		}
	}
	series, err := queryPrometheus(ctx, address, `count({environment="local",service!=""})`)
	if err != nil {
		t.Fatal(err)
	}
	duration, err := queryPrometheus(ctx, address, `max(scrape_duration_seconds{environment="local"})`)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Prometheus collected %g series from %d local targets; slowest scrape %.3fs", series, len(addrs), duration)
	return func() {
		checkExporterDown(ctx, t, address)
		process.stop(t)
		restarted := startPrometheus()
		checkExporterDown(ctx, t, address)
		restarted.stop(t)
	}
}

// checkExporterDown отличает прекратившийся scrape от живой цели без трафика.
func checkExporterDown(ctx context.Context, t *testing.T, address string) {
	t.Helper()
	attempt, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for attempt.Err() == nil {
		up, err := queryPrometheus(attempt, address, `up{service="runtime-agent"}`)
		if err == nil && up == 0 {
			if _, err = queryPrometheus(attempt, address, `max_over_time(timestamp(up{service="runtime-agent"} == 1)[1m:1s])`); err != nil {
				t.Fatal("historical success was lost after exporter failure", err)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("runtime-agent remained healthy after stop")
}

// queryPrometheus читает единственное числовое значение через API локального Prometheus.
func queryPrometheus(ctx context.Context, address, expression string) (float64, error) {
	endpoint := "http://" + address + "/api/v1/query?query=" + url.QueryEscape(expression)
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var result struct {
		Status string
		Data   struct {
			Result []struct{ Value [2]json.RawMessage }
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	if result.Status != "success" || len(result.Data.Result) != 1 {
		return 0, fmt.Errorf("unexpected Prometheus response status %q with %d samples", result.Status, len(result.Data.Result))
	}
	var number string
	if err := json.Unmarshal(result.Data.Result[0].Value[1], &number); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(number, 64)
}
