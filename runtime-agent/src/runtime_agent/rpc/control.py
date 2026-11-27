"""Преобразуйте Protobuf в существующие kernel/session usecase и обратно."""

import hashlib
import json
import secrets
from uuid import UUID

import grpc
from pydantic import ValidationError

from ..generated.cellestial.agent.v1 import agent_pb2 as pb
from ..generated.cellestial.agent.v1 import agent_pb2_grpc as rpc
from ..generated.cellestial.common.v1 import error_pb2 as common
from ..models import AgentError, Execute, Inspect, StopExecutions
from .interceptors import abort

MAX_OBJECT = 64 << 20
CHUNK = 1 << 20


def json_bytes(value):
    """Сохраните исходный JSON Jupyter без NaN, с явным null при отсутствии результата."""
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"), allow_nan=False).encode()


def health(snapshot):
    """Верните только безопасный typed health DTO без токена/адреса агента."""
    value = pb.HealthResponse(
        session_id=snapshot.get("session_id") or "",
        language=snapshot.get("language") or "",
        state=snapshot.get("state") or "",
        reason=snapshot.get("reason") or "",
        generation=snapshot.get("generation") or "",
        ready=snapshot.get("ready", False),
        draining=snapshot.get("draining", False),
        queued=snapshot.get("queued", 0),
    )
    if snapshot.get("active_execution_id") is not None:
        value.active_execution_id = snapshot["active_execution_id"]
    return value


def object_parts(payload, identifier):
    """Ограничьте объект до 64 MiB и подтвердите его длину/SHA-256 в обязательном End."""
    data = payload.SerializeToString(deterministic=True)
    if len(data) > MAX_OBJECT:
        raise AgentError(400, "object size limit exceeded")
    digest = hashlib.sha256(data).digest()
    header = common.ObjectHeader(object_id=identifier, length=len(data), sha256=digest)
    return header, data, common.ObjectEnd(length=len(data), sha256=digest)


async def result_frames(value):
    """Передавайте один результат последовательными ограниченными chunks с backpressure."""
    header, data, end = object_parts(
        pb.ExecutionPayload(result_json=json_bytes(value.get("result"))), value["execution_id"]
    )
    yield pb.ResultFrame(
        header=pb.ExecutionHeader(
            execution_id=value["execution_id"], state=value["state"], object=header
        )
    )
    for offset in range(0, len(data), CHUNK):
        yield pb.ResultFrame(
            chunk=common.ObjectChunk(offset=offset, payload=data[offset : offset + CHUNK])
        )
    yield pb.ResultFrame(end=end)


class Binding:
    """Проверяйте назначение, kernel generation и устойчивый fence перед прикладной командой."""

    def __init__(self, agent):
        """Сохраните прикладной Agent, не создавая второй kernel или очередь."""
        self.agent = agent

    async def target(self, target, context, *, assigning=False, health_only=False):
        """Отвергните чужую session/generation и устаревшую версию до эффекта."""
        if not target.session_id:
            if health_only and self.agent.session is None:
                return
            raise AgentError(400, "session ID required")
        try:
            sid = str(UUID(target.session_id))
        except ValueError:
            raise AgentError(400, "invalid session ID") from None
        if target.fence > 2**63 - 1:
            raise AgentError(400, "invalid fence")
        assigned = self.agent.session
        if assigned is None and health_only:
            return
        if assigned is None and not assigning:
            raise AgentError(503, "runtime not assigned")
        if assigned is not None and assigned.id != sid:
            await abort(context, grpc.StatusCode.PERMISSION_DENIED, "session_forbidden")
        if target.generation and assigned is not None and target.generation != assigned.generation:
            await abort(context, grpc.StatusCode.FAILED_PRECONDITION, "generation_conflict")
        if not self.agent.journal.claim_rpc_fence(sid, target.fence):
            await abort(context, grpc.StatusCode.ABORTED, "stale_fence")


