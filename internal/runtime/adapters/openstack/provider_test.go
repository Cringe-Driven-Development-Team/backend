package openstack

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/config"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/usecase"
	"github.com/google/uuid"
	th "github.com/gophercloud/gophercloud/v2/testhelper"
	fakeclient "github.com/gophercloud/gophercloud/v2/testhelper/client"
)

func sessionID() uuid.UUID   { return uuid.MustParse("11111111-1111-1111-1111-111111111111") }
func operationID() uuid.UUID { return uuid.MustParse("22222222-2222-2222-2222-222222222222") }

func testConfig() config.RuntimeConfig {
	return config.RuntimeConfig{
		AvailabilityZone: "ru-7a",
		NetworkID:        "net-1",
		SubnetID:         "subnet-1",
		SecurityGroupID:  "sg-1",
		ImageID:          "img-1",
		FlavorID:         "flavor-1",
		VolumeType:       "fast.ru-7a",
		SSHKeyName:       "provisioner",
		RootDiskGiB:      20,
		EnvName:          "production",
	}
}

func setup(t *testing.T) (th.FakeServer, *Provider) {
	t.Helper()
	srv := th.SetupHTTP()
	t.Cleanup(srv.Teardown)
	c := fakeclient.ServiceClient(srv)
	p := newProvider(c, c, c, testConfig())
	p.poll = time.Millisecond
	return srv, p
}

func testEnv() domain.Environment {
	return domain.Environment{SessionID: sessionID(), OperationID: operationID()}
}

func ownedMeta(env string, session uuid.UUID) map[string]string {
	return map[string]string{
		"cellestial_runtime": "1",
		"cellestial_env":     env,
		"session_id":         session.String(),
		"operation_id":       operationID().String(),
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Error(err)
	}
}

func decodeBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return body
}

func TestEnsureVolumeCreatesWithMetadata(t *testing.T) {
	srv, p := setup(t)
	srv.Mux.HandleFunc("GET /volumes/detail", func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("metadata"); !strings.Contains(q, sessionID().String()) || !strings.Contains(q, "production") {
			t.Errorf("list metadata filter = %q", q)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"volumes": []any{}})
	})
	srv.Mux.HandleFunc("POST /volumes", func(w http.ResponseWriter, r *http.Request) {
		th.TestJSONRequest(t, r, fmt.Sprintf(`{"volume":{
			"name":"runtime-%s","size":20,"imageRef":"img-1","volume_type":"fast.ru-7a","availability_zone":"ru-7a",
			"metadata":{"cellestial_runtime":"1","cellestial_env":"production","session_id":"%s","operation_id":"%s"}}}`,
			sessionID(), sessionID(), operationID()))
		writeJSON(t, w, http.StatusAccepted, map[string]any{"volume": map[string]any{"id": "vol-1", "status": "creating"}})
	})
	var gets atomic.Int32
	srv.Mux.HandleFunc("GET /volumes/vol-1", func(w http.ResponseWriter, r *http.Request) {
		status := "creating"
		if gets.Add(1) > 1 {
			status = "available"
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"volume": map[string]any{"id": "vol-1", "status": status}})
	})

	id, err := p.EnsureVolume(context.Background(), testEnv())
	if err != nil || id != "vol-1" {
		t.Fatalf("EnsureVolume = %q, %v", id, err)
	}
	if gets.Load() < 2 {
		t.Fatalf("volume was not awaited: %d gets", gets.Load())
	}
}

func TestEnsureVolumeFindsExistingByMetadata(t *testing.T) {
	srv, p := setup(t)
	srv.Mux.HandleFunc("GET /volumes/detail", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"volumes": []any{
			map[string]any{"id": "vol-7", "status": "available", "metadata": ownedMeta("production", sessionID())},
		}})
	})
	srv.Mux.HandleFunc("GET /volumes/vol-7", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"volume": map[string]any{"id": "vol-7", "status": "available"}})
	})
	srv.Mux.HandleFunc("POST /volumes", func(w http.ResponseWriter, r *http.Request) {
		t.Error("volume must not be created twice")
		w.WriteHeader(http.StatusInternalServerError)
	})

	id, err := p.EnsureVolume(context.Background(), testEnv())
	if err != nil || id != "vol-7" {
		t.Fatalf("EnsureVolume = %q, %v", id, err)
	}
}

