"""Собирайте метрики только данного процесса без идентификаторов и payload в labels."""

from prometheus_client import (
    CollectorRegistry,
    Counter,
    Gauge,
    GCCollector,
    Histogram,
    PlatformCollector,
    ProcessCollector,
    generate_latest,
)


class Metrics:
    """Владейте registry с конечным реестром RPC/method/status/type/direction."""

    def __init__(self, version):
        """Зарегистрируйте RPC, runtime/process и отдельную build-info gauge."""
        self.registry = CollectorRegistry()
        self.requests = Counter(
            "runtime_agent_rpc_server_requests_total",
            "Completed server RPCs.",
            ("method", "status", "type"),
            registry=self.registry,
        )
        self.duration = Histogram(
            "runtime_agent_rpc_server_duration_seconds",
            "Server RPC duration.",
            ("method", "status", "type"),
            registry=self.registry,
        )
        self.active = Gauge(
            "runtime_agent_rpc_server_active",
            "Active server RPCs.",
            ("method", "type"),
            registry=self.registry,
        )
        self.messages = Counter(
            "runtime_agent_rpc_server_messages_total",
            "Transferred protobuf messages.",
            ("method", "direction"),
            registry=self.registry,
        )
        self.bytes = Counter(
            "runtime_agent_rpc_server_message_bytes_total",
            "Uncompressed protobuf bytes.",
            ("method", "direction"),
            registry=self.registry,
        )
        self.kernel_state = Gauge(
            "runtime_agent_kernel_state",
            "Current kernel lifecycle state.",
            ("state",),
            registry=self.registry,
        )
        self.kernel_states = (
            "waiting",
            "starting",
            "ready",
            "draining",
            "stopped",
            "failed",
            "other",
        )
        self.build = Gauge(
            "runtime_agent_build_info",
            "Running agent version.",
            ("version",),
            registry=self.registry,
        )
        self.build.labels(version=version).set(1)
        ProcessCollector(registry=self.registry)
        GCCollector(registry=self.registry)
        PlatformCollector(registry=self.registry)

    def message(self, method, direction, value):
        """Учтите protobuf size, не сериализуя содержимое в metric label."""
        self.messages.labels(method, direction).inc()
        self.bytes.labels(method, direction).inc(value.ByteSize())

    def observe_kernel(self, snapshot):
        """Выставьте только известную метку состояния без параметров kernel."""
        state = str(snapshot.get("state", "other")).lower()
        selected = state if state in self.kernel_states else "other"
        for value in self.kernel_states:
            self.kernel_state.labels(state=value).set(value == selected)

    def render(self):
        """Верните самостоятельный Prometheus scrape в bytes."""
        return generate_latest(self.registry)
