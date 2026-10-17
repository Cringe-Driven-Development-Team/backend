"""Coordinate per-session execution queues, lifecycle operations, and recovery."""

import asyncio
import json
import logging
import time
import uuid
from contextlib import suppress
from dataclasses import dataclass, field

from . import inspection
from .journal import encode
from .models import AgentError

log = logging.getLogger(__name__)
OUTPUT_TYPES = {
    "stream",
    "display_data",
    "execute_result",
    "update_display_data",
    "clear_output",
    "error",
    "comm_open",
    "comm_msg",
    "comm_close",
}


class KernelDiedError(ConnectionError):
    """Signal that Jupyter reported a dead kernel."""

    pass


@dataclass
class Active:
    """Track completion of one dispatched execution.

    Attributes:
        command (dict): Validated execution or inspection request.
        msg_id (str): Jupyter request ID used to match incoming messages.
        done (asyncio.Event): Set after both execute_reply and matching idle status
            arrive.
        reply (dict | None): Received execute_reply content, if any.
        idle (bool): Whether a matching idle status has been received.
        interrupted (bool): Whether manual interruption was requested.
        stdout (str): Captured stdout used to decode R variable inspection.
    """

    command: dict
    msg_id: str
    done: asyncio.Event = field(default_factory=asyncio.Event)
    reply: dict | None = None
    idle: bool = False
    interrupted: bool = False
    stdout: str = ""


