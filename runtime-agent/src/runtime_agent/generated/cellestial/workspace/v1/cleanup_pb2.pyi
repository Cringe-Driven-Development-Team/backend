from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class BeginUserCleanupRequest(_message.Message):
    __slots__ = ("user_id", "operation_id")
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    OPERATION_ID_FIELD_NUMBER: _ClassVar[int]
    user_id: int
    operation_id: str
    def __init__(self, user_id: _Optional[int] = ..., operation_id: _Optional[str] = ...) -> None: ...

class GetUserCleanupRequest(_message.Message):
    __slots__ = ("operation_id",)
    OPERATION_ID_FIELD_NUMBER: _ClassVar[int]
    operation_id: str
    def __init__(self, operation_id: _Optional[str] = ...) -> None: ...

class Cleanup(_message.Message):
    __slots__ = ("operation_id", "user_id", "stage", "done", "error_code")
    OPERATION_ID_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    STAGE_FIELD_NUMBER: _ClassVar[int]
    DONE_FIELD_NUMBER: _ClassVar[int]
    ERROR_CODE_FIELD_NUMBER: _ClassVar[int]
    operation_id: str
    user_id: int
    stage: str
    done: bool
    error_code: str
    def __init__(self, operation_id: _Optional[str] = ..., user_id: _Optional[int] = ..., stage: _Optional[str] = ..., done: bool = ..., error_code: _Optional[str] = ...) -> None: ...
