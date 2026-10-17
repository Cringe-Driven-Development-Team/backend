"""Load and validate runtime-agent configuration."""

import math
import os
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class Settings:
    """Store immutable configuration for the agent and its sessions.

    Attributes:
        token (str): Shared bearer token for the private API.
        state_dir (Path): Directory containing SQLite state and the process lock.
        socket_dir (Path): Parent directory for per-instance Unix sockets.
        max_sessions (int): Maximum number of sessions reserving capacity.
        max_output_bytes (int): Serialized output budget per execution or unattributed
            generation.
        idle_seconds (float): Idle lifetime of a session, in seconds.
        startup_seconds (float): Jupyter startup and restart deadline, in seconds.
        interrupt_seconds (float): Time to wait for interrupt confirmation, in seconds.
        monitor_seconds (float): Interval between lifecycle checks, in seconds.
        python_image (str): Docker image used for Python sessions.
        r_image (str): Docker image used for R sessions.
    """

    token: str
    state_dir: Path
    socket_dir: Path
    max_sessions: int = 1
    max_output_bytes: int = 10 * 1024 * 1024
    idle_seconds: float = 900
    startup_seconds: float = 30
    interrupt_seconds: float = 5
    monitor_seconds: float = 2
    python_image: str = "cellestial-kernel-python:local"
    r_image: str = "cellestial-kernel-r:local"

    def __post_init__(self):
        """Validate credentials, limits, and Unix socket path length.

        Raises:
            ValueError: The token, a numeric limit, or the socket path is invalid.
        """
        if len(self.token) < 32 or not self.token.isascii():
            raise ValueError("RUNTIME_TOKEN must contain at least 32 ASCII characters")
        for name in (
            "max_sessions",
            "max_output_bytes",
            "idle_seconds",
            "startup_seconds",
            "interrupt_seconds",
            "monitor_seconds",
        ):
            if not math.isfinite(getattr(self, name)) or getattr(self, name) <= 0:
                raise ValueError(f"{name} must be positive")
        # Linux sockaddr_un has room for 107 bytes plus the terminating NUL.
        socket_path = self.socket_dir / ("0" * 16) / ("0" * 36) / "jupyter.sock"
        if len(os.fsencode(socket_path)) > 107:
            raise ValueError("SOCKET_DIR is too long for a Unix socket")

    @classmethod
    def from_env(cls):
        """Read settings from environment variables and apply defaults.

        Returns:
            Settings: Validated configuration with absolute state and socket paths.

        Raises:
            ValueError: An environment value cannot be parsed or fails validation.
        """
        return cls(
            token=os.environ.get("RUNTIME_TOKEN", ""),
            state_dir=Path(os.environ.get("STATE_DIR", "/var/lib/cellestial-agent")).resolve(),
            socket_dir=Path(os.environ.get("SOCKET_DIR", "/run/cellestial-agent")).resolve(),
            max_sessions=int(os.environ.get("MAX_SESSIONS", "1")),
            max_output_bytes=int(os.environ.get("MAX_OUTPUT_BYTES", str(10 * 1024 * 1024))),
            idle_seconds=float(os.environ.get("IDLE_TIMEOUT_SECONDS", "900")),
            python_image=os.environ.get("PYTHON_IMAGE", "cellestial-kernel-python:local"),
            r_image=os.environ.get("R_IMAGE", "cellestial-kernel-r:local"),
        )