func TestEnsureVolumeReusesKnownID(t *testing.T) {
	srv, p := setup(t)
	srv.Mux.HandleFunc("GET /volumes/vol-3", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"volume": map[string]any{"id": "vol-3", "status": "available"}})
	})
	env := testEnv()
	env.Resources.VolumeID = "vol-3"
	id, err := p.EnsureVolume(context.Background(), env)
	if err != nil || id != "vol-3" {
		t.Fatalf("EnsureVolume = %q, %v", id, err)
	}
}

func TestEnsureServerBootFromVolume(t *testing.T) {
	srv, p := setup(t)
	const userData = "#cloud-config\nssh_deletekeys: true\n"
	srv.Mux.HandleFunc("GET /servers/detail", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("name"); got != "^runtime-"+sessionID().String()+"$" {
			t.Errorf("name filter = %q", got)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"servers": []any{}})
	})
	srv.Mux.HandleFunc("POST /servers", func(w http.ResponseWriter, r *http.Request) {
		body := decodeBody(t, r)["server"].(map[string]any)
		bdm := body["block_device_mapping_v2"].([]any)[0].(map[string]any)
		checks := []struct {
			name      string
			got, want any
		}{
			{"name", body["name"], "runtime-" + sessionID().String()},
			{"flavorRef", body["flavorRef"], "flavor-1"},
			{"availability_zone", body["availability_zone"], "ru-7a"},
			{"key_name", body["key_name"], "provisioner"},
			{"user_data", body["user_data"], base64.StdEncoding.EncodeToString([]byte(userData))},
			{"port", body["networks"].([]any)[0].(map[string]any)["port"], "port-1"},
			{"bdm.uuid", bdm["uuid"], "vol-1"},
			{"bdm.boot_index", bdm["boot_index"], float64(0)},
			{"bdm.delete_on_termination", bdm["delete_on_termination"], true},
			{"bdm.source_type", bdm["source_type"], "volume"},
			{"bdm.destination_type", bdm["destination_type"], "volume"},
			{"metadata.session_id", body["metadata"].(map[string]any)["session_id"], sessionID().String()},
			{"metadata.operation_id", body["metadata"].(map[string]any)["operation_id"], operationID().String()},
			{"metadata.cellestial_env", body["metadata"].(map[string]any)["cellestial_env"], "production"},
			{"metadata.cellestial_runtime", body["metadata"].(map[string]any)["cellestial_runtime"], "1"},
		}
		for _, c := range checks {
			if c.got != c.want {
				t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
			}
		}
		writeJSON(t, w, http.StatusAccepted, map[string]any{"server": map[string]any{"id": "srv-1"}})
	})
	var gets atomic.Int32
	srv.Mux.HandleFunc("GET /servers/srv-1", func(w http.ResponseWriter, r *http.Request) {
		status := "BUILD"
		if gets.Add(1) > 1 {
			status = "ACTIVE"
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"server": map[string]any{"id": "srv-1", "status": status}})
	})

	env := testEnv()
	env.Resources = domain.Resources{VolumeID: "vol-1", PortID: "port-1", PrivateIP: "10.0.0.5"}
	id, err := p.EnsureServer(context.Background(), env, userData)
	if err != nil || id != "srv-1" {
		t.Fatalf("EnsureServer = %q, %v", id, err)
	}
	if gets.Load() < 2 {
		t.Fatalf("server was not awaited: %d gets", gets.Load())
	}
}

func TestEnsureServerErrorStatusIsPermanent(t *testing.T) {
	srv, p := setup(t)
	srv.Mux.HandleFunc("GET /servers/srv-9", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"server": map[string]any{
			"id": "srv-9", "status": "ERROR", "fault": map[string]any{"message": "No valid host was found"},
		}})
	})
	env := testEnv()
	env.Resources = domain.Resources{ServerID: "srv-9", VolumeID: "vol-1", PortID: "port-1"}
	id, err := p.EnsureServer(context.Background(), env, "x")
	if !errors.Is(err, usecase.ErrCloudPermanent) || id != "srv-9" {
		t.Fatalf("EnsureServer = %q, %v; want srv-9 and ErrCloudPermanent", id, err)
	}
}

