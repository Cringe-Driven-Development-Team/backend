"""Проверьте версии и checksum генераторов до записи generated artifacts."""

import hashlib
import importlib.metadata
import json
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
lock = json.loads((ROOT / "tools/proto/versions.lock").read_text())
for package, version in lock["python"].items():
    if importlib.metadata.version(package) != version:
        raise SystemExit(f"Unexpected generator version: {package}")
if hashlib.sha256((ROOT / "tools/proto/requirements.txt").read_bytes()).hexdigest() != lock[
    "python_lock_sha256"
]:
    raise SystemExit("Generator requirements checksum differs from versions.lock")
version = subprocess.check_output([sys.executable, "-m", "grpc_tools.protoc", "--version"], text=True)
if version.strip() != lock["protoc"]:
    raise SystemExit("Unexpected protoc version")
for module in lock["go_generators"]:
    actual = json.loads(
        subprocess.check_output(
            [sys.argv[1], "mod", "download", "-json", module["Path"] + "@" + module["Version"]],
            text=True,
        )
    )
    if any(actual[key] != module[key] for key in ("Path", "Version", "Sum", "GoModSum")):
        raise SystemExit("Go generator checksum differs from versions.lock")
