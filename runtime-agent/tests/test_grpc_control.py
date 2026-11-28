"""Проверьте настоящую Python gRPC-границу, не подменяя session/journal."""

from uuid import uuid4

import grpc
import pytest

from runtime_agent.generated.cellestial.agent.v1 import agent_pb2 as pb
from runtime_agent.generated.cellestial.agent.v1 import agent_pb2_grpc as rpc
from runtime_agent.journal import Journal
from runtime_agent.rpc.config import RPCSettings
from runtime_agent.rpc.server import build_server
from runtime_agent.runtime import Agent

from .conftest import FakeContainers, until
from .grpc_helpers import TestPKI


@pytest.fixture
async def grpc_agent(settings, tmp_path, rpc_timeouts):
    """Запустите production gRPC factory с тестовыми credentials и настоящим journal."""
    pki = TestPKI(tmp_path)
    key, cert = pki.certificate("agent.test.internal.cellestial.ru", server=True)
    cert_file, key_file = tmp_path / "server.pem", tmp_path / "server.key"
    cert_file.write_bytes(cert)
    key_file.write_bytes(key)
    journal = Journal(settings.state_dir)
    backend = FakeContainers()
    agent = Agent(settings, journal, backend)
    await agent.start()
    await until(lambda: agent.warm is not None)
    cfg = RPCSettings(cert_file=cert_file, key_file=key_file, ca_file=pki.ca_file, **rpc_timeouts)
    server, metrics = build_server(cfg, agent)
    port = server.add_secure_port("127.0.0.1:0", cfg.credentials())
    await server.start()
    channel = grpc.aio.secure_channel(
        f"127.0.0.1:{port}",
        pki.channel_credentials("environments.internal.cellestial.ru"),
        options=(("grpc.ssl_target_name_override", "agent.test.internal.cellestial.ru"),),
    )
    try:
        yield rpc.ControlStub(channel), agent, backend, metrics, pki, port
    finally:
        await channel.close()
        await server.stop(0)
        await agent.close()
        journal.close()


async def test_grpc_assignment_execute_and_recovery_boundary(grpc_agent):
    """Сохраните идемпотентное принятие команды и разделение correlation/operation ID."""
    stub, agent, backend, metrics, _, _ = grpc_agent
    target = pb.Target(session_id=str(uuid4()), fence=1)
    assigned = await stub.Assign(pb.AssignRequest(target=target, language="python"))
    assert assigned.ready
    repeated = await stub.Assign(pb.AssignRequest(target=target, language="python"))
    assert repeated.session_id == assigned.session_id
    request = pb.ExecuteRequest(
        target=target, execution_id="execution-1", code="print(42)", timeout_seconds=10
    )
    frames = [
        frame
        async for frame in stub.Execute(request, metadata=(("x-request-id", "correlation-1"),))
    ]
    assert frames[0].header.execution_id == "execution-1"
    assert frames[-1].HasField("end")
    await until(lambda: len(backend.kernel.sent) == 1)
    # Новый correlation не меняет fingerprint и не запускает команду повторно.
    repeated = [
        frame
        async for frame in stub.Execute(request, metadata=(("x-request-id", "correlation-2"),))
    ]
    assert repeated[0].header.execution_id == "execution-1"
    assert len(backend.kernel.sent) == 1
    assert agent.journal.execution_request_id(target.session_id, "execution-1") == "correlation-1"
    backend.kernel.complete()
    await until(
        lambda: agent.journal.execution(target.session_id, "execution-1")["state"] == "SUCCEEDED"
    )
    with pytest.raises(grpc.aio.AioRpcError) as error:
        await stub.Health(pb.Target(session_id=str(uuid4()), fence=1))
    assert error.value.code() == grpc.StatusCode.PERMISSION_DENIED
    with pytest.raises(grpc.aio.AioRpcError) as error:
        await stub.Health(pb.Target(session_id=target.session_id, fence=0))
    assert error.value.code() == grpc.StatusCode.ABORTED
    assert b"cellestial_grpc_calls_total" in metrics.render()


async def test_grpc_service_identity_and_correlation(grpc_agent):
    """Отвергните доверенный чужой SAN и верните correlation даже при отказе auth."""
    _, _, _, _, pki, port = grpc_agent
    async with grpc.aio.secure_channel(
        f"127.0.0.1:{port}",
        pki.channel_credentials("workspace.internal.cellestial.ru"),
        options=(("grpc.ssl_target_name_override", "agent.test.internal.cellestial.ru"),),
    ) as channel:
        call = rpc.ControlStub(channel).Health(
            pb.Target(), metadata=(("x-request-id", "rejected-request"),)
        )
        with pytest.raises(grpc.aio.AioRpcError) as error:
            await call
        assert error.value.code() == grpc.StatusCode.PERMISSION_DENIED
        assert (await call.initial_metadata())["x-request-id"] == "rejected-request"


