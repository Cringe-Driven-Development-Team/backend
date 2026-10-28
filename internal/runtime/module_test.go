package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/config"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/usecase"
)

func TestRuntimeDisabledDoesNotStartWorker(t *testing.T) {
	svc, w, err := Build(context.Background(), config.RuntimeConfig{}, nil)
	if err != nil || w != nil || svc == nil {
		t.Fatalf("svc=%v worker=%v err=%v", svc, w, err)
	}
	if _, err := svc.RequestCreate(context.Background(), usecase.CreateEnvironment{SessionID: uuid.New(), Language: "python"}); !errors.Is(err, usecase.ErrDisabled) {
		t.Fatalf("err=%v", err)
	}
}

func TestRuntimeFakeBuilds(t *testing.T) {
	cfg := config.RuntimeConfig{Enabled: true, Provider: "fake", SecretsKey: make([]byte, 32), MaxActive: 1, MaxConcurrentOps: 1}
	svc, w, err := Build(context.Background(), cfg, nil)
	if err != nil || svc == nil || w == nil {
		t.Fatalf("svc=%v worker=%v err=%v", svc, w, err)
	}
}

func TestRuntimeInitErrorOnBadCA(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(bad, []byte("not pem SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.RuntimeConfig{
		Enabled: true, Provider: "openstack", SecretsKey: make([]byte, 32),
		TLSCACertFile: bad, TLSCAKeyFile: bad, TLSClientCertFile: bad, TLSClientKeyFile: bad,
	}
	if _, w, err := Build(context.Background(), cfg, nil); err == nil || w != nil {
		t.Fatalf("expected error, got worker=%v err=%v", w, err)
	}
}
