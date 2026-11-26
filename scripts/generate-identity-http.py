#!/usr/bin/env python3
"""Соберите отдельный HTTP-контракт Identity и адаптер к существующим handlers."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

sys.dont_write_bytecode = True

from workspace_http_contract import relay_paths

ROOT = Path(__file__).resolve().parent.parent
MODULE = "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development"


def generate(check=False, service="identity"):
    """Сохраните wire-схемы и сгенерируйте bindings без копии бизнес-логики HTTP."""
    os.chdir(ROOT)
    source = json.loads(Path("spec/openapi.json").read_text())
    service_title = service.capitalize()
    api_package = f"{service}api"
    source["info"]["title"] = f"Cellestial {service_title} API"
    identity_paths = ("/auth/", "/users/")
    source["paths"] = {
        p: v for p, v in source["paths"].items()
        if p.startswith(identity_paths) == (service == "identity")
        and (service == "identity" or "/files/" not in p)
    }
    source["components"]["schemas"]["Error"]["properties"]["request_id"] = {"type": "string", "maxLength": 64}
    source["components"]["schemas"]["Error"]["properties"]["code"]["enum"].extend(
        ["dependency_unavailable", "deadline_exceeded"]
    )
    relay = {}
    if service == "workspace":
        relay = relay_paths(source)
        source["paths"].update(relay)
        source["paths"]["/notebooks/import"]["post"]["responses"]["429"] = {
            "description": "Достигнут лимит одновременных передач",
            "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Error"}}},
        }
        source["info"]["description"] = (
            "Внутренний HTTP API Workspace под префиксом /api/v1. Все операции требуют mTLS "
            "с идентичностью BFF и X-BFF-Key. Пользовательская сессия проверяется через Identity."
        )
        schemas = source["components"]["schemas"]
        schemas["Error"]["properties"]["code"]["enum"].extend(["conflict", "runtime_not_ready"])
        runtime_error = schemas["RuntimeError"]["properties"]
        runtime_error["request_id"] = {"type": "string", "maxLength": 64}
        runtime_error["code"]["enum"].append("dependency_unavailable")
        schemas["RuntimeSession"]["properties"]["state"]["description"] = (
            "Состояние пользовательской runtime-сессии. Терминальное состояние освобождает слот "
            "после подтверждённой очистки; при cleanup_pending слот остаётся занят."
        )
        for path, entry in source["paths"].items():
            if path in relay:
                continue
            for operation in entry.values():
                if not isinstance(operation, dict) or "operationId" not in operation:
                    continue
                if "description" in operation:
                    desc = operation["description"]
                    operation["description"] = desc.replace("Bearer JWT → Redis ValidateAccess", "Bearer → Identity ValidateSession")
                for status, desc in (("500", "Внутренняя ошибка сервиса"), ("503", "Identity или другая зависимость недоступны"), ("504", "Истёк срок ожидания зависимости")):
                    operation["responses"].setdefault(status, {
                        "description": desc, "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Error"}}}
                    })
                if path.endswith(("/kernel/interrupt", "/kernel/restart")):
                    operation["responses"]["429"] = {
                        "description": "Достигнут лимит runtime",
                        "content": {"application/json": {"schema": {"$ref": "#/components/schemas/RuntimeError"}}},
                    }
    components = source["components"]
    seen = set()

    def visit(value):
        if isinstance(value, dict):
            for requirement in value.get("security", []):
                for name in requirement:
                    seen.add("#/components/securitySchemes/" + name)
            ref = value.get("$ref", "")
            if ref.startswith("#/components/") and ref not in seen:
                seen.add(ref)
                _, _, group, name = ref.split("/")
                visit(components[group][name])
            for child in value.values():
                visit(child)
        elif isinstance(value, list):
            for child in value:
                visit(child)

    visit(source["paths"])
    visit({"security": source.get("security", [])})
    source["components"] = {group: {name: value for name, value in items.items() if f"#/components/{group}/{name}" in seen} for group, items in components.items()}
    source["tags"] = [tag for tag in source.get("tags", []) if tag["name"] in {t for path in source["paths"].values() for op in path.values() if isinstance(op, dict) for t in op.get("tags", [])}]
    contract = Path(f"spec/{service}/openapi.json")
    contract.parent.mkdir(parents=True, exist_ok=True)
    contract_text = json.dumps(source, ensure_ascii=False, indent=2) + "\n"
    if check:
        if not contract.exists() or contract.read_text() != contract_text:
            raise SystemExit(f"{service_title} OpenAPI is outdated; run make {service}-http")
    else:
        contract.write_text(contract_text)
    # Публичный контракт самодостаточен. Go-модели пока общие с действующим HTTP API;
    # aliases позволяют переиспользовать handlers, не копируя их условия/ошибки.
    for path in relay:
        del source["paths"][path]
    for name, schema in source["components"]["schemas"].items():
        if name.startswith("WorkspaceFile"):
            continue
        schema["x-go-type"] = "legacyapi." + name
        schema["x-go-type-import"] = {"path": MODULE + "/internal/transport/http/api", "name": "legacyapi"}
    with tempfile.TemporaryDirectory(prefix=f"{service}-http-") as tmp:
        spec = Path(tmp) / "openapi.json"
        spec.write_text(json.dumps(source))
        config = Path(tmp) / "cfg.yaml"
        generated_path = Path(tmp) / "api.gen.go"
        config.write_text(f"package: {api_package}\noutput: {generated_path}\ngenerate:\n  gorilla-server: true\n  strict-server: true\n  models: true\n  embedded-spec: true\n")
        Path(f"internal/transport/http/{api_package}").mkdir(parents=True, exist_ok=True)
        subprocess.run([os.environ.get("GO", "go"), "tool", "oapi-codegen", "-config", str(config), str(spec)], check=True)
        generated = generated_path.read_text()
        destination = Path(f"internal/transport/http/{api_package}/api.gen.go")
        if check:
            if not destination.exists() or destination.read_text() != generated:
                raise SystemExit(f"{service_title} Go bindings are outdated; run make {service}-http")
        else:
            destination.write_text(generated)
    interface = generated.split("type StrictServerInterface interface {", 1)[1].split("\n}", 1)[0]
    methods = re.findall(r"(\w+)\(ctx context.Context, request (\w+)\) \((\w+), error\)", interface)
    if service == "workspace":
        write_workspace_adapter(interface, generated, check)
        return
    adapter = ['// Code generated by generate-identity-http.py. DO NOT EDIT.\npackage delivery\n', 'import ("context"; legacyapi "' + MODULE + '/internal/transport/http/api"; identityapi "' + MODULE + '/internal/transport/http/identityapi")\n', 'type identityAdapter struct { handler *Handler }\n']
    for method, request, response in methods:
        fields = re.search(r'type ' + request + r' struct \{(.*?)\n\}', generated, re.S).group(1)
        args = []
        for name, typ in re.findall(r'^\s*(\w+)\s+([^\s]+)', fields, re.M):
            value = f'legacyapi.{typ}(req.{name})' if name == 'Params' else f'req.{name}'
            args.append(f'{name}: {value}')
        adapter.append(f'func (h identityAdapter) {method}(ctx context.Context, req identityapi.{request}) (identityapi.{response}, error) {{ return h.handler.{method}(ctx, legacyapi.{request}{{' + ', '.join(args) + '}) }\n')
    output = Path("internal/user/delivery/http/identity_adapter.gen.go")
    formatted = subprocess.run(["gofmt"], input="\n".join(adapter), text=True, capture_output=True, check=True).stdout
    if check:
        if not output.exists() or output.read_text() != formatted:
            raise SystemExit("Identity HTTP adapter is outdated; run make identity-http")
    else:
        output.write_text(formatted)


def write_workspace_adapter(interface, generated, check):
    """Передайте вызовы Workspace существующим handlers через общий набор DTO."""
    methods = re.findall(r"(\w+)\(ctx context.Context, request (\w+)\) \((\w+), error\)", interface)
    adapter = [
        "// Code generated by generate-identity-http.py. DO NOT EDIT.\npackage httptransport",
        f'import ("context"; "{MODULE}/internal/transport/http/api"; "{MODULE}/internal/transport/http/workspaceapi")',
        "type workspaceAdapter struct { server WorkspaceHandlers }",
    ]
    for method, request, response in methods:
        fields = re.search(r'type ' + request + r' struct \{(.*?)\n\}', generated, re.S).group(1)
        args = []
        for name, typ in re.findall(r'^\s*(\w+)\s+([^\s]+)', fields, re.M):
            value = f'req.{name}'
            if name == 'Params':
                value = f'api.{typ}(req.{name})'
            elif name == 'Body' and typ.startswith('*') and '.' not in typ:
                value = f'(*api.{typ[1:]})(req.{name})'
            args.append(f'{name}: {value}')
        adapter.append(
            f'func (h workspaceAdapter) {method}(ctx context.Context, req workspaceapi.{request}) '
            f'(workspaceapi.{response}, error) {{ return h.server.{method}(ctx, api.{request}{{'
            + ', '.join(args) + '}) }'
        )
    output = Path("internal/transport/http/workspace_adapter.gen.go")
    formatted = subprocess.run(["gofmt"], input="\n".join(adapter), text=True, capture_output=True, check=True).stdout
    if check:
        if not output.exists() or output.read_text() != formatted:
            raise SystemExit("Workspace HTTP adapter is outdated; run make workspace-http")
    else:
        output.write_text(formatted)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    parser.add_argument("--service", choices=("identity", "workspace"), default="identity")
    args = parser.parse_args()
    generate(check=args.check, service=args.service)
