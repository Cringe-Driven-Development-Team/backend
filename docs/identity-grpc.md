# Самостоятельный Identity

`cmd/identity` запускает HTTP API пользователей для BFF, внутренний gRPC API для
Workspace и отдельный admin listener. Собственные данные — `identity.users` и
Redis auth-сессии/лимитеры; секреты — JWT, TOTP, BFF key, TLS и S3 аватаров.
Сервисы Workspace и Environments, offline-перенос существующих данных и
согласованное физическое удаление пользователя реализуются следующими частями #123.

## Файлы и путь запроса

- [cmd/identity/main.go](../cmd/identity/main.go) — конфигурация, логгер и сигналы.
- [service.go](../internal/app/identity/service.go) — wiring, listeners, readiness,
  health worker и bounded shutdown; API не применяет миграции.
- [HTTP OpenAPI](../spec/identity/openapi.json) — отдельный самодостаточный контракт:
  register/login/refresh/logout/logout-all, 2FA, профиль/пароль/аватар и suggestions.
  Пути сохраняют `/api/v1`; BFF передаёт Bearer в `Authorization`.
- [Identity router](../internal/user/delivery/http/identity_router.go) проверяет
  X-BFF-Key до пользовательской авторизации и чтения тела. Отсутствующий, неверный
  или повторный ключ возвращает `403 s2s_forbidden`, включая гостевые маршруты.
  Сгенерированный адаптер переиспользует существующие HTTP handlers/usecases;
  Go-модели временно общие с монолитом, публичный OpenAPI самодостаточен.
- [Protobuf](../proto/cellestial/identity/v1/identity.proto) →
  [interceptors](../internal/transport/rpc/) →
  [Session handler](../internal/user/delivery/grpc/session.go) →
  [SessionValidator](../internal/user/usecase/validation.go) → JWT/Redis.
  Redis проверяет sid/владельца/версию отзыва: одной подписи JWT недостаточно.
- [Users handler](../internal/user/delivery/grpc/directory.go) →
  [Directory](../internal/user/usecase/directory.go) → один batch SQL запрос в
  `identity.users`. До 100 ID/логинов, только `{id, login}`; отсутствующие записи
  пропускаются, дубликаты не дублируют ответ. Сбой БД возвращает ошибку, а не пустой успех.

## Транспорт

HTTPS принимает только доверенный клиентский сертификат с точным DNS SAN
`bff.internal.cellestial.ru`; gRPC Session/Users —
`workspace.internal.cellestial.ru`. Проверяются цепочка CA, срок и EKU. TLS 1.3,
plaintext fallback отсутствует. CN не используется. Серверный сертификат должен
содержать `identity.internal.cellestial.ru` (проверяется клиентом).

Стандартные gRPC health Check/Watch доступны Workspace и отдельному
`probe.internal.cellestial.ru`. На unary и stream действуют service authorization,
request_id, recovery, deadline, один итоговый лог и метрики. Identity RPC имеет
бюджет 2s; входящий более короткий deadline сохраняется. Автоматические повторы
мутаций не включены. HTTP default timeout 15s. Metadata/header limit 16 KiB,
gRPC message limit 8 MiB. HTTP JSON ограничен 512 KiB, аватар — 6 MiB на запрос.

| Ситуация | gRPC status |
|---|---|
| Подтверждённая JWT/Redis-сессия | OK с claims/expiry |
| Пустой токен, больше 8192 bytes, некорректный batch | InvalidArgument |
| Неверный/просроченный JWT или отозванная сессия | Unauthenticated |
| Доверенный сертификат с чужим SAN/методом | PermissionDenied |
| Недоверенный CA / неверный server SAN | TLS отклонён |
| Redis/PostgreSQL недоступен | Unavailable, без сырой ошибки |
| Panic | Internal, без содержимого panic |
| Deadline / отмена | DeadlineExceeded / Canceled |

Correlation: `[A-Za-z0-9._-]{1,64}`. Неверный или повторный заголовок заменяется UUID.
Возвращается HTTP `X-Request-ID` / gRPC metadata `x-request-id`, включая ошибки.
Пароли, Bearer/refresh, TOTP-секреты и тела запросов не журналируются. Runtime JSON
request_id остаётся отдельным ключом идемпотентности и Identity не обрабатывается.

## Конфигурация и миграции

Процесс читает окружение, не загружает `.env` автоматически. Обязательные настройки:
`DATABASE_URL`, `JWT_SECRET`, `BFF_API_KEY`, `TOTP_ENCRYPTION_KEY` (base64, 32 bytes),
`TLS_CERT_FILE`, `TLS_KEY_FILE`, `TLS_CA_FILE`, `S3_AVATARS_BUCKET`,
`S3_AVATARS_PUBLIC_URL`. AWS SDK читает `AWS_REGION`, credentials и при необходимости
`AWS_ENDPOINT_URL_S3` из штатного окружения. S3 bucket должен быть подготовлен заранее.

