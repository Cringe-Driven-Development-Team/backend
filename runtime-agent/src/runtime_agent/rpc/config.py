"""Загружайте отдельную конфигурацию mTLS и lifecycle gRPC-входа."""

import os
from dataclasses import dataclass
from pathlib import Path

import grpc


@dataclass(frozen=True)
class RPCSettings:
    """Храните TLS credentials и ограниченные бюджеты без plaintext режима."""

    cert_file: Path
    key_file: Path
    ca_file: Path
    address: str = "0.0.0.0:9090"
    admin_address: str = "0.0.0.0:9091"
    version: str = "local"
    unary_seconds: float = 5
    stream_seconds: float = 7200
    idle_seconds: float = 60
    shutdown_seconds: float = 30

    def __post_init__(self):
        """Отвергните неограниченные/неположительные бюджеты до открытия listeners."""
        import math

        for name in ("unary_seconds", "stream_seconds", "idle_seconds", "shutdown_seconds"):
            value = getattr(self, name)
            if not math.isfinite(value) or value <= 0:
                raise ValueError(f"Invalid RPC {name}")

    def credentials(self):
        """Потребуйте действующий клиентский сертификат доверенного CA."""
        try:
            return grpc.ssl_server_credentials(
                ((self.key_file.read_bytes(), self.cert_file.read_bytes()),),
                root_certificates=self.ca_file.read_bytes(),
                require_client_auth=True,
            )
        except (OSError, ValueError):
            raise ValueError("Cannot load agent TLS credentials") from None

    @classmethod
    def from_env(cls):
        """Прочитайте только агентские TLS/transport настройки из окружения."""
        files = {}
        for name, field in (
            ("TLS_CERT_FILE", "cert_file"),
            ("TLS_KEY_FILE", "key_file"),
            ("TLS_CA_FILE", "ca_file"),
        ):
            value = os.environ.get(name, "")
            if not value:
                raise ValueError(f"Missing {name}")
            files[field] = Path(value)
        return cls(
            **files,
            address=os.environ.get("GRPC_ADDR", "0.0.0.0:9090"),
            admin_address=os.environ.get("ADMIN_ADDR", "0.0.0.0:9091"),
            version=os.environ.get("VERSION", "local"),
            unary_seconds=float(os.environ.get("RPC_TIMEOUT", "5")),
            stream_seconds=float(os.environ.get("STREAM_TIMEOUT", "7200")),
            idle_seconds=float(os.environ.get("STREAM_IDLE_TIMEOUT", "60")),
            shutdown_seconds=float(os.environ.get("SHUTDOWN_TIMEOUT", "30")),
        )
