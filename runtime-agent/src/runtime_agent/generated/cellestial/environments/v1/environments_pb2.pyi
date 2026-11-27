from runtime_agent.generated.cellestial.agent.v1 import agent_pb2 as _agent_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class CreateRequest(_message.Message):
    __slots__ = ("session_id", "operation_id", "fingerprint", "language", "profile", "fence")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    OPERATION_ID_FIELD_NUMBER: _ClassVar[int]
    FINGERPRINT_FIELD_NUMBER: _ClassVar[int]
    LANGUAGE_FIELD_NUMBER: _ClassVar[int]
    PROFILE_FIELD_NUMBER: _ClassVar[int]
    FENCE_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    operation_id: str
    fingerprint: bytes
    language: str
    profile: str
    fence: int
    def __init__(self, session_id: _Optional[str] = ..., operation_id: _Optional[str] = ..., fingerprint: _Optional[bytes] = ..., language: _Optional[str] = ..., profile: _Optional[str] = ..., fence: _Optional[int] = ...) -> None: ...

class EnvironmentRequest(_message.Message):
    __slots__ = ("session_id",)
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    def __init__(self, session_id: _Optional[str] = ...) -> None: ...

class DeleteRequest(_message.Message):
    __slots__ = ("session_id", "operation_id", "fingerprint", "reason_code", "fence")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    OPERATION_ID_FIELD_NUMBER: _ClassVar[int]
    FINGERPRINT_FIELD_NUMBER: _ClassVar[int]
    REASON_CODE_FIELD_NUMBER: _ClassVar[int]
    FENCE_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    operation_id: str
    fingerprint: bytes
    reason_code: str
    fence: int
    def __init__(self, session_id: _Optional[str] = ..., operation_id: _Optional[str] = ..., fingerprint: _Optional[bytes] = ..., reason_code: _Optional[str] = ..., fence: _Optional[int] = ...) -> None: ...

class Environment(_message.Message):
    __slots__ = ("session_id", "state", "reason_code", "cleanup_confirmed")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    REASON_CODE_FIELD_NUMBER: _ClassVar[int]
    CLEANUP_CONFIRMED_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    state: str
    reason_code: str
    cleanup_confirmed: bool
    def __init__(self, session_id: _Optional[str] = ..., state: _Optional[str] = ..., reason_code: _Optional[str] = ..., cleanup_confirmed: bool = ...) -> None: ...

class OperationRequest(_message.Message):
    __slots__ = ("operation_id",)
    OPERATION_ID_FIELD_NUMBER: _ClassVar[int]
    operation_id: str
    def __init__(self, operation_id: _Optional[str] = ...) -> None: ...

class Operation(_message.Message):
    __slots__ = ("operation_id", "session_id", "state", "done", "cleanup_confirmed", "error_code")
    OPERATION_ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    DONE_FIELD_NUMBER: _ClassVar[int]
    CLEANUP_CONFIRMED_FIELD_NUMBER: _ClassVar[int]
    ERROR_CODE_FIELD_NUMBER: _ClassVar[int]
    operation_id: str
    session_id: str
    state: str
    done: bool
    cleanup_confirmed: bool
    error_code: str
    def __init__(self, operation_id: _Optional[str] = ..., session_id: _Optional[str] = ..., state: _Optional[str] = ..., done: bool = ..., cleanup_confirmed: bool = ..., error_code: _Optional[str] = ...) -> None: ...
