// Package ansible запускает ansible-playbook для настройки VPS сессии.
package ansible

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/usecase"
)

// Artifacts — статические пути артефактов агента.
type Artifacts struct {
	AgentWheel, AgentWheelSHA256File, AgentRequirements, AgentService, CACertFile string
}

// Runner реализует usecase.HostConfigurator через ansible-playbook.
type Runner struct {
	workDir, playbook, sshKeyFile, sshUser, binary string
	art                                            Artifacts
}

var _ usecase.HostConfigurator = (*Runner)(nil)

// New создаёт Runner.
//
// Создаёт workDir (0700) и удаляет в нём оставшиеся от убитого воркера каталоги операций
// (только вида "<uuid>-<цифры>"; сам workDir и посторонние файлы не трогаются).
func New(workDir, playbook, sshKeyFile, sshUser, binary string, artifacts Artifacts) (*Runner, error) {
	absWork, err := filepath.Abs(workDir)
	if err != nil {
		return nil, fmt.Errorf("ansible work dir: %w", err)
	}
	absPlaybook, err := filepath.Abs(playbook)
	if err != nil {
		return nil, fmt.Errorf("ansible playbook path: %w", err)
	}
	if err = os.MkdirAll(absWork, 0o700); err != nil {
		return nil, fmt.Errorf("create work dir: %w", err)
	}
	entries, err := os.ReadDir(absWork)
	if err != nil {
		return nil, fmt.Errorf("read work dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() || !opDirName.MatchString(e.Name()) {
			continue
		}
		if err = os.RemoveAll(filepath.Join(absWork, e.Name())); err != nil {
			return nil, fmt.Errorf("remove stale operation dir: %w", err)
		}
	}
	return &Runner{workDir: absWork, playbook: absPlaybook, sshKeyFile: sshKeyFile, sshUser: sshUser, binary: binary, art: artifacts}, nil
}

var opDirName = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}-[0-9]+$`)

func (r *Runner) vars(c usecase.HostConfig) (map[string]any, error) {
	sha, err := os.ReadFile(r.art.AgentWheelSHA256File)
	if err != nil {
		return nil, fmt.Errorf("read wheel sha256: %w", err)
	}
	ca, err := os.ReadFile(r.art.CACertFile)
	if err != nil {
		return nil, fmt.Errorf("read CA cert: %w", err)
	}
	blocked := c.BlockedPublicIPs
	if blocked == nil {
		blocked = []string{}
	}
	return map[string]any{
		"session_id":         c.SessionID.String(),
		"language":           c.Language,
		"private_ip":         c.PrivateIP,
		"token":              c.Token,
		"server_cert_pem":    string(c.ServerCertPEM),
		"server_key_pem":     string(c.ServerKeyPEM),
		"python_image":       c.PythonImage,
		"r_image":            c.RImage,
		"kernel_dns":         c.KernelDNS,
		"blocked_public_ips": blocked,
		"agent_wheel":        r.art.AgentWheel,
		"agent_wheel_sha256": strings.TrimSpace(string(sha)),
		"agent_requirements": r.art.AgentRequirements,
		"agent_service":      r.art.AgentService,
		"ca_cert_pem":        string(ca),
	}, nil
}

func writeJSON(path string, v any, mode os.FileMode) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, mode)
}

// Configure запускает playbook против VPS; временный каталог удаляется всегда.
func (r *Runner) Configure(ctx context.Context, c usecase.HostConfig) error {
	if err := os.MkdirAll(r.workDir, 0o700); err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}
	dir, err := os.MkdirTemp(r.workDir, c.SessionID.String()+"-")
	if err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}
	defer os.RemoveAll(dir)
	if err = os.Chmod(dir, 0o700); err != nil {
		return err
	}

	vars, err := r.vars(c)
	if err != nil {
		return err
	}
	inventory := map[string]any{"runtime": map[string]any{"hosts": map[string]any{
		c.PrivateIP: map[string]any{
			"ansible_host":                 c.PrivateIP,
			"ansible_user":                 r.sshUser,
			"ansible_python_interpreter":   "/usr/bin/python3",
			"ansible_ssh_private_key_file": r.sshKeyFile,
		},
	}}}
	invPath := filepath.Join(dir, "inventory.json")
	varsPath := filepath.Join(dir, "vars.json")
	knownHosts := filepath.Join(dir, "known_hosts")
	if err := writeJSON(invPath, inventory, 0o600); err != nil {
		return err
	}
	if err := writeJSON(varsPath, vars, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(knownHosts, []byte(c.PrivateIP+" "+strings.TrimSpace(c.HostPublicKey)+"\n"), 0o600); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, r.binary, r.playbook, "-i", invPath, "-e", "@"+varsPath)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"ANSIBLE_SSH_ARGS=-o UserKnownHostsFile=" + knownHosts + " -o StrictHostKeyChecking=yes",
		"ANSIBLE_HOST_KEY_CHECKING=True",
		// ansible-core требует UTF-8 локаль; в контейнере API её может не быть.
		"LC_ALL=C.UTF-8",
		"LANG=C.UTF-8",
	}
	cmd.Dir = filepath.Dir(r.playbook) // ansible.cfg читается из cwd
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	lw := newLogWriter(c.Token, c.SessionID.String())
	cmd.Stdout, cmd.Stderr = lw, lw

	runErr := cmd.Run()
	lw.flush()
	if runErr != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("ansible-playbook: %w", ctx.Err())
		}
		return fmt.Errorf("ansible-playbook: %w", runErr)
	}
	return nil
}

// logWriter разбивает вывод на строки и пишет в slog с маскированием токена.
type logWriter struct {
	pr   *io.PipeReader
	pw   *io.PipeWriter
	done chan struct{}
}

func newLogWriter(token, session string) *logWriter {
	pr, pw := io.Pipe()
	l := &logWriter{pr: pr, pw: pw, done: make(chan struct{})}
	go func() {
		defer close(l.done)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if token != "" {
				line = strings.ReplaceAll(line, token, "***")
			}
			slog.Info(line, "component", "ansible", "session_id", session)
		}
		_, _ = io.Copy(io.Discard, pr)
	}()
	return l
}

func (l *logWriter) Write(p []byte) (int, error) { return l.pw.Write(p) }

func (l *logWriter) flush() {
	_ = l.pw.Close()
	<-l.done
}