| Настройка | Default |
|---|---|
| HTTP_ADDR / GRPC_ADDR / ADMIN_ADDR | :8080 / :9090 / :9091 |
| REDIS_HOST / REDIS_PORT / REDIS_DB | redis / 6379 / 0 |
| REDIS_OPERATION_TIMEOUT | 1s |
| ACCESS_TOKEN_TTL / REFRESH_TOKEN_TTL | 15m / 720h |
| HTTP_TIMEOUT / SHUTDOWN_TIMEOUT | 15s / 30s |
| VERSION / LOG_LEVEL | local / info |

`REDIS_PASSWORD` необязателен. JWT, Redis DB/namespace и TOTP key при переносе
сохраняются, чтобы не инвалидировать действующие сессии и 2FA.

`go run ./cmd/identity-migrate` применяет baseline отдельной ролью владельца через
свой `DATABASE_URL`. Он создаёт `identity` и свою `identity.goose_db_version`.
Повторный запуск идемпотентен. Это миграция новой схемы; автоматического копирования
или удаления `public.users` нет. Перенос существующей БД остаётся offline-задачей.

API использует другую DB-role: без SUPERUSER/CREATEDB/CREATEROLE/REPLICATION/BYPASSRLS,
без CREATE в identity, с USAGE схемы/sequence, SELECT/INSERT/UPDATE/DELETE users
и SELECT goose_db_version. Запуск с отсутствующей/неподдерживаемой миграцией или
неподходящей ролью отклоняется. Роль и grants создаёт инфраструктура; они не выдаются API.
Миграция `00003` создаёт назначения роли `onlineconf_admin`. После обновления
выдайте роли приложения только `SELECT` на `identity.onlineconf_admins`.
До выкладки обновите шаблон grants в infra: если он выдаёт права на все
таблицы схемы, после общих grants выполните
`REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON identity.onlineconf_admins FROM <identity_app_role>;`.
Identity отклоняет запуск, если app-роль может менять назначения.
RPC `OnlineConfAccess.Authorize` доступен mTLS-принципалу
`onlineconf.internal.cellestial.ru` и проверяет действующую сессию и роль
при каждом запросе. Назначение и отзыв описаны в
[локальной разработке](local-development.md#onlineconf).

Права на чужие schemas не выдаются; межсхемных FK в baseline нет.

## Health и метрики

Admin listener предназначен для закрытой сети, без прикладных API:
`GET /livez`, `GET /readyz`, `GET /metrics`. Readiness проверяет PostgreSQL и Redis
с бюджетом 1s; отказ даёт 503. gRPC health обновляется отдельным worker, при
shutdown переходит в NOT_SERVING. SIGINT/SIGTERM останавливают все listeners и
health worker до закрытия DB/Redis; общий предел graceful shutdown — 30s.

Метрики имеют префикс `identity_`: HTTP/RPC requests, duration, active calls,
RPC messages/bytes, DB pool, build-info. Добавлены Go runtime/process collectors.
Labels ограничены известными method/route/status/direction; неизвестный route
или RPC нормализуется в `unmatched`. ID, токены, логины и произвольный текст labels
не становятся. VERSION публикуется только в build-info. Сбор scrape/dashboard и
образы/deployment — связанные #119/infra#163; release cards — .github#78.

## Генерация и проверки

Источники HTTP пока `spec/openapi.json` (Identity paths выделяет скрипт), RPC —
`proto/cellestial/identity/v1/identity.proto`. Generated Go вручную не менять.

```bash
python3 -m venv /tmp/cellestial-proto
/tmp/cellestial-proto/bin/pip install -r tools/proto/requirements.txt
PROTO_PYTHON=/tmp/cellestial-proto/bin/python make identity-proto identity-proto-check
make identity-http identity-http-check identity-build
```

Версии: oapi-codegen закреплён в go.mod; protoc-gen-go 1.36.12,
protoc-gen-go-grpc 1.6.1; grpcio-tools/grpcio 1.78.0, Python protobuf 6.33.6.
Python tooling используется как воспроизводимый protoc; агентские Python outputs
будут добавлены вместе с его gRPC-контрактом.

Для полного теста нужны **одноразовые** PostgreSQL (роль, способная создать тестовую
DB/role) и Redis. Тест создаёт/удаляет отдельную DB и DB-role, использует Redis DB 2;
проверки первого RPC используют также DB 0/1. Не указывать рабочие данные.

```bash
IDENTITY_TEST_DATABASE_URL='postgres://test_owner:password@127.0.0.1:15432/postgres?sslmode=disable' \
REDIS_TEST_ADDR=127.0.0.1:16379 make identity-test
```

Тесты поднимают настоящие HTTPS/gRPC listeners с временным CA и сертификатами,
проверяют auth/profile/2FA/refresh, отзывы, batch lookup, role isolation, health,
метрики, auth-before-body, cancel/panic и безопасные ошибки. Пакетные тесты без
адресов пропускают интеграцию и не заменяют этот target.
`REDIS_TEST_ADDR=127.0.0.1:16379 make identity-grpc-test` проверяет отдельно первый RPC.