func TestEnsurePortTags(t *testing.T) {
	srv, p := setup(t)
	srv.Mux.HandleFunc("GET /ports", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"ports": []any{}})
	})
	srv.Mux.HandleFunc("POST /ports", func(w http.ResponseWriter, r *http.Request) {
		body := decodeBody(t, r)["port"].(map[string]any)
		if body["network_id"] != "net-1" || body["name"] != "runtime-"+sessionID().String() {
			t.Errorf("port body = %v", body)
		}
		if sg := body["security_groups"].([]any); len(sg) != 1 || sg[0] != "sg-1" {
			t.Errorf("security_groups = %v", sg)
		}
		if ip := body["fixed_ips"].([]any)[0].(map[string]any); ip["subnet_id"] != "subnet-1" {
			t.Errorf("fixed_ips = %v", ip)
		}
		desc, _ := body["description"].(string)
		if !strings.Contains(desc, "operation_id="+operationID().String()) || !strings.Contains(desc, "session_id="+sessionID().String()) {
			t.Errorf("description = %q", desc)
		}
		writeJSON(t, w, http.StatusCreated, map[string]any{"port": map[string]any{
			"id": "port-1", "fixed_ips": []any{map[string]any{"subnet_id": "subnet-1", "ip_address": "10.0.0.5"}},
		}})
	})
	var tagged atomic.Bool
	srv.Mux.HandleFunc("PUT /ports/port-1/tags", func(w http.ResponseWriter, r *http.Request) {
		th.TestJSONRequest(t, r, fmt.Sprintf(`{"tags":["cellestial_runtime=1","cellestial_env=production","session_id=%s"]}`, sessionID()))
		tagged.Store(true)
		writeJSON(t, w, http.StatusOK, map[string]any{"tags": []string{}})
	})

	id, ip, err := p.EnsurePort(context.Background(), testEnv())
	if err != nil || id != "port-1" || ip != "10.0.0.5" {
		t.Fatalf("EnsurePort = %q, %q, %v", id, ip, err)
	}
	if !tagged.Load() {
		t.Fatal("port tags were not set")
	}
}

func TestEnsurePortFindsByTags(t *testing.T) {
	srv, p := setup(t)
	srv.Mux.HandleFunc("GET /ports", func(w http.ResponseWriter, r *http.Request) {
		want := fmt.Sprintf("cellestial_runtime=1,cellestial_env=production,session_id=%s", sessionID())
		if got := r.URL.Query().Get("tags"); got != want {
			t.Errorf("tags filter = %q", got)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"ports": []any{map[string]any{
			"id": "port-4", "fixed_ips": []any{map[string]any{"subnet_id": "subnet-1", "ip_address": "10.0.0.9"}},
			"tags": []string{"cellestial_runtime=1", "cellestial_env=production", "session_id=" + sessionID().String()},
		}}})
	})
	srv.Mux.HandleFunc("POST /ports", func(w http.ResponseWriter, r *http.Request) {
		t.Error("port must not be created twice")
		w.WriteHeader(http.StatusInternalServerError)
	})
	id, ip, err := p.EnsurePort(context.Background(), testEnv())
	if err != nil || id != "port-4" || ip != "10.0.0.9" {
		t.Fatalf("EnsurePort = %q, %q, %v", id, ip, err)
	}
}

func TestDeleteServer404IsSuccess(t *testing.T) {
	srv, p := setup(t)
	srv.Mux.HandleFunc("DELETE /servers/srv-1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if err := p.DeleteServer(context.Background(), "srv-1"); err != nil {
		t.Fatalf("DeleteServer = %v", err)
	}
}

