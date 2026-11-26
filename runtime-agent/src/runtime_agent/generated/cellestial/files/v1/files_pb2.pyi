from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Workspace(_message.Message):
    __slots__ = ("session_id", "generation", "fence", "socket_id")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    FENCE_FIELD_NUMBER: _ClassVar[int]
    SOCKET_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    generation: str
    fence: int
    socket_id: str
    def __init__(self, session_id: _Optional[str] = ..., generation: _Optional[str] = ..., fence: _Optional[int] = ..., socket_id: _Optional[str] = ...) -> None: ...

class CommandRequest(_message.Message):
    __slots__ = ("workspace", "request_id", "operation_id", "list", "mkdir", "rename", "delete", "upload_begin", "download_begin", "upload_status", "upload_commit", "transfer_status", "transfer_cancel", "subscribe", "unsubscribe")
    WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    OPERATION_ID_FIELD_NUMBER: _ClassVar[int]
    LIST_FIELD_NUMBER: _ClassVar[int]
    MKDIR_FIELD_NUMBER: _ClassVar[int]
    RENAME_FIELD_NUMBER: _ClassVar[int]
    DELETE_FIELD_NUMBER: _ClassVar[int]
    UPLOAD_BEGIN_FIELD_NUMBER: _ClassVar[int]
    DOWNLOAD_BEGIN_FIELD_NUMBER: _ClassVar[int]
    UPLOAD_STATUS_FIELD_NUMBER: _ClassVar[int]
    UPLOAD_COMMIT_FIELD_NUMBER: _ClassVar[int]
    TRANSFER_STATUS_FIELD_NUMBER: _ClassVar[int]
    TRANSFER_CANCEL_FIELD_NUMBER: _ClassVar[int]
    SUBSCRIBE_FIELD_NUMBER: _ClassVar[int]
    UNSUBSCRIBE_FIELD_NUMBER: _ClassVar[int]
    workspace: Workspace
    request_id: str
    operation_id: str
    list: List
    mkdir: Mkdir
    rename: Rename
    delete: Delete
    upload_begin: UploadBegin
    download_begin: DownloadBegin
    upload_status: TransferReference
    upload_commit: TransferReference
    transfer_status: TransferReference
    transfer_cancel: TransferReference
    subscribe: WatchPaths
    unsubscribe: WatchPaths
    def __init__(self, workspace: _Optional[_Union[Workspace, _Mapping]] = ..., request_id: _Optional[str] = ..., operation_id: _Optional[str] = ..., list: _Optional[_Union[List, _Mapping]] = ..., mkdir: _Optional[_Union[Mkdir, _Mapping]] = ..., rename: _Optional[_Union[Rename, _Mapping]] = ..., delete: _Optional[_Union[Delete, _Mapping]] = ..., upload_begin: _Optional[_Union[UploadBegin, _Mapping]] = ..., download_begin: _Optional[_Union[DownloadBegin, _Mapping]] = ..., upload_status: _Optional[_Union[TransferReference, _Mapping]] = ..., upload_commit: _Optional[_Union[TransferReference, _Mapping]] = ..., transfer_status: _Optional[_Union[TransferReference, _Mapping]] = ..., transfer_cancel: _Optional[_Union[TransferReference, _Mapping]] = ..., subscribe: _Optional[_Union[WatchPaths, _Mapping]] = ..., unsubscribe: _Optional[_Union[WatchPaths, _Mapping]] = ...) -> None: ...

class List(_message.Message):
    __slots__ = ("path", "limit", "cursor")
    PATH_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    CURSOR_FIELD_NUMBER: _ClassVar[int]
    path: str
    limit: int
    cursor: str
    def __init__(self, path: _Optional[str] = ..., limit: _Optional[int] = ..., cursor: _Optional[str] = ...) -> None: ...

class Mkdir(_message.Message):
    __slots__ = ("path",)
    PATH_FIELD_NUMBER: _ClassVar[int]
    path: str
    def __init__(self, path: _Optional[str] = ...) -> None: ...

class Rename(_message.Message):
    __slots__ = ("path", "name", "expected_version")
    PATH_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_VERSION_FIELD_NUMBER: _ClassVar[int]
    path: str
    name: str
    expected_version: str
    def __init__(self, path: _Optional[str] = ..., name: _Optional[str] = ..., expected_version: _Optional[str] = ...) -> None: ...

