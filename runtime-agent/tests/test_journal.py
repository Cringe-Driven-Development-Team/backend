import pytest

from runtime_agent.journal import Journal

from .conftest import command


def test_recovery_does_not_replay_and_lock_is_exclusive(tmp_path):
    journal = Journal(tmp_path)
    with pytest.raises(RuntimeError, match="Another agent"):
        Journal(tmp_path)
    journal.create("session", "python")
    journal.accept("session", command())
    journal.accept("session", command("queued"))
    journal.transition("session", "run-1", "RUNNING")
    instance = journal.instance_id
    journal.close()
    reopened = Journal(tmp_path)
    try:
        reopened.recover()
        assert reopened.instance_id == instance
        assert reopened.execution("session", "run-1")["state"] == "UNKNOWN"
        assert reopened.execution("session", "queued")["state"] == "UNKNOWN"
        assert reopened.session("session")["reason"] == "agent_restarted"
        events = reopened.events("session", 0)
        assert events[-2]["state"] == "UNKNOWN"
        assert events[-1]["state"] == "STOPPED"
        assert reopened.existing("session", command())
    finally:
        reopened.close()
