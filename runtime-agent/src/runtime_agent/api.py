"""Expose authenticated HTTP commands and WebSocket event streams."""

import asyncio
import hmac
from contextlib import asynccontextmanager
from uuid import UUID

import anyio
from fastapi import FastAPI, Query, WebSocket, WebSocketDisconnect
from fastapi.responses import JSONResponse

from .config import Settings
from .containers import Containers
from .journal import Journal
from .models import AgentError, CreateSession, Execute, Inspect
from .runtime import Agent


class HTTPGuard:
    """Authorize before reading a body, and bound memory before JSON validation."""

    def __init__(self, app, token):
        """Configure the ASGI application and expected bearer credentials.

        Args:
            app (Callable): Downstream ASGI application.
            token (str): Validated ASCII API token.
        """
        self.app = app
        self.expected = ("Bearer " + token).encode("ascii")

    async def __call__(self, scope, receive, send):
        """Authorize HTTP requests and enforce a 2 MiB request body limit.

        Non-HTTP scopes pass through unchanged; WebSocket authentication is handled by its
        endpoint.

        Args:
            scope (dict): ASGI connection scope.
            receive (Callable): ASGI event receiver.
            send (Callable): ASGI response sender.
        """
        if scope["type"] != "http":
            return await self.app(scope, receive, send)
        headers = dict(scope["headers"])
        if not hmac.compare_digest(headers.get(b"authorization", b""), self.expected):
            return await JSONResponse({"detail": "Invalid runtime token"}, 401)(
                scope, receive, send
            )
        body = bytearray()
        while True:
            message = await receive()
            if message["type"] == "http.disconnect":
                return
            body.extend(message.get("body", b""))
            if len(body) > 2 * 1024 * 1024:
                return await JSONResponse({"detail": "Request body too large"}, 413)(
                    scope, receive, send
                )
            if not message.get("more_body", False):
                break
        consumed = False

        async def buffered_receive():
            """Deliver the buffered body once, then use the original receiver.

            Returns:
                dict: Next ASGI request or disconnect event.
            """
            nonlocal consumed
            if not consumed:
                consumed = True
                return {"type": "http.request", "body": bytes(body), "more_body": False}
            return await receive()

        await self.app(scope, buffered_receive, send)


