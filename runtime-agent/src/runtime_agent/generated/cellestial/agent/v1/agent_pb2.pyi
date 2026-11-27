from runtime_agent.generated.cellestial.common.v1 import error_pb2 as _error_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Target(_message.Message):
    __slots__ = ("session_id", "generation", "fence", "operation_id")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    FENCE_FIELD_NUMBER: _ClassVar[int]
    OPERATION_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    generation: str
    fence: int
    operation_id: str
    def __init__(self, session_id: _Optional[str] = ..., generation: _Optional[str] = ..., fence: _Optional[int] = ..., operation_id: _Optional[str] = ...) -> None: ...

class AssignRequest(_message.Message):
    __slots__ = ("target", "language")
    TARGET_FIELD_NUMBER: _ClassVar[int]
    LANGUAGE_FIELD_NUMBER: _ClassVar[int]
    target: Target
    language: str
    def __init__(self, target: _Optional[_Union[Target, _Mapping]] = ..., language: _Optional[str] = ...) -> None: ...

class HealthResponse(_message.Message):
    __slots__ = ("session_id", "language", "state", "reason", "generation", "ready", "draining", "active_execution_id", "queued")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    LANGUAGE_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    READY_FIELD_NUMBER: _ClassVar[int]
    DRAINING_FIELD_NUMBER: _ClassVar[int]
    ACTIVE_EXECUTION_ID_FIELD_NUMBER: _ClassVar[int]
    QUEUED_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    language: str
    state: str
    reason: str
    generation: str
    ready: bool
    draining: bool
    active_execution_id: str
    queued: int
    def __init__(self, session_id: _Optional[str] = ..., language: _Optional[str] = ..., state: _Optional[str] = ..., reason: _Optional[str] = ..., generation: _Optional[str] = ..., ready: bool = ..., draining: bool = ..., active_execution_id: _Optional[str] = ..., queued: _Optional[int] = ...) -> None: ...

class ExecuteRequest(_message.Message):
    __slots__ = ("target", "execution_id", "code", "timeout_seconds")
    TARGET_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_ID_FIELD_NUMBER: _ClassVar[int]
    CODE_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_SECONDS_FIELD_NUMBER: _ClassVar[int]
    target: Target
    execution_id: str
    code: str
    timeout_seconds: float
    def __init__(self, target: _Optional[_Union[Target, _Mapping]] = ..., execution_id: _Optional[str] = ..., code: _Optional[str] = ..., timeout_seconds: _Optional[float] = ...) -> None: ...

class ExecutionRequest(_message.Message):
    __slots__ = ("target", "execution_id")
    TARGET_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_ID_FIELD_NUMBER: _ClassVar[int]
    target: Target
    execution_id: str
    def __init__(self, target: _Optional[_Union[Target, _Mapping]] = ..., execution_id: _Optional[str] = ...) -> None: ...

class StopExecutionsRequest(_message.Message):
    __slots__ = ("target", "execution_ids")
    TARGET_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_IDS_FIELD_NUMBER: _ClassVar[int]
    target: Target
    execution_ids: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, target: _Optional[_Union[Target, _Mapping]] = ..., execution_ids: _Optional[_Iterable[str]] = ...) -> None: ...

class ControlResponse(_message.Message):
    __slots__ = ("generation", "accepted")
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    ACCEPTED_FIELD_NUMBER: _ClassVar[int]
    generation: str
    accepted: bool
    def __init__(self, generation: _Optional[str] = ..., accepted: bool = ...) -> None: ...

class ExecutionHeader(_message.Message):
    __slots__ = ("execution_id", "state", "object")
    EXECUTION_ID_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    OBJECT_FIELD_NUMBER: _ClassVar[int]
    execution_id: str
    state: str
    object: _error_pb2.ObjectHeader
    def __init__(self, execution_id: _Optional[str] = ..., state: _Optional[str] = ..., object: _Optional[_Union[_error_pb2.ObjectHeader, _Mapping]] = ...) -> None: ...

class ExecutionPayload(_message.Message):
    __slots__ = ("result_json",)
    RESULT_JSON_FIELD_NUMBER: _ClassVar[int]
    result_json: bytes
    def __init__(self, result_json: _Optional[bytes] = ...) -> None: ...

