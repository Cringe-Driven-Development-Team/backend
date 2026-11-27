#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
proto_python=${PROTO_PYTHON:-python3}
go_command=${GO:-go}
tool_dir=$(mktemp -d)
trap 'rm -rf "$tool_dir"' EXIT
"$proto_python" tools/proto/verify.py "$go_command"
GOBIN="$tool_dir" "$go_command" install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
GOBIN="$tool_dir" "$go_command" install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.1
proto_include=$("$proto_python" -c 'from pathlib import Path; import grpc_tools; print(Path(grpc_tools.__file__).parent / "_proto")')
output_dir=${PROTO_OUTPUT_DIR:-.}
python_dir="$output_dir/runtime-agent/src/runtime_agent/generated"
mkdir -p "$output_dir" "$python_dir"
find proto -type f -name '*.proto' -print0 | LC_ALL=C sort -z > "$tool_dir/sources"
mapfile -d '' -t sources < "$tool_dir/sources"
if (( ${#sources[@]} == 0 )); then
    echo 'No .proto files found under proto/' >&2
    exit 1
fi
module=github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development
"$proto_python" -m grpc_tools.protoc -I proto -I "$proto_include" \
    --plugin="protoc-gen-go=$tool_dir/protoc-gen-go" \
    --plugin="protoc-gen-go-grpc=$tool_dir/protoc-gen-go-grpc" \
    --go_out="$output_dir" --go_opt="module=$module" \
    --go-grpc_out="$output_dir" --go-grpc_opt="module=$module" \
    --python_out="$python_dir" --grpc_python_out="$python_dir" --pyi_out="$python_dir" \
    "${sources[@]}"
# protoc импортирует абсолютный cellestial.*; generated живёт внутри runtime_agent.
"$proto_python" - "$python_dir" <<'PY'
import pathlib
import sys
root = pathlib.Path(sys.argv[1])
for path in sorted(p for p in root.rglob("*") if p.suffix in (".py", ".pyi") and "__pycache__" not in p.parts):
    text = path.read_text().replace("from cellestial.", "from runtime_agent.generated.cellestial.")
    path.write_text(text)
for directory in [root, *sorted(p for p in root.rglob("*") if p.is_dir() and "__pycache__" not in p.parts)]:
    (directory / "__init__.py").write_text('"""Сгенерированные protobuf-контракты Cellestial."""\n')
PY