class Delete(_message.Message):
    __slots__ = ("path", "expected_version", "recursive")
    PATH_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_VERSION_FIELD_NUMBER: _ClassVar[int]
    RECURSIVE_FIELD_NUMBER: _ClassVar[int]
    path: str
    expected_version: str
    recursive: bool
    def __init__(self, path: _Optional[str] = ..., expected_version: _Optional[str] = ..., recursive: bool = ...) -> None: ...

class UploadBegin(_message.Message):
    __slots__ = ("path", "size_bytes", "sha256")
    PATH_FIELD_NUMBER: _ClassVar[int]
    SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
    SHA256_FIELD_NUMBER: _ClassVar[int]
    path: str
    size_bytes: int
    sha256: str
    def __init__(self, path: _Optional[str] = ..., size_bytes: _Optional[int] = ..., sha256: _Optional[str] = ...) -> None: ...

class DownloadBegin(_message.Message):
    __slots__ = ("path",)
    PATH_FIELD_NUMBER: _ClassVar[int]
    path: str
    def __init__(self, path: _Optional[str] = ...) -> None: ...

class TransferReference(_message.Message):
    __slots__ = ("transfer_id",)
    TRANSFER_ID_FIELD_NUMBER: _ClassVar[int]
    transfer_id: str
    def __init__(self, transfer_id: _Optional[str] = ...) -> None: ...

class Error(_message.Message):
    __slots__ = ("code", "message", "retryable", "resync_required", "operation_id", "transfer_id")
    CODE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    RETRYABLE_FIELD_NUMBER: _ClassVar[int]
    RESYNC_REQUIRED_FIELD_NUMBER: _ClassVar[int]
    OPERATION_ID_FIELD_NUMBER: _ClassVar[int]
    TRANSFER_ID_FIELD_NUMBER: _ClassVar[int]
    code: str
    message: str
    retryable: bool
    resync_required: bool
    operation_id: str
    transfer_id: str
    def __init__(self, code: _Optional[str] = ..., message: _Optional[str] = ..., retryable: bool = ..., resync_required: bool = ..., operation_id: _Optional[str] = ..., transfer_id: _Optional[str] = ...) -> None: ...

class Entry(_message.Message):
    __slots__ = ("name", "path", "type", "size_bytes", "modified_at", "version", "actions")
    NAME_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
    MODIFIED_AT_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    ACTIONS_FIELD_NUMBER: _ClassVar[int]
    name: str
    path: str
    type: str
    size_bytes: int
    modified_at: str
    version: str
    actions: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, name: _Optional[str] = ..., path: _Optional[str] = ..., type: _Optional[str] = ..., size_bytes: _Optional[int] = ..., modified_at: _Optional[str] = ..., version: _Optional[str] = ..., actions: _Optional[_Iterable[str]] = ...) -> None: ...

class Quota(_message.Message):
    __slots__ = ("capacity_bytes", "used_bytes", "available_bytes", "source")
    CAPACITY_BYTES_FIELD_NUMBER: _ClassVar[int]
    USED_BYTES_FIELD_NUMBER: _ClassVar[int]
    AVAILABLE_BYTES_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    capacity_bytes: int
    used_bytes: int
    available_bytes: int
    source: str
    def __init__(self, capacity_bytes: _Optional[int] = ..., used_bytes: _Optional[int] = ..., available_bytes: _Optional[int] = ..., source: _Optional[str] = ...) -> None: ...

class Directory(_message.Message):
    __slots__ = ("path", "items", "next_cursor", "revision", "generation", "quota", "seq")
    PATH_FIELD_NUMBER: _ClassVar[int]
    ITEMS_FIELD_NUMBER: _ClassVar[int]
    NEXT_CURSOR_FIELD_NUMBER: _ClassVar[int]
    REVISION_FIELD_NUMBER: _ClassVar[int]
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    QUOTA_FIELD_NUMBER: _ClassVar[int]
    SEQ_FIELD_NUMBER: _ClassVar[int]
    path: str
    items: _containers.RepeatedCompositeFieldContainer[Entry]
    next_cursor: str
    revision: str
    generation: str
    quota: Quota
    seq: int
    def __init__(self, path: _Optional[str] = ..., items: _Optional[_Iterable[_Union[Entry, _Mapping]]] = ..., next_cursor: _Optional[str] = ..., revision: _Optional[str] = ..., generation: _Optional[str] = ..., quota: _Optional[_Union[Quota, _Mapping]] = ..., seq: _Optional[int] = ...) -> None: ...

