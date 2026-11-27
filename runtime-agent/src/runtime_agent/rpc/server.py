"""Собирайте защищённый gRPC server вокруг существующего прикладного агента."""

import asyncio

import grpc
from grpc_health.v1 import health_pb2, health_pb2_grpc

from ..generated.cellestial.agent.v1 import agent_pb2_grpc
from ..generated.cellestial.files.v1 import files_pb2_grpc
from .control import Control
from .files import Files
from .interceptors import Transport
from .metrics import Metrics


def build_server(settings, agent):
    """Верните server/registry; caller владеет listen/start/stop и закрытием Agent."""
    metrics = Metrics(settings.version)
    server = grpc.aio.server(
        interceptors=(Transport(settings, metrics),),
        options=(
            ("grpc.max_receive_message_length", 8 << 20),
            ("grpc.max_send_message_length", 8 << 20),
            ("grpc.max_metadata_size", 16 << 10),
        ),
        maximum_concurrent_rpcs=128,
    )
    agent_pb2_grpc.add_ControlServicer_to_server(Control(agent), server)
    files_pb2_grpc.add_FilesServicer_to_server(Files(agent), server)
    health_service = AgentHealth(agent)
    health_pb2_grpc.add_HealthServicer_to_server(health_service, server)
    return server, metrics


class AgentHealth(health_pb2_grpc.HealthServicer):
    """Читайте ту же готовность, что admin listener, включая shutdown/degraded Agent."""

    def __init__(self, agent):
        """Свяжите health с живым Agent, без кеширования положительного результата."""
        self.agent = agent

    def state(self):
        """Верните NOT_SERVING, если агент не запущен или уже останавливается."""
        return (
            health_pb2.HealthCheckResponse.SERVING
            if self.agent.ready and not self.agent.stopping
            else health_pb2.HealthCheckResponse.NOT_SERVING
        )

    async def Check(self, request, context):
        """Проверьте целый процесс; незарегистрированное имя сервиса не считается ready."""
        if request.service not in ("", "cellestial.agent.v1.Control", "cellestial.files.v1.Files"):
            await context.abort(grpc.StatusCode.NOT_FOUND, "health service not found")
        return health_pb2.HealthCheckResponse(status=self.state())

    async def Watch(self, request, context):
        """Публикуйте изменения готовности до отмены или server deadline."""
        previous = None
        while True:
            state = (
                self.state()
                if request.service
                in ("", "cellestial.agent.v1.Control", "cellestial.files.v1.Files")
                else health_pb2.HealthCheckResponse.SERVICE_UNKNOWN
            )
            if previous != state:
                yield health_pb2.HealthCheckResponse(status=state)
                previous = state
            await asyncio.sleep(0.2)
