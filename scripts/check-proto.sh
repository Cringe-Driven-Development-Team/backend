#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT
PROTO_OUTPUT_DIR="$temporary" bash scripts/generate-proto.sh
diff -ru gen/cellestial "$temporary/gen/cellestial"
diff -ru --exclude=__pycache__ runtime-agent/src/runtime_agent/generated "$temporary/runtime-agent/src/runtime_agent/generated"
PYTHONPATH=runtime-agent/src "${PROTO_PYTHON:-python3}" - <<'PY'
from runtime_agent.generated.cellestial.agent.v1 import agent_pb2, agent_pb2_grpc
from runtime_agent.generated.cellestial.files.v1 import files_pb2, files_pb2_grpc
assert agent_pb2.DESCRIPTOR.package == "cellestial.agent.v1"
assert files_pb2.DESCRIPTOR.package == "cellestial.files.v1"
PY