class Control(rpc.ControlServicer):
    """Делегируйте gRPC существующей durable очереди и журналу агента."""

    def __init__(self, agent):
        """Свяжите транспорт с единственным прикладным Agent."""
        self.agent = agent
        self.binding = Binding(agent)

    async def Assign(self, request, context):
        """Назначьте один runtime; повтор того же session/fence не создаёт kernel заново."""
        if request.language != "python":
            raise AgentError(400, "unsupported language")
        await self.binding.target(request.target, context, assigning=True)
        # Token пока нужен прежнему Agent.assign как внутренний ключ binding. Он не идёт в RPC.
        token = self.agent.token or secrets.token_urlsafe(48)
        return health(
            await self.agent.assign(str(UUID(request.target.session_id)), "python", token)
        )

    async def Health(self, request, context):
        """Разрешите только health/assign до назначения; не продлевайте idle пользователя."""
        await self.binding.target(request, context, health_only=True)
        return health(self.agent.snapshot())

    async def Execute(self, request, context):
        """Сохраните команду до ACK; потеря ответа не меняет execution fingerprint."""
        await self.binding.target(request.target, context)
        try:
            command = Execute(
                execution_id=request.execution_id,
                code=request.code,
                timeout_seconds=request.timeout_seconds,
            ).model_dump()
        except ValidationError:
            raise AgentError(400, "invalid execution") from None
        if len(request.code.encode()) > CHUNK:
            raise AgentError(400, "source limit exceeded")
        value = self.agent.accept({**command, "kind": "execute"})
        async for frame in result_frames(value):
            yield frame

    async def GetExecution(self, request, context):
        """Прочитайте сохранённый исход без повторной отправки кода kernel."""
        await self.binding.target(request.target, context)
        self._execution_id(request.execution_id)
        value = self.agent.journal.execution(self.agent.assigned().id, request.execution_id)
        async for frame in result_frames(value):
            yield frame

    async def InspectVariables(self, request, context):
        """Поставьте инспекцию в ту же FIFO очередь с прежним execution ID."""
        await self.binding.target(request.target, context)
        self._execution_id(request.execution_id)
        value = self.agent.accept(
            {
                "execution_id": request.execution_id,
                "kind": "inspect",
                "timeout_seconds": 5.0,
            }
        )
        async for frame in result_frames(value):
            yield frame

    @staticmethod
    def _execution_id(value):
        """Переиспользуйте прежнюю валидацию идентификатора команды."""
        try:
            Inspect(execution_id=value)
        except ValidationError:
            raise AgentError(400, "invalid execution ID") from None

    async def ReadEvents(self, request, context):
        """Отдайте целые события chunks, затем обязательный маркер страницы."""
        await self.binding.target(request.target, context)
        count, limit = request.max_events or 100, request.max_bytes or 16 << 20
        if request.after < 0 or count > 100 or limit > 16 << 20:
            raise AgentError(400, "invalid event page")
        sid = self.agent.assigned().id
        values = self.agent.journal.events(sid, request.after)
        used, after = 0, request.after
        for value in values[:count]:
            payload = pb.EventPayload(
                content_json=json_bytes(value.get("content")),
                metadata_json=json_bytes(value.get("metadata")),
                result_json=json_bytes(value.get("result")),
                buffers=value.get("buffers_base64") or (),
            )
            header, data, end = object_parts(payload, str(value["seq"]))
            if used and used + len(data) > limit:
                break
            yield pb.EventFrame(
                header=pb.EventHeader(
                    seq=value["seq"],
                    session_id=sid,
                    execution_id=value.get("execution_id") or "",
                    kind=value.get("kind") or "",
                    state=value.get("state") or "",
                    reason=value.get("reason") or "",
                    generation=value.get("generation") or "",
                    channel=value.get("channel") or "",
                    parent_msg_id=value.get("parent_msg_id") or "",
                    msg_type=value.get("msg_type") or "",
                    timestamp=value.get("timestamp") or 0,
                    object=header,
                )
            )
            for offset in range(0, len(data), CHUNK):
                yield pb.EventFrame(
                    chunk=common.ObjectChunk(offset=offset, payload=data[offset : offset + CHUNK])
                )
            yield pb.EventFrame(end=end)
            after = value["seq"]
            used += len(data)
        yield pb.EventFrame(
            page_end=pb.EventPageEnd(
                next_seq=after, has_more=self.agent.journal.has_events(sid, after)
            )
        )

    async def Activity(self, request, context):
        """Продлите idle только по явному действию пользователя."""
        await self.binding.target(request, context)
        self.agent.live().touch()
        return health(self.agent.snapshot())

    async def Drain(self, request, context):
        """Запретите приём новых команд, не объявляя очистку VPS завершённой."""
        await self.binding.target(request, context)
        await self.agent.live().drain()
        return health(self.agent.snapshot())

    async def Stop(self, request, context):
        """Завершите kernel и сохраните журнал для последующего чтения."""
        await self.binding.target(request, context)
        return health(await self.agent.stop_session())

    async def Interrupt(self, request, context):
        """Прервите команду один раз; сетевой клиент не повторяет неоднозначный исход."""
        await self.binding.target(request, context)
        await self.agent.live().interrupt()
        return pb.ControlResponse(
            accepted=True, generation=self.agent.snapshot().get("generation") or ""
        )

    async def Restart(self, request, context):
        """Перезапустите свободный kernel без автоматического повтора потерянного ответа."""
        await self.binding.target(request, context)
        session = self.agent.live()
        await session.restart()
        return pb.ControlResponse(accepted=True, generation=session.generation)

    async def StopExecutions(self, request, context):
        """Используйте прежний operation ID/fencing остановки исполнений."""
        await self.binding.target(request.target, context)
        try:
            value = StopExecutions(
                operation_id=request.target.operation_id, execution_ids=list(request.execution_ids)
            )
        except ValidationError:
            raise AgentError(400, "invalid stop operation") from None
        await self.agent.live().stop_executions(value.operation_id, value.execution_ids)
        return pb.ControlResponse(accepted=True)
