"""Передавайте typed файловые команды и bounded byte streams существующему WorkspaceService."""

import asyncio
import hashlib
import re
from contextlib import suppress

from ..generated.cellestial.agent.v1 import agent_pb2
from ..generated.cellestial.files.v1 import files_pb2 as pb
from ..generated.cellestial.files.v1 import files_pb2_grpc as rpc
from ..models import AgentError
from ..workspace_transfers import CHUNK, CREDIT, FileError, public_transfer
from .control import Binding


def quota(value):
    """Преобразуйте только опубликованные поля квоты без filesystem internals."""
    value = value or {}
    return pb.Quota(
        **{key: value.get(key, 0) for key in ("capacity_bytes", "used_bytes", "available_bytes")},
        source=value.get("source", ""),
    )


def entry(value):
    """Сохраните optional size для каталогов и версию исходного inode."""
    result = pb.Entry(
        **{key: value.get(key, "") for key in ("name", "path", "type", "modified_at", "version")},
        actions=value.get("actions", ()),
    )
    if value.get("size_bytes") is not None:
        result.size_bytes = value["size_bytes"]
    return result


def transfer(value):
    """Сохраните uint64 offsets и presence hash/version, не раскрывая staging identity."""
    result = pb.Transfer(
        **{key: value.get(key, "") for key in ("transfer_id", "direction", "path", "state")},
        **{
            key: value.get(key, 0)
            for key in ("size_bytes", "offset", "chunk_bytes", "credit_bytes")
        },
        generation=value.get("workspace_generation", ""),
        updated=value.get("updated", 0),
    )
    for key in ("sha256", "version"):
        if value.get(key) is not None:
            setattr(result, key, value[key])
    return result


def directory(value, generation):
    """Перенесите весь снимок подписки, включая items, курсор и квоту."""
    result = pb.Directory(
        path=value["path"],
        items=[entry(item) for item in value["items"]],
        revision=value["revision"],
        generation=generation,
        quota=quota(value.get("quota")),
        seq=value.get("seq", 0),
    )
    if value.get("next_cursor") is not None:
        result.next_cursor = value["next_cursor"]
    return result


def file_error(value):
    """Передайте безопасный прежний code, flags и operation/transfer IDs."""
    return pb.Error(
        **{key: value.get(key, "") for key in ("code", "message", "operation_id", "transfer_id")},
        retryable=value.get("retryable", False),
        resync_required=value.get("resync_required", False),
    )


def event(workspace, value):
    """Сохраните watcher seq, пути, revisions и resync markers типизированным DTO."""
    result = pb.Event(
        workspace=workspace,
        seq=value.get("seq", 0),
        type=value.get("type", ""),
        **{
            key: value.get(key, "")
            for key in (
                "path",
                "revision",
                "old_path",
                "new_path",
                "operation_id",
                "transfer_id",
                "directory",
                "synchronization",
                "reason",
            )
        },
        resync_required=value.get("resync_required", False),
    )
    if value.get("entry"):
        result.entry.CopyFrom(entry(value["entry"]))
    if value.get("quota"):
        result.quota.CopyFrom(quota(value["quota"]))
    if value.get("transfer"):
        result.transfer.CopyFrom(transfer(value["transfer"]))
    if value.get("code"):
        result.error.CopyFrom(file_error(value))
    return result


def command_value(request):
    """Преобразуйте oneof в прежнюю проверяемую команду; raw HTTP method/path не принимается."""
    kind = request.WhichOneof("command")
    if kind is None:
        raise AgentError(400, "file command required")
    result = dict(
        protocol_version=1,
        type=kind,
        request_id=request.request_id,
        session_id=request.workspace.session_id,
        workspace_generation=request.workspace.generation,
    )
    value = getattr(request, kind)
    if kind in ("mkdir", "rename", "delete", "upload_begin", "download_begin"):
        result.update(operation_id=request.operation_id, path=value.path)
    if kind == "list":
        result.update(path=value.path, limit=value.limit or 100)
        if value.cursor:
            result["cursor"] = value.cursor
    elif kind == "rename":
        result.update(name=value.name, version=value.expected_version)
    elif kind == "delete":
        result.update(version=value.expected_version, recursive=value.recursive)
    elif kind == "upload_begin":
        result["size_bytes"] = value.size_bytes
        if value.HasField("sha256"):
            result["sha256"] = value.sha256
    elif kind in ("subscribe", "unsubscribe"):
        result["paths"] = list(value.paths)
        if kind == "subscribe" and value.HasField("after"):
            result["after"] = value.after
    elif kind in ("upload_status", "upload_commit", "transfer_status", "transfer_cancel"):
        result["transfer_id"] = value.transfer_id
    return result