class Session:
    """Serialize execution and control operations for one isolated kernel.

    The queue worker and control methods share a lock for dispatch and interrupts.
    Persisted message correlations also associate late output with completed commands.
    """

    def __init__(self, agent, session_id, language):
        """Initialize session state without starting its container.

        Args:
            agent (Agent): Owning coordinator and shared infrastructure.
            session_id (str): Canonical UUID of the session.
            language (str): Validated kernel language, python or r.
        """
        self.agent = agent
        self.id = session_id
        self.language = language
        self.journal = agent.journal
        self.settings = agent.settings
        self.lock = asyncio.Lock()
        self.queue = asyncio.Queue(maxsize=100)
        self.transport = None
        self.generation = None
        self.active = None
        self.pending = None
        self.worker = None
        self.reader = None
        self.starter = None
        self.ready = False
        self.draining = False
        self.closed = False
        self.unattributed_bytes = 0
        self.unattributed_truncated = False
        self.last_activity = time.monotonic()

    def touch(self):
        """Refresh both monotonic and persisted activity timestamps."""
        self.last_activity = time.monotonic()
        self.journal.touch(self.id)

    def queued(self):
        """Count waiting commands, including one awaiting the control lock.

        Returns:
            int: Number of accepted commands that have not started running.
        """
        return self.queue.qsize() + int(self.pending is not None and self.active is None)

    async def start(self):
        """Start the kernel, record readiness, and launch reader and worker tasks.

        Startup errors are logged and converted to a FAILED session with cleanup attempted.
        """
        try:
            async with self.lock:
                self.transport = await self.agent.containers.start(self.id, self.language)
                self.generation = str(uuid.uuid4())
                self.ready = True
                self.touch()
                self.journal.session_state(self.id, "READY", generation=self.generation)
                self.journal.event(self.id, "kernel_started", generation=self.generation)
                self.reader = asyncio.create_task(self.read(), name=f"reader:{self.id}")
                self.worker = asyncio.create_task(self.run(), name=f"worker:{self.id}")
        except Exception:
            log.exception("Session startup failed: %s", self.id)
            await self.finish("FAILED", "startup_failed")
        finally:
            self.agent.notify()

    def accept(self, command):
        """Return an existing command or durably enqueue a new one.

        Args:
            command (dict): Validated command including execution_id, kind, and
                timeout_seconds.

        Returns:
            dict: Execution ID, state, and decoded result, if available.

        Raises:
            AgentError: The ID conflicts (409), the queue is full (429), or the session is
                unavailable (503).
        """
        if self.journal.existing(self.id, command):
            return self.journal.execution(self.id, command["execution_id"])
        if not self.ready or self.draining or self.agent.stopping:
            raise AgentError(503, "Session is not accepting commands")
        if self.queued() >= 100:
            raise AgentError(429, "Execution queue is full")
        self.journal.accept(self.id, command)
        self.queue.put_nowait(command)
        self.touch()
        self.agent.notify()
        return self.journal.execution(self.id, command["execution_id"])

    async def read(self):
        """Consume kernel messages until cancellation or a channel failure.

        Kernel death and malformed or lost channels fail the session and trigger cleanup.
        """
        try:
            while True:
                self.on_message(await self.transport.receive())
        except asyncio.CancelledError:
            raise
        except KernelDiedError:
            self.ready = False
            await self.finish("FAILED", "kernel_died")
        except Exception:
            log.exception("Kernel channel failed: %s", self.id)
            self.ready = False
            self.journal.event(self.id, "channel_failed", generation=self.generation)
            await self.finish("FAILED", "channel_lost")

    def on_message(self, msg):
        """Journal a kernel message and update its active execution tracker.

        Args:
            msg (dict): Decoded Jupyter message with channel, header, parent_header, and
                content.

        Raises:
            KernelDiedError: Jupyter reports that the kernel is dead.
            ValueError: Inspection stdout exceeds its 1 MiB limit or serialization fails.
            KeyError: A required protocol field is missing.
        """
        parent = msg.get("parent_header", {}).get("msg_id")
        kind = msg["header"]["msg_type"]
        channel, content = msg["channel"], msg["content"]
        if kind == "status" and content.get("execution_state") == "dead":
            self.journal.event(self.id, "kernel_died", generation=self.generation)
            raise KernelDiedError("Kernel died")
        row = self.journal.parent(self.id, self.generation, parent)
        payload = {
            "channel": channel,
            "msg_type": kind,
            "content": content,
            "metadata": msg.get("metadata", {}),
            "parent_msg_id": parent,
            "generation": self.generation,
        }
        if msg.get("buffers_base64"):
            payload["buffers_base64"] = msg["buffers_base64"]
        if row is not None:
            inspect = json.loads(row["command"])["kind"] == "inspect"
            if not inspect:
                if kind in OUTPUT_TYPES:
                    self.journal.output(self.id, row, payload, self.settings.max_output_bytes)
                else:
                    self.journal.event(self.id, "kernel_message", row["id"], **payload)
            active = self.active
            if active is not None and active.msg_id == parent:
                if inspect and kind == "stream" and content.get("name") == "stdout":
                    if len(active.stdout.encode()) + len(content["text"].encode()) > 1024 * 1024:
                        raise ValueError("Inspection output exceeds 1 MiB")
                    active.stdout += content["text"]
                if channel == "shell" and kind == "execute_reply":
                    active.reply = content
                if channel == "iopub" and kind == "status":
                    active.idle |= content.get("execution_state") == "idle"
                if active.reply is not None and active.idle:
                    active.done.set()
        elif channel == "iopub" and kind in OUTPUT_TYPES:
            size = len(encode(payload).encode())
            if (
                self.unattributed_bytes + size <= self.settings.max_output_bytes
                and not self.unattributed_truncated
            ):
                self.unattributed_bytes += size
                self.journal.event(self.id, "unattributed_output", **payload)
            elif not self.unattributed_truncated:
                self.unattributed_truncated = True
                self.journal.event(
                    self.id,
                    "output_truncated",
                    generation=self.generation,
                    limit=self.settings.max_output_bytes,
                )
        self.agent.notify()

    async def run(self):
        """Execute queued commands sequentially and persist their final states.

        Completion requires both reply and idle messages. Unconfirmed timeouts or execution
        failures stop the session; cancellation records UNKNOWN for dispatched work.
        """
        while True:
            command = await self.queue.get()
            self.pending = command
            execution_id = command["execution_id"]
            failure = None
            try:
                # Dispatch and control share a lock: a delayed interrupt must never
                # hit the next cell after the previous cell completes naturally.
                async with self.lock:
                    if not self.ready or self.draining:
                        self.journal.transition(
                            self.id, execution_id, "CANCELLED", {"reason": "session_unavailable"}
                        )
                        continue
                    active = Active(command, str(uuid.uuid4()))
                    self.active = active
                    self.journal.transition(self.id, execution_id, "RUNNING")
                    # Correlation is durable before sending an execution request.
                    self.journal.dispatched(self.id, execution_id, active.msg_id, self.generation)
                    await asyncio.wait_for(
                        self.transport.send(
                            "execute_request",
                            inspection.request_content(command, self.language),
                            active.msg_id,
                        ),
                        10,
                    )
                timed_out = False
                try:
                    await asyncio.wait_for(active.done.wait(), command["timeout_seconds"])
                except TimeoutError:
                    timed_out = True
                    async with self.lock:
                        if not active.done.is_set():
                            await self.transport.interrupt()
                            await asyncio.wait_for(
                                active.done.wait(), self.settings.interrupt_seconds
                            )
                reply = active.reply
                if timed_out:
                    state, result = "TIMED_OUT", reply
                elif active.interrupted:
                    state, result = "INTERRUPTED", reply
                else:
                    state = "SUCCEEDED" if reply["status"] == "ok" else "FAILED"
                    result = reply
                if command["kind"] == "inspect" and state == "SUCCEEDED":
                    try:
                        result = inspection.result(reply, self.language, active.stdout)
                    except (ValueError, KeyError, SyntaxError):
                        state, result = "FAILED", {"reason": "invalid_inspection_result"}
                self.journal.transition(self.id, execution_id, state, result)
            except asyncio.CancelledError:
                self.journal.transition(
                    self.id,
                    execution_id,
                    "UNKNOWN" if self.active else "CANCELLED",
                    {"reason": "session_stopping"},
                )
                raise
            except Exception:
                log.exception("Execution lost: %s/%s", self.id, execution_id)
                self.ready = False
                self.journal.transition(
                    self.id, execution_id, "UNKNOWN", {"reason": "kernel_result_unavailable"}
                )
                failure = "kernel_result_unavailable"
            finally:
                self.active = None
                self.pending = None
                self.queue.task_done()
                self.touch()
                self.agent.notify()
            if failure:
                await self.finish("FAILED", failure)
                return

    async def interrupt_locked(self):
        """Request an interrupt and await confirmation with the lock held.

        The caller must hold the session control lock so the signal cannot reach a later
        command.

        Raises:
            TimeoutError: The interrupt request or completion confirmation times out.
        """
        active = self.active
        if active is None or active.done.is_set():
            return
        active.interrupted = True
        await self.transport.interrupt()
        await asyncio.wait_for(active.done.wait(), self.settings.interrupt_seconds)

    async def interrupt(self):
        """Interrupt the active command, failing the session if unconfirmed."""
        try:
            async with self.lock:
                self.touch()
                await self.interrupt_locked()
        except Exception:
            await self.finish("FAILED", "interrupt_unconfirmed")

    async def drain(self):
        """Reject new commands, cancel queued work, and interrupt the active command."""
        async with self.lock:
            if self.closed:
                return
            self.draining = True
            self.touch()
            self.journal.session_state(self.id, "DRAINING")
            self.cancel_queued()
        await self.interrupt()
        self.agent.notify()

    def cancel_queued(self):
        """Mark commands still in the queue CANCELLED and release queue counters."""
        while not self.queue.empty():
            command = self.queue.get_nowait()
            self.journal.transition(
                self.id, command["execution_id"], "CANCELLED", {"reason": "session_unavailable"}
            )
            self.queue.task_done()

    async def restart(self):
        """Restart an idle kernel while preserving its workspace and journal.

        Raises:
            AgentError: The session is not ready and idle (409), or the restart fails (503).
        """
        try:
            async with self.lock:
                if (
                    self.closed
                    or self.draining
                    or not self.ready
                    or self.active
                    or self.pending
                    or not self.queue.empty()
                ):
                    raise AgentError(409, "Restart requires a ready, idle session")
                self.ready = False
                self.reader.cancel()
                await asyncio.gather(self.reader, return_exceptions=True)
                await self.transport.restart(self.settings.startup_seconds)
                self.generation = str(uuid.uuid4())
                self.unattributed_bytes = 0
                self.unattributed_truncated = False
                self.journal.session_state(self.id, "READY", generation=self.generation)
                self.journal.event(self.id, "kernel_started", generation=self.generation)
                self.reader = asyncio.create_task(self.read(), name=f"reader:{self.id}")
                self.ready = True
                self.touch()
                self.agent.notify()
        except AgentError:
            raise
        except Exception:
            await self.finish("FAILED", "restart_failed")
            raise AgentError(503, "Kernel restart failed") from None

    async def finish(self, state="STOPPED", reason="requested"):
        """Stop tasks and remove the container while preserving the event journal.

        Repeated calls on a closed session are harmless. Failed container removal leaves the
        session DRAINING and its capacity reserved for a monitor-driven cleanup retry.

        Args:
            state (str): Terminal session state to record after successful cleanup.
            reason (str): Reason for stopping the session.
        """
        async with self.lock:
            if self.closed:
                return
            self.ready = False
            self.draining = True
            tasks = [
                task
                for task in (self.worker, self.reader)
                if task is not None and task is not asyncio.current_task()
            ]
            for task in tasks:
                task.cancel()
            await asyncio.gather(*tasks, return_exceptions=True)
            self.cancel_queued()
            self.journal.unfinished(self.id, "UNKNOWN", reason)
            if self.transport is not None:
                with suppress(Exception):
                    await self.transport.close()
            try:
                await self.agent.containers.remove(self.id)
            except Exception:
                # Keep the slot occupied until cleanup succeeds. The monitor retries.
                log.exception("Container cleanup failed: %s", self.id)
                self.agent.cleanup_pending[self.id] = (state, reason)
                self.journal.session_state(self.id, "DRAINING", "cleanup_failed")
                self.agent.notify()
                return
            self.agent.cleanup_pending.pop(self.id, None)
            self.journal.session_state(self.id, state, reason)
            self.journal.event(self.id, "runtime_stopped", generation=self.generation)
            self.closed = True
            self.agent.notify()


