"""Проверьте auth/recovery/deadline/correlation на настоящем Python gRPC transport."""

import time

import grpc
import pytest
from grpc_health.v1 import health_pb2, health_pb2_grpc

from runtime_agent.generated.cellestial.agent.v1 import agent_pb2 as pb
from runtime_agent.rpc.interceptors import correlation

from .conftest import until
from .test_grpc_control import grpc_agent as grpc_agent


@pytest.fixture
def rpc_timeouts():
    """Ускорьте server cap, не подменяя transport или обработчик."""
    return {"unary_seconds": 0.2, "stream_seconds": 0.3, "idle_seconds": 0.1}


async def test_server_deadline_before_first_request(grpc_agent):
    """Открытый stream без первого сообщения ограничивается сервером, не deadline клиента."""
    _, _, _, _, pki, port = grpc_agent
    async with grpc.aio.secure_channel(
        f"127.0.0.1:{port}",
        pki.channel_credentials("environments.internal.cellestial.ru"),
        options=(("grpc.ssl_target_name_override", "agent.test.internal.cellestial.ru"),),
    ) as channel:
        method = channel.stream_stream(
            "/cellestial.agent.v1.Control/Execute",
            request_serializer=pb.ExecuteRequest.SerializeToString,
            response_deserializer=pb.ResultFrame.FromString,
        )
        call = method(timeout=2)
        started = time.monotonic()
        with pytest.raises(grpc.aio.AioRpcError) as error:
            await call.read()
        assert error.value.code() == grpc.StatusCode.DEADLINE_EXCEEDED
        assert time.monotonic() - started < 0.8


async def test_recovery_and_cancel_log_once(grpc_agent, monkeypatch, caplog):
    """Panic и stream cancel завершают active metric и одну безопасную запись."""
    stub, agent, _, metrics, pki, port = grpc_agent
    caplog.set_level("INFO", logger="runtime_agent.observability")

    def fail():
        """Сымитируйте непредвиденный сбой за прикладной границей."""
        raise RuntimeError("private-runtime-password")

    monkeypatch.setattr(agent, "snapshot", fail)
    with pytest.raises(grpc.aio.AioRpcError) as error:
        await stub.Health(pb.Target(), metadata=(("x-request-id", "panic-correlation"),))
    assert error.value.code() == grpc.StatusCode.INTERNAL
    assert "private-runtime-password" not in str(error.value)
    assert "private-runtime-password" not in caplog.text
    records = [record for record in caplog.records if record.getMessage() == "rpc.request"]
    assert len(records) == 1
    assert records[0].event_fields["status"] == "INTERNAL"
    async with grpc.aio.secure_channel(
        f"127.0.0.1:{port}",
        pki.channel_credentials("probe.internal.cellestial.ru"),
        options=(("grpc.ssl_target_name_override", "agent.test.internal.cellestial.ru"),),
    ) as channel:
        call = health_pb2_grpc.HealthStub(channel).Watch(health_pb2.HealthCheckRequest())
        await call.read()
        call.cancel()
        await until(lambda: b'"Canceled"' in metrics.render())
    assert (
        b'cellestial_grpc_active_streams{grpc_method="Watch",grpc_service="grpc.health.v1.Health",'
        b'rpc_side="server",rpc_type="stream"} 0.0' in metrics.render()
    )


def test_correlation_rejects_duplicates_and_unsafe_values():
    """Примените тот же ASCII/length формат, что на Go HTTP/RPC границах."""
    for value in ("", "a" * 65, "has space", "текст", "bad\nvalue"):
        actual = correlation((("x-request-id", value),))
        assert actual != value and actual
    assert correlation((("x-request-id", "valid_123.A-b"),)) == "valid_123.A-b"
    assert correlation((("x-request-id", "first"), ("x-request-id", "second"))) not in (
        "first",
        "second",
    )


async def test_unknown_rpcs_use_one_counter_series(grpc_agent):
    """Учтите неизвестные RPC под одним набором labels, сохраняя Unimplemented."""
    _, _, _, metrics, pki, port = grpc_agent
    async with grpc.aio.secure_channel(
        f"127.0.0.1:{port}",
        pki.channel_credentials("environments.internal.cellestial.ru"),
        options=(("grpc.ssl_target_name_override", "agent.test.internal.cellestial.ru"),),
    ) as channel:
        for method in ("/private.Service/One", "/private.Service/Two"):
            with pytest.raises(grpc.aio.AioRpcError) as error:
                await channel.unary_unary(method)(b"", timeout=1)
            assert error.value.code() == grpc.StatusCode.UNIMPLEMENTED
    assert (
        b'grpc_code="Unimplemented",grpc_method="unmatched",grpc_service="unmatched",'
        b'rpc_side="server",rpc_type="unknown"} 2.0'
    ) in metrics.render()