def select_range(bounds, size):
    """Проверьте один диапазон на актуальном snapshot файла и ограничьте конец длиной."""
    if size == 0:
        raise ValueError("empty file")
    if bounds.HasField("suffix"):
        if bounds.suffix == 0 or bounds.HasField("start") or bounds.HasField("end"):
            raise ValueError("invalid suffix")
        size_selected = min(bounds.suffix, size)
        return size - size_selected, size_selected
    if not bounds.HasField("start") or bounds.start >= size:
        raise ValueError("range start past file")
    end = min(bounds.end, size - 1) if bounds.HasField("end") else size - 1
    if end < bounds.start:
        raise ValueError("invalid range end")
    return bounds.start, end - bounds.start + 1


class Files(rpc.FilesServicer):
    """Переиспользуйте path/openat2/quota/checkpoint инварианты прежнего файлового backend."""

    def __init__(self, agent):
        """Используйте тот же Agent/WorkspaceService, что execution transport."""
        self.agent = agent
        self.binding = Binding(agent)

    async def service(self, workspace, context):
        """Подтвердите назначение, файловую generation и готовность на каждой операции."""
        await self.binding.target(
            agent_pb2.Target(session_id=workspace.session_id, fence=workspace.fence), context
        )
        if workspace.generation != self.agent.assigned().id:
            raise FileError("workspace_closed", resync=True)
        self.agent.live()
        if self.agent.settings.workspace_path is None:
            raise FileError("runtime_not_ready")
        async with self.agent.file_start:
            if self.agent.workspace is None:
                await self.agent.open_workspace()
        self.agent.workspace.check()
        return self.agent.workspace

    @staticmethod
    def client(service, socket_id, *, create):
        """Ограничьте четыре socket и сохраняйте subscriptions между короткими HTTP/RPC вызовами."""
        if not re.fullmatch(r"[A-Za-z0-9._-]{1,64}", socket_id):
            raise FileError("invalid_path")
        state = service.clients.get(socket_id)
        if state is None:
            if not create:
                raise FileError("resync_required", resync=True)
            if len(service.clients) >= 4:
                raise FileError("rate_limited")
            state = service.clients[socket_id] = {"paths": set(), "updated": 0}
        state["updated"] = asyncio.get_running_loop().time()
        return state

    async def Check(self, request, context):
        """Верните готовность только после всех проверок привязки и файлового backend."""
        service = await self.service(request.workspace, context)
        return pb.CheckResponse(ready=True, generation=service.session.id)

    async def OpenSocket(self, request, context):
        """Создайте или продлите существующий socket того же runtime/generation."""
        service = await self.service(request.workspace, context)
        self.client(service, request.workspace.socket_id, create=True)
        return pb.SocketResponse(
            socket_id=request.workspace.socket_id, generation=service.session.id
        )

    async def CloseSocket(self, request, context):
        """Идемпотентно снимите watch refs и освободите слот socket."""
        service = await self.service(request.workspace, context)
        service.drop_client(request.workspace.socket_id)
        return pb.SocketResponse(
            socket_id=request.workspace.socket_id, generation=service.session.id, closed=True
        )

    async def Command(self, request, context):
        """Сохраните прежнюю control envelope и различимые доменные ошибки."""
        service = await self.service(request.workspace, context)
        kind = request.WhichOneof("command")
        no_socket = kind in ("upload_status", "upload_commit", "transfer_status", "transfer_cancel")
        state = (
            None if no_socket else self.client(service, request.workspace.socket_id, create=True)
        )
        response = await service.command(command_value(request), state["paths"] if state else set())
        result = pb.CommandResponse(request_id=request.request_id)
        if "error" in response:
            result.error.CopyFrom(file_error(response["error"]))
            return result
        value = response["result"]
        if kind == "list":
            result.directory.CopyFrom(directory(value, service.session.id))
        elif "transfer_id" in value:
            result.transfer.CopyFrom(transfer(value))
        elif kind == "subscribe":
            result.watch.CopyFrom(
                pb.WatchResult(
                    snapshots=[
                        pb.SnapshotMarker(
                            path=snapshot["path"],
                            revision=snapshot["revision"],
                            seq=value["seq"],
                            complete=True,
                        )
                        for snapshot in value["snapshots"]
                    ],
                    seq=value["seq"],
                    events=[event(request.workspace, item) for item in value["events"]],
                    synchronization=value["synchronization"],
                )
            )
        else:
            result.operation.CopyFrom(
                pb.Operation(
                    operation_id=value.get("operation_id", ""),
                    revision=value.get("revision", ""),
                    resync_required=value.get("resync_required", False),
                    resulting_seq=value.get("resulting_seq", 0),
                )
            )
        return result

    async def Subscribe(self, request, context):
        """Верните одну bounded replay страницу с маркером cursor, не теряя watch socket."""
        service = await self.service(request.workspace, context)
        self.client(service, request.workspace.socket_id, create=False)
        after = request.after if request.HasField("after") else service.watch.seq
        if after > 2**63 - 1:
            raise AgentError(400, "invalid replay cursor")
        if request.paths:
            command = pb.CommandRequest(
                workspace=request.workspace,
                request_id="subscription",
                subscribe=pb.WatchPaths(paths=request.paths, after=after),
            )
            response = await self.Command(command, context)
            if response.HasField("error"):
                raise FileError(response.error.code, resync=response.error.resync_required)
            for snapshot in response.watch.snapshots:
                yield pb.Event(workspace=request.workspace, seq=snapshot.seq, snapshot=snapshot)
            values = list(response.watch.events)
        else:
            values = [event(request.workspace, item) for item in service.watch.replay(after)]
            if not values:
                service.watch.wakeup.clear()
                try:
                    await asyncio.wait_for(service.watch.wakeup.wait(), 15)
                except TimeoutError:
                    pass
                values = [event(request.workspace, item) for item in service.watch.replay(after)]
        for value in values:
            yield value
            after = value.seq
        yield pb.Event(
            workspace=request.workspace,
            seq=after,
            replay=pb.ReplayMarker(next_seq=after, has_more=bool(service.watch.replay(after))),
        )

    async def Upload(self, requests, context):
        """Подтвердите offset только после fdatasync/FULL checkpoint, сохранив retry по digest."""
        async for request in requests:
            service = await self.service(request.workspace, context)
            if not 0 < len(request.payload) <= CHUNK:
                raise FileError("file_too_large")
            item = await asyncio.to_thread(
                service.transfers.chunk, request.transfer_id, request.offset, request.payload
            )
            service.progress(request.transfer_id, len(request.payload))
            service.watch.emit("transfer_updated", transfer=public_transfer(item))
            yield pb.UploadAck(
                transfer_id=request.transfer_id,
                offset=item["offset"],
                credit_bytes=CREDIT,
                transfer=transfer(public_transfer(item)),
            )

    async def Download(self, request, context):
        """Читайте descriptor порциями; отмена освобождает его без ложного completed."""
        service = await self.service(request.workspace, context)
        item = await asyncio.to_thread(service.transfers.open_download, request.transfer_id)
        size, start = item["size_bytes"], request.offset
        complete = False
        digest = hashlib.sha256()
        try:
            partial = False
            if request.HasField("range"):
                etag = '"' + item["version"] + '"'
                if not request.if_range or request.if_range == etag:
                    try:
                        start, length = select_range(request.range, size)
                    except ValueError:
                        yield pb.DownloadChunk(
                            total_bytes=size, error=pb.Error(code="invalid_range")
                        )
                        return
                    partial = True
                else:
                    start, length = 0, size
            else:
                if start > size or (start == size and size != 0):
                    raise FileError("invalid_range")
                length = request.length or size - start
                if length > size - start:
                    raise FileError("invalid_range")
            end = start + length
            yield pb.DownloadChunk(
                offset=start,
                version=item["version"],
                total_bytes=size,
                selected_bytes=length,
                filename=item["path"].rpartition("/")[2],
                partial=partial,
            )
            offset = start
            while offset < end:
                service.check()
                data = await asyncio.to_thread(
                    service.transfers.read, request.transfer_id, offset, min(CHUNK, end - offset)
                )
                if not data:
                    raise FileError("file_changed", resync=True)
                digest.update(data)
                yield pb.DownloadChunk(offset=offset, payload=data)
                offset += len(data)
                await asyncio.to_thread(
                    service.transfers.acknowledge_download, request.transfer_id, offset
                )
                service.progress(request.transfer_id, len(data), "download")
            yield pb.DownloadChunk(offset=offset, complete=True, sha256=digest.hexdigest())
            complete = True
        except FileError as error:
            service.watch.emit(
                "transfer_error",
                transfer_id=request.transfer_id,
                code=error.code,
                resync_required=error.resync,
            )
            raise
        finally:
            service.active_progress.pop(request.transfer_id, None)
            with suppress(FileError):
                item = await asyncio.shield(
                    asyncio.to_thread(
                        service.transfers.finish_download, request.transfer_id, complete
                    )
                )
                service.watch.emit("transfer_updated", transfer=public_transfer(item))
