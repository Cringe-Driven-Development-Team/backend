#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

# PROTO_PYTHON и GO позволяют использовать локальные окружения без глобальной установки.
proto_python=${PROTO_PYTHON:-python3}
go_command=${GO:-go}
tool_dir=$(mktemp -d)
trap 'rm -rf "$tool_dir"' EXIT

"$proto_python" - <<'PY'
import importlib.metadata
for package, version in (("grpcio-tools", "1.78.0"), ("grpcio", "1.78.0"), ("protobuf", "6.33.6")):
    if importlib.metadata.version(package) != version:
        raise SystemExit("Install tools/proto/requirements.txt into the generator environment")
PY
GOBIN="$tool_dir" "$go_command" install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
GOBIN="$tool_dir" "$go_command" install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.1
proto_include=$("$proto_python" -c 'from pathlib import Path; import grpc_tools; print(Path(grpc_tools.__file__).parent / "_proto")')
output_dir=${PROTO_OUTPUT_DIR:-.}
mkdir -p "$output_dir"
module=github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development
"$proto_python" -m grpc_tools.protoc -I proto -I "$proto_include" \
    --plugin="protoc-gen-go=$tool_dir/protoc-gen-go" \
    --plugin="protoc-gen-go-grpc=$tool_dir/protoc-gen-go-grpc" \
    --go_out="$output_dir" --go_opt="module=$module" \
    --go-grpc_out="$output_dir" --go-grpc_opt="module=$module" \
    proto/cellestial/identity/v1/identity.proto
