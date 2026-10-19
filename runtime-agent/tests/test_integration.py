"""Real protocol checks; enable explicitly so unit tests need no local daemon."""

import asyncio
import os
import signal
import sys
from dataclasses import replace
from pathlib import Path

import pytest

from runtime_agent.api import create_app
from runtime_agent.containers import Containers
from runtime_agent.jupyter import Jupyter

from .conftest import command, new_session


async def completed(agent, sid, eid):
    async with asyncio.timeout(30):
        while True:
            result = agent.journal.execution(sid, eid)
            if result["state"] not in ("QUEUED", "RUNNING"):
                return result
            await asyncio.sleep(0.02)


def all_events(agent, sid):
    events = []
    after = 0
    while batch := agent.journal.events(sid, after):
        events.extend(batch)
        after = batch[-1]["seq"]
    return events


class LocalJupyter:
    """Exercise the production transport against a real server without Docker."""

    def __init__(self, settings, instance_id):
        self.settings = settings
        self.processes = {}
        self.logs = {}

    async def ping(self):
        return True

    async def recover(self):
        pass

    async def start(self, sid, language):
        directory = self.settings.socket_dir / sid
        directory.mkdir()
        config = Path(__file__).parents[1] / "images/jupyter_server_config.py"
        log = self.logs[sid] = (directory / "server.log").open("w+")
        environment = {
            **os.environ,
            "JUPYTER_TOKEN": "local-test-only",
            "JUPYTER_RUNTIME_DIR": str(directory / "runtime"),
            "JUPYTER_CONFIG_DIR": str(directory / "config"),
        }
        self.processes[sid] = await asyncio.create_subprocess_exec(
            sys.executable,
            "-m",
            "jupyter_server",
            f"--config={config}",
            f"--ServerApp.sock={directory / 'jupyter.sock'}",
            f"--ServerApp.root_dir={directory}",
            "--ServerApp.allow_root=True",
            env=environment,
            stdout=log,
            stderr=asyncio.subprocess.STDOUT,
            start_new_session=True,
        )
        transport = Jupyter(directory / "jupyter.sock", "local-test-only", language)
        try:
            await transport.start(30)
            return transport
        except BaseException:
            await transport.close()
            log.seek(0)
            print(log.read())
            await self.remove(sid)
            raise

    async def alive(self, sid):
        return self.processes[sid].returncode is None

    async def remove(self, sid):
        process = self.processes.pop(sid, None)
        if process and process.returncode is None:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                await asyncio.wait_for(process.wait(), 5)
            except TimeoutError:
                os.killpg(process.pid, signal.SIGKILL)
                await process.wait()
        if sid in self.logs:
            self.logs.pop(sid).close()

    async def close(self):
        for sid in list(self.processes):
            await self.remove(sid)


@pytest.fixture(params=["local", "docker"])
async def real_agent(settings, request):
    backend = request.param
    if backend == "local":
        if os.environ.get("RUN_JUPYTER_TESTS") != "1":
            pytest.skip("Set RUN_JUPYTER_TESTS=1 for local Jupyter integration")
        factory = LocalJupyter
    else:
        if os.environ.get("RUN_DOCKER_TESTS") != "1":
            pytest.skip("Set RUN_DOCKER_TESTS=1 for Docker integration")
        factory = Containers
    settings = replace(settings, max_sessions=2, interrupt_seconds=5, monitor_seconds=0.1)
    app = create_app(settings, factory)
    async with app.router.lifespan_context(app):
        yield app.state.agent, backend


@pytest.mark.jupyter
@pytest.mark.docker
@pytest.mark.parametrize("language", ["python", "r"])
async def test_real_execution_inspection_interrupt_restart(real_agent, language):
    agent, backend = real_agent
    if backend == "local" and language == "r":
        pytest.skip("R is provided by the Docker image")
    sid, session = await new_session(agent, language)
    assert session.ready, agent.snapshot(sid)
    code = "x = 42\nprint(x)\nx * 2" if language == "python" else "x <- 42; print(x); x * 2"
    agent.accept(sid, command(code=code))
    assert (await completed(agent, sid, "run-1"))["state"] == "SUCCEEDED"
    assert agent.accept(sid, command(code=code))["state"] == "SUCCEEDED"
    agent.accept(sid, {"execution_id": "inspect", "kind": "inspect", "timeout_seconds": 5.0})
    variables = await completed(agent, sid, "inspect")
    assert variables["state"] == "SUCCEEDED", variables
    x = next(v for v in variables["result"]["variables"] if v["name"] == "x")
    assert x["preview"] == "42"
    agent.accept(sid, command("next", "x + 1"))
    assert (await completed(agent, sid, "next"))["state"] == "SUCCEEDED"
    error_code = "raise ValueError('expected')" if language == "python" else "stop('expected')"
    agent.accept(sid, command("error", error_code))
    assert (await completed(agent, sid, "error"))["state"] == "FAILED"
    rich = (
        "from IPython.display import display\ndisplay({'text/html': '<b>hi</b>'}, raw=True)"
        if language == "python"
        else "IRdisplay::display_html('<b>hi</b>')"
    )
    agent.accept(sid, command("rich", rich))
    assert (await completed(agent, sid, "rich"))["state"] == "SUCCEEDED"
    assert any(
        e.get("content", {}).get("data", {}).get("text/html") == "<b>hi</b>"
        for e in all_events(agent, sid)
    )
    sleep = "import time; time.sleep(20)" if language == "python" else "Sys.sleep(20)"
    agent.accept(sid, command("timeout", sleep, 1.0))
    assert (await completed(agent, sid, "timeout"))["state"] == "TIMED_OUT"
    agent.accept(sid, command("interrupt", sleep))
    async with asyncio.timeout(5):
        while session.active is None:
            await asyncio.sleep(0.01)
    await asyncio.sleep(0.2)
    await session.interrupt()
    assert (await completed(agent, sid, "interrupt"))["state"] == "INTERRUPTED"
    generation = session.generation
    await session.restart()
    assert generation != session.generation
    agent.accept(sid, command("cleared", "x"))
    assert (await completed(agent, sid, "cleared"))["state"] == "FAILED"
    # IRkernel replaces quit()/q() with an ask_exit payload for the frontend;
    # kill the process directly to exercise kernel death before an execute_reply.
    crash = (
        "import os; os._exit(1)"
        if language == "python"
        else "tools::pskill(Sys.getpid(), signal = 9L)"
    )
    agent.accept(sid, command("crash", crash))
    assert (await completed(agent, sid, "crash"))["state"] == "UNKNOWN"
    async with asyncio.timeout(10):
        while not session.closed:
            await asyncio.sleep(0.01)
    assert session.closed
    assert agent.snapshot(sid)["reason"] == "kernel_died"
    assert all_events(agent, sid)[-1]["kind"] == "runtime_stopped"


