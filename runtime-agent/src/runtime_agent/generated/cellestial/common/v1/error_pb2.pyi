from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class ErrorDetail(_message.Message):
    __slots__ = ("code", "message", "retryable", "request_id", "details_json")
    CODE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    RETRYABLE_FIELD_NUMBER: _ClassVar[int]
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    DETAILS_JSON_FIELD_NUMBER: _ClassVar[int]
    code: str
    message: str
    retryable: bool
    request_id: str
    details_json: bytes
    def __init__(self, code: _Optional[str] = ..., message: _Optional[str] = ..., retryable: bool = ..., request_id: _Optional[str] = ..., details_json: _Optional[bytes] = ...) -> None: ...

class ObjectHeader(_message.Message):
    __slots__ = ("object_id", "length", "sha256")
    OBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    LENGTH_FIELD_NUMBER: _ClassVar[int]
    SHA256_FIELD_NUMBER: _ClassVar[int]
    object_id: str
    length: int
    sha256: bytes
    def __init__(self, object_id: _Optional[str] = ..., length: _Optional[int] = ..., sha256: _Optional[bytes] = ...) -> None: ...

class ObjectChunk(_message.Message):
    __slots__ = ("offset", "payload")
    OFFSET_FIELD_NUMBER: _ClassVar[int]
    PAYLOAD_FIELD_NUMBER: _ClassVar[int]
    offset: int
    payload: bytes
    def __init__(self, offset: _Optional[int] = ..., payload: _Optional[bytes] = ...) -> None: ...

class ObjectEnd(_message.Message):
    __slots__ = ("length", "sha256")
    LENGTH_FIELD_NUMBER: _ClassVar[int]
    SHA256_FIELD_NUMBER: _ClassVar[int]
    length: int
    sha256: bytes
    def __init__(self, length: _Optional[int] = ..., sha256: _Optional[bytes] = ...) -> None: ...
