"""Проверяйте формат scrape и недоступность устаревших runtime-показателей."""

from runtime_agent.rpc.metrics import Metrics


def test_runtime_snapshot_disappears_when_collection_fails():
    """Сбой сбора помечается отдельно от нулевого использования."""
    metrics = Metrics("test")
    metrics.resources.update({"memory_usage_bytes": 1024, "workspace_used_bytes": 512}, "tmpfs")
    assert b'runtime_agent_workspace_used_bytes{storage="tmpfs"} 512.0' in metrics.render()
    metrics.resources.update({}, None)
    scrape = metrics.render()
    assert b"runtime_agent_kernel_memory_usage_bytes" not in scrape
    assert b"runtime_agent_workspace_metrics_available 0.0" in scrape


def test_grpc_stream_metrics_contract():
    """Контракт соответствует Go-сервисам и разделяет два вида длительности."""
    metrics = Metrics("test")
    metrics.begin("/cellestial.files.v1.Files/Download", "stream")
    metrics.finish("/cellestial.files.v1.Files/Download", "stream", "DEADLINE_EXCEEDED", 10)
    scrape = metrics.render()
    assert b'grpc_code="DeadlineExceeded",grpc_method="Download"' in scrape
    assert b"cellestial_grpc_stream_duration_seconds_bucket" in scrape
    assert b"cellestial_grpc_call_duration_seconds_bucket" not in scrape


def test_sampler_does_not_call_a_foreign_container():
    """Совпадение имени без метки владения не разрешает stats или exec."""
    from types import SimpleNamespace
    from unittest.mock import Mock

    from runtime_agent.monitoring import KernelSampler

    container = Mock()
    containers = Mock()
    containers.client.containers.get.return_value = container
    containers.settings = SimpleNamespace(workspace_path=None)
    containers.owns.return_value = False
    resources = Metrics("test").resources

    KernelSampler(containers, resources)._sample()

    container.stats.assert_not_called()
    container.exec_run.assert_not_called()
    assert resources.snapshot == ({}, None)


def test_collection_error_does_not_refresh_success(monkeypatch):
    """Не выдавайте сбой опроса Docker за обновление снимка."""
    metrics = Metrics("test")
    monkeypatch.setattr("runtime_agent.rpc.metrics.time.time", lambda: 1000)
    metrics.resources.observe_collection("docker", 0.04, success=True, failed=False)
    monkeypatch.setattr("runtime_agent.rpc.metrics.time.time", lambda: 2000)
    metrics.resources.observe_collection("docker", 0.02, success=False, failed=True)
    scrape = metrics.render()
    assert b'resource_collection_last_success_timestamp_seconds{source="docker"} 1000.0' in scrape
    assert b'resource_collection_errors_total{source="docker"} 1.0' in scrape
    assert b'resource_collection_duration_seconds_count{source="docker"} 2.0' in scrape
