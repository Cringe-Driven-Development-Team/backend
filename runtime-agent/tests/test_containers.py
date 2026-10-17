from uuid import uuid4

from runtime_agent.containers import Containers


async def test_recovery_does_not_delete_foreign_directories(settings, monkeypatch):
    class Docker:
        @property
        def containers(self):
            return self

        def list(self, **kwargs):
            assert kwargs["filters"] == {"label": f"ru.cellestial.runtime-agent={instance}"}
            return []

    instance = str(uuid4())
    monkeypatch.setattr("runtime_agent.containers.docker.from_env", lambda **_: Docker())
    monkeypatch.setattr("runtime_agent.containers.os.getuid", lambda: 1000)
    backend = Containers(settings, instance)
    foreign = settings.socket_dir / "another-service"
    foreign.mkdir()
    (foreign / "keep").write_text("unrelated data")
    own_session = backend.root / str(uuid4())
    own_session.mkdir(parents=True)
    (own_session / "old-socket").touch()
    await backend.recover()
    assert not own_session.exists()
    assert (foreign / "keep").read_text() == "unrelated data"
