import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ValidateSessionRequest(_message.Message):
    __slots__ = ("access_token",)
    ACCESS_TOKEN_FIELD_NUMBER: _ClassVar[int]
    access_token: str
    def __init__(self, access_token: _Optional[str] = ...) -> None: ...

class ValidateSessionResponse(_message.Message):
    __slots__ = ("user_id", "session_id", "session_version", "expires_at")
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_VERSION_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    user_id: int
    session_id: str
    session_version: int
    expires_at: _timestamp_pb2.Timestamp
    def __init__(self, user_id: _Optional[int] = ..., session_id: _Optional[str] = ..., session_version: _Optional[int] = ..., expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class BatchGetUsersRequest(_message.Message):
    __slots__ = ("user_ids",)
    USER_IDS_FIELD_NUMBER: _ClassVar[int]
    user_ids: _containers.RepeatedScalarFieldContainer[int]
    def __init__(self, user_ids: _Optional[_Iterable[int]] = ...) -> None: ...

class BatchResolveUsersRequest(_message.Message):
    __slots__ = ("logins",)
    LOGINS_FIELD_NUMBER: _ClassVar[int]
    logins: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, logins: _Optional[_Iterable[str]] = ...) -> None: ...

class User(_message.Message):
    __slots__ = ("id", "login", "avatar_url")
    ID_FIELD_NUMBER: _ClassVar[int]
    LOGIN_FIELD_NUMBER: _ClassVar[int]
    AVATAR_URL_FIELD_NUMBER: _ClassVar[int]
    id: int
    login: str
    avatar_url: str
    def __init__(self, id: _Optional[int] = ..., login: _Optional[str] = ..., avatar_url: _Optional[str] = ...) -> None: ...

class UsersResponse(_message.Message):
    __slots__ = ("users", "results")
    USERS_FIELD_NUMBER: _ClassVar[int]
    RESULTS_FIELD_NUMBER: _ClassVar[int]
    users: _containers.RepeatedCompositeFieldContainer[User]
    results: _containers.RepeatedCompositeFieldContainer[UserResolution]
    def __init__(self, users: _Optional[_Iterable[_Union[User, _Mapping]]] = ..., results: _Optional[_Iterable[_Union[UserResolution, _Mapping]]] = ...) -> None: ...

class RequestUserDeletionRequest(_message.Message):
    __slots__ = ("user_id", "operation_id")
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    OPERATION_ID_FIELD_NUMBER: _ClassVar[int]
    user_id: int
    operation_id: str
    def __init__(self, user_id: _Optional[int] = ..., operation_id: _Optional[str] = ...) -> None: ...

class GetUserDeletionRequest(_message.Message):
    __slots__ = ("operation_id",)
    OPERATION_ID_FIELD_NUMBER: _ClassVar[int]
    operation_id: str
    def __init__(self, operation_id: _Optional[str] = ...) -> None: ...

class UserDeletion(_message.Message):
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

class UserResolution(_message.Message):
    __slots__ = ("requested_id", "requested_login", "user", "not_found")
    REQUESTED_ID_FIELD_NUMBER: _ClassVar[int]
    REQUESTED_LOGIN_FIELD_NUMBER: _ClassVar[int]
    USER_FIELD_NUMBER: _ClassVar[int]
    NOT_FOUND_FIELD_NUMBER: _ClassVar[int]
    requested_id: int
    requested_login: str
    user: User
    not_found: bool
    def __init__(self, requested_id: _Optional[int] = ..., requested_login: _Optional[str] = ..., user: _Optional[_Union[User, _Mapping]] = ..., not_found: bool = ...) -> None: ...