def create_app(settings=None, containers_factory=Containers):
    """Build the private agent API with lifecycle-managed infrastructure.

    Args:
        settings (Settings | None): Configuration, or None to load environment
            variables.
        containers_factory (Callable): Factory accepting settings and an instance ID to
            create a container backend.

    Returns:
        FastAPI: Application with HTTP routes, middleware, and a WebSocket event
        endpoint.

    Raises:
        ValueError: Environment configuration is invalid.
    """
    settings = settings or Settings.from_env()

    @asynccontextmanager
    async def lifespan(app):
        """Acquire persistent state and manage agent startup and shutdown.

        Args:
            app (FastAPI): Application receiving its agent through app.state.

        Yields:
            None: Control while the started application serves requests.
        """
        journal = Journal(settings.state_dir)
        agent = None
        try:
            agent = Agent(settings, journal, containers_factory(settings, journal.instance_id))
            app.state.agent = agent
            await agent.start()
            yield
        finally:
            try:
                if agent:
                    await agent.close()
            finally:
                journal.close()

    app = FastAPI(
        title="Cellestial runtime agent", lifespan=lifespan, docs_url=None, redoc_url=None
    )
    app.add_middleware(HTTPGuard, token=settings.token)

    @app.exception_handler(AgentError)
    async def agent_error(request, exc):
        """Translate an application error into its public JSON response.

        Args:
            request (Request): Request that raised the error; unused by this handler.
            exc (AgentError): Application error containing status and detail.

        Returns:
            JSONResponse: Error detail with the supplied HTTP status.
        """
        return JSONResponse({"detail": exc.detail}, status_code=exc.status)

    @app.get("/health")
    async def health():
        """Report agent readiness and session capacity.

        Returns:
            JSONResponse: Health snapshot with HTTP 200 when ready, otherwise 503.
        """
        agent = app.state.agent
        return JSONResponse(
            {
                "ready": agent.ready,
                "docker_available": agent.ready,
                "active_sessions": agent.used_slots(),
                "available_slots": max(0, settings.max_sessions - agent.used_slots()),
            },
            status_code=200 if agent.ready else 503,
        )

    @app.post("/sessions", status_code=202)
    async def create_session(body: CreateSession):
        """Accept an idempotent session creation request.

        Args:
            body (CreateSession): Validated session ID and selected kernel language.

        Returns:
            dict: New or existing session snapshot.
        """
        return app.state.agent.create(str(body.session_id), body.language)

    @app.get("/sessions/{session_id}")
    async def session(session_id: UUID):
        """Read a session snapshot.

        Args:
            session_id (UUID): Session identifier from the request path.

        Returns:
            dict: Persisted and live session status.
        """
        return app.state.agent.snapshot(str(session_id))

    @app.get("/sessions/{session_id}/health")
    async def session_health(session_id: UUID):
        """Report whether a session accepts new commands.

        Args:
            session_id (UUID): Session identifier from the request path.

        Returns:
            JSONResponse: Session snapshot with HTTP 200 when ready, otherwise 503.
        """
        snapshot = app.state.agent.snapshot(str(session_id))
        return JSONResponse(snapshot, status_code=200 if snapshot["ready"] else 503)

    @app.post("/sessions/{session_id}/activity")
    async def activity(session_id: UUID):
        """Refresh the activity clock of a live session.

        Args:
            session_id (UUID): Session identifier from the request path.

        Returns:
            dict: Updated session snapshot.
        """
        app.state.agent.live(str(session_id)).touch()
        return app.state.agent.snapshot(str(session_id))

    @app.post("/sessions/{session_id}/executions", status_code=202)
    async def execute(session_id: UUID, body: Execute):
        """Accept a code execution request or retrieve its idempotent result.

        Args:
            session_id (UUID): Session identifier from the request path.
            body (Execute): Validated code, execution ID, and timeout.

        Returns:
            dict: Execution ID, state, and decoded result, if available.
        """
        return app.state.agent.accept(str(session_id), {**body.model_dump(), "kind": "execute"})

    @app.get("/sessions/{session_id}/executions/{execution_id:path}")
    async def execution(session_id: UUID, execution_id: str):
        """Read an execution state and its available result.

        Args:
            session_id (UUID): Session identifier from the request path.
            execution_id (str): Command identifier within the session.

        Returns:
            dict: Execution ID, state, and decoded result, if available.
        """
        app.state.agent.journal.session(str(session_id))
        return app.state.agent.journal.execution(str(session_id), execution_id)

    @app.post("/sessions/{session_id}/variables", status_code=202)
    async def variables(session_id: UUID, body: Inspect):
        """Queue variable inspection with a five-second execution timeout.

        Args:
            session_id (UUID): Session identifier from the request path.
            body (Inspect): Validated inspection execution ID.

        Returns:
            dict: Execution ID, state, and decoded result, if available.
        """
        return app.state.agent.accept(
            str(session_id),
            {
                "execution_id": body.execution_id,
                "kind": "inspect",
                "timeout_seconds": 5.0,
            },
        )

    @app.post("/sessions/{session_id}/kernel/interrupt")
    async def interrupt(session_id: UUID):
        """Request interruption of the active execution.

        Args:
            session_id (UUID): Session identifier from the request path.

        Returns:
            dict: Acknowledgement that interruption was requested.
        """
        await app.state.agent.live(str(session_id)).interrupt()
        return {"interrupt_requested": True}

    @app.post("/sessions/{session_id}/kernel/restart")
    async def restart(session_id: UUID):
        """Restart a ready, idle kernel and reset its memory.

        Args:
            session_id (UUID): Session identifier from the request path.

        Returns:
            dict: New kernel generation UUID.
        """
        runtime = app.state.agent.live(str(session_id))
        await runtime.restart()
        return {"generation": runtime.generation}

    @app.post("/sessions/{session_id}/runtime/drain")
    async def drain(session_id: UUID):
        """Stop accepting work and interrupt the active execution.

        Args:
            session_id (UUID): Session identifier from the request path.

        Returns:
            dict: Acknowledgement that draining was requested.
        """
        await app.state.agent.live(str(session_id)).drain()
        return {"draining": True}

    @app.post("/sessions/{session_id}/runtime/stop")
    async def stop(session_id: UUID):
        """Stop a session and report whether resource cleanup completed.

        Args:
            session_id (UUID): Session identifier from the request path.

        Returns:
            JSONResponse: Stopped flag and generation; HTTP 503 while cleanup remains
            pending.
        """
        snapshot = await app.state.agent.stop_session(str(session_id))
        return JSONResponse(
            {
                "stopped": snapshot["state"] in ("STOPPED", "FAILED"),
                "generation": snapshot["generation"],
            },
            status_code=503 if snapshot["state"] == "DRAINING" else 200,
        )

    @app.get("/sessions/{session_id}/events")
    async def events(session_id: UUID, after: int = Query(default=0, ge=0, le=2**63 - 1)):
        """Read a page of events after an exclusive sequence cursor.

        Args:
            session_id (UUID): Session identifier from the request path.
            after (int): Last received sequence number, or zero for the first page.

        Returns:
            dict: An events list containing up to 100 journal records.
        """
        app.state.agent.journal.session(str(session_id))
        return {"events": app.state.agent.journal.events(str(session_id), after)}

    @app.websocket("/sessions/{session_id}/events/ws")
    async def stream(ws: WebSocket, session_id: UUID):
        """Authenticate a WebSocket and stream replayed and new session events.

        Invalid credentials, cursors, or missing sessions reject the handshake. The stream
        sends heartbeats while idle and bounds send time for slow clients.

        Args:
            ws (WebSocket): Connection carrying the bearer header and optional after cursor.
            session_id (UUID): Session identifier from the request path.
        """
        expected = ("Bearer " + settings.token).encode("ascii")
        if not hmac.compare_digest(ws.headers.get("authorization", "").encode(), expected):
            await ws.close(code=1008)
            return
        agent = app.state.agent
        try:
            after = int(ws.query_params.get("after", "0"))
            if not 0 <= after <= 2**63 - 1:
                raise ValueError()
            agent.journal.session(str(session_id))
        except (ValueError, AgentError):
            await ws.close(code=1008)
            return
        await ws.accept()
        wakeup = asyncio.Event()
        agent.subscribers.add(wakeup)

        async def replay():
            """Send journal pages and subsequent events, updating the connection cursor.

            Raises:
                TimeoutError: Sending an event or heartbeat exceeds ten seconds.
            """
            nonlocal after
            while True:
                wakeup.clear()
                batch = agent.journal.events(str(session_id), after)
                if batch:
                    for event in batch:
                        await asyncio.wait_for(ws.send_json(event), 10)
                        after = event["seq"]
                    # Yield even if an in-process client writes instantly.
                    await asyncio.sleep(0)
                else:
                    try:
                        await asyncio.wait_for(wakeup.wait(), 15)
                    except TimeoutError:
                        await asyncio.wait_for(
                            ws.send_json({"kind": "heartbeat", "after": after}), 10
                        )

        async def disconnected():
            """Read incoming WebSocket events until the client disconnects."""
            while (await ws.receive())["type"] != "websocket.disconnect":
                pass

        tasks = [asyncio.create_task(replay()), asyncio.create_task(disconnected())]
        try:
            done, _ = await asyncio.wait(tasks, return_when=asyncio.FIRST_COMPLETED)
            for task in done:
                task.result()
        except (WebSocketDisconnect, TimeoutError, RuntimeError, OSError):
            pass
        finally:
            agent.subscribers.discard(wakeup)
            for task in tasks:
                task.cancel()
            # ASGI servers may cancel the handler while it is closing. Shield
            # cleanup from repeated cancellation until both child tasks exit.
            with anyio.CancelScope(shield=True):
                await asyncio.gather(*tasks, return_exceptions=True)

    return app
