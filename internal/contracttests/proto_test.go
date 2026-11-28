// Package contracttests проверяет согласованность публичных и внутренних контрактов сервисов.
package contracttests

import (
	"strings"
	"testing"

	agentv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/agent/v1"
	environmentsv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/environments/v1"
	filesv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/files/v1"
	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	workspacev1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/workspace/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestProtoMethodInventory закрепляет все межсервисные usecase и версионированные namespaces.
func TestProtoMethodInventory(t *testing.T) {
	for _, contract := range []struct {
		file     protoreflect.FileDescriptor
		services map[string]string
	}{
		{identityv1.File_cellestial_identity_v1_identity_proto, map[string]string{"Session": "ValidateSession", "Users": "BatchGetUsers BatchResolveUsers", "UserAdministration": "RequestUserDeletion GetUserDeletion"}},
		{workspacev1.File_cellestial_workspace_v1_cleanup_proto, map[string]string{"UserCleanup": "BeginUserCleanup GetUserCleanup"}},
		{environmentsv1.File_cellestial_environments_v1_environments_proto, map[string]string{"Control": "RequestCreate GetEnvironment RequestDelete GetOperation", "SessionAgent": "Health Execute GetExecution ReadEvents InspectVariables Activity Drain Stop Interrupt Restart StopExecutions"}},
		{agentv1.File_cellestial_agent_v1_agent_proto, map[string]string{"Control": "Assign Health Execute GetExecution ReadEvents InspectVariables Activity Drain Stop Interrupt Restart StopExecutions"}},
		{filesv1.File_cellestial_files_v1_files_proto, map[string]string{"Files": "Check OpenSocket CloseSocket Command Subscribe Upload Download"}},
	} {
		if !strings.HasPrefix(string(contract.file.Package()), "cellestial.") {
			t.Errorf("incorrect namespace: %s", contract.file.Package())
		}
		for serviceName, required := range contract.services {
			svc := contract.file.Services().ByName(protoreflect.Name(serviceName))
			if svc == nil {
				t.Errorf("missing service %s", serviceName)
				continue
			}
			for _, method := range strings.Fields(required) {
				if svc.Methods().ByName(protoreflect.Name(method)) == nil {
					t.Errorf("missing %s.%s", serviceName, method)
				}
			}
		}
	}
}

// TestProtoStreamingAndPresence запрещает перевод больших объектов в безлимитный unary.
func TestProtoStreamingAndPresence(t *testing.T) {
	files := filesv1.File_cellestial_files_v1_files_proto.Services().ByName("Files")
	upload := files.Methods().ByName("Upload")
	if !upload.IsStreamingClient() || !upload.IsStreamingServer() {
		t.Fatal("Upload must be bidirectional")
	}
	for _, name := range []protoreflect.Name{"Download", "Subscribe"} {
		if !files.Methods().ByName(name).IsStreamingServer() {
			t.Fatalf("%s must stream", name)
		}
	}
	for _, svc := range []protoreflect.ServiceDescriptor{agentv1.File_cellestial_agent_v1_agent_proto.Services().ByName("Control"), environmentsv1.File_cellestial_environments_v1_environments_proto.Services().ByName("SessionAgent")} {
		for _, name := range []protoreflect.Name{"GetExecution", "ReadEvents", "Execute", "InspectVariables"} {
			if !svc.Methods().ByName(name).IsStreamingServer() {
				t.Errorf("%s.%s must stream", svc.Name(), name)
			}
		}
	}
	if !(&filesv1.UploadBegin{}).ProtoReflect().Descriptor().Fields().ByName("sha256").HasPresence() {
		t.Fatal("missing sha256 presence")
	}
	if (&filesv1.UploadChunk{}).ProtoReflect().Descriptor().Fields().ByName("offset").Kind() != protoreflect.Uint64Kind {
		t.Fatal("upload offset must be uint64")
	}
}

// TestProtoSafeBoundary не позволяет вернуть Workspace адрес или credentials агента.
func TestProtoSafeBoundary(t *testing.T) {
	fields := (&environmentsv1.Environment{}).ProtoReflect().Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		name := string(fields.Get(i).Name())
		for _, secret := range []string{"endpoint", "token", "private_ip", "ssh", "credential"} {
			if strings.Contains(name, secret) {
				t.Fatalf("unsafe environment field %s", name)
			}
		}
	}
	if fields.ByName("cleanup_confirmed") == nil {
		t.Fatal("cleanup acknowledgement must be explicit")
	}
	userFields := (&identityv1.User{}).ProtoReflect().Descriptor().Fields()
	if userFields.Len() != 3 || userFields.ByName("id") == nil || userFields.ByName("login") == nil ||
		userFields.ByName("avatar_url") == nil || userFields.ByName("avatar_url").Kind() != protoreflect.StringKind {
		t.Fatal("user summary must only contain id, login, and a string avatar_url")
	}
}