func TestDeleteWaitsUntilGone(t *testing.T) {
	srv, p := setup(t)
	srv.Mux.HandleFunc("DELETE /servers/srv-1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	var gets atomic.Int32
	srv.Mux.HandleFunc("GET /servers/srv-1", func(w http.ResponseWriter, r *http.Request) {
		if gets.Add(1) < 3 {
			writeJSON(t, w, http.StatusOK, map[string]any{"server": map[string]any{"id": "srv-1", "status": "ACTIVE"}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	if err := p.DeleteServer(context.Background(), "srv-1"); err != nil {
		t.Fatalf("DeleteServer = %v", err)
	}
	if gets.Load() != 3 {
		t.Fatalf("gets = %d, want 3", gets.Load())
	}

	// Без исчезновения ресурса удаление завершается по дедлайну контекста.
	gets.Store(-1000)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := p.DeleteServer(ctx, "srv-1"); err == nil {
		t.Fatal("DeleteServer must fail when the server does not disappear before the deadline")
	}
}

func TestQuotaErrorIsPermanent(t *testing.T) {
	srv, p := setup(t)
	srv.Mux.HandleFunc("GET /volumes/detail", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"volumes": []any{}})
	})
	srv.Mux.HandleFunc("POST /volumes", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusRequestEntityTooLarge, map[string]any{"overLimit": map[string]any{
			"code": 413, "message": "VolumeLimitExceeded: Maximum number of volumes allowed (10) exceeded for quota 'volumes'.",
		}})
	})
	_, err := p.EnsureVolume(context.Background(), testEnv())
	if !errors.Is(err, usecase.ErrCloudPermanent) {
		t.Fatalf("err = %v, want ErrCloudPermanent", err)
	}

	srv.Mux.HandleFunc("DELETE /ports/port-1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusForbidden, map[string]any{"NeutronError": map[string]any{"message": "forbidden"}})
	})
	if err := p.DeletePort(context.Background(), "port-1"); !errors.Is(err, usecase.ErrCloudPermanent) {
		t.Fatalf("403 err = %v, want ErrCloudPermanent", err)
	}
}

func TestServerErrorIsTransient(t *testing.T) {
	srv, p := setup(t)
	srv.Mux.HandleFunc("GET /volumes/detail", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"volumes": []any{}})
	})
	srv.Mux.HandleFunc("POST /volumes", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	_, err := p.EnsureVolume(context.Background(), testEnv())
	if !errors.Is(err, usecase.ErrCloudTransient) {
		t.Fatalf("err = %v, want ErrCloudTransient", err)
	}

	srv.Mux.HandleFunc("DELETE /volumes/vol-1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	if err := p.DeleteVolume(context.Background(), "vol-1"); !errors.Is(err, usecase.ErrCloudTransient) {
		t.Fatalf("429 err = %v, want ErrCloudTransient", err)
	}

	srv.Teardown()
	if _, _, err := p.EnsurePort(context.Background(), testEnv()); !errors.Is(err, usecase.ErrCloudTransient) {
		t.Fatalf("connection err = %v, want ErrCloudTransient", err)
	}
}

func TestListOwnedFiltersEnv(t *testing.T) {
	srv, p := setup(t)
	other := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	srv.Mux.HandleFunc("GET /servers/detail", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"servers": []any{
			map[string]any{"id": "srv-prod", "metadata": ownedMeta("production", sessionID())},
			map[string]any{"id": "srv-dev", "metadata": ownedMeta("dev", other)},
			map[string]any{"id": "srv-foreign", "metadata": map[string]string{"session_id": other.String()}},
		}})
	})
	srv.Mux.HandleFunc("GET /volumes/detail", func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("metadata"); !strings.Contains(q, "production") {
			t.Errorf("volume metadata filter = %q", q)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"volumes": []any{
			map[string]any{"id": "vol-prod", "metadata": ownedMeta("production", sessionID())},
			map[string]any{"id": "vol-dev", "metadata": ownedMeta("dev", other)},
		}})
	})
	srv.Mux.HandleFunc("GET /ports", func(w http.ResponseWriter, r *http.Request) {
		// Второй запрос — все порты сети рантайма: те же порты должны схлопнуться без дублей.
		if r.URL.Query().Get("network_id") == "" {
			if got := r.URL.Query().Get("tags"); got != "cellestial_runtime=1,cellestial_env=production" {
				t.Errorf("port tags filter = %q", got)
			}
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"ports": []any{
			map[string]any{"id": "port-prod", "tags": []string{"cellestial_runtime=1", "cellestial_env=production", "session_id=" + sessionID().String()}},
			map[string]any{"id": "port-dev", "tags": []string{"cellestial_runtime=1", "cellestial_env=dev", "session_id=" + other.String()}},
		}})
	})

	got, err := p.ListOwned(context.Background())
	if err != nil {
		t.Fatalf("ListOwned: %v", err)
	}
	want := map[string]bool{"server/srv-prod": true, "volume/vol-prod": true, "port/port-prod": true}
	if len(got) != len(want) {
		t.Fatalf("ListOwned = %+v", got)
	}
	for _, r := range got {
		if !want[r.Kind+"/"+r.ID] || r.SessionID != sessionID() {
			t.Fatalf("unexpected resource %+v", r)
		}
	}
}