async def test_grpc_events_preserve_jupyter_buffers(grpc_agent):
    """Передайте все буферы из существующего формата SQLite journal."""
    stub, agent, _, _, _, _ = grpc_agent
    target = pb.Target(session_id=str(uuid4()), fence=1)
    await stub.Assign(pb.AssignRequest(target=target, language="python"))
    agent.journal.event(target.session_id, "jupyter", buffers_base64=["AAEC", "AwQ="])
    frames = [frame async for frame in stub.ReadEvents(pb.EventsRequest(target=target))]
    chunks = []
    for frame in frames:
        if frame.HasField("chunk"):
            chunks.append(frame.chunk.payload)
        if frame.HasField("end"):
            payload = pb.EventPayload.FromString(b"".join(chunks))
            if list(payload.buffers) == ["AAEC", "AwQ="]:
                return
            chunks.clear()
    pytest.fail("gRPC event did not carry the stored Jupyter buffers")


@pytest.mark.docker
async def test_grpc_real_kernel_roundtrip(settings, tmp_path):
    """Выполните команду через mTLS до настоящего Docker/Jupyter kernel."""
    import os
    from dataclasses import replace

    if os.environ.get("RUN_DOCKER_TESTS") != "1":
        pytest.skip("Set RUN_DOCKER_TESTS=1")
    import docker

    from runtime_agent.containers import Containers

    docker_client = docker.from_env()
    network = docker_client.networks.create(
        "cellestial-grpc-test-" + uuid4().hex[:12],
        driver="bridge",
        enable_ipv6=False,
        labels={"ru.cellestial.test": "grpc"},
    )
    setup = replace(
        settings,
        kernel_network=network.name,
        python_image=os.environ.get("TEST_KERNEL_IMAGE", settings.python_image),
    )
    pki = TestPKI(tmp_path)
    key, cert = pki.certificate("agent.test.internal.cellestial.ru", server=True)
    cert_file, key_file = tmp_path / "kernel-test.crt", tmp_path / "kernel-test.key"
    cert_file.write_bytes(cert)
    key_file.write_bytes(key)
    journal = Journal(setup.state_dir)
    agent = Agent(setup, journal, Containers(setup, journal.instance_id))
    server = None
    try:
        await agent.start()
        await agent.warming
        assert agent.warm is not None
        cfg = RPCSettings(cert_file=cert_file, key_file=key_file, ca_file=pki.ca_file)
        server, _ = build_server(cfg, agent)
        port = server.add_secure_port("127.0.0.1:0", cfg.credentials())
        await server.start()
        async with grpc.aio.secure_channel(
            f"127.0.0.1:{port}",
            pki.channel_credentials("environments.internal.cellestial.ru"),
            options=(("grpc.ssl_target_name_override", "agent.test.internal.cellestial.ru"),),
        ) as channel:
            client = rpc.ControlStub(channel)
            target = pb.Target(session_id=str(uuid4()), fence=1)
            assigned = await client.Assign(pb.AssignRequest(target=target, language="python"))
            assert assigned.ready
            request = pb.ExecuteRequest(
                target=target, execution_id="docker-kernel", code="print(6 * 7)", timeout_seconds=10
            )
            metadata = (("x-request-id", "real-kernel-over-grpc"),)
            frames = [frame async for frame in client.Execute(request, metadata=metadata)]
            assert frames[0].header.execution_id == "docker-kernel"
            await until(
                lambda: (
                    journal.execution(target.session_id, "docker-kernel")["state"] == "SUCCEEDED"
                )
            )
            assert (
                journal.execution_request_id(target.session_id, "docker-kernel")
                == "real-kernel-over-grpc"
            )
            reply = [
                frame
                async for frame in client.GetExecution(
                    pb.ExecutionRequest(target=target, execution_id="docker-kernel")
                )
            ]
            assert reply[0].header.state == "SUCCEEDED"
            assert reply[-1].HasField("end")
    finally:
        try:
            if server is not None:
                await server.stop(0)
            await agent.close()
        finally:
            journal.close()
            network.remove()
            docker_client.close()
