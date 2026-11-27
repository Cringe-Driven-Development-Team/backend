package grpcdelivery_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	agentv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/agent/v1"
	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/testutil/pgtest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const testBFFKey = "full-chain-bff-key"

type runningService struct {
	cmd    *exec.Cmd
	logs   *safeLogBuffer
	done   chan error
	exited bool
}

// TestMicroservicesProcesses проверяет реальные процессы, DB-роли и транспорт до Python agent.
func TestMicroservicesProcesses(t *testing.T) {
	binaries := os.Getenv("MICROSERVICES_BIN_DIR")
	python := os.Getenv("MICROSERVICES_AGENT_PYTHON")
	redis := os.Getenv("REDIS_TEST_ADDR")
	if binaries == "" || python == "" || redis == "" {
		t.Skip("set process binaries, agent Python and disposable Redis")
	}
	db := pgtest.NewDatabase(t, "SERVICE_TEST_DATABASE_URL")
	timeout := 55 * time.Second
	if os.Getenv("MICROSERVICES_LARGE_FILES") == "1" || os.Getenv("MICROSERVICES_LARGE_NOTEBOOK") == "1" {
		timeout = 4 * time.Minute
	}
	largeUpload := os.Getenv("MICROSERVICES_LARGE_UPLOAD") == "1"
	if largeUpload {
		timeout = 12 * time.Minute
	}
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	if _, err := db.Exec(ctx, "SELECT setval('identity.users_id_seq',$1,false)", time.Now().UnixMicro()); err != nil {
		t.Fatal(err)
	}
	pki := newPKI(t)
	idHTTP, idRPC, idAdmin := loopback(t), loopback(t), loopback(t)
	wsHTTP, wsRPC, wsAdmin := loopback(t), loopback(t), loopback(t)
	envRPC, envAdmin := loopback(t), loopback(t)
	agentRPC, agentAdmin, agentMetrics := loopback(t), loopback(t), loopback(t)
	_, agentPort, _ := net.SplitHostPort(agentRPC)
	redisHost, redisPort, err := net.SplitHostPort(redis)
	if err != nil {
		t.Fatal(err)
	}
	identityEnv := processTLS(t, pki, "identity.internal.cellestial.ru")
	identityEnv["DATABASE_URL"] = appDatabase(t, db, "identity")
	identityEnv["HTTP_ADDR"], identityEnv["GRPC_ADDR"], identityEnv["ADMIN_ADDR"] = idHTTP, idRPC, idAdmin
	identityEnv["WORKSPACE_GRPC_ADDR"] = wsRPC
	identityEnv["REDIS_HOST"], identityEnv["REDIS_PORT"], identityEnv["REDIS_DB"] = redisHost, redisPort, "11"
	identityEnv["BFF_API_KEY"] = testBFFKey
	identityEnv["JWT_SECRET"] = "full-chain-jwt-secret"
	identityEnv["TOTP_ENCRYPTION_KEY"] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	identityEnv["AWS_REGION"], identityEnv["AWS_ACCESS_KEY_ID"], identityEnv["AWS_SECRET_ACCESS_KEY"] = "us-east-1", "test", "test"
	identityEnv["AWS_EC2_METADATA_DISABLED"] = "true"
	identityEnv["S3_AVATARS_BUCKET"], identityEnv["S3_AVATARS_PUBLIC_URL"] = "avatars", "https://cdn.example/avatars"
	identity := startProcess(t, exec.Command(filepath.Join(binaries, "identity")), identityEnv)
	env := processTLS(t, pki, "environments.internal.cellestial.ru")
	env["DATABASE_URL"] = appDatabase(t, db, "environments")
	env["GRPC_ADDR"], env["ADMIN_ADDR"], env["AGENT_GRPC_PORT"] = envRPC, envAdmin, agentPort
	env["RUNTIME_PROVIDER"], env["RUNTIME_WARM_POOL_SIZE"] = "fake", "0"
	env["RUNTIME_SECRETS_KEY"] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	environments := startProcess(t, exec.Command(filepath.Join(binaries, "environments")), env)
	ws := processTLS(t, pki, "workspace.internal.cellestial.ru")
	ws["DATABASE_URL"] = appDatabase(t, db, "workspace")
	ws["HTTP_ADDR"], ws["GRPC_ADDR"], ws["ADMIN_ADDR"] = wsHTTP, wsRPC, wsAdmin
	ws["IDENTITY_GRPC_ADDR"], ws["ENVIRONMENTS_GRPC_ADDR"] = idRPC, envRPC
	ws["BFF_API_KEY"], ws["WORKSPACE_CURSOR_KEY"] = testBFFKey, strings.Repeat("k", 32)
	ws["HTTP_TIMEOUT"] = "2m"
	ws["WORKERS_PAUSED"] = "true"
	workspaceBin := filepath.Join(binaries, "workspace")
	workspace := startProcess(t, exec.Command(workspaceBin), ws)
	for addr, proc := range map[string]*runningService{idAdmin: identity, wsAdmin: workspace, envAdmin: environments} {
		waitReady(ctx, t, addr, proc)
	}

	identityHTTP := testHTTP(pki, pki.certificate(t, "bff.internal.cellestial.ru", x509.ExtKeyUsageClientAuth), "identity")
	workspaceHTTP := testHTTP(pki, pki.certificate(t, "bff.internal.cellestial.ru", x509.ExtKeyUsageClientAuth), "workspace")
	t.Cleanup(identityHTTP.CloseIdleConnections)
	t.Cleanup(workspaceHTTP.CloseIdleConnections)
	wrongPeer := testHTTP(pki, pki.certificate(t, "identity.internal.cellestial.ru", x509.ExtKeyUsageClientAuth), "workspace")
	defer wrongPeer.CloseIdleConnections()
	if resp, peerErr := wrongPeer.Get("https://" + wsHTTP + "/api/v1/notebooks"); peerErr == nil {
		resp.Body.Close()
		t.Fatal("Workspace accepted a non-BFF certificate")
	}
	denied := processRequest(ctx, t, workspaceHTTP, wsHTTP, "GET", "/notebooks/1", "", "", "", 403)
	if !bytes.Contains(denied, []byte("s2s_forbidden")) {
		t.Fatal(string(denied))
	}
	var registered struct {
		Tokens struct {
			Access string `json:"access_token"`
		} `json:"tokens"`
	}
	login := "chain_" + strconv.FormatInt(time.Now().UnixNano()%1_000_000_000, 10)
	body := processRequest(ctx, t, identityHTTP, idHTTP, "POST", "/auth/register", testBFFKey, "",
		fmt.Sprintf(`{"login":%q,"password":"long-test-password"}`, login), 201)
	if err = json.Unmarshal(body, &registered); err != nil || registered.Tokens.Access == "" {
		t.Fatal("missing registered access token")
	}
	bearer := registered.Tokens.Access
	if os.Getenv("MICROSERVICES_LARGE_NOTEBOOK") == "1" {
		testLargeNotebook(ctx, t, workspaceHTTP, wsHTTP, bearer)
	}
	var profile struct{ ID int64 }
	body = processRequest(ctx, t, identityHTTP, idHTTP, "GET", "/users/me", testBFFKey, bearer, "", 200)
	if err = json.Unmarshal(body, &profile); err != nil || profile.ID <= 0 {
		t.Fatal("missing user ID")
	}
	var notebook struct{ ID int64 }
	body = processRequest(ctx, t, workspaceHTTP, wsHTTP, "POST", "/notebooks", testBFFKey, bearer, `{"name":"Проверка"}`, 201)
	if err = json.Unmarshal(body, &notebook); err != nil || notebook.ID <= 0 {
		t.Fatal("missing notebook ID")
	}
	book := fmt.Sprintf("/notebooks/%d", notebook.ID)
	exported := processRequest(ctx, t, workspaceHTTP, wsHTTP, "GET", book+"/export", testBFFKey, bearer, "", 200)
	if !json.Valid(exported) || !bytes.Contains(exported, []byte(`"nbformat":4`)) {
		t.Fatal("invalid streamed IPYNB")
	}
	processRequest(ctx, t, workspaceHTTP, wsHTTP, "POST", book+"/export", testBFFKey, bearer, "", 405)
	checkScrape(ctx, t, http.DefaultClient, "http://"+wsAdmin+"/metrics",
		`cellestial_http_requests_total{method="GET",route="/api/v1/notebooks/{id}/export",status_code="200"} 1`,
		`cellestial_http_requests_total{method="POST",route="/api/v1/notebooks/{id}/export",status_code="405"} 1`)
	var cell string
	if err = db.QueryRow(ctx, `SELECT cell_id FROM workspace.notebook_cells WHERE notebook_id=$1 ORDER BY position LIMIT 1`, notebook.ID).Scan(&cell); err != nil {
		t.Fatal(err)
	}

	session := uuid.New()
	settings := processTLS(t, pki, "agent."+session.String()+".internal.cellestial.ru")
	settings["GRPC_ADDR"], settings["ADMIN_ADDR"] = agentRPC, agentAdmin
	monitoringPKI := newPKI(t)
	settings["METRICS_ADDR"] = agentMetrics
	for key, value := range processTLS(t, monitoringPKI, "agent-metrics.internal.cellestial.ru") {
		settings["METRICS_"+key] = value
	}
	settings["STATE_DIR"], settings["WORKSPACE_PATH"] = t.TempDir(), t.TempDir()
	for _, entry := range []string{"data", "staging"} {
		if err = os.Mkdir(filepath.Join(settings["WORKSPACE_PATH"], entry), 0700); err != nil {
			t.Fatal(err)
		}
	}

	settings["KERNEL_NETWORK"], settings["KERNEL_DNS"] = "test-kernel", "1.1.1.1"
	agentRoot, err := filepath.Abs("../../../../runtime-agent")
	if err != nil {
		t.Fatal(err)
	}
	settings["PYTHONPATH"] = filepath.Join(agentRoot, "src") + string(os.PathListSeparator) + agentRoot
	agentCmd := exec.Command(python, "-c", processAgentCode)
	agentCmd.Dir = agentRoot
	agent := startProcess(t, agentCmd, settings)
	waitReady(ctx, t, agentAdmin, agent)
	assignAgent(ctx, t, pki, agentRPC, session, agent)

	serverID := uuid.New()
	if _, execErr := db.Exec(ctx, `INSERT INTO environments.runtime_pool(id,state,server_id,image_id,agent_version) VALUES($1,'READY',$2,'test','test')`, uuid.New(), serverID.String()); execErr != nil {
		t.Fatal(execErr)
	}
	inventory := "http://" + envAdmin + "/metrics"
	checkScrape(ctx, t, http.DefaultClient, inventory, "cellestial_runtime_inventory_available 1",
		fmt.Sprintf(`cellestial_runtime_target_info{server_id="%s",state="warm"} 1`, serverID))

	seedRuntime(ctx, t, db, session, profile.ID, notebook.ID)
	if _, execErr := db.Exec(ctx, `UPDATE environments.runtime_environments SET server_id=$1 WHERE session_id=$2`, serverID.String(), session); execErr != nil {
		t.Fatal(execErr)
	}
	checkScrape(ctx, t, http.DefaultClient, inventory,
		fmt.Sprintf(`cellestial_runtime_target_info{server_id="%s",state="active"} 1`, serverID))
	if _, execErr := db.Exec(ctx, `UPDATE environments.runtime_pool SET state='CLAIMED', session_id=$1 WHERE server_id=$2`, session, serverID.String()); execErr != nil {
		t.Fatal(execErr)
	}
	if _, execErr := db.Exec(ctx, `UPDATE environments.runtime_environments SET server_id=NULL WHERE session_id=$1`, session); execErr != nil {
		t.Fatal(execErr)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, inventory, nil)
	scrape, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(scrape.Body)
	_ = scrape.Body.Close()
	if readErr != nil || scrape.StatusCode != http.StatusOK ||
		!bytes.Contains(data, []byte("cellestial_runtime_inventory_available 1")) ||
		bytes.Contains(data, []byte(serverID.String())) {
		t.Fatalf("deleted VPS is still expected or inventory failed: %v", readErr)
	}
	if _, execErr := db.Exec(ctx, `UPDATE environments.runtime_environments SET server_id=$1 WHERE session_id=$2`, serverID.String(), session); execErr != nil {
		t.Fatal(execErr)
	}

	sid := "/runtime-sessions/" + session.String()
	checkPath := sid + "/workspace/check"
	processRequest(ctx, t, workspaceHTTP, wsHTTP, "GET", checkPath, testBFFKey, bearer, "", 200)
	verifyAgentRetry(ctx, t, workspaceHTTP, wsHTTP, checkPath, bearer, identity, workspace, environments, agent)
	testFileRange(ctx, t, workspaceHTTP, wsHTTP, settings["WORKSPACE_PATH"], session, bearer)
	if largeUpload {
		testLargeUpload(ctx, t, workspaceHTTP, wsHTTP, settings["WORKSPACE_PATH"], session, bearer)
	}
	hash := sha256.Sum256(nil)
	executionRequest := fmt.Sprintf(`{"execution_id":"process-execution","cell_id":%q,"source_hash":%q,"timeout_seconds":5}`, cell, hex.EncodeToString(hash[:]))
	const durableCorrelation = "durable-execution-correlation"
	processRequest(ctx, t, workspaceHTTP, wsHTTP, "POST", sid+"/executions", testBFFKey, bearer, executionRequest, 202,
		map[string]string{"X-Request-ID": durableCorrelation})
	pending := processRequest(ctx, t, workspaceHTTP, wsHTTP, "GET", sid+"/executions/process-execution", testBFFKey, bearer, "", 200)
	if !bytes.Contains(pending, []byte(`"state":"QUEUED"`)) {
		t.Fatalf("paused worker must leave execution queued: %s", pending)
	}
	workspace.stop(t)
	workspaceHTTP.CloseIdleConnections()
	ws["WORKERS_PAUSED"] = "false"
	workspace = startProcess(t, exec.Command(workspaceBin), ws)
	waitReady(ctx, t, wsAdmin, workspace)
	var finished bool
	for ctx.Err() == nil {
		payload := processRequest(ctx, t, workspaceHTTP, wsHTTP, "GET", sid+"/executions/process-execution", testBFFKey, bearer, "", 200)
		if bytes.Contains(payload, []byte(`"state":"SUCCEEDED"`)) {
			finished = true
			break
		}
		if bytes.Contains(payload, []byte(`"state":"FAILED"`)) {
			t.Fatal(string(payload))
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !finished {
		t.Fatal("Python execution did not reach Workspace")
	}
	for _, proc := range []*runningService{environments, agent} {
		if !strings.Contains(proc.logs.String(), durableCorrelation) {
			t.Fatalf("execution correlation lost after Workspace restart: %s", proc.cmd.Path)
		}
	}
	for _, proc := range []*runningService{identity, workspace, environments, agent} {
		if !strings.Contains(proc.logs.String(), "process-correlation") {
			t.Fatal("missing cross-process correlation")
		}
		if strings.Contains(proc.logs.String(), testBFFKey) || strings.Contains(proc.logs.String(), bearer) {
			t.Fatal("credentials in process logs")
		}
	}
	checkProcessMetrics(ctx, t, wsAdmin, "workspace_execution_queue_length")
	scraper := monitoringPKI.certificate(t, "prometheus.internal.cellestial.ru", x509.ExtKeyUsageClientAuth)
	monitorClient := testHTTP(monitoringPKI, scraper, "agent-metrics")
	checkScrape(ctx, t, monitorClient, "https://"+agentMetrics+"/metrics", "runtime_agent_kernel_state", "cellestial_grpc_calls_total")
	checkMetricsTransport(ctx, t, pki, monitoringPKI, scraper, agentMetrics, agentRPC, session)
	var checkMonitoringAfterStop func()
	if os.Getenv("MONITORING_PROMETHEUS_BIN") != "" {
		checkMonitoringAfterStop = checkPrometheus(ctx, t, map[string]string{
			"identity": idAdmin, "workspace": wsAdmin, "environments": envAdmin, "runtime-agent": agentMetrics,
		}, processTLS(t, monitoringPKI, "prometheus.internal.cellestial.ru"))
	}
	checkProcessMetrics(ctx, t, envAdmin, "environments_provisioning_pending")
	checkProcessMetrics(ctx, t, idAdmin, "identity_build_info")
	deleteAccount(ctx, t, pki, idRPC, profile.ID)
	processRequest(ctx, t, workspaceHTTP, wsHTTP, "GET", book, testBFFKey, bearer, "", 401)
	agent.stop(t)
	if checkMonitoringAfterStop != nil {
		checkMonitoringAfterStop()
	}
	for _, proc := range []*runningService{workspace, identity, environments} {
		proc.stop(t)
	}
}

// testLargeUpload передаёт ровно 3 ГиБ через HTTP и оба gRPC hop с повтором первого чанка.
func testLargeUpload(ctx context.Context, t *testing.T, client *http.Client, address, root string, session uuid.UUID, bearer string) {
	t.Helper()
	const size int64 = 3 << 30
	const chunkSize = 1 << 20
	payload := strings.Repeat("12345678", chunkSize/8)
	sum := sha256.New()
	for range size / chunkSize {
		_, _ = io.WriteString(sum, payload)
	}
	hash := hex.EncodeToString(sum.Sum(nil))
	prefix := "/runtime-sessions/" + session.String() + "/workspace"
	headers := map[string]string{"X-Workspace-Socket": "process-upload", "X-Request-ID": "large-upload-transport"}
	command := func(fields string) []byte {
		return processRequest(ctx, t, client, address, "POST", prefix+"/command", testBFFKey, bearer,
			fmt.Sprintf(`{"protocol_version":1,"request_id":"upload-business-key","session_id":%q,"workspace_generation":%q,%s}`, session, session, fields), 200, headers)
	}
	reply := command(fmt.Sprintf(`"type":"upload_begin","operation_id":%q,"path":"large-upload.bin","size_bytes":%d,"sha256":%q`, uuid.NewString(), size, hash))
	var begin struct {
		Result struct {
			ID string `json:"transfer_id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(reply, &begin); err != nil || begin.Result.ID == "" {
		t.Fatalf("large upload admission: %s (%v)", reply, err)
	}
	id := begin.Result.ID
	chunkHeaders := map[string]string{"Content-Type": "application/octet-stream", "X-Request-ID": "large-upload-transport"}
	send := func(offset int64) {
		data := processRequest(ctx, t, client, address, "POST", fmt.Sprintf("%s/transfers/%s/chunks?offset=%d", prefix, id, offset),
			testBFFKey, bearer, payload, 200, chunkHeaders)
		var ack struct {
			Offset int64 `json:"offset"`
		}
		if err := json.Unmarshal(data, &ack); err != nil || ack.Offset != offset+chunkSize {
			t.Fatalf("bad upload acknowledgement at %d: %s (%v)", offset, data, err)
		}
	}
	send(0)
	send(0)
	for offset := int64(chunkSize); offset < size; offset += chunkSize {
		send(offset)
	}
	commit := fmt.Sprintf(`"type":"upload_commit","transfer_id":%q`, id)
	for range 2 {
		if data := command(commit); !bytes.Contains(data, []byte(`"state":"completed"`)) {
			t.Fatalf("large upload commit: %s", data)
		}
	}
	file, err := os.Open(filepath.Join(root, "data", "large-upload.bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	sum.Reset()
	if n, err := io.Copy(sum, file); err != nil || n != size || hex.EncodeToString(sum.Sum(nil)) != hash {
		t.Fatalf("published upload does not match %d bytes and digest: n=%d err=%v", size, n, err)
	}
}

// verifyAgentRetry проверяет оба исхода HTTP-вызова и один correlation ID во всех процессах.
func verifyAgentRetry(ctx context.Context, t *testing.T, client *http.Client, address, path, bearer string, identity, workspace, environments, agent *runningService) {
	t.Helper()
	const retryID, errorID = "agent-check-retry", "agent-check-error"
	processRequest(ctx, t, client, address, "GET", path, testBFFKey, bearer, "", 200, map[string]string{"X-Request-ID": retryID})
	failed := processRequest(ctx, t, client, address, "GET", path, testBFFKey, bearer, "", 500, map[string]string{"X-Request-ID": errorID})
	if !bytes.Contains(failed, []byte(`"code":"internal"`)) || !bytes.Contains(failed, []byte(errorID)) || bytes.Contains(failed, []byte("private-check-error")) {
		t.Fatalf("unexpected public error: %s", failed)
	}
	// HTTP-ответ может прийти раньше, чем os/exec скопирует строку процесса в буфер.
	logContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for _, process := range []*runningService{identity, workspace, environments, agent} {
		for {
			logs := process.logs.String()
			if strings.Contains(logs, "private-check-error") {
				t.Fatalf("leaked dependency error in %s", process.cmd.Path)
			}
			if strings.Contains(logs, retryID) && strings.Contains(logs, errorID) {
				break
			}
			select {
			case <-logContext.Done():
				t.Fatalf("missing retry or error correlation in %s", process.cmd.Path)
			case <-ticker.C:
			}
		}
	}
	retried := false
	for line := range strings.SplitSeq(workspace.logs.String(), "\n") {
		if strings.Contains(line, retryID) && strings.Contains(line, `"method":"/cellestial.files.v1.Files/Check"`) &&
			strings.Contains(line, `"attempts":2`) && strings.Contains(line, `"status":"OK"`) {
			retried = true
			break
		}
	}
	if !retried {
		t.Fatal("file readiness read did not retry once with the original request ID")
	}
}

const processAgentCode = `
import asyncio, signal
from tests.conftest import FakeContainers, FakeKernel
from runtime_agent.config import Settings
from runtime_agent.observability import configure_logging, request_id
from runtime_agent.rpc.config import RPCSettings
from runtime_agent.rpc.files import Files
from runtime_agent.rpc.main import run
from runtime_agent.models import AgentError

original = FakeKernel.send
async def send(self, kind, content, msg_id):
    await original(self, kind, content, msg_id)
    if kind == "execute_request":
        self.complete(msg_id)
FakeKernel.send = send

original_check = Files.Check
failed_checks = set()
async def check(self, request, context):
    correlation = request_id.get()
    if correlation == "agent-check-error":
        raise RuntimeError("private-check-error")
    if correlation == "agent-check-retry" and correlation not in failed_checks:
        failed_checks.add(correlation)
        raise AgentError(503, "private-check-error")
    return await original_check(self, request, context)
Files.Check = check

async def main():
    configure_logging("info")
    stop = asyncio.Event()
    for sig in (signal.SIGINT, signal.SIGTERM):
        asyncio.get_running_loop().add_signal_handler(sig, stop.set)
    await run(Settings.from_env(), RPCSettings.from_env(), stop, FakeContainers)

asyncio.run(main())
`

func appDatabase(t *testing.T, db *pgxpool.Pool, schema string) string {
	t.Helper()
	name := "process_" + schema + "_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	q := pgx.Identifier{name}.Sanitize()
	ctx := t.Context()
	if _, err := db.Exec(ctx, "CREATE ROLE "+q+" LOGIN PASSWORD 'test-process-password'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DROP OWNED BY "+q)
		_, _ = db.Exec(context.Background(), "DROP ROLE "+q)
	})
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA " + schema + " TO " + q,
		"GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + schema + " TO " + q,
		"REVOKE INSERT,UPDATE,DELETE ON " + schema + ".goose_db_version FROM " + q,
		"GRANT USAGE ON ALL SEQUENCES IN SCHEMA " + schema + " TO " + q,
	} {
		if _, err := db.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	dsn, err := url.Parse(db.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	dsn.User = url.UserPassword(name, "test-process-password")
	return dsn.String()
}

func loopback(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().String()
}

func processTLS(t *testing.T, pki *testPKI, name string) map[string]string {
	t.Helper()
	pair := pki.certificate(t, name, x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth)
	key, err := x509.MarshalPKCS8PrivateKey(pair.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := map[string]string{}
	for setting, block := range map[string]pem.Block{
		"TLS_CERT_FILE": {Type: "CERTIFICATE", Bytes: pair.Certificate[0]},
		"TLS_KEY_FILE":  {Type: "PRIVATE KEY", Bytes: key},
		"TLS_CA_FILE":   {Type: "CERTIFICATE", Bytes: pki.ca.Raw},
	} {
		path := filepath.Join(dir, setting+".pem")
		if err = os.WriteFile(path, pem.EncodeToMemory(&block), 0600); err != nil {
			t.Fatal(err)
		}
		out[setting] = path
	}
	return out
}

func startProcess(t *testing.T, cmd *exec.Cmd, env map[string]string) *runningService {
	t.Helper()
	logs := &safeLogBuffer{}
	cmd.Stdout, cmd.Stderr = logs, logs
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8"}
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &runningService{cmd: cmd, logs: logs, done: make(chan error, 1)}
	go func() { p.done <- cmd.Wait() }()
	t.Cleanup(func() {
		if !p.exited {
			_ = cmd.Process.Kill()
			<-p.done
		}
	})
	return p
}

func (p *runningService) stop(t *testing.T) {
	t.Helper()
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case err := <-p.done:
		p.exited = true
		if err != nil {
			t.Fatalf("graceful process exit: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("process did not stop")
	}
}

func waitReady(ctx context.Context, t *testing.T, addr string, p *runningService) {
	t.Helper()
	probe := http.Client{Timeout: 250 * time.Millisecond}
	for ctx.Err() == nil {
		resp, err := probe.Get("http://" + addr + "/readyz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		select {
		case err := <-p.done:
			p.exited = true
			t.Fatalf("process exited before readiness: %v (%s)", err, p.logs.String())
		case <-time.After(25 * time.Millisecond):
		}
	}
	t.Fatalf("process readiness timed out: %s", p.logs.String())
}

func testHTTP(pki *testPKI, pair tls.Certificate, server string) *http.Client {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pki.pool, Certificates: []tls.Certificate{pair},
		ServerName: server + ".internal.cellestial.ru"}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}, Timeout: 5 * time.Second}
}

func processRequest(ctx context.Context, t *testing.T, httpClient *http.Client, addr, method, path, key, token, body string, want int, extraHeaders ...map[string]string) []byte {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, "https://"+addr+"/api/v1"+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Request-ID", "process-correlation")
	if key != "" {
		req.Header.Set("X-BFF-Key", key)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, headers := range extraHeaders {
		for name, value := range headers {
			req.Header.Set(name, value)
		}
	}
	res, err := httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != want || res.Header.Get("X-Request-ID") != req.Header.Get("X-Request-ID") {
		t.Fatalf("%s %s status=%d want=%d error=%v body=%s", method, path, res.StatusCode, want, err, string(data))
	}
	return data
}

func dialProcess(t *testing.T, pki *testPKI, addr, peer, caller string) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{RootCAs: pki.pool,
		ServerName: peer + ".internal.cellestial.ru", Certificates: []tls.Certificate{pki.certificate(t, caller+".internal.cellestial.ru", x509.ExtKeyUsageClientAuth)}})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func assignAgent(parent context.Context, t *testing.T, pki *testPKI, addr string, session uuid.UUID, process *runningService) {
	t.Helper()
	ctx, cancelAll := context.WithTimeout(parent, 6*time.Second)
	defer cancelAll()
	var last string
	client := agentv1.NewControlClient(dialProcess(t, pki, addr, "agent."+session.String(), "environments"))
	for ctx.Err() == nil {
		current, cancel := context.WithTimeout(ctx, time.Second)
		h, err := client.Assign(current, &agentv1.AssignRequest{Target: &agentv1.Target{SessionId: session.String(), Fence: 1}, Language: "python"})
		cancel()
		if err == nil && h.GetReady() {
			return
		}
		last = fmt.Sprintf("state=%s error=%v", h.GetState(), err)
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("agent did not accept assignment: %s (%s)", last, process.logs.String())
}

func seedRuntime(ctx context.Context, t *testing.T, db *pgxpool.Pool, session uuid.UUID, uid, book int64) {
	t.Helper()
	now := time.Now().UTC()
	record, err := json.Marshal(map[string]any{"OwnerID": uid, "RequestID": "prepared-runtime", "AgentStarted": true,
		"CorrelationID": "process-correlation", "public": map[string]any{"id": session, "notebook_id": book, "language": "python",
			"state": "READY", "stage": "ready", "created_at": now, "last_activity_at": now}})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `INSERT INTO environments.runtime_environments(session_id,operation_id,language,profile,state,desired_state,private_ip,agent_version)
		VALUES($1,$2,'python','default','READY','READY','127.0.0.1','test')`, session, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO workspace.runtime_sessions(id,owner_id,notebook_id,request_id,language,state,data)
		VALUES($1,$2,$3,'prepared-runtime','python','READY',$4)`, session, uid, book, record); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE workspace.user_guards SET active_session_id=$2 WHERE user_id=$1`, uid, session); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

// checkProcessMetrics проверяет метрики Go-сервиса на приватном admin listener.
func checkProcessMetrics(ctx context.Context, t *testing.T, addr, name string) {
	t.Helper()
	checkScrape(ctx, t, http.DefaultClient, "http://"+addr+"/metrics", name)
}

// checkScrape проверяет HTTP и конкретные семейства/labels без correlation в них.
func checkScrape(ctx context.Context, t *testing.T, client *http.Client, endpoint string, expected ...string) {
	t.Helper()
	r, _ := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	resp, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != 200 || bytes.Contains(data, []byte("process-correlation")) {
		t.Fatalf("unexpected scrape status=%d err=%v", resp.StatusCode, err)
	}
	for _, name := range expected {
		if !bytes.Contains(data, []byte(name)) {
			t.Fatalf("missing scrape sample %s", name)
		}
	}
}

// checkMetricsTransport проверяет раздельное доверие управляющего и scrape-входов.
func checkMetricsTransport(ctx context.Context, t *testing.T, pki, monitor *testPKI, scraper tls.Certificate, metrics, agent string, session uuid.UUID) {
	t.Helper()
	for _, client := range []*http.Client{
		{Transport: &http.Transport{TLSClientConfig: &tls.Config{
			RootCAs: monitor.pool, ServerName: "agent-metrics.internal.cellestial.ru",
		}}},
		testHTTP(monitor, pki.certificate(t, "environments.internal.cellestial.ru", x509.ExtKeyUsageClientAuth), "agent-metrics"),
	} {
		r, _ := http.NewRequestWithContext(ctx, "GET", "https://"+metrics+"/metrics", nil)
		if resp, err := client.Do(r); err == nil {
			resp.Body.Close()
			t.Fatal("metrics accepted missing or foreign certificate")
		}
	}
	conn, err := grpc.NewClient(agent, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		RootCAs: pki.pool, ServerName: "agent." + session.String() + ".internal.cellestial.ru",
		Certificates: []tls.Certificate{scraper},
	})))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	attempt, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if _, err = agentv1.NewControlClient(conn).Health(attempt, &agentv1.Target{SessionId: session.String()}); err == nil {
		t.Fatal("control RPC accepted monitoring certificate")
	}
}

func deleteAccount(ctx context.Context, t *testing.T, pki *testPKI, addr string, id int64) {
	t.Helper()
	client := identityv1.NewUserAdministrationClient(dialProcess(t, pki, addr, "identity", "identity-admin"))
	op := uuid.NewString()
	if _, err := client.RequestUserDeletion(ctx, &identityv1.RequestUserDeletionRequest{UserId: id, OperationId: op}); err != nil {
		t.Fatal(err)
	}
	for ctx.Err() == nil {
		reply, err := client.GetUserDeletion(ctx, &identityv1.GetUserDeletionRequest{OperationId: op})
		if err != nil {
			t.Fatal(err)
		}
		if reply.Done {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatal("deletion did not reach a confirmed terminal state")
}

// testFileRange проверяет реальный диапазон файла за границей int32 через оба gRPC hop.
func testFileRange(ctx context.Context, t *testing.T, transport *http.Client, address, root string, session uuid.UUID, bearer string) {
	t.Helper()
	const size = 3 << 30
	file, err := os.Create(filepath.Join(root, "data", "range.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(size); err == nil {
		_, err = file.WriteAt([]byte("tail"), size-4)
	}
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	prefix := "/runtime-sessions/" + session.String() + "/workspace"
	begin := func() string {
		request := fmt.Sprintf(`{"protocol_version":1,"type":"download_begin","request_id":"file-operation","session_id":%q,
			"workspace_generation":%q,"operation_id":%q,"path":"range.bin"}`, session, session, uuid.NewString())
		result := processRequest(ctx, t, transport, address, "POST", prefix+"/command", testBFFKey, bearer, request, 200,
			map[string]string{"X-Workspace-Socket": "process-socket"})
		var view struct {
			Result struct {
				ID string `json:"transfer_id"`
			}
		}
		if decodeErr := json.Unmarshal(result, &view); decodeErr != nil || view.Result.ID == "" {
			t.Fatalf("download_begin failed: %s", result)
		}
		return prefix + "/transfers/" + view.Result.ID + "/download"
	}
	data := processRequest(ctx, t, transport, address, "GET", begin(), testBFFKey, bearer, "", 206,
		map[string]string{"Range": fmt.Sprintf("bytes=%d-", size-4)})
	if string(data) != "tail" {
		t.Fatalf("range did not return expected file bytes: %q", data)
	}
	if os.Getenv("MICROSERVICES_LARGE_FILES") != "1" {
		return
	}
	largeClient := *transport
	largeClient.Timeout = 0
	req, err := http.NewRequestWithContext(ctx, "GET", "https://"+address+"/api/v1"+begin(), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-BFF-Key", testBFFKey)
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("X-Request-ID", "process-correlation")
	response, err := largeClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.ContentLength != size {
		t.Fatalf("full file response: %d %d", response.StatusCode, response.ContentLength)
	}
	sum := sha256.New()
	n, err := io.Copy(sum, response.Body)
	if err != nil || n != size {
		t.Fatalf("full download read %d bytes: %v", n, err)
	}
	source, err := os.Open(filepath.Join(root, "data", "range.bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	expected := sha256.New()
	if _, err = io.Copy(expected, source); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sum.Sum(nil), expected.Sum(nil)) {
		t.Fatal("large file digest mismatch")
	}
}

// testLargeNotebook проверяет 128 МиБ на HTTP-границе и Content-Digest согласованного экспорта.
func testLargeNotebook(ctx context.Context, t *testing.T, client *http.Client, address, bearer string) {
	t.Helper()
	payload, err := os.CreateTemp(t.TempDir(), "large-ipynb-*")
	if err != nil {
		t.Fatal(err)
	}
	defer payload.Close()
	multi := multipart.NewWriter(payload)
	part, err := multi.CreateFormFile("file", "large.ipynb")
	if err != nil {
		t.Fatal(err)
	}
	const start = `{"nbformat":4,"nbformat_minor":5,"metadata":{},"cells":[`
	const stop = `]}`
	const closeCell = `"}}}`
	const size = 128 << 20
	openCell := func(i int) string {
		return fmt.Sprintf(`{"id":"cell-%d","cell_type":"markdown","source":"text","metadata":{},"attachments":{"file":{"text/plain":"`, i)
	}
	write := func(text string) {
		if _, writeErr := io.WriteString(part, text); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	overhead := len(start) + len(stop) + 3
	for i := range 4 {
		overhead += len(openCell(i)) + len(closeCell)
	}
	chunk := strings.Repeat("a", 64<<10)
	write(start)
	left := size - overhead
	for i := range 4 {
		if i != 0 {
			write(",")
		}
		write(openCell(i))
		current := min(left, 32<<20)
		for current > 0 {
			n := min(current, len(chunk))
			write(chunk[:n])
			current -= n
			left -= n
		}
		write(closeCell)
	}
	write(stop)
	if err = multi.WriteField("name", "128 MiB"); err == nil {
		err = multi.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	bodySize, err := payload.Seek(0, io.SeekCurrent)
	if err == nil {
		_, err = payload.Seek(0, io.SeekStart)
	}
	if err != nil {
		t.Fatal(err)
	}
	transport := *client
	transport.Timeout = 0
	request, err := http.NewRequestWithContext(ctx, "POST", "https://"+address+"/api/v1/notebooks/import", payload)
	if err != nil {
		t.Fatal(err)
	}
	request.ContentLength = bodySize
	request.Header.Set("X-BFF-Key", testBFFKey)
	request.Header.Set("Authorization", "Bearer "+bearer)
	request.Header.Set("Content-Type", multi.FormDataContentType())
	res, err := transport.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 16<<10))
	if err != nil || res.StatusCode != 201 {
		t.Fatalf("large import: %d %s %v", res.StatusCode, data, err)
	}
	var notebook struct {
		View struct{ ID int64 } `json:"notebook"`
	}
	if err = json.Unmarshal(data, &notebook); err != nil || notebook.View.ID <= 0 {
		t.Fatalf("large import response: %s", data)
	}
	url := fmt.Sprintf("https://%s/api/v1/notebooks/%d/export", address, notebook.View.ID)
	request, err = http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-BFF-Key", testBFFKey)
	request.Header.Set("Authorization", "Bearer "+bearer)
	res, err = transport.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	sum := sha256.New()
	n, err := io.Copy(sum, res.Body)
	wantDigest := "sha-256=:" + base64.StdEncoding.EncodeToString(sum.Sum(nil)) + ":"
	if err != nil || n != size || res.StatusCode != 200 || res.Header.Get("Content-Digest") != wantDigest {
		t.Fatalf("large export: status=%d bytes=%d verified_digest=%v err=%v", res.StatusCode, n, res.Header.Get("Content-Digest") == wantDigest, err)
	}
}
