"""Manage isolated Docker containers and their Unix socket directories."""

import asyncio
import os
import secrets
import shutil
from uuid import UUID

import docker
from docker.errors import NotFound
from docker.types import LogConfig

from .jupyter import Jupyter

LABEL = "ru.cellestial.runtime-agent"
_ROOT_ERROR = "Run the agent as a dedicated non-root user in the docker group"


class Containers:
    """Own Docker resources belonging to one persistent agent instance.

    Blocking Docker operations run in worker threads. Session containers have no
    network, limited resources, and access only to their own control directory
    and temporary files.
    """

    def __init__(self, settings, instance_id):
        """Connect to Docker and select an instance-specific socket directory.

        Args:
            settings (Settings): Validated agent configuration.
            instance_id (str): Persistent agent UUID used for names and
                ownership labels.

        Raises:
            RuntimeError: The process runs as root.
            docker.errors.DockerException: The Docker client cannot be
                initialized.
        """
        if os.getuid() == 0:
            raise RuntimeError(_ROOT_ERROR)
        self.settings = settings
        self.instance_id = instance_id
        self.root = settings.socket_dir / instance_id.replace("-", "")[:16]
        self.client = docker.from_env(timeout=10)

    async def ping(self):
        """Check whether the Docker daemon responds.

        Returns:
            bool: True on success.

        Raises:
            docker.errors.APIError: The daemon rejects the request.
        """
        return await asyncio.to_thread(self.client.ping)

    def name(self, session_id):
        """Build the deterministic Docker name of a session.

        Args:
            session_id (str): Canonical UUID of the session.

        Returns:
            str: Container name scoped to this agent instance.
        """
        return f"cellestial-{self.instance_id}-{session_id}"

    async def recover(self):
        """Remove containers and socket directories left by this instance.

        Other ownership labels and directories outside this instance namespace
        are preserved. Run this before accepting new sessions.

        Raises:
            docker.errors.APIError: Docker resources cannot be listed or
                removed.
            OSError: An owned socket directory cannot be created or removed.
        """
        containers = await asyncio.to_thread(
            self.client.containers.list,
            all=True,
            filters={"label": f"{LABEL}={self.instance_id}"},
        )
        for container in containers:
            await asyncio.to_thread(container.remove, force=True, v=True)
        # No user files are persisted: only socket directories are on the host.
        self.root.mkdir(parents=True, exist_ok=True, mode=0o700)
        for child in self.root.iterdir():
            try:
                UUID(child.name)
            except ValueError:
                continue
            if child.is_dir() and not child.is_symlink():
                await asyncio.to_thread(shutil.rmtree, child)

    async def start(self, session_id, language):
        """Start an isolated container and connect to its kernel.

        The instance socket root must exist. If startup fails, opened transport
        resources are closed and container cleanup is attempted before
        propagating the error.

        Args:
            session_id (str): Canonical UUID of the session.
            language (str): Validated kernel language, python or r.

        Returns:
            Jupyter: Connected transport whose kernel has confirmed readiness.

        Raises:
            docker.errors.DockerException: The container cannot be created,
                started, or removed.
            TimeoutError: Jupyter does not become ready before the startup
                deadline.
            OSError: The session control directory cannot be created or removed.
        """
        directory = self.root / session_id
        directory.mkdir(mode=0o700)
        token = secrets.token_urlsafe(32)
        if language == "python":
            image = self.settings.python_image
        else:
            image = self.settings.r_image
        try:
            await asyncio.to_thread(
                self.client.containers.run,
                image,
                name=self.name(session_id),
                detach=True,
                user=f"{os.getuid()}:{os.getgid()}",
                network_mode="none",
                read_only=True,
                cap_drop=["ALL"],
                security_opt=["no-new-privileges:true"],
                init=True,
                mem_limit="1g",
                memswap_limit="1g",
                nano_cpus=1_000_000_000,
                pids_limit=128,
                labels={
                    LABEL: self.instance_id,
                    "ru.cellestial.session": session_id,
                },
                volumes={str(directory): {"bind": "/control", "mode": "rw"}},
                tmpfs={
                    "/workspace": "rw,nosuid,nodev,size=256m,mode=1777",
                    "/tmp": "rw,nosuid,nodev,size=64m,mode=1777",
                },
                environment={
                    "JUPYTER_TOKEN": token,
                    "HOME": "/tmp",
                    "JUPYTER_RUNTIME_DIR": "/tmp/jupyter",
                    "JUPYTER_CONFIG_DIR": "/tmp/config",
                },
                working_dir="/workspace",
                log_config=LogConfig(
                    type="local",
                    config={"max-size": "1m", "max-file": "2"},
                ),
                restart_policy={"Name": "no"},
            )
            transport = Jupyter(directory / "jupyter.sock", token, language)
            try:
                await transport.start(self.settings.startup_seconds)
            except BaseException:
                await transport.close()
                raise
            return transport
        except BaseException:
            await self.remove(session_id)
            raise

    async def alive(self, session_id):
        """Check whether the session container is running.

        Args:
            session_id (str): Canonical UUID of the session.

        Returns:
            bool: False when the container is missing or stopped; True when
            it is running.

        Raises:
            docker.errors.APIError: Docker fails to inspect the container.
        """
        try:
            container = await asyncio.to_thread(
                self.client.containers.get,
                self.name(session_id),
            )
            return container.status == "running"
        except NotFound:
            return False

    async def remove(self, session_id):
        """Force-remove a session container and its control directory.

        A container that has already disappeared is treated as successfully
        removed.

        Args:
            session_id (str): Canonical UUID of the session.

        Raises:
            docker.errors.APIError: Docker cannot remove the container.
            OSError: The control directory cannot be removed.
        """
        try:
            container = await asyncio.to_thread(
                self.client.containers.get,
                self.name(session_id),
            )
            await asyncio.to_thread(container.remove, force=True, v=True)
        except NotFound:
            pass
        directory = self.root / session_id
        if directory.exists():
            await asyncio.to_thread(shutil.rmtree, directory)

    async def close(self):
        """Release the Docker client connection pool."""
        await asyncio.to_thread(self.client.close)
