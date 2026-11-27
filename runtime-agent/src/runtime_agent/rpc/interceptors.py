"""Применяйте один порядок correlation, metrics, recovery, deadline и service authorization."""

import asyncio
import time
from contextlib import aclosing, asynccontextmanager
from uuid import uuid4

import grpc
from google.protobuf.any_pb2 import Any
from google.rpc.status_pb2 import Status

from ..generated.cellestial.agent.v1 import agent_pb2
from ..generated.cellestial.common.v1.error_pb2 import ErrorDetail
from ..generated.cellestial.files.v1 import files_pb2
from ..models import AgentError
from ..observability import log, request_id
from ..workspace_transfers import FileError
from .config import RPCSettings
from .metrics import Metrics


def correlation(metadata):
    """Примите один ASCII request ID либо создайте новый, не смешивая его с operation ID."""
    import re

    values = [value for key, value in metadata if key == "x-request-id"]
    if (
        len(values) == 1
        and isinstance(values[0], str)
        and re.fullmatch(r"[A-Za-z0-9._-]{1,64}", values[0])
    ):
        return values[0]
    return str(uuid4())


def method_inventory():
    """Верните только опубликованные full method names для ограниченной кардинальности."""
    methods = {}
    for descriptor in (agent_pb2.DESCRIPTOR, files_pb2.DESCRIPTOR):
        for service in descriptor.services_by_name.values():
            for method in service.methods:
                methods[f"/{service.full_name}/{method.name}"] = (
                    "environments.internal.cellestial.ru",
                )
    for method in ("Check", "Watch"):
        methods[f"/grpc.health.v1.Health/{method}"] = (
            "environments.internal.cellestial.ru",
            "probe.internal.cellestial.ru",
        )
    return methods


async def abort(context, code, business):
    """Отправьте rich status с безопасным бизнес-кодом и исходным request_id."""
    detail = Any()
    detail.Pack(ErrorDetail(code=business, message=business, request_id=request_id.get() or ""))
    context.set_trailing_metadata(
        (
            ("x-request-id", request_id.get() or ""),
            (
                "grpc-status-details-bin",
                Status(code=code.value[0], message=business, details=[detail]).SerializeToString(),
            ),
        )
    )
    await context.abort(code, business)