class ResultFrame(_message.Message):
    __slots__ = ("header", "chunk", "end")
    HEADER_FIELD_NUMBER: _ClassVar[int]
    CHUNK_FIELD_NUMBER: _ClassVar[int]
    END_FIELD_NUMBER: _ClassVar[int]
    header: ExecutionHeader
    chunk: _error_pb2.ObjectChunk
    end: _error_pb2.ObjectEnd
    def __init__(self, header: _Optional[_Union[ExecutionHeader, _Mapping]] = ..., chunk: _Optional[_Union[_error_pb2.ObjectChunk, _Mapping]] = ..., end: _Optional[_Union[_error_pb2.ObjectEnd, _Mapping]] = ...) -> None: ...

class EventsRequest(_message.Message):
    __slots__ = ("target", "after", "max_events", "max_bytes")
    TARGET_FIELD_NUMBER: _ClassVar[int]
    AFTER_FIELD_NUMBER: _ClassVar[int]
    MAX_EVENTS_FIELD_NUMBER: _ClassVar[int]
    MAX_BYTES_FIELD_NUMBER: _ClassVar[int]
    target: Target
    after: int
    max_events: int
    max_bytes: int
    def __init__(self, target: _Optional[_Union[Target, _Mapping]] = ..., after: _Optional[int] = ..., max_events: _Optional[int] = ..., max_bytes: _Optional[int] = ...) -> None: ...

class EventHeader(_message.Message):
    __slots__ = ("seq", "session_id", "execution_id", "kind", "state", "reason", "generation", "channel", "parent_msg_id", "msg_type", "timestamp", "object")
    SEQ_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_ID_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    CHANNEL_FIELD_NUMBER: _ClassVar[int]
    PARENT_MSG_ID_FIELD_NUMBER: _ClassVar[int]
    MSG_TYPE_FIELD_NUMBER: _ClassVar[int]
    TIMESTAMP_FIELD_NUMBER: _ClassVar[int]
    OBJECT_FIELD_NUMBER: _ClassVar[int]
    seq: int
    session_id: str
    execution_id: str
    kind: str
    state: str
    reason: str
    generation: str
    channel: str
    parent_msg_id: str
    msg_type: str
    timestamp: float
    object: _error_pb2.ObjectHeader
    def __init__(self, seq: _Optional[int] = ..., session_id: _Optional[str] = ..., execution_id: _Optional[str] = ..., kind: _Optional[str] = ..., state: _Optional[str] = ..., reason: _Optional[str] = ..., generation: _Optional[str] = ..., channel: _Optional[str] = ..., parent_msg_id: _Optional[str] = ..., msg_type: _Optional[str] = ..., timestamp: _Optional[float] = ..., object: _Optional[_Union[_error_pb2.ObjectHeader, _Mapping]] = ...) -> None: ...

class EventPayload(_message.Message):
    __slots__ = ("content_json", "metadata_json", "result_json", "buffers")
    CONTENT_JSON_FIELD_NUMBER: _ClassVar[int]
    METADATA_JSON_FIELD_NUMBER: _ClassVar[int]
    RESULT_JSON_FIELD_NUMBER: _ClassVar[int]
    BUFFERS_FIELD_NUMBER: _ClassVar[int]
    content_json: bytes
    metadata_json: bytes
    result_json: bytes
    buffers: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, content_json: _Optional[bytes] = ..., metadata_json: _Optional[bytes] = ..., result_json: _Optional[bytes] = ..., buffers: _Optional[_Iterable[str]] = ...) -> None: ...

class EventPageEnd(_message.Message):
    __slots__ = ("next_seq", "has_more")
    NEXT_SEQ_FIELD_NUMBER: _ClassVar[int]
    HAS_MORE_FIELD_NUMBER: _ClassVar[int]
    next_seq: int
    has_more: bool
    def __init__(self, next_seq: _Optional[int] = ..., has_more: bool = ...) -> None: ...

class EventFrame(_message.Message):
    __slots__ = ("header", "chunk", "end", "page_end")
    HEADER_FIELD_NUMBER: _ClassVar[int]
    CHUNK_FIELD_NUMBER: _ClassVar[int]
    END_FIELD_NUMBER: _ClassVar[int]
    PAGE_END_FIELD_NUMBER: _ClassVar[int]
    header: EventHeader
    chunk: _error_pb2.ObjectChunk
    end: _error_pb2.ObjectEnd
    page_end: EventPageEnd
    def __init__(self, header: _Optional[_Union[EventHeader, _Mapping]] = ..., chunk: _Optional[_Union[_error_pb2.ObjectChunk, _Mapping]] = ..., end: _Optional[_Union[_error_pb2.ObjectEnd, _Mapping]] = ..., page_end: _Optional[_Union[EventPageEnd, _Mapping]] = ...) -> None: ...
