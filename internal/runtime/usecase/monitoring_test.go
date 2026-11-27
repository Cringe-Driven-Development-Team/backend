package usecase

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

type monitoringHost struct{ config HostConfig }

// Configure сохраняет параметры вызова Ansible для проверки.
func (h *monitoringHost) Configure(_ context.Context, config HostConfig) error {
	h.config = config
	return nil
}

// TestPrepareHostMonitoring проверяет отдельную пару и параметры scraper на штатном шаге подготовки.
func TestPrepareHostMonitoring(t *testing.T) {
	host := &monitoringHost{}
	transport := &AgentGRPCSettings{
		MonitoringScraperIP: "10.0.0.4", MonitoringClientCA: []byte("scraper-ca"),
		IssueCertificate: func(_ string, dns string) ([]byte, []byte, error) {
			return []byte("cert:" + dns), []byte("key:" + dns), nil
		},
	}
	worker := Worker{host: host, s: WorkerSettings{AgentGRPC: transport}}
	id := uuid.New()
	if err := worker.prepareHost(t.Context(), id, "10.0.0.8", "host-key"); err != nil {
		t.Fatal(err)
	}
	want := &HostMonitoring{
		ScraperIP: "10.0.0.4", ClientCAPEM: []byte("scraper-ca"),
		ServerCertPEM: []byte("cert:runtime-metrics.internal.cellestial.ru"),
		ServerKeyPEM:  []byte("key:runtime-metrics.internal.cellestial.ru"),
	}
	if !reflect.DeepEqual(host.config.Monitoring, want) || string(host.config.ServerKeyPEM) != "key:"+AgentDNS(id) {
		t.Fatal("expected separate TLS pairs for gRPC and metrics")
	}
}
