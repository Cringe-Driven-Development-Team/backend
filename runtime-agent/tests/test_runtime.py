import asyncio
import json
from dataclasses import replace
from uuid import uuid4

import pytest

from runtime_agent.models import AgentError

from .conftest import command, new_session, until


async def test_idempotency_and_both_completion_orders(env):
    agent, backend, _ = env
    sid, session = await new_session(agent)
    first = agent.accept(sid, command())
    assert first["state"] == "QUEUED"
    assert agent.accept(sid, command()) == first
    with pytest.raises(AgentError) as error:
        agent.accept(sid, command(code="different"))
    assert error.value.status == 409
    kernel = backend.kernels[sid]
    await until(lambda: kernel.sent)
    kernel.emit(kernel.sent[-1][2], "execute_reply", {"status": "ok"}, "shell")
    await asyncio.sleep(0.01)
    assert agent.journal.execution(sid, "run-1")["state"] == "RUNNING"
    kernel.emit(kernel.sent[-1][2], "status", {"execution_state": "idle"})
    await until(lambda: session.active is None)
    agent.accept(sid, command("run-2"))
    await until(lambda: len(kernel.sent) == 2)
    kernel.complete(idle_first=True)
    await until(lambda: session.active is None)
    assert agent.journal.execution(sid, "run-2")["state"] == "SUCCEEDED"
    assert len(kernel.sent) == 2
    await session.finish()
    assert agent.accept(sid, command())["state"] == "SUCCEEDED"


async def test_queue_capacity_and_drain(env):
    agent, backend, _ = env
    sid, session = await new_session(agent)
    agent.accept(sid, command())
    await until(lambda: session.active)
    for i in range(100):
        agent.accept(sid, command(f"q-{i}"))
    with pytest.raises(AgentError) as error:
        agent.accept(sid, command("overflow"))
    assert error.value.status == 429
    with pytest.raises(AgentError) as error:
        await session.restart()
    assert error.value.status == 409
    await session.drain()
    await until(lambda: session.active is None)
    assert agent.journal.execution(sid, "run-1")["state"] == "INTERRUPTED"
    assert agent.journal.execution(sid, "q-99")["state"] == "CANCELLED"
    assert not session.queue.qsize()
    assert not agent.snapshot(sid)["ready"]
    assert len(backend.kernels[sid].sent) == 1
    with pytest.raises(AgentError) as error:
        agent.accept(sid, command("new"))
    assert error.value.status == 503


async def test_late_output_and_cumulative_limit(env):
    agent, backend, _ = env
    agent.settings = replace(agent.settings, max_output_bytes=450)
    sid, session = await new_session(agent)
    agent.accept(sid, command())
    kernel = backend.kernels[sid]
    await until(lambda: kernel.sent)
    old_parent = kernel.sent[0][2]
    kernel.complete()
    await until(lambda: session.active is None)
    agent.accept(sid, command("run-2"))
    await until(lambda: len(kernel.sent) == 2)
    kernel.emit(old_parent, "stream", {"name": "stdout", "text": "late"})
    kernel.emit(old_parent, "stream", {"name": "stdout", "text": "x" * 500})
    kernel.emit(old_parent, "stream", {"name": "stdout", "text": "x" * 500})
    kernel.emit("unknown", "stream", {"name": "stdout", "text": "background"})
    kernel.complete()
    await until(lambda: session.active is None)
    events = agent.journal.events(sid, 0)
    late = next(e for e in events if e.get("content", {}).get("text") == "late")
    assert late["execution_id"] == "run-1"
    assert len([e for e in events if e["kind"] == "output_truncated"]) == 1
    assert any(e["kind"] == "unattributed_output" and e["execution_id"] is None for e in events)


@pytest.mark.parametrize("respond,state", [(True, "TIMED_OUT"), (False, "UNKNOWN")])
async def test_timeout(env, respond, state):
    agent, backend, _ = env
    sid, session = await new_session(agent)
    backend.kernels[sid].respond_to_interrupt = respond
    agent.accept(sid, command(timeout=0.01))
    await until(lambda: agent.journal.execution(sid, "run-1")["state"] == state)
    if not respond:
        await until(lambda: session.closed)
        assert sid in backend.removed


async def test_channel_loss_cancels_queue_without_replay(env):
    agent, backend, _ = env
    sid, session = await new_session(agent)
    agent.accept(sid, command())
    await until(lambda: session.active)
    agent.accept(sid, command("queued"))
    backend.kernels[sid].messages.put_nowait(ConnectionError())
    await until(lambda: session.closed)
    assert agent.journal.execution(sid, "run-1")["state"] == "UNKNOWN"
    assert agent.journal.execution(sid, "queued")["state"] == "CANCELLED"
    assert agent.snapshot(sid)["reason"] == "channel_lost"


async def test_restart_keeps_journal_changes_generation(env):
    agent, _, _ = env
    sid, session = await new_session(agent)
    generation = session.generation
    cursor = agent.journal.events(sid, 0)[-1]["seq"]
    await session.restart()
    assert generation != session.generation
    assert agent.journal.events(sid, cursor)[-1]["seq"] > cursor