class Transfer(_message.Message):
    __slots__ = ("transfer_id", "direction", "path", "size_bytes", "offset", "state", "generation", "sha256", "version", "chunk_bytes", "credit_bytes", "updated")
    TRANSFER_ID_FIELD_NUMBER: _ClassVar[int]
    DIRECTION_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
    OFFSET_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    SHA256_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    CHUNK_BYTES_FIELD_NUMBER: _ClassVar[int]
    CREDIT_BYTES_FIELD_NUMBER: _ClassVar[int]
    UPDATED_FIELD_NUMBER: _ClassVar[int]
    transfer_id: str
    direction: str
    path: str
    size_bytes: int
    offset: int
    state: str
    generation: str
    sha256: str
    version: str
    chunk_bytes: int
    credit_bytes: int
    updated: float
    def __init__(self, transfer_id: _Optional[str] = ..., direction: _Optional[str] = ..., path: _Optional[str] = ..., size_bytes: _Optional[int] = ..., offset: _Optional[int] = ..., state: _Optional[str] = ..., generation: _Optional[str] = ..., sha256: _Optional[str] = ..., version: _Optional[str] = ..., chunk_bytes: _Optional[int] = ..., credit_bytes: _Optional[int] = ..., updated: _Optional[float] = ...) -> None: ...

class Operation(_message.Message):
    __slots__ = ("operation_id", "revision", "resync_required", "resulting_seq")
    OPERATION_ID_FIELD_NUMBER: _ClassVar[int]
    REVISION_FIELD_NUMBER: _ClassVar[int]
    RESYNC_REQUIRED_FIELD_NUMBER: _ClassVar[int]
    RESULTING_SEQ_FIELD_NUMBER: _ClassVar[int]
    operation_id: str
    revision: str
    resync_required: bool
    resulting_seq: int
    def __init__(self, operation_id: _Optional[str] = ..., revision: _Optional[str] = ..., resync_required: bool = ..., resulting_seq: _Optional[int] = ...) -> None: ...

class CommandResponse(_message.Message):
    __slots__ = ("request_id", "directory", "transfer", "operation", "error", "watch")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    DIRECTORY_FIELD_NUMBER: _ClassVar[int]
    TRANSFER_FIELD_NUMBER: _ClassVar[int]
    OPERATION_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    WATCH_FIELD_NUMBER: _ClassVar[int]
    request_id: str
    directory: Directory
    transfer: Transfer
    operation: Operation
    error: Error
    watch: WatchResult
    def __init__(self, request_id: _Optional[str] = ..., directory: _Optional[_Union[Directory, _Mapping]] = ..., transfer: _Optional[_Union[Transfer, _Mapping]] = ..., operation: _Optional[_Union[Operation, _Mapping]] = ..., error: _Optional[_Union[Error, _Mapping]] = ..., watch: _Optional[_Union[WatchResult, _Mapping]] = ...) -> None: ...

class CheckRequest(_message.Message):
    __slots__ = ("workspace",)
    WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    workspace: Workspace
    def __init__(self, workspace: _Optional[_Union[Workspace, _Mapping]] = ...) -> None: ...

class CheckResponse(_message.Message):
    __slots__ = ("ready", "generation")
    READY_FIELD_NUMBER: _ClassVar[int]
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    ready: bool
    generation: str
    def __init__(self, ready: bool = ..., generation: _Optional[str] = ...) -> None: ...

class SocketRequest(_message.Message):
    __slots__ = ("workspace",)
    WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    workspace: Workspace
    def __init__(self, workspace: _Optional[_Union[Workspace, _Mapping]] = ...) -> None: ...

class SocketResponse(_message.Message):
    __slots__ = ("socket_id", "generation", "closed")
    SOCKET_ID_FIELD_NUMBER: _ClassVar[int]
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    CLOSED_FIELD_NUMBER: _ClassVar[int]
    socket_id: str
    generation: str
    closed: bool
    def __init__(self, socket_id: _Optional[str] = ..., generation: _Optional[str] = ..., closed: bool = ...) -> None: ...

class WatchPaths(_message.Message):
    __slots__ = ("paths", "after")
    PATHS_FIELD_NUMBER: _ClassVar[int]
    AFTER_FIELD_NUMBER: _ClassVar[int]
    paths: _containers.RepeatedScalarFieldContainer[str]
    after: int
    def __init__(self, paths: _Optional[_Iterable[str]] = ..., after: _Optional[int] = ...) -> None: ...

