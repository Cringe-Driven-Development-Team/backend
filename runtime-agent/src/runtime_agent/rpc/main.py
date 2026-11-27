"""Запускайте gRPC-агент и отдельный HTTP listener для health и метрик."""

import asyncio
import contextlib
import logging
import os
import signal
import ssl

from fastapi import FastAPI, Response
from prometheus_client import CONTENT_TYPE_LATEST
from uvicorn import Config, Server

from ..config import Settings
from ..containers import Containers
from ..journal import Journal
from ..monitoring import KernelSampler, observe_loop
from ..observability import AccessLog, configure_logging
from ..runtime import Agent
from .config import RPCSettings
from .server import build_server

log = logging.getLogger("runtime_agent.observability")


class AdminServer(Server):
    """Оставьте обработку сигналов основному gRPC процессу."""

    @contextlib.contextmanager
    def capture_signals(self):
        """Не заменяйте обработчики SIGTERM и SIGINT в admin task."""
        yield


def admin_app(agent, metrics):
    """Предоставьте только состояние агента без payload."""
    app = FastAPI(openapi_url=None, docs_url=None, redoc_url=None)

    @app.get("/livez")
    async def live():
        """Подтвердите, что процесс отвечает на административные запросы."""
        return Response(status_code=200)

    @app.get("/readyz")
    async def ready():
        """Не объявляйте готовность до старта или во время завершения."""
        return Response(status_code=200 if agent.ready and not agent.stopping else 503)

    return AccessLog(metrics.wrap_http(app, {"/livez", "/readyz"}))


def metrics_app(agent, metrics):
    """Предоставьте только GET /metrics после проверки клиентского сертификата."""
    app = FastAPI(openapi_url=None, docs_url=None, redoc_url=None, redirect_slashes=False)

    @app.get("/metrics")
    async def scrape():
        """Верните метрики только данного агента."""
        metrics.observe_kernel(agent.snapshot())
        return Response(metrics.render(), headers={"Content-Type": CONTENT_TYPE_LATEST})

    return AccessLog(metrics.wrap_http(app, {"/metrics"}))


def admin_server(address, app, **tls):
    """Проверьте адрес admin listener и отключите access log Uvicorn."""
    host, separator, port = address.rpartition(":")
    if not separator or not host or not port.isascii() or not port.isdecimal():
        raise ValueError("Invalid ADMIN_ADDR")
    number = int(port)
    if not 0 <= number <= 65535:
        raise ValueError("Invalid ADMIN_ADDR")
    return AdminServer(
        Config(
            app,
            host=host.removeprefix("[").removesuffix("]"),
            port=number,
            access_log=False,
            proxy_headers=False,
            timeout_graceful_shutdown=2,
            **tls,
        )
    )


async def run(settings, rpc_settings, stopped, containers_factory=Containers):
    """Владейте listeners, kernel и SQLite в порядке их запуска и остановки."""
    journal = Journal(settings.state_dir)
    agent = None
    try:
        containers = containers_factory(settings, journal.instance_id)
        agent = Agent(settings, journal, containers)
        rpc, metrics = build_server(rpc_settings, agent)
        sampler = None
        if isinstance(containers, Containers):
            containers.observe_http = metrics.observe_client
            containers.observe_http_body = metrics.observe_client_body
            sampler = KernelSampler(containers, metrics.resources)
            containers.observe_removal = sampler.observe_removal
        await agent.start()
        if not rpc.add_secure_port(rpc_settings.address, rpc_settings.credentials()):
            raise RuntimeError("Cannot listen on agent gRPC address")
        servers = [admin_server(rpc_settings.admin_address, admin_app(agent, metrics))]
        if rpc_settings.metrics_address:
            servers.append(
                admin_server(
                    rpc_settings.metrics_address,
                    metrics_app(agent, metrics),
                    ssl_keyfile=rpc_settings.metrics_key_file,
                    ssl_certfile=rpc_settings.metrics_cert_file,
                    ssl_ca_certs=rpc_settings.metrics_ca_file,
                    ssl_cert_reqs=ssl.CERT_REQUIRED,
                )
            )
        await rpc.start()
        async with asyncio.TaskGroup() as tasks:
            listeners = [tasks.create_task(server.serve()) for server in servers]
            stop_task = tasks.create_task(stopped.wait())
            rpc_task = tasks.create_task(rpc.wait_for_termination())
            collectors = [tasks.create_task(observe_loop(metrics.loop_lag))]
            if sampler is not None:
                collectors.append(tasks.create_task(sampler.run()))
            try:
                done, _ = await asyncio.wait(
                    (*listeners, stop_task, rpc_task), return_when=asyncio.FIRST_COMPLETED
                )
                if stop_task not in done:
                    raise RuntimeError("Agent listener stopped unexpectedly")
            finally:
                for server in servers:
                    server.should_exit = True
                for collector in collectors:
                    collector.cancel()
                agent.stopping = True
                stop_task.cancel()
                await rpc.stop(rpc_settings.shutdown_seconds)
    finally:
        try:
            if agent is not None:
                await agent.close()
        finally:
            journal.close()


async def serve():
    """Свяжите настройки и системные сигналы со стартом процесса."""
    stopped = asyncio.Event()
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, stopped.set)
    await run(Settings.from_env(), RPCSettings.from_env(), stopped)


def main():
    """Установите файловую маску и завершите процесс безопасной ошибкой при отказе."""
    os.umask(0o077)
    configure_logging(os.environ.get("LOG_LEVEL", "info"))
    try:
        asyncio.run(serve())
    except (OSError, ValueError, RuntimeError, ExceptionGroup):
        log.error("agent.startup.failed")
        raise SystemExit(1) from None


if __name__ == "__main__":
    main()
