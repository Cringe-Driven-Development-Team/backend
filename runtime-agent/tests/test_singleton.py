"""A VPS is assigned once; reboot never replays or creates a new kernel."""

from dataclasses import replace

import pytest

from runtime_agent.api import create_app
from runtime_agent.config import Settings

from .conftest import AUTH, FakeContainers, command, until


async def test_unprefixed_api_and_no_session_creation(env):
    agent, backend, client = env
    assert (await client.get("/health")).json()["session_id"] == agent.settings.session_id
    assert (await client.post("/sessions", json={})).status_code == 404
    assert (
        await client.post("/executions", json={"execution_id": "a", "code": "1"})
    ).status_code == 202
    kernel = backend.kernels[agent.settings.session_id]
    await until(lambda: kernel.sent)
    kernel.complete()
    await until(lambda: agent.session.active is None)
    assert (await client.get("/executions/a")).json()["state"] == "SUCCEEDED"
    assert (await client.post("/runtime/stop")).json()["stopped"]
    assert (await client.get("/health")).status_code == 503
    assert (await client.get("/executions/a")).json()["state"] == "SUCCEEDED"


async def test_reboot_retains_journal_without_starting_kernel(settings):
    first = FakeContainers()
    app = create_app(settings, lambda *_: first)
    async with app.router.lifespan_context(app):
        app.state.agent.accept(command())
        await until(lambda: app.state.agent.session.active)
    second = FakeContainers()
    app = create_app(settings, lambda *_: second)
    async with app.router.lifespan_context(app):
        assert second.kernels == {}
        assert app.state.agent.snapshot()["state"] == "STOPPED"
        assert app.state.agent.journal.execution(settings.session_id, "run-1")["state"] == "UNKNOWN"


async def test_reusing_state_for_another_assignment_fails(settings):
    app = create_app(settings, FakeContainers)
    async with app.router.lifespan_context(app):
        pass
    other = replace(settings, session_id="00000000-0000-0000-0000-000000000099")
    app = create_app(other, FakeContainers)
    with pytest.raises(RuntimeError, match="another session"):
        async with app.router.lifespan_context(app):
            pass


def test_assignment_configuration_is_required(monkeypatch):
    monkeypatch.setenv("RUNTIME_TOKEN", AUTH["Authorization"].removeprefix("Bearer "))
    for name in ("SESSION_ID", "KERNEL_LANGUAGE", "KERNEL_NETWORK"):
        monkeypatch.delenv(name, raising=False)
    with pytest.raises(ValueError):
        Settings.from_env()


async def test_failed_container_start_leaves_readable_failed_runtime(settings):
    backend = FakeContainers()
    backend.fail_start = True
    app = create_app(settings, lambda *_: backend)
    async with app.router.lifespan_context(app):
        snapshot = app.state.agent.snapshot()
        assert snapshot["state"] == "FAILED"
        assert snapshot["reason"] == "startup_failed"
        assert not snapshot["ready"]


@pytest.mark.parametrize(
    "changes",
    [
        {"language": "julia"},
        {"session_id": "invalid"},
        {"kernel_network": "bridge"},
        {"kernel_network": "host"},
        {"kernel_network": "none"},
        {"kernel_dns": "::1"},
        {"max_output_bytes": 0},
    ],
)
def test_invalid_assignment_is_rejected(settings, changes):
    with pytest.raises(ValueError):
        replace(settings, **changes)