class Transport(grpc.aio.ServerInterceptor):
    """Оборачивайте unary и stream, включая ожидание первого сообщения и half-close."""

    def __init__(self, settings: RPCSettings, metrics: Metrics):
        """Свяжите лимиты с собственным registry и неизменяемым реестром методов."""
        self.settings = settings
        self.metrics = metrics
        self.allowed = method_inventory()

    @asynccontextmanager
    async def scope(self, method, context, streaming):
        """Запишите один итоговый outcome, включая auth, validation, panic и cancel."""
        name = method if method in self.allowed else "unmatched"
        kind = "stream" if streaming else "unary"
        identifier = correlation(context.invocation_metadata())
        token = request_id.set(identifier)
        started = time.monotonic()
        outcome = grpc.StatusCode.OK
        self.metrics.active.labels(name, kind).inc()
        try:
            await context.send_initial_metadata((("x-request-id", identifier),))
            metadata_size = sum(
                len(key) + len(value) + 32 for key, value in context.invocation_metadata()
            )
            if metadata_size > 16 << 10:
                await abort(context, grpc.StatusCode.RESOURCE_EXHAUSTED, "metadata_limit_exceeded")
            auth = context.auth_context()
            peers = auth.get("x509_subject_alternative_name", ())
            if not auth.get("transport_security_type"):
                await abort(
                    context, grpc.StatusCode.UNAUTHENTICATED, "service_authentication_required"
                )
            if not any(
                peer.decode("ascii", errors="replace") in self.allowed.get(method, ())
                for peer in peers
            ):
                await abort(context, grpc.StatusCode.PERMISSION_DENIED, "service_forbidden")
            budget = self.settings.stream_seconds if streaming else self.settings.unary_seconds
            remaining = context.time_remaining()
            if remaining is not None:
                budget = min(budget, remaining)
            async with asyncio.timeout(budget):
                yield name
        except TimeoutError:
            outcome = grpc.StatusCode.DEADLINE_EXCEEDED
            await abort(context, outcome, "deadline_exceeded")
        except asyncio.CancelledError:
            remaining = context.time_remaining()
            outcome = (
                grpc.StatusCode.DEADLINE_EXCEEDED
                if remaining is not None and remaining <= 0
                else grpc.StatusCode.CANCELLED
            )
            raise
        except grpc.aio.AbortError:
            outcome = context.code() or grpc.StatusCode.UNKNOWN
            raise
        except FileError as error:
            outcome = {
                "runtime_not_ready": grpc.StatusCode.UNAVAILABLE,
                "workspace_closed": grpc.StatusCode.FAILED_PRECONDITION,
                "rate_limited": grpc.StatusCode.RESOURCE_EXHAUSTED,
                "quota_exceeded": grpc.StatusCode.RESOURCE_EXHAUSTED,
                "file_too_large": grpc.StatusCode.RESOURCE_EXHAUSTED,
                "invalid_path": grpc.StatusCode.INVALID_ARGUMENT,
                "invalid_range": grpc.StatusCode.OUT_OF_RANGE,
                "permission_denied": grpc.StatusCode.PERMISSION_DENIED,
                "not_found": grpc.StatusCode.NOT_FOUND,
                "file_changed": grpc.StatusCode.ABORTED,
            }.get(error.code, grpc.StatusCode.FAILED_PRECONDITION)
            await abort(context, outcome, error.code)
        except AgentError as error:
            outcome, business = {
                400: (grpc.StatusCode.INVALID_ARGUMENT, "validation_error"),
                403: (grpc.StatusCode.PERMISSION_DENIED, "forbidden"),
                404: (grpc.StatusCode.NOT_FOUND, "not_found"),
                409: (grpc.StatusCode.FAILED_PRECONDITION, "conflict"),
                429: (grpc.StatusCode.RESOURCE_EXHAUSTED, "rate_limited"),
                503: (grpc.StatusCode.UNAVAILABLE, "runtime_not_ready"),
            }.get(error.status, (grpc.StatusCode.INTERNAL, "internal"))
            await abort(context, outcome, business)
        except Exception:
            outcome = grpc.StatusCode.INTERNAL
            await abort(context, outcome, "internal")
        finally:
            self.metrics.active.labels(name, kind).dec()
            self.metrics.requests.labels(name, outcome.name, kind).inc()
            self.metrics.duration.labels(name, outcome.name, kind).observe(
                time.monotonic() - started
            )
            log.info(
                "rpc.request",
                extra={
                    "event_fields": {
                        "service": "runtime-agent",
                        "version": self.settings.version,
                        "request_id": identifier,
                        "method": name,
                        "status": outcome.name,
                        "duration_ms": (time.monotonic() - started) * 1000,
                    }
                },
            )
            request_id.reset(token)

    async def received(self, messages, method):
        """Читайте с backpressure и idle limit; EOF не считается новым сообщением."""
        iterator = messages.__aiter__()
        while True:
            try:
                value = await asyncio.wait_for(anext(iterator), self.settings.idle_seconds)
            except StopAsyncIteration:
                return
            limit = 2 << 20 if method.startswith("/cellestial.files.") else 8 << 20
            if value.ByteSize() > limit:
                raise AgentError(400, "message limit exceeded")
            self.metrics.message(method, "received", value)
            yield value

    async def one(self, messages, method):
        """Потребуйте ровно один unary request и half-close в пределах server budget."""
        iterator = self.received(messages, method)
        try:
            request = await anext(iterator)
        except StopAsyncIteration:
            raise AgentError(400, "request required") from None
        try:
            await anext(iterator)
        except StopAsyncIteration:
            return request
        raise AgentError(400, "unary request cardinality")

    async def intercept_service(self, continuation, details):
        """Начните scope до чтения первого сообщения, даже у protobuf unary методов."""
        handler = await continuation(details)
        if handler is None:
            return None
        method = details.method
        if handler.response_streaming:

            async def stream(messages, context):
                async with self.scope(method, context, True):
                    source = (
                        self.received(messages, method)
                        if handler.request_streaming
                        else await self.one(messages, method)
                    )
                    behavior = (
                        handler.stream_stream if handler.request_streaming else handler.unary_stream
                    )
                    async with aclosing(behavior(source, context)) as iterator:
                        async for value in iterator:
                            await asyncio.wait_for(context.write(value), self.settings.idle_seconds)
                            self.metrics.message(method, "sent", value)

            return grpc.stream_stream_rpc_method_handler(
                stream,
                request_deserializer=handler.request_deserializer,
                response_serializer=handler.response_serializer,
            )

        async def unary(messages, context):
            async with self.scope(method, context, False):
                source = (
                    self.received(messages, method)
                    if handler.request_streaming
                    else await self.one(messages, method)
                )
                behavior = (
                    handler.stream_unary if handler.request_streaming else handler.unary_unary
                )
                value = await behavior(source, context)
                self.metrics.message(method, "sent", value)
                return value

        return grpc.stream_unary_rpc_method_handler(
            unary,
            request_deserializer=handler.request_deserializer,
            response_serializer=handler.response_serializer,
        )