@pytest.mark.docker
async def test_docker_isolation_limits_and_kernel_death(settings):
    if os.environ.get("RUN_DOCKER_TESTS") != "1":
        pytest.skip("Set RUN_DOCKER_TESTS=1")
    settings = replace(settings, max_sessions=2, monitor_seconds=0.1)
    app = create_app(settings)
    async with app.router.lifespan_context(app):
        agent = app.state.agent
        sid, first = await new_session(agent)
        other, second = await new_session(agent)
        assert first.ready and second.ready
        container = await asyncio.to_thread(
            agent.containers.client.containers.get, agent.containers.name(sid)
        )
        config = container.attrs["HostConfig"]
        assert config["NetworkMode"] == "none"
        assert config["ReadonlyRootfs"]
        assert config["Memory"] == config["MemorySwap"] == 1024**3
        assert config["NanoCpus"] == 1_000_000_000
        assert config["PidsLimit"] == 128
        code = """import os, socket
from pathlib import Path
assert 'RUNTIME_TOKEN' not in os.environ
assert not Path('/var/run/docker.sock').exists()
assert not Path('/var/lib/cellestial-agent').exists()
try:
    socket.create_connection(('1.1.1.1', 443), timeout=1)
except OSError:
    pass
else:
    raise AssertionError('network must be disabled')
x = 42
Path('/workspace/only-first').write_text('private')
"""
        agent.accept(sid, command(code=code))
        assert (await completed(agent, sid, "run-1"))["state"] == "SUCCEEDED"
        agent.accept(
            other,
            command(
                code="from pathlib import Path\n"
                "assert not Path('/workspace/only-first').exists()\n"
                "assert 'x' not in globals()"
            ),
        )
        assert (await completed(agent, other, "run-1"))["state"] == "SUCCEEDED"
        agent.accept(sid, command("oom", "blob = bytearray(2 * 1024**3)"))
        assert (await completed(agent, sid, "oom"))["state"] == "UNKNOWN"
        async with asyncio.timeout(30):
            while not first.closed:
                await asyncio.sleep(0.05)
        assert second.ready


@pytest.mark.docker
async def test_docker_recovery_removes_only_owned_containers(settings):
    if os.environ.get("RUN_DOCKER_TESTS") != "1":
        pytest.skip("Set RUN_DOCKER_TESTS=1")
    from runtime_agent.journal import Journal

    journal = Journal(settings.state_dir)
    backend = Containers(settings, journal.instance_id)
    agent = None
    foreign = None
    try:
        from runtime_agent.runtime import Agent

        agent = Agent(settings, journal, backend)
        await agent.start()
        foreign = await asyncio.to_thread(
            backend.client.containers.run,
            settings.python_image,
            ["sleep", "60"],
            detach=True,
            network_mode="none",
            labels={"ru.cellestial.runtime-agent": "another-agent"},
        )
        sid, session = await new_session(agent)
        assert session.ready
        agent.accept(sid, command(code="import time; time.sleep(20)"))
        async with asyncio.timeout(5):
            while not session.active:
                await asyncio.sleep(0.01)
        # Simulate an ungraceful agent loss without cleaning its container/state.
        agent.monitor.cancel()
        session.worker.cancel()
        session.reader.cancel()
        await asyncio.gather(agent.monitor, session.worker, session.reader, return_exceptions=True)
        await session.transport.close()
        journal.transition(sid, "run-1", "RUNNING")
        await backend.recover()
        journal.recover()
        await asyncio.to_thread(foreign.reload)
        assert foreign.status == "running"
        assert not await backend.alive(sid)
        assert journal.execution(sid, "run-1")["state"] == "UNKNOWN"
        assert journal.session(sid)["reason"] == "agent_restarted"
        session.closed = True
    finally:
        if foreign is not None:
            await asyncio.to_thread(foreign.remove, force=True, v=True)
        if agent:
            await agent.close()
        journal.close()