class WatchResult(_message.Message):
    __slots__ = ("snapshots", "seq", "events", "synchronization")
    SNAPSHOTS_FIELD_NUMBER: _ClassVar[int]
    SEQ_FIELD_NUMBER: _ClassVar[int]
    EVENTS_FIELD_NUMBER: _ClassVar[int]
    SYNCHRONIZATION_FIELD_NUMBER: _ClassVar[int]
    snapshots: _containers.RepeatedCompositeFieldContainer[SnapshotMarker]
    seq: int
    events: _containers.RepeatedCompositeFieldContainer[Event]
    synchronization: str
    def __init__(self, snapshots: _Optional[_Iterable[_Union[SnapshotMarker, _Mapping]]] = ..., seq: _Optional[int] = ..., events: _Optional[_Iterable[_Union[Event, _Mapping]]] = ..., synchronization: _Optional[str] = ...) -> None: ...

class SnapshotMarker(_message.Message):
    __slots__ = ("path", "revision", "seq", "complete")
    PATH_FIELD_NUMBER: _ClassVar[int]
    REVISION_FIELD_NUMBER: _ClassVar[int]
    SEQ_FIELD_NUMBER: _ClassVar[int]
    COMPLETE_FIELD_NUMBER: _ClassVar[int]
    path: str
    revision: str
    seq: int
    complete: bool
    def __init__(self, path: _Optional[str] = ..., revision: _Optional[str] = ..., seq: _Optional[int] = ..., complete: bool = ...) -> None: ...

class ReplayMarker(_message.Message):
    __slots__ = ("next_seq", "has_more")
    NEXT_SEQ_FIELD_NUMBER: _ClassVar[int]
    HAS_MORE_FIELD_NUMBER: _ClassVar[int]
    next_seq: int
    has_more: bool
    def __init__(self, next_seq: _Optional[int] = ..., has_more: bool = ...) -> None: ...

class SubscriptionRequest(_message.Message):
    __slots__ = ("workspace", "paths", "after")
    WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    PATHS_FIELD_NUMBER: _ClassVar[int]
    AFTER_FIELD_NUMBER: _ClassVar[int]
    workspace: Workspace
    paths: _containers.RepeatedScalarFieldContainer[str]
    after: int
    def __init__(self, workspace: _Optional[_Union[Workspace, _Mapping]] = ..., paths: _Optional[_Iterable[str]] = ..., after: _Optional[int] = ...) -> None: ...

class Event(_message.Message):
    __slots__ = ("workspace", "seq", "type", "path", "revision", "entry", "old_path", "new_path", "quota", "transfer", "error", "operation_id", "snapshot", "replay", "directory", "synchronization", "reason", "resync_required", "transfer_id")
    WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    SEQ_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    REVISION_FIELD_NUMBER: _ClassVar[int]
    ENTRY_FIELD_NUMBER: _ClassVar[int]
    OLD_PATH_FIELD_NUMBER: _ClassVar[int]
    NEW_PATH_FIELD_NUMBER: _ClassVar[int]
    QUOTA_FIELD_NUMBER: _ClassVar[int]
    TRANSFER_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    OPERATION_ID_FIELD_NUMBER: _ClassVar[int]
    SNAPSHOT_FIELD_NUMBER: _ClassVar[int]
    REPLAY_FIELD_NUMBER: _ClassVar[int]
    DIRECTORY_FIELD_NUMBER: _ClassVar[int]
    SYNCHRONIZATION_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    RESYNC_REQUIRED_FIELD_NUMBER: _ClassVar[int]
    TRANSFER_ID_FIELD_NUMBER: _ClassVar[int]
    workspace: Workspace
    seq: int
    type: str
    path: str
    revision: str
    entry: Entry
    old_path: str
    new_path: str
    quota: Quota
    transfer: Transfer
    error: Error
    operation_id: str
    snapshot: SnapshotMarker
    replay: ReplayMarker
    directory: str
    synchronization: str
    reason: str
    resync_required: bool
    transfer_id: str
    def __init__(self, workspace: _Optional[_Union[Workspace, _Mapping]] = ..., seq: _Optional[int] = ..., type: _Optional[str] = ..., path: _Optional[str] = ..., revision: _Optional[str] = ..., entry: _Optional[_Union[Entry, _Mapping]] = ..., old_path: _Optional[str] = ..., new_path: _Optional[str] = ..., quota: _Optional[_Union[Quota, _Mapping]] = ..., transfer: _Optional[_Union[Transfer, _Mapping]] = ..., error: _Optional[_Union[Error, _Mapping]] = ..., operation_id: _Optional[str] = ..., snapshot: _Optional[_Union[SnapshotMarker, _Mapping]] = ..., replay: _Optional[_Union[ReplayMarker, _Mapping]] = ..., directory: _Optional[str] = ..., synchronization: _Optional[str] = ..., reason: _Optional[str] = ..., resync_required: bool = ..., transfer_id: _Optional[str] = ...) -> None: ...

