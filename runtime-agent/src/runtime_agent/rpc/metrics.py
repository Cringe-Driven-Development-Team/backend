"""Собирайте метрики данного процесса с ограниченным набором labels."""

import time

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
from prometheus_client.core import CounterMetricFamily, GaugeMetricFamily

LATENCY_BUCKETS = (0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600)
STREAM_BUCKETS = (
    0.01,
    0.05,
    0.1,
    0.25,
    0.5,
    1,
    2.5,
    5,
    10,
    30,
    60,
    120,
    300,
    600,
    1800,
    3600,
    7200,
)
HTTP_METHODS = frozenset(
    ("CONNECT", "DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT", "TRACE")
)


def _rpc_labels(method, kind):
    """Разберите зарегистрированное имя RPC или конечную категорию unmatched."""
    service, sep, operation = method.removeprefix("/").partition("/")
    return (
        (service, operation, kind, "server") if sep else ("unmatched", "unmatched", kind, "server")
    )


class RuntimeResources:
    """Публикуйте один последний снимок, исключая устаревшие ресурсы."""

    def __init__(self, registry=None):
        """Начните без успешного снимка и подготовьте статистику опроса."""
        self.snapshot: tuple[dict[str, float], str | None] = ({}, None)

        self.collection_duration = Histogram(
            "runtime_agent_resource_collection_duration_seconds",
            "Background resource poll duration.",
            ("source",),
            buckets=LATENCY_BUCKETS,
            registry=registry,
        )
        self.collection_errors = Counter(
            "runtime_agent_resource_collection_errors_total",
            "Failed resource collection attempts.",
            ("source",),
            registry=registry,
        )
        self.collection_success = Gauge(
            "runtime_agent_resource_collection_last_success_timestamp_seconds",
            "Unix timestamp of the last successful sample.",
            ("source",),
            registry=registry,
        )

    def observe_collection(self, source, elapsed, *, success, failed):
        """Запишите стоимость прохода и обновите timestamp только при наличии данных."""
        self.collection_duration.labels(source).observe(elapsed)
        if success:
            self.collection_success.labels(source).set(time.time())
        if failed:
            self.collection_errors.labels(source).inc()

    def update(self, values, storage):
        """Опубликуйте значения и тип workspace одной заменой снимка."""
        self.snapshot = (values, storage)

    def collect(self):
        """Отдайте доступные ресурсы без labels контейнеров и путей."""
        values, storage = self.snapshot
        for name, help_text in (
            ("cpu_limit_cores", "Kernel CPU limit in cores."),
            ("memory_usage_bytes", "Current cgroup memory usage."),
            ("memory_limit_bytes", "Kernel memory limit."),
        ):
            if name in values:
                yield GaugeMetricFamily(
                    f"runtime_agent_kernel_{name}", help_text, value=values[name]
                )
        for name, help_text in (
            ("cpu_seconds", "Accumulated container CPU time."),
            ("oom_kills", "Observed OOM kills across kernel containers."),
        ):
            if name in values:
                yield CounterMetricFamily(
                    f"runtime_agent_kernel_{name}", help_text, value=values[name]
                )
        for name in ("used_bytes", "quota_bytes"):
            key = f"workspace_{name}"
            if storage is not None and key in values:
                metric = GaugeMetricFamily(
                    f"runtime_agent_{key}",
                    f"Workspace {name.replace('_', ' ')}.",
                    labels=["storage"],
                )
                metric.add_metric([storage], values[key])
                yield metric
        yield GaugeMetricFamily(
            "runtime_agent_kernel_metrics_available",
            "Whether kernel resource collection succeeded.",
            value="memory_usage_bytes" in values,
        )
        yield GaugeMetricFamily(
            "runtime_agent_workspace_metrics_available",
            "Whether workspace resource collection succeeded.",
            value="workspace_used_bytes" in values,
        )


