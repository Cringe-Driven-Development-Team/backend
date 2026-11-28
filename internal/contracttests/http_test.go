package contracttests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// contractOps читает опубликованный JSON, не доверяя сгенерированному interface inventory.
func contractOps(t *testing.T, name string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "spec", name))
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err = json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	result := map[string]bool{}
	for _, path := range spec.Paths {
		for _, raw := range path {
			var op struct {
				ID string `json:"operationId"`
			}
			if json.Unmarshal(raw, &op) == nil && op.ID != "" {
				if result[op.ID] {
					t.Fatalf("duplicate operation ID %s", op.ID)
				}
				result[op.ID] = true
			}
		}
	}
	return result
}

// TestHTTPOperationOwnership гарантирует, что разделение не потеряло notebook/comments/runtime операции.
func TestHTTPOperationOwnership(t *testing.T) {
	identity := contractOps(t, "identity/openapi.json")
	workspace := contractOps(t, "workspace/openapi.json")
	legacy := contractOps(t, "openapi.json")
	for _, browser := range []string{"openWorkspaceControl", "openWorkspaceUpload", "downloadWorkspaceFile", "syncWorkspaceSessionCookie"} {
		delete(legacy, browser)
	}
	for _, op := range []string{"checkWorkspace", "workspaceCommand", "pollWorkspaceEvents", "closeWorkspaceSocket", "uploadWorkspaceChunk", "downloadWorkspaceTransfer", "exportNotebook"} {
		if !workspace[op] {
			t.Errorf("missing implemented file relay %s", op)
		}
		delete(workspace, op)
	}
	for id := range identity {
		if workspace[id] {
			t.Errorf("operation owned by two services: %s", id)
		}
		if !legacy[id] {
			t.Errorf("unknown Identity operation: %s", id)
		}
		delete(legacy, id)
	}
	for id := range workspace {
		if !legacy[id] {
			t.Errorf("unknown Workspace operation: %s", id)
		}
		delete(legacy, id)
	}
	for _, id := range []string{
		"onlineConfListKeys", "onlineConfCreateKey", "onlineConfGetKey",
		"onlineConfUpdateKey", "onlineConfDeleteKey", "onlineConfResolveUser",
	} {
		if !legacy[id] {
			t.Errorf("missing or multiply assigned OnlineConf operation: %s", id)
		}
		delete(legacy, id)
	}
	if len(legacy) != 0 {
		t.Fatalf("unassigned HTTP operations: %v", legacy)
	}
}