class Agent:
    """Own the session registry, shared journal, and lifecycle monitor."""

    def __init__(self, settings, journal, containers):
        """Initialize the coordinator without recovering or starting sessions.

        Args:
            settings (Settings): Validated agent configuration.
            journal (Journal): Open, exclusively owned persistent journal.
            containers (Containers): Container backend, or an implementation of the same
                interface.
        """
        self.settings = settings
        self.journal = journal
        self.containers = containers
        self.sessions = {}
        self.cleanup_pending = {}
        self.subscribers = set()
        self.stopping = False
        self.ready = False
        self.monitor = None

    def notify(self):
        # Each subscriber owns its wakeup; one reader cannot clear another's signal.
        """Wake each event subscriber independently to avoid lost notifications."""
        for wakeup in self.subscribers:
            wakeup.set()

    async def start(self):
        """Remove stale resources, recover persisted state, and start monitoring.

        Readiness is enabled only after Docker recovery and journal recovery succeed.
        """
        await self.containers.ping()
        await self.containers.recover()
        self.journal.recover()
        self.ready = True
        self.monitor = asyncio.create_task(self.watch(), name="session-monitor")

    def used_slots(self):
        """Count sessions still reserving container capacity.

        Returns:
            int: Number of sessions not yet successfully closed, including startups and
            cleanup retries.
        """
        return sum(not session.closed for session in self.sessions.values())

    def create(self, session_id, language):
        """Reserve capacity and schedule session startup, or return a previous session.

        Args:
            session_id (str): Canonical UUID of the session.
            language (str): Validated kernel language, python or r.

        Returns:
            dict: Current session snapshot; new sessions initially have STARTING state.

        Raises:
            AgentError: The language conflicts (409), capacity is exhausted (429), or the
                agent is unavailable (503).
        """
        try:
            existing = self.journal.session(session_id)
        except AgentError:
            existing = None
        if existing:
            if existing["language"] != language:
                raise AgentError(409, "session_id already used for another language")
            return self.snapshot(session_id)
        if not self.ready or self.stopping:
            raise AgentError(503, "Agent is unavailable")
        if self.used_slots() >= self.settings.max_sessions:
            raise AgentError(429, "Session capacity reached")
        self.journal.create(session_id, language)
        session = Session(self, session_id, language)
        self.sessions[session_id] = session
        session.starter = asyncio.create_task(session.start(), name=f"start:{session_id}")
        return self.snapshot(session_id)

    def snapshot(self, session_id):
        """Combine persisted metadata with current in-memory execution status.

        Args:
            session_id (str): Canonical UUID of the session.

        Returns:
            dict: Session metadata, readiness, draining flag, active execution ID, and queue
            size.

        Raises:
            AgentError: The session does not exist (HTTP 404).
        """
        result = self.journal.session(session_id)
        session = self.sessions.get(session_id)
        return {
            **result,
            "ready": bool(session and session.ready and not session.draining),
            "draining": result["state"] == "DRAINING",
            "active_execution_id": session.active.command["execution_id"]
            if session and session.active
            else None,
            "queued": session.queued() if session else 0,
        }

    def live(self, session_id):
        """Look up an in-memory session that has not been closed.

        Args:
            session_id (str): Canonical UUID of the session.

        Returns:
            Session: Session object, which may still be starting or draining.

        Raises:
            AgentError: The session is unknown (404) or no longer running in this process
                (503).
        """
        self.journal.session(session_id)
        session = self.sessions.get(session_id)
        if session is None or session.closed:
            raise AgentError(503, "Session is stopped")
        return session

    def accept(self, session_id, command):
        """Resolve an idempotent retry or submit a command to a live session.

        Args:
            session_id (str): Canonical UUID of the session.
            command (dict): Validated command including execution_id, kind, and
                timeout_seconds.

        Returns:
            dict: Execution ID, state, and decoded result, if available.

        Raises:
            AgentError: The session is missing (404), the ID conflicts (409), the queue is
                full (429), or the session is unavailable (503).
        """
        self.journal.session(session_id)
        if self.journal.existing(session_id, command):
            return self.journal.execution(session_id, command["execution_id"])
        return self.live(session_id).accept(command)

    async def stop_session(self, session_id):
        """Wait for startup and stop a session without discarding its journal.

        Args:
            session_id (str): Canonical UUID of the session.

        Returns:
            dict: Session snapshot after cleanup has been attempted.

        Raises:
            AgentError: The session does not exist (HTTP 404).
        """
        self.journal.session(session_id)
        session = self.sessions.get(session_id)
        if session:
            # Never cancel a Docker create running in a thread: wait, then remove it.
            await asyncio.shield(session.starter)
            await session.finish()
        return self.snapshot(session_id)

    async def check_sessions(self):
        """Retry cleanup and stop dead or idle sessions during one monitor pass."""
        for session in list(self.sessions.values()):
            if session.closed or not session.starter.done():
                continue
            if session.id in self.cleanup_pending:
                await session.finish(*self.cleanup_pending[session.id])
                continue
            if session.lock.locked():
                continue
            if not await self.containers.alive(session.id):
                await session.finish("FAILED", "kernel_died")
            elif (
                not session.active
                and not session.pending
                and session.queue.empty()
                and time.monotonic() - session.last_activity >= self.settings.idle_seconds
            ):
                await session.finish("STOPPED", "idle_timeout")

    async def watch(self):
        """Periodically check Docker and session health until cancelled.

        Unexpected monitor errors are logged and disable agent readiness until a later
        successful check.
        """
        while True:
            await asyncio.sleep(self.settings.monitor_seconds)
            try:
                await self.containers.ping()
                self.ready = not self.stopping
                await self.check_sessions()
            except Exception:
                self.ready = False
                log.exception("Session monitor failed")

    async def close(self):
        """Reject new work, await startups, stop sessions, and close the Docker client."""
        self.stopping = True
        self.ready = False
        if self.monitor:
            self.monitor.cancel()
            await asyncio.gather(self.monitor, return_exceptions=True)
        # Startups are bounded; waiting prevents orphan containers after shutdown.
        await asyncio.gather(*(s.starter for s in self.sessions.values()), return_exceptions=True)
        await asyncio.gather(*(s.finish(reason="agent_stopping") for s in self.sessions.values()))
        await self.containers.close()