class Metrics:
    """Владейте registry для RPC, HTTP, процесса и kernel."""

    def __init__(self, version):
        """Зарегистрируйте process collectors и прикладные семейства."""
        self.registry = CollectorRegistry()
        labels = ("grpc_service", "grpc_method", "rpc_type", "rpc_side")
        calls = (*labels, "grpc_code")
        message = (*labels, "direction")
        self.requests = Counter(
            "cellestial_grpc_calls_total", "Completed gRPC calls.", calls, registry=self.registry
        )
        self.unary = Histogram(
            "cellestial_grpc_call_duration_seconds",
            "Unary gRPC duration in seconds.",
            calls,
            buckets=LATENCY_BUCKETS,
            registry=self.registry,
        )
        self.stream = Histogram(
            "cellestial_grpc_stream_duration_seconds",
            "gRPC stream lifetime in seconds.",
            calls,
            buckets=STREAM_BUCKETS,
            registry=self.registry,
        )
        self.active = Gauge(
            "cellestial_grpc_active_streams", "Active gRPC streams.", labels, registry=self.registry
        )
        self.messages = Counter(
            "cellestial_grpc_stream_messages_total",
            "Successfully transferred stream messages.",
            message,
            registry=self.registry,
        )
        self.bytes = Counter(
            "cellestial_grpc_stream_message_bytes_total",
            "Uncompressed protobuf stream bytes.",
            message,
            registry=self.registry,
        )
        self.kernel_state = Gauge(
            "runtime_agent_kernel_state",
            "Current kernel lifecycle state.",
            ("state",),
            registry=self.registry,
        )
        self.queue = Gauge(
            "runtime_agent_kernel_queue_length", "Queued kernel commands.", registry=self.registry
        )
        self.loop_lag = Gauge(
            "runtime_agent_event_loop_lag_seconds",
            "Event loop timer delay.",
            registry=self.registry,
        )
        self.resources = RuntimeResources(self.registry)
        self.registry.register(self.resources)
        self.http_requests, self.http_duration = self.http_metrics(
            "http", ("method", "route", "status_code")
        )
        self.client_requests, self.client_duration = self.http_metrics(
            "http_client", ("dependency", "operation", "method", "status_code")
        )
        self.client_body_duration = Histogram(
            "cellestial_http_client_response_body_duration_seconds",
            "Time from HTTP headers to completion or disposal of the response body.",
            ("dependency", "operation", "method", "status_code", "outcome"),
            buckets=LATENCY_BUCKETS,
            registry=self.registry,
        )
        self.http_stream_errors = Counter(
            "cellestial_http_stream_errors_total",
            "Response failures after HTTP headers were sent.",
            ("method", "route", "status_code"),
            registry=self.registry,
        )
        build = Gauge(
            "runtime_agent_build_info",
            "Running agent version.",
            ("version",),
            registry=self.registry,
        )
        build.labels(version=version).set(1)
        ProcessCollector(registry=self.registry)
        GCCollector(registry=self.registry)
        PlatformCollector(registry=self.registry)

    def http_metrics(self, prefix, labels):
        """Создайте counter и histogram для одного направления HTTP."""
        return (
            Counter(
                f"cellestial_{prefix}_requests_total",
                "HTTP requests.",
                labels,
                registry=self.registry,
            ),
            Histogram(
                f"cellestial_{prefix}_request_duration_seconds",
                "HTTP request duration in seconds.",
                labels,
                buckets=LATENCY_BUCKETS,
                registry=self.registry,
            ),
        )

    def begin(self, method, kind):
        """Учтите активный stream до чтения первого сообщения."""
        if kind == "stream":
            self.active.labels(*_rpc_labels(method, kind)).inc()

    def finish(self, method, kind, code, seconds):
        """Запишите один итоговый RPC и его длительность."""
        labels = _rpc_labels(method, kind)
        histogram = self.unary
        if kind == "stream":
            self.active.labels(*labels).dec()
            histogram = self.stream
        code = {"OK": "OK", "CANCELLED": "Canceled"}.get(code, code.title().replace("_", ""))
        self.requests.labels(*labels, code).inc()
        histogram.labels(*labels, code).observe(seconds)

    def message(self, method, direction, value):
        """Учтите размер успешно переданного сообщения потока."""
        labels = (*_rpc_labels(method, "stream"), direction)
        self.messages.labels(*labels).inc()
        self.bytes.labels(*labels).inc(value.ByteSize())

    def observe_kernel(self, snapshot):
        """Публикуйте конечное состояние и количество ожидающих команд."""
        state = str(snapshot.get("state", "other")).lower()
        states = ("waiting", "starting", "ready", "draining", "stopped", "failed", "other")
        selected = state if state in states else "other"
        for value in states:
            self.kernel_state.labels(state=value).set(value == selected)
        self.queue.set(snapshot.get("queued", 0))

    def observe_client(self, operation, method, status, seconds):
        """Запишите исходящую HTTP-попытку к Jupyter по известной операции."""
        labels = ("jupyter", operation, method if method in HTTP_METHODS else "other", str(status))
        self.client_requests.labels(*labels).inc()
        self.client_duration.labels(*labels).observe(seconds)

    def observe_client_body(self, operation, method, status, seconds, outcome):
        """Учтите завершение чтения ответа Jupyter отдельно от получения заголовков."""
        self.client_body_duration.labels(
            "jupyter",
            operation,
            method if method in HTTP_METHODS else "other",
            str(status),
            outcome,
        ).observe(seconds)

    def wrap_http(self, app, routes):
        """Измерьте административный ASGI-вход, включая отказ и обрыв ответа."""

        async def observed(scope, receive, send):
            if scope["type"] != "http":
                await app(scope, receive, send)
                return
            start = time.monotonic()
            status = 0
            failed = False

            async def observe_send(message):
                nonlocal status
                if message["type"] == "http.response.start":
                    status = message["status"]
                await send(message)

            try:
                await app(scope, receive, observe_send)
            except BaseException:
                failed = status != 0
                raise
            finally:
                method, path = scope["method"], scope["path"]
                labels = (
                    method if method in HTTP_METHODS else "other",
                    path if path in routes else "unmatched",
                    str(status or 500),
                )
                self.http_requests.labels(*labels).inc()
                self.http_duration.labels(*labels).observe(time.monotonic() - start)
                if failed:
                    self.http_stream_errors.labels(*labels).inc()

        return observed

    def render(self):
        """Верните самостоятельный Prometheus scrape в bytes."""
        return generate_latest(self.registry)
