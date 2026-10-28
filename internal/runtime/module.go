// Package runtime собирает модуль создания VPS пользовательских сессий.
package runtime

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/config"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/adapters/agent"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/adapters/ansible"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/adapters/fake"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/adapters/openstack"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/adapters/pki"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/adapters/postgres"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/usecase"
)

const (
	workerLease    = 60 * time.Second
	workerPoll     = 2 * time.Second
	fakeCloudDelay = 200 * time.Millisecond
	artifactsEnv   = "RUNTIME_ARTIFACTS_DIR"
	artifactsBase  = "/app"
)

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// Build собирает сервис и воркер. При cfg.Enabled=false возвращает сервис, отвечающий
// ErrDisabled, и nil-воркер; адаптеры не создаются. cfg содержит секреты и не логируется.
func Build(ctx context.Context, cfg config.RuntimeConfig, pool *pgxpool.Pool) (usecase.EnvironmentService, *usecase.Worker, error) {
	profiles := map[string]bool{"default": true}
	// Репозиторий — тонкая обёртка над пулом (без внешних соединений), нужна и выключенному
	// сервису: Get/RequestDelete читают только БД.
	repo := postgres.NewRepository(pool)
	if !cfg.Enabled {
		return usecase.NewService(repo, usecase.Settings{Enabled: false, Profiles: profiles}), nil, nil
	}
	svc := usecase.NewService(repo, usecase.Settings{
		Enabled: true, MaxActive: cfg.MaxActive, AgentVersion: cfg.AgentVersion, Profiles: profiles,
	})
	clock := systemClock{}

	var (
		cloud usecase.CloudProvider
		host  usecase.HostConfigurator
		ag    usecase.AgentClient
		sec   usecase.Secrets
	)
	switch cfg.Provider {
	case "fake":
		caCert, caKey, err := throwawayCA()
		if err != nil {
			return nil, nil, fmt.Errorf("runtime: fake CA: %w", err)
		}
		if sec, err = pki.New(cfg.SecretsKey, caCert, caKey, clock); err != nil {
			return nil, nil, fmt.Errorf("runtime: secrets: %w", err)
		}
		c, h, a := fake.New(fakeCloudDelay)
		cloud, host, ag = c, h, a
	case "openstack":
		var err error
		cloud, host, ag, sec, err = buildOpenStack(ctx, cfg, clock)
		if err != nil {
			return nil, nil, err
		}
	default:
		return nil, nil, fmt.Errorf("runtime: unknown provider %q", cfg.Provider)
	}

	name, _ := os.Hostname()
	var suffix [4]byte
	_, _ = rand.Read(suffix[:])
	worker := usecase.NewWorker(repo, repo, cloud, host, ag, sec, clock, usecase.WorkerSettings{
		Owner:       fmt.Sprintf("%s-%x", name, suffix),
		Concurrency: cfg.MaxConcurrentOps, Lease: workerLease, PollInterval: workerPoll,
		ReconcileInterval: cfg.ReconcileInterval, StartupTimeout: cfg.StartupTimeout,
		AnsibleTimeout: cfg.AnsibleTimeout, DrainTimeout: cfg.DrainTimeout, DeleteTimeout: cfg.DeleteTimeout,
		PythonImage: cfg.PythonImage, RImage: cfg.RImage, KernelDNS: cfg.KernelDNS,
		BlockedPublicIPs: cfg.BlockedPublicIPs, EnvName: cfg.EnvName,
	})
	return svc, worker, nil
}

func buildOpenStack(ctx context.Context, cfg config.RuntimeConfig, clock usecase.Clock) (usecase.CloudProvider, usecase.HostConfigurator, usecase.AgentClient, usecase.Secrets, error) {
	read := func(path string) ([]byte, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("runtime: read %s: %w", filepath.Base(path), err)
		}
		return b, nil
	}
	caCert, err := read(cfg.TLSCACertFile)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	caKey, err := read(cfg.TLSCAKeyFile)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	clientCert, err := read(cfg.TLSClientCertFile)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	clientKey, err := read(cfg.TLSClientKeyFile)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	sec, err := pki.New(cfg.SecretsKey, caCert, caKey, clock)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("runtime: secrets: %w", err)
	}
	ag, err := agent.New(caCert, clientCert, clientKey)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("runtime: agent client: %w", err)
	}
	art, playbook, err := artifacts(cfg.TLSCACertFile)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	host, err := ansible.New(cfg.WorkDir, playbook, cfg.SSHPrivateKeyFile, cfg.SSHUser, "ansible-playbook", art)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("runtime: ansible: %w", err)
	}
	cloud, err := openstack.New(ctx, cfg)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("runtime: openstack: %w", err)
	}
	return cloud, host, ag, sec, nil
}

// artifacts находит артефакты агента и плейбук (ruling R8).
func artifacts(caCertFile string) (ansible.Artifacts, string, error) {
	base := os.Getenv(artifactsEnv)
	if base == "" {
		base = artifactsBase
	}
	base, err := filepath.Abs(base)
	if err != nil {
		return ansible.Artifacts{}, "", fmt.Errorf("runtime: artifacts dir: %w", err)
	}
	caAbs, err := filepath.Abs(caCertFile)
	if err != nil {
		return ansible.Artifacts{}, "", fmt.Errorf("runtime: CA cert path: %w", err)
	}
	agentDir := filepath.Join(base, "agent")
	wheels, err := filepath.Glob(filepath.Join(agentDir, "cellestial_agent-*.whl"))
	if err != nil || len(wheels) != 1 {
		return ansible.Artifacts{}, "", fmt.Errorf("runtime: expected exactly one cellestial_agent-*.whl in %s, found %d", agentDir, len(wheels))
	}
	art := ansible.Artifacts{
		AgentWheel:           wheels[0],
		AgentWheelSHA256File: wheels[0] + ".sha256",
		AgentRequirements:    filepath.Join(agentDir, "requirements.txt"),
		AgentService:         filepath.Join(agentDir, "cellestial-agent.service"),
		CACertFile:           caAbs,
	}
	for _, p := range []string{art.AgentWheelSHA256File, art.AgentRequirements, art.AgentService} {
		if _, err := os.Stat(p); err != nil {
			return ansible.Artifacts{}, "", fmt.Errorf("runtime: artifact %s: %w", filepath.Base(p), err)
		}
	}
	return art, filepath.Join(base, "deploy", "runtime", "site.yml"), nil
}

// throwawayCA создаёт одноразовый CA в памяти для RUNTIME_PROVIDER=fake.
func throwawayCA() (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "cellestial-fake-ca"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(1, 0, 0),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	kd, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kd}), nil
}