func TestNewDoesNotLeakPassword(t *testing.T) {
	srv := th.SetupHTTP()
	defer srv.Teardown()
	srv.Mux.HandleFunc("POST /v3/auth/tokens", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		writeJSON(t, w, http.StatusUnauthorized, map[string]any{"error": map[string]any{"message": "The request you have made requires authentication."}})
	})
	cfg := testConfig()
	cfg.OSAuthURL = srv.Endpoint() + "v3/"
	cfg.OSUsername, cfg.OSPassword, cfg.OSUserDomainName, cfg.OSProjectID, cfg.OSRegion = "svc", "s3cr3t-pa55", "123", "proj", "ru-7"
	_, err := New(context.Background(), cfg)
	if err == nil {
		t.Fatal("New must fail on 401")
	}
	if strings.Contains(err.Error(), "s3cr3t-pa55") {
		t.Fatalf("error leaks password: %v", err)
	}
	if !errors.Is(err, usecase.ErrCloudPermanent) {
		t.Fatalf("err = %v, want ErrCloudPermanent", err)
	}
}

func TestListOwnedFindsUntaggedPortByDescription(t *testing.T) {
	srv, p := setup(t)
	other := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	srv.Mux.HandleFunc("GET /servers/detail", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"servers": []any{}})
	})
	srv.Mux.HandleFunc("GET /volumes/detail", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"volumes": []any{}})
	})
	srv.Mux.HandleFunc("GET /ports", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("network_id") != "net-1" {
			writeJSON(t, w, http.StatusOK, map[string]any{"ports": []any{}})
			return
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"ports": []any{
			map[string]any{"id": "port-untagged", "description": fmt.Sprintf(
				"cellestial_runtime=1 cellestial_env=production session_id=%s operation_id=%s", sessionID(), operationID())},
			map[string]any{"id": "port-dev", "description": fmt.Sprintf(
				"cellestial_runtime=1 cellestial_env=dev session_id=%s operation_id=%s", other, operationID())},
			map[string]any{"id": "port-foreign", "description": "router interface"},
		}})
	})

	got, err := p.ListOwned(context.Background())
	if err != nil {
		t.Fatalf("ListOwned: %v", err)
	}
	if len(got) != 1 || got[0].Kind != "port" || got[0].ID != "port-untagged" || got[0].SessionID != sessionID() {
		t.Fatalf("ListOwned = %+v", got)
	}
}

func TestEnsurePortByIDRetagsUntagged(t *testing.T) {
	srv, p := setup(t)
	srv.Mux.HandleFunc("GET /ports/port-1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"port": map[string]any{
			"id": "port-1", "tags": []string{"keep-me"},
			"fixed_ips": []any{map[string]any{"subnet_id": "subnet-1", "ip_address": "10.0.0.5"}},
		}})
	})
	var tagged atomic.Bool
	srv.Mux.HandleFunc("PUT /ports/port-1/tags", func(w http.ResponseWriter, r *http.Request) {
		th.TestJSONRequest(t, r, fmt.Sprintf(
			`{"tags":["keep-me","cellestial_runtime=1","cellestial_env=production","session_id=%s"]}`, sessionID()))
		tagged.Store(true)
		writeJSON(t, w, http.StatusOK, map[string]any{"tags": []string{}})
	})
	env := testEnv()
	env.Resources.PortID = "port-1"
	id, ip, err := p.EnsurePort(context.Background(), env)
	if err != nil || id != "port-1" || ip != "10.0.0.5" {
		t.Fatalf("EnsurePort = %q, %q, %v", id, ip, err)
	}
	if !tagged.Load() {
		t.Fatal("port found by ID must be retagged")
	}

	// Порт со всеми тегами повторно не помечается.
	tagged.Store(false)
	srv.Mux.HandleFunc("GET /ports/port-2", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"port": map[string]any{
			"id": "port-2", "tags": []string{"cellestial_runtime=1", "cellestial_env=production", "session_id=" + sessionID().String()},
		}})
	})
	srv.Mux.HandleFunc("PUT /ports/port-2/tags", func(w http.ResponseWriter, r *http.Request) {
		t.Error("fully tagged port must not be retagged")
		w.WriteHeader(http.StatusInternalServerError)
	})
	env.Resources.PortID = "port-2"
	if _, _, err := p.EnsurePort(context.Background(), env); err != nil {
		t.Fatalf("EnsurePort tagged = %v", err)
	}
}
