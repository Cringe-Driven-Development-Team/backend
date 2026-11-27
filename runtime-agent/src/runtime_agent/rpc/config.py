"""Загружайте отдельную конфигурацию mTLS и lifecycle gRPC-входа."""

import os
import ssl
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
    metrics_address: str | None = None
    metrics_cert_file: Path | None = None
    metrics_key_file: Path | None = None
    metrics_ca_file: Path | None = None
    version: str = "local"
    unary_seconds: float = 5
    stream_seconds: float = 7200
    idle_seconds: float = 60
    shutdown_seconds: float = 30

    def __post_init__(self):
        """Отвергните неограниченные/неположительные бюджеты до открытия listeners."""
        import math

        monitoring = (
            self.metrics_address,
            self.metrics_cert_file,
            self.metrics_key_file,
            self.metrics_ca_file,
        )
        if any(monitoring) and not all(monitoring):
            raise ValueError("Incomplete metrics TLS configuration")
        if self.metrics_ca_file:
            control_ca = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            control_ca.load_verify_locations(cafile=self.ca_file)
            monitoring_ca = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            monitoring_ca.load_verify_locations(cafile=self.metrics_ca_file)
            if set(control_ca.get_ca_certs(binary_form=True)).intersection(
                monitoring_ca.get_ca_certs(binary_form=True)
            ):
                raise ValueError("Metrics requires a separate client CA")

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
            metrics_address=os.environ.get("METRICS_ADDR") or None,
            metrics_cert_file=_path_from_env("METRICS_TLS_CERT_FILE"),
            metrics_key_file=_path_from_env("METRICS_TLS_KEY_FILE"),
            metrics_ca_file=_path_from_env("METRICS_TLS_CA_FILE"),
            version=os.environ.get("VERSION", "local"),
            unary_seconds=float(os.environ.get("RPC_TIMEOUT", "5")),
            stream_seconds=float(os.environ.get("STREAM_TIMEOUT", "7200")),
            idle_seconds=float(os.environ.get("STREAM_IDLE_TIMEOUT", "60")),
            shutdown_seconds=float(os.environ.get("SHUTDOWN_TIMEOUT", "30")),
        )


def _path_from_env(name):
    """Прочитайте путь к настроенному TLS-артефакту."""
    value = os.environ.get(name)
    return Path(value) if value else None