async def test_idle_clock_does_not_kill_active_execution(env):
    agent, backend, _ = env
    sid, session = await new_session(agent)
    agent.accept(sid, command())
    await until(lambda: session.active)
    session.last_activity -= 1000
    await agent.check_sessions()
    assert not session.closed
    backend.kernels[sid].complete()
    await until(lambda: session.active is None)
    await agent.check_sessions()
    assert not session.closed
    session.last_activity -= 1000
    await agent.check_sessions()
    assert session.closed
    assert agent.snapshot(sid)["reason"] == "idle_timeout"


async def test_starting_session_reserves_capacity_and_stop_waits(env):
    agent, backend, _ = env
    backend.start_gate = asyncio.Event()
    sid = str(uuid4())
    agent.create(sid, "python")
    with pytest.raises(AgentError) as error:
        agent.create(str(uuid4()), "r")
    assert error.value.status == 429
    stopping = asyncio.create_task(agent.stop_session(sid))
    await asyncio.sleep(0.01)
    assert not stopping.done()
    backend.start_gate.set()
    await stopping
    assert agent.sessions[sid].closed
    assert sid in backend.removed


async def test_cleanup_failure_keeps_slot_until_retry(env):
    agent, backend, _ = env
    sid, session = await new_session(agent)
    backend.fail_remove = True
    await session.finish()
    assert not session.closed
    assert agent.used_slots() == 1
    assert agent.snapshot(sid)["reason"] == "cleanup_failed"
    backend.fail_remove = False
    await agent.check_sessions()
    assert session.closed
    assert agent.used_slots() == 0


@pytest.mark.parametrize("language", ["python", "r"])
async def test_inspection_is_queued_and_returns_common_shape(env, language):
    agent, backend, _ = env
    sid, session = await new_session(agent, language)
    agent.accept(sid, command())
    agent.accept(sid, {"execution_id": "inspect", "kind": "inspect", "timeout_seconds": 5.0})
    kernel = backend.kernels[sid]
    await until(lambda: kernel.sent)
    assert len(kernel.sent) == 1
    kernel.complete()
    await until(lambda: len(kernel.sent) == 2)
    values = [{"name": "x", "type": "int", "preview": "42"}]
    if language == "python":
        kernel.complete(
            user_expressions={
                "variables": {"status": "ok", "data": {"text/plain": repr(json.dumps(values))}}
            }
        )
    else:
        kernel.emit(kernel.sent[-1][2], "stream", {"name": "stdout", "text": json.dumps(values)})
        kernel.complete()
    await until(lambda: session.active is None)
    assert agent.journal.execution(sid, "inspect")["result"] == {"variables": values}
    assert not kernel.sent[-1][1]["store_history"]


async def test_unconfirmed_manual_interrupt_stops_session(env):
    agent, backend, _ = env
    sid, session = await new_session(agent)
    backend.kernels[sid].respond_to_interrupt = False
    agent.accept(sid, command())
    await until(lambda: session.active)
    await session.interrupt()
    assert session.closed
    assert agent.journal.execution(sid, "run-1")["state"] == "UNKNOWN"


async def test_start_failure_releases_capacity(env):
    agent, backend, _ = env
    backend.fail_start = True
    sid, session = await new_session(agent)
    assert session.closed
    assert agent.snapshot(sid)["reason"] == "startup_failed"
    assert agent.used_slots() == 0
    backend.fail_start = False
    _, healthy = await new_session(agent)
    assert healthy.ready


async def test_no_cross_session_state_and_stop_cancels_only_its_queue(env):
    agent, backend, _ = env
    agent.settings = replace(agent.settings, max_sessions=2)
    sid, first = await new_session(agent)
    other, second = await new_session(agent, "r")
    for session_id in (sid, other):
        agent.accept(session_id, command())
    await until(lambda: first.active and second.active)
    agent.accept(sid, command("queued"))
    await first.finish()
    assert agent.journal.execution(sid, "queued")["state"] == "CANCELLED"
    assert second.ready
    backend.kernels[other].complete()
    await until(lambda: second.active is None)
    assert agent.journal.execution(other, "run-1")["state"] == "SUCCEEDED"


async def test_delayed_interrupt_cannot_hit_next_cell(env):
    agent, backend, _ = env
    sid, session = await new_session(agent)
    kernel = backend.kernels[sid]
    agent.accept(sid, command())
    agent.accept(sid, command("next"))
    await until(lambda: kernel.sent)
    entered = asyncio.Event()
    release = asyncio.Event()

    async def delayed_interrupt():
        entered.set()
        await release.wait()

    kernel.interrupt = delayed_interrupt
    interrupting = asyncio.create_task(session.interrupt())
    await entered.wait()
    kernel.complete()
    await until(lambda: session.pending and session.pending["execution_id"] == "next")
    assert len(kernel.sent) == 1
    assert session.queued() == 1
    release.set()
    await interrupting
    await until(lambda: len(kernel.sent) == 2)
    kernel.complete()
    await until(lambda: session.active is None)
    assert agent.journal.execution(sid, "next")["state"] == "SUCCEEDED"