class UploadChunk(_message.Message):
    __slots__ = ("workspace", "transfer_id", "offset", "payload")
    WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    TRANSFER_ID_FIELD_NUMBER: _ClassVar[int]
    OFFSET_FIELD_NUMBER: _ClassVar[int]
    PAYLOAD_FIELD_NUMBER: _ClassVar[int]
    workspace: Workspace
    transfer_id: str
    offset: int
    payload: bytes
    def __init__(self, workspace: _Optional[_Union[Workspace, _Mapping]] = ..., transfer_id: _Optional[str] = ..., offset: _Optional[int] = ..., payload: _Optional[bytes] = ...) -> None: ...

class UploadAck(_message.Message):
    __slots__ = ("transfer_id", "offset", "credit_bytes", "error", "transfer")
    TRANSFER_ID_FIELD_NUMBER: _ClassVar[int]
    OFFSET_FIELD_NUMBER: _ClassVar[int]
    CREDIT_BYTES_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    TRANSFER_FIELD_NUMBER: _ClassVar[int]
    transfer_id: str
    offset: int
    credit_bytes: int
    error: Error
    transfer: Transfer
    def __init__(self, transfer_id: _Optional[str] = ..., offset: _Optional[int] = ..., credit_bytes: _Optional[int] = ..., error: _Optional[_Union[Error, _Mapping]] = ..., transfer: _Optional[_Union[Transfer, _Mapping]] = ...) -> None: ...

class ByteRange(_message.Message):
    __slots__ = ("start", "end", "suffix")
    START_FIELD_NUMBER: _ClassVar[int]
    END_FIELD_NUMBER: _ClassVar[int]
    SUFFIX_FIELD_NUMBER: _ClassVar[int]
    start: int
    end: int
    suffix: int
    def __init__(self, start: _Optional[int] = ..., end: _Optional[int] = ..., suffix: _Optional[int] = ...) -> None: ...

class DownloadRequest(_message.Message):
    __slots__ = ("workspace", "transfer_id", "offset", "length", "range", "if_range")
    WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    TRANSFER_ID_FIELD_NUMBER: _ClassVar[int]
    OFFSET_FIELD_NUMBER: _ClassVar[int]
    LENGTH_FIELD_NUMBER: _ClassVar[int]
    RANGE_FIELD_NUMBER: _ClassVar[int]
    IF_RANGE_FIELD_NUMBER: _ClassVar[int]
    workspace: Workspace
    transfer_id: str
    offset: int
    length: int
    range: ByteRange
    if_range: str
    def __init__(self, workspace: _Optional[_Union[Workspace, _Mapping]] = ..., transfer_id: _Optional[str] = ..., offset: _Optional[int] = ..., length: _Optional[int] = ..., range: _Optional[_Union[ByteRange, _Mapping]] = ..., if_range: _Optional[str] = ...) -> None: ...

class DownloadChunk(_message.Message):
    __slots__ = ("offset", "payload", "error", "version", "total_bytes", "sha256", "complete", "selected_bytes", "filename", "partial")
    OFFSET_FIELD_NUMBER: _ClassVar[int]
    PAYLOAD_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    TOTAL_BYTES_FIELD_NUMBER: _ClassVar[int]
    SHA256_FIELD_NUMBER: _ClassVar[int]
    COMPLETE_FIELD_NUMBER: _ClassVar[int]
    SELECTED_BYTES_FIELD_NUMBER: _ClassVar[int]
    FILENAME_FIELD_NUMBER: _ClassVar[int]
    PARTIAL_FIELD_NUMBER: _ClassVar[int]
    offset: int
    payload: bytes
    error: Error
    version: str
    total_bytes: int
    sha256: str
    complete: bool
    selected_bytes: int
    filename: str
    partial: bool
    def __init__(self, offset: _Optional[int] = ..., payload: _Optional[bytes] = ..., error: _Optional[_Union[Error, _Mapping]] = ..., version: _Optional[str] = ..., total_bytes: _Optional[int] = ..., sha256: _Optional[str] = ..., complete: bool = ..., selected_bytes: _Optional[int] = ..., filename: _Optional[str] = ..., partial: bool = ...) -> None: ...
