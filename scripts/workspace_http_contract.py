"""Дополнения к HTTP-контракту Workspace для внутренних файловых маршрутов."""

import json
from pathlib import Path


def schema_ref(name):
    return {"$ref": f"#/components/schemas/{name}"}


def response(description, schema=None, media="application/json", headers=()):
    value = {"description": description}
    if schema is not None:
        value["content"] = {media: {"schema": schema}}
    if headers:
        value["headers"] = {name: {"schema": {"type": "string"}} for name in headers}
    return value


def parameter(name, kind, schema, required=True):
    return {"name": name, "in": kind, "required": required, "schema": schema}


def body(schema, media="application/json"):
    return {"required": True, "content": {media: {"schema": schema}}}


def relay_paths(source):
    """Добавьте wire-схемы и маршруты BFF, обслуживаемые потоковым HTTP delivery."""
    schemas = source["components"]["schemas"]
    folder = Path("spec/workspace/v1")
    command = json.loads((folder / "commands.schema.json").read_text())
    messages = json.loads((folder / "messages.schema.json").read_text())
    names = {name: "WorkspaceFile" + name.capitalize() for name in messages["$defs"]}

    def import_schema(value):
        if isinstance(value, list):
            return [import_schema(item) for item in value]
        if not isinstance(value, dict):
            return value
        if value.get("$ref", "").startswith("#/$defs/"):
            return schema_ref(names[value["$ref"].rsplit("/", 1)[-1]])
        return {key: import_schema(item) for key, item in value.items() if key not in ("$id", "$schema")}

    for name, schema in messages["$defs"].items():
        schemas[names[name]] = import_schema(schema)
    schemas["WorkspaceFileCommand"] = import_schema(command)
    schemas["WorkspaceFileCommandResult"] = import_schema(messages["oneOf"][0])
    schemas["WorkspaceFileErrorEnvelope"] = {
        "type": "object", "properties": {"error": schema_ref(names["error"])},
        "required": ["error"], "additionalProperties": False,
    }
    error = schema_ref("Error")
    file_error = {"oneOf": [error, schema_ref("WorkspaceFileErrorEnvelope")]}

    def operation(operation_id, summary, responses, params=(), request=None):
        key = parameter("X-BFF-Key", "header", {"type": "string"})
        session_id = parameter("id", "path", {"type": "string", "format": "uuid"})
        item = {
            "operationId": operation_id, "summary": summary, "tags": ["WorkspaceFiles"],
            "security": [{"bearerAuth": []}], "parameters": [session_id, key, *params],
            "responses": {
                "400": response("Неверный запрос", file_error),
                "401": response("Нужна действующая пользовательская сессия", error),
                "403": response("Ключ BFF не прошёл проверку", error),
                "404": response("Runtime-сессия или ресурс не найдены", file_error),
                "409": response("Конфликт состояния", file_error),
                "410": response("Рабочая директория закрыта", file_error),
                "429": response("Достигнут лимит запросов", file_error),
                "500": response("Внутренняя ошибка сервиса", error),
                "503": response("Runtime или зависимый сервис недоступны", file_error),
                "504": response("Истёк срок ожидания зависимого сервиса", error),
                **responses,
            },
        }
        if request is not None:
            item["requestBody"] = request
        return item

    source["tags"].append({"name": "WorkspaceFiles", "description": "Файловый HTTP API между BFF и Workspace."})
    socket = parameter("X-Workspace-Socket", "header", {"type": "string", "minLength": 1, "maxLength": 64})
    transfer = parameter("transfer_id", "path", {"type": "string", "format": "uuid"})
    after = parameter("after", "query", {"type": "integer", "format": "int64", "minimum": 0, "default": 0}, False)
    offset = parameter("offset", "query", {"type": "integer", "format": "int64", "minimum": 0, "maximum": 3 * 1024**3})
    binary = {"type": "string", "format": "binary"}
    events = {"type": "object", "additionalProperties": False, "required": ["events", "seq"], "properties": {
        "events": {"type": "array", "items": schema_ref(names["event"])},
        "seq": {"type": "integer", "format": "int64", "minimum": 0},
    }}
    ready = {"type": "object", "required": ["workspace_generation", "ready"], "additionalProperties": False, "properties": {
        "workspace_generation": {"type": "string", "format": "uuid"}, "ready": {"type": "boolean"},
    }}
    download_headers = ("Accept-Ranges", "ETag", "Content-Length", "Content-Disposition")
    prefix = "/runtime-sessions/{id}/workspace/"
    return {
        "/notebooks/{id}/export": {"get": {
            "operationId": "exportNotebook",
            "summary": "Скачивание согласованной версии блокнота как IPYNB",
            "tags": ["notebooks"],
            "security": [{"bearerAuth": []}, {}],
            "parameters": [
                parameter("id", "path", {"type": "integer", "format": "int64", "minimum": 1}),
                parameter("X-BFF-Key", "header", {"type": "string"}),
            ],
            "responses": {
                "200": response("IPYNB и хеш одного согласованного снимка", binary, "application/x-ipynb+json",
                                ("ETag", "Content-Digest", "Content-Disposition", "Content-Length", "Cache-Control")),
                "400": response("Неверный ID блокнота", schema_ref("Error")),
                "401": response("Переданный Bearer недействителен", schema_ref("Error")),
                "403": response("Ключ BFF не прошёл проверку", schema_ref("Error")),
                "404": response("Блокнот не найден или недоступен", schema_ref("Error")),
                "413": response("Документ больше 128 МиБ", schema_ref("Error")),
                "429": response("Достигнут лимит одновременных передач", schema_ref("Error")),
                "500": response("Не удалось подготовить согласованный снимок", schema_ref("Error")),
                "503": response("Зависимость недоступна", schema_ref("Error")),
                "504": response("Истёк срок ожидания зависимости", schema_ref("Error")),
            },
        }},
        prefix + "check": {"get": operation("checkWorkspace", "Проверка готовности файловой директории", {
            "200": response("Директория готова", ready),
        })},
        prefix + "command": {"post": operation("workspaceCommand", "Выполнение файловой команды", {
            "200": response("Результат или прикладная ошибка команды", schema_ref("WorkspaceFileCommandResult")),
            "400": response("Неверная команда или идентификатор сокета", file_error),
            "413": response("Команда превышает 64 КиБ"),
            "429": response("Достигнут лимит файловых соединений", file_error),
        }, (socket,), body(schema_ref("WorkspaceFileCommand")))},
        prefix + "events": {"get": operation("pollWorkspaceEvents", "Чтение файловых событий с ожиданием до 15 секунд", {
            "200": response("События после переданного номера", events),
            "409": response("Подписку нужно восстановить", file_error),
        }, (socket, after))},
        prefix + "socket": {"delete": operation("closeWorkspaceSocket", "Закрытие файловой подписки", {
            "204": response("Подписка закрыта"),
        }, (socket,))},
        prefix + "transfers/{transfer_id}/chunks": {"post": operation("uploadWorkspaceChunk", "Загрузка порции файла до 1 МиБ", {
            "200": response("Состояние передачи", schema_ref(names["transfer"])),
            "409": response("Конфликт передачи", file_error),
            "413": response("Порция превышает 1 МиБ"),
        }, (transfer, offset), body(binary, "application/octet-stream"))},
        prefix + "transfers/{transfer_id}/download": {"get": operation("downloadWorkspaceTransfer", "Потоковое скачивание файла до 3 ГиБ", {
            "200": response("Содержимое файла", binary, "application/octet-stream", download_headers),
            "206": response("Запрошенный диапазон файла", binary, "application/octet-stream", (*download_headers, "Content-Range")),
            "409": response("Файл или передача изменились", file_error),
            "416": response("Диапазон недоступен", headers=("Content-Range",)),
        }, (transfer, parameter("Range", "header", {"type": "string"}, False), parameter("If-Range", "header", {"type": "string"}, False)))},
    }
