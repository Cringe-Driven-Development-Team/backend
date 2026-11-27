"""Проверьте durable ACK, integrity и cleanup файлов на настоящем gRPC/mTLS."""

import hashlib
from dataclasses import replace
from uuid import uuid4

import grpc
import pytest

from runtime_agent.generated.cellestial.agent.v1 import agent_pb2 as agent_pb
from runtime_agent.generated.cellestial.files.v1 import files_pb2 as pb
from runtime_agent.generated.cellestial.files.v1 import files_pb2_grpc as rpc

from .test_grpc_control import grpc_agent as grpc_agent


@pytest.fixture
def settings(settings, tmp_path):
    """Подготовьте настоящий workspace с обязательными data/staging каталогами."""
    root = tmp_path / "workspace"
    (root / "data").mkdir(parents=True)
    (root / "staging").mkdir(mode=0o700)
    return replace(settings, workspace_path=root)


async def test_grpc_file_transfer_and_socket_lifecycle(grpc_agent):
    """Проверьте offset retry, hash/no-replace commit и download с обязательным End."""
    control, agent, _, _, pki, port = grpc_agent
    sid = str(uuid4())
    await control.Assign(
        agent_pb.AssignRequest(target=agent_pb.Target(session_id=sid, fence=1), language="python")
    )
    async with grpc.aio.secure_channel(
        f"127.0.0.1:{port}",
        pki.channel_credentials("environments.internal.cellestial.ru"),
        options=(("grpc.ssl_target_name_override", "agent.test.internal.cellestial.ru"),),
    ) as channel:
        stub = rpc.FilesStub(channel)
        workspace = pb.Workspace(session_id=sid, generation=sid, fence=1, socket_id=str(uuid4()))
        checked = await stub.Check(pb.CheckRequest(workspace=workspace))
        assert checked.ready and checked.generation == sid
        opened = await stub.OpenSocket(pb.SocketRequest(workspace=workspace))
        assert opened.socket_id == workspace.socket_id
        mkdir = await stub.Command(
            pb.CommandRequest(
                workspace=workspace,
                request_id="mkdir-request",
                operation_id=str(uuid4()),
                mkdir=pb.Mkdir(path="folder"),
            )
        )
        assert mkdir.HasField("operation")
        payload = b"a" * (1 << 20) + b"b" * 17
        upload = await stub.Command(
            pb.CommandRequest(
                workspace=workspace,
                request_id="upload-request",
                operation_id=str(uuid4()),
                upload_begin=pb.UploadBegin(
                    path="folder/data",
                    size_bytes=len(payload),
                    sha256=hashlib.sha256(payload).hexdigest(),
                ),
            )
        )
        assert upload.HasField("transfer")
        tid = upload.transfer.transfer_id

        async def chunks():
            """Повторите первый offset с тем же digest, не добавляя файл второй раз."""
            for offset, body in (
                (0, payload[: 1 << 20]),
                (0, payload[: 1 << 20]),
                (1 << 20, payload[1 << 20 :]),
            ):
                yield pb.UploadChunk(
                    workspace=workspace, transfer_id=tid, offset=offset, payload=body
                )

        acks = [ack async for ack in stub.Upload(chunks())]
        assert [ack.offset for ack in acks] == [1 << 20, 1 << 20, len(payload)]
        assert all(not ack.HasField("error") and ack.credit_bytes <= 4 << 20 for ack in acks)
        commit = pb.CommandRequest(
            workspace=workspace,
            request_id="commit-request",
            upload_commit=pb.TransferReference(transfer_id=tid),
        )
        committed = await stub.Command(commit)
        assert committed.transfer.state == "completed"
        assert (await stub.Command(commit)).transfer == committed.transfer
        assert (agent.settings.workspace_path / "data/folder/data").read_bytes() == payload
        download = await stub.Command(
            pb.CommandRequest(
                workspace=workspace,
                request_id="download-request",
                operation_id=str(uuid4()),
                download_begin=pb.DownloadBegin(path="folder/data"),
            )
        )
        frames = [
            frame
            async for frame in stub.Download(
                pb.DownloadRequest(
                    workspace=workspace,
                    transfer_id=download.transfer.transfer_id,
                )
            )
        ]
        assert frames[-1].complete
        assert frames[-1].sha256 == hashlib.sha256(payload).hexdigest()
        assert b"".join(frame.payload for frame in frames) == payload
        assert not agent.workspace.transfers.downloads
        closed = await stub.CloseSocket(pb.SocketRequest(workspace=workspace))
        assert closed.closed
        assert workspace.socket_id not in agent.workspace.clients


async def test_download_cancel_closes_descriptor_and_large_offset(grpc_agent):
    """Проверьте cancel/Range >2GiB без копии полного файла в памяти."""
    from .conftest import until

    control, agent, _, _, pki, port = grpc_agent
    sid = str(uuid4())
    await control.Assign(
        agent_pb.AssignRequest(target=agent_pb.Target(session_id=sid, fence=1), language="python")
    )
    path = agent.settings.workspace_path / "data/large"
    with path.open("wb") as output:
        output.truncate(3 * 1024**3)
        output.seek(2 * 1024**3 + 7)
        output.write(b"range-marker")
    async with grpc.aio.secure_channel(
        f"127.0.0.1:{port}",
        pki.channel_credentials("environments.internal.cellestial.ru"),
        options=(("grpc.ssl_target_name_override", "agent.test.internal.cellestial.ru"),),
    ) as channel:
        stub = rpc.FilesStub(channel)
        workspace = pb.Workspace(session_id=sid, generation=sid, fence=1, socket_id=str(uuid4()))
        download = await stub.Command(
            pb.CommandRequest(
                workspace=workspace,
                request_id="range",
                operation_id=str(uuid4()),
                download_begin=pb.DownloadBegin(path="large"),
            )
        )
        tid = download.transfer.transfer_id
        frames = [
            frame
            async for frame in stub.Download(
                pb.DownloadRequest(
                    workspace=workspace,
                    transfer_id=tid,
                    offset=2 * 1024**3 + 7,
                    length=12,
                )
            )
        ]
        assert frames[0].offset == 2 * 1024**3 + 7
        assert b"".join(frame.payload for frame in frames) == b"range-marker"
        call = stub.Download(pb.DownloadRequest(workspace=workspace, transfer_id=tid))
        await call.read()
        call.cancel()
        await until(lambda: not agent.workspace.transfers.downloads)
        assert agent.workspace.transfers.get(tid)["state"] == "active"
