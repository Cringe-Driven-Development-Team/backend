"""Снимайте ресурсы kernel на хосте и передавайте в scrape готовый снимок."""

import asyncio
import logging
import os
import threading
import time
from pathlib import Path

from docker.errors import DockerException, NotFound

from .lifecycle import complete_before_cancel

log = logging.getLogger("runtime_agent.observability")


def _oom_kills(container_id, state):
    """Прочитайте cgroup v2 текущего контейнера, если доступен его PID."""
    pid = state.get("Pid", 0)
    if pid:
        try:
            for line in Path(f"/proc/{pid}/cgroup").read_text().splitlines():
                if line.startswith("0::") and container_id in line:
                    root = Path("/sys/fs/cgroup")
                    group = (root / line[3:].lstrip("/")).resolve()
                    if group.is_relative_to(root):
                        events = dict(
                            entry.split()
                            for entry in (group / "memory.events").read_text().splitlines()
                        )
                        return int(events["oom_kill"])
        except (OSError, KeyError, ValueError):
            pass
    return int(bool(state.get("OOMKilled")))


def _workspace_usage(root, container):
    """Измерьте только выделенную файловую систему /workspace."""
    if root is not None:
        if not os.path.ismount(root):
            raise ValueError("Workspace mount is unavailable")
        stats = os.statvfs(root)
        path = (
            str(root)
            .replace("\\", "\\134")
            .replace(" ", "\\040")
            .replace("\t", "\\011")
            .replace("\n", "\\012")
        )
        storage = "disk"
        for line in Path("/proc/self/mountinfo").read_text().splitlines():
            mount, _, fs = line.partition(" - ")
            if mount.split()[4] == path and fs.startswith("tmpfs "):
                storage = "tmpfs"
        return (
            (stats.f_blocks - stats.f_bfree) * stats.f_frsize,
            stats.f_blocks * stats.f_frsize,
            storage,
        )
    if container is None or container.status != "running":
        raise ValueError("Workspace tmpfs is unavailable")
    code, output = container.exec_run(["/usr/bin/df", "-Pk", "/workspace"], user="65532:65532")
    if code != 0:
        raise ValueError("Cannot measure workspace tmpfs")
    fields = output.decode("ascii").splitlines()[-1].split()
    if len(fields) != 6 or fields[-1] != "/workspace":
        raise ValueError("Unexpected workspace filesystem")
    return int(fields[2]) * 1024, int(fields[1]) * 1024, "tmpfs"


class KernelSampler:
    """Опросите один управляемый контейнер и сохраните счётчики между kernel restart."""

    def __init__(self, containers, resources):
        """Свяжите хостовый Docker adapter с collector одного агента."""
        self.containers = containers
        self.resources = resources
        self.container_id = self.retired_id = None
        self.lock = threading.Lock()
        self.cpu_total = self.oom_total = 0
        self.previous_cpu = self.previous_oom = 0

    def _counters(self, container, cpu, oom):
        """Перенесите только новые CPU секунды и OOM kills в счётчики процесса."""
        if container.id != self.container_id:
            self.container_id = container.id
            self.previous_cpu = self.previous_oom = 0
        self.oom_total += max(0, oom - self.previous_oom)
        self.previous_oom = max(oom, self.previous_oom)
        if cpu is not None:
            self.cpu_total += max(0, cpu - self.previous_cpu)
            self.previous_cpu = max(cpu, self.previous_cpu)
        return {"cpu_seconds": self.cpu_total / 1e9, "oom_kills": self.oom_total}

    def observe_removal(self, container):
        """Сохраните последний доступный OOM и уберите ресурсы удалённого контейнера."""
        oom = _oom_kills(container.id, container.attrs["State"])
        with self.lock:
            totals = self._counters(container, None, oom)
            self.retired_id = container.id
            values, storage = self.resources.snapshot
            mounted = self.containers.settings.workspace_path is not None
            if mounted:
                totals.update(
                    {key: value for key, value in values.items() if key.startswith("workspace_")}
                )
            self.resources.update(totals, storage if mounted else None)

    def _sample(self):
        """Соберите метрики Docker и workspace независимо, без изменения kernel."""
        started = time.monotonic()
        docker_failed = False
        values = {}
        container = None
        cpu = None
        oom = 0
        try:
            container = self.containers.client.containers.get(self.containers.name())
            if not self.containers.owns(container):
                container = None
                docker_failed = True
                log.warning("monitoring.kernel.foreign_container")
        except NotFound:
            pass
        except (DockerException, OSError):
            docker_failed = True
            log.warning("monitoring.kernel.unavailable")
        if container is not None:
            try:
                oom = _oom_kills(container.id, container.attrs["State"])
                if container.status == "running":
                    snapshot = container.stats(stream=False, one_shot=True)
                    cpu = snapshot["cpu_stats"]["cpu_usage"]["total_usage"]
                    memory = snapshot["memory_stats"]
                    limits = container.attrs["HostConfig"]
                    values.update(
                        memory_usage_bytes=memory["usage"], memory_limit_bytes=memory["limit"]
                    )
                    nanos = limits.get("NanoCpus", 0)
                    if nanos:
                        values["cpu_limit_cores"] = nanos / 1e9
                    elif limits.get("CpuQuota", 0) > 0 and limits.get("CpuPeriod", 0) > 0:
                        values["cpu_limit_cores"] = limits["CpuQuota"] / limits["CpuPeriod"]
            except (DockerException, OSError, KeyError, TypeError, ValueError):
                docker_failed = True
                log.warning("monitoring.kernel.unavailable")
        self.resources.observe_collection(
            "docker",
            time.monotonic() - started,
            success="memory_usage_bytes" in values,
            failed=docker_failed,
        )
        storage = None
        started = time.monotonic()
        workspace_expected = self.containers.settings.workspace_path is not None or (
            container is not None and container.status == "running"
        )
        try:
            used, quota, storage = _workspace_usage(
                self.containers.settings.workspace_path, container
            )
            values.update(workspace_used_bytes=used, workspace_quota_bytes=quota)
        except (DockerException, OSError, ValueError, IndexError):
            pass
        self.resources.observe_collection(
            "workspace",
            time.monotonic() - started,
            success=storage is not None,
            failed=workspace_expected and storage is None,
        )
        with self.lock:
            if container is not None and container.id == self.retired_id:
                return
            if container is not None:
                values.update(self._counters(container, cpu, oom))
            elif self.container_id is not None:
                values.update(cpu_seconds=self.cpu_total / 1e9, oom_kills=self.oom_total)
            self.resources.update(values, storage)

    async def run(self):
        """Собирайте независимый от scrape снимок с паузой 5 секунд между опросами."""
        while True:
            await complete_before_cancel(asyncio.to_thread(self._sample))
            await asyncio.sleep(5)


async def observe_loop(gauge):
    """Публикуйте задержку таймера event loop в секундах."""
    loop = asyncio.get_running_loop()
    while True:
        started = loop.time()
        await asyncio.sleep(1)
        gauge.set(max(0, loop.time() - started - 1))
