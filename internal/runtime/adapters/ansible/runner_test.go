package ansible

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/usecase"
)

const secretToken = "tok-super-secret-123"

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type env struct {
	home, work string
	runner     *Runner
}

func setup(t *testing.T) env {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := filepath.Join(t.TempDir(), "work")
	art := t.TempDir()
	write := func(name, s string) string {
		p := filepath.Join(art, name)
		noErr(t, os.WriteFile(p, []byte(s), 0o600))
		return p
	}
	bin, err := filepath.Abs("testdata/fake-ansible-playbook")
	noErr(t, err)
	r := New(work, "playbook.yml", "/keys/id", "ubuntu", bin, Artifacts{
		AgentWheel:           "/a/agent.whl",
		AgentWheelSHA256File: write("sha", "abc123\n"),
		AgentRequirements:    "/a/req.txt",
		AgentService:         "/a/agent.service",
		CACertFile:           write("ca.pem", "CA-PEM"),
	})
	return env{home: home, work: work, runner: r}
}

func cfg() usecase.HostConfig {
	return usecase.HostConfig{
		SessionID: uuid.New(), Language: "python", PrivateIP: "10.0.0.5",
		HostPublicKey: "ssh-ed25519 AAAATESTKEY", Token: secretToken,
		ServerCertPEM: []byte("CERT"), ServerKeyPEM: []byte("KEY"),
		PythonImage: "py:1", RImage: "r:1", KernelDNS: "1.1.1.1",
		BlockedPublicIPs: []string{"1.2.3.4"},
	}
}

func readHome(t *testing.T, e env, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.home, name))
	noErr(t, err)
	return strings.TrimSpace(string(b))
}

func TestRunnerWritesFilesWithModes(t *testing.T) {
	e := setup(t)
	noErr(t, e.runner.Configure(context.Background(), cfg()))
	eq(t, "dir=700 vars=600", readHome(t, e, "modes.txt"))
	eq(t, "10.0.0.5 ssh-ed25519 AAAATESTKEY", readHome(t, e, "known_hosts.txt"))
}

func TestRunnerPassesNoSecretsInArgs(t *testing.T) {
	e := setup(t)
	noErr(t, e.runner.Configure(context.Background(), cfg()))
	args := readHome(t, e, "args.txt")
	notContains(t, args, secretToken)
	notContains(t, args, "KEY")
	contains(t, args, "playbook.yml")
}

func TestRunnerKillsProcessGroupOnCancel(t *testing.T) {
	e := setup(t)
	noErr(t, os.WriteFile(filepath.Join(e.home, "sleep"), nil, 0o600))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.runner.Configure(ctx, cfg()) }()
	pidFile := filepath.Join(e.home, "sleep.pid")
	eventually(t, func() bool { _, err := os.Stat(pidFile); return err == nil }, 5*time.Second, 20*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	b, err := os.ReadFile(pidFile)
	noErr(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	noErr(t, err)
	cancel()
	select {
	case err := <-done:
		isErr(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Configure did not return")
	}
	eventually(t, func() bool { return syscall.Kill(pid, 0) != nil }, 5*time.Second, 20*time.Millisecond)
}

func TestRunnerRemovesWorkDir(t *testing.T) {
	e := setup(t)
	noErr(t, e.runner.Configure(context.Background(), cfg()))
	dir := readHome(t, e, "dir.txt")
	_, err := os.Stat(dir)
	isTrue(t, os.IsNotExist(err))
}

func TestRunnerLogsMaskToken(t *testing.T) {
	e := setup(t)
	buf := &lockedBuf{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	noErr(t, e.runner.Configure(context.Background(), cfg()))
	out := buf.String()
	contains(t, out, "using token ***")
	notContains(t, out, secretToken)
}

func TestRunnerVarsContent(t *testing.T) {
	e := setup(t)
	// script copies nothing else; check via the generated vars before removal using a hook
	c := cfg()
	vars, err := e.runner.vars(c)
	noErr(t, err)
	eq(t, "abc123", vars["agent_wheel_sha256"])
	eq(t, "CA-PEM", vars["ca_cert_pem"])
	eq(t, secretToken, vars["token"])
	eq(t, []string{"1.2.3.4"}, vars["blocked_public_ips"])
	if _, ok := vars["host_public_key"]; ok {
		t.Fatal("host key in vars")
	}
}
