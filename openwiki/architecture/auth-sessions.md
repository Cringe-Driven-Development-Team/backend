---
type: guide
title: "BFF, авторизация и Redis-сессии"
description: "Порядок S2S и Bearer, ротация refresh и отзыв сессий в Redis."
tags: [auth, redis, security]
verified:
  - by: openwiki/0.7.2
    at: 2026-10-31T14:34:07.758Z
sources:
  - id: openwiki-source-a009d602e55f848ea26930dd
    resource: repo://internal/app/app.go
  - id: openwiki-source-55b426b919298d5817843eb0
    resource: repo://internal/transport/http/router.go
  - id: openwiki-source-39d8294d108f5e15812daead
    resource: repo://internal/transport/http/security/bff.go
  - id: openwiki-source-dbc18a3bac0b60994e4038e4
    resource: repo://internal/user/adapters/redis/session.go
  - id: openwiki-source-c91b579389d2163426bf78c6
    resource: repo://internal/user/adapters/redis/session.lua
  - id: openwiki-source-1f5edfaa55907e3a11216234
    resource: repo://internal/user/usecase/user.go
generated: { by: "codex", at: "2026-10-31T14:34:07.758Z" }
---

# BFF, авторизация и Redis-сессии

Это описание кода на исходном SHA из checkpoint, без утверждения о выкладке на прод.

## Два уровня доступа

`NewRouter` требует непустой `BFF_API_KEY`. Внешние обёртки идут в порядке request ID → logging → recover → S2S → Bearer, затем роутер и Validator. S2S защищает `/api/v1` и вложенные пути: ровно один непустой `X-BFF-Key` сравнивается через SHA-256 и constant-time comparison. Неверный ключ даёт 403 до разбора пользовательского токена и тела. `GET /health` доступен отдельно. Request ID назначается также отказам S2S, 404 и 405.

Backend не читает browser cookies для Bearer. Он разбирает единственный `Authorization`, проверяет токен, затем сверяет claims с `SessionStore.ValidateAccess`. Только после этого principal попадает в context. Недействительная сессия даёт 401; ошибка хранилища идёт в обработчик серверных ошибок. Отсутствие пригодного Bearer оставляет запрос без principal, а обязательность авторизации конкретной операции определяет Validator. Клиент и cookie-сессия BFF принадлежат frontend; формы HTTP-запросов — Apidog, см. [контракт](../operations/startup-contract.md).

## Жизненный цикл

Register хеширует пароль и создаёт пользователя в PostgreSQL перед созданием сессии. Это последовательность двух хранилищ: ошибка Redis после записи пользователя не откатывает регистрацию. Login проверяет пароль через порт PasswordHasher. Новая сессия получает UUID и текущую версию пользователя; наружу возвращаются access и refresh, хранилищу передаётся hash refresh.

Refresh находит старую сессию по hash, проверяет срок, готовит новую пару и вызывает Rotate. Redis Lua перепроверяет состояние, записывает новый индекс refresh и auth-session, удаляет старый индекс. SID и версия сохраняются; access deadline берётся как максимум старого и нового. Поэтому ротация refresh сама по себе не является отзывом ранее выданного access. Logout удаляет связанные auth и refresh записи. RevokeAll меняет версию пользователя, вследствие чего старые claims перестают проходить проверку.

## Redis — источник состояния авторизации

Адаптер рассчитан на standalone primary. Lua сопоставляет user ID, SID и version, проверяет сроки по Redis TIME; идентификатор пользователя и версия хранятся десятичными строками. Чтение access/refresh не продлевает TTL. Отсутствие нужной записи или несовпадение версии закрывает доступ. Повреждение записи вызывает ошибку, а не успешную авторизацию.

Каждая операция ограничена context timeout. Адаптер использует EVAL и не повторяет мутации; сборка клиента отключает go-redis retries. Timeout может означать уже выполненную запись: повтор не равнозначен безопасному откату. Нельзя описывать Redis как необязательный cache или обещать fallback в PostgreSQL.

## Проверки и соседние темы

`internal/transport/http/bff_test.go` проверяет приоритет S2S и request ID; `s2s_side_effects_test.go` — отсутствие обращений к хранилищу блокнотов при отказе. `session_integration_test.go` проверяет отзыв между экземплярами; `internal/user/adapters/redis/unknown_outcome_test.go` — неопределённый исход мутаций. Эти тесты являются кодовыми свидетельствами, а не проверкой прода.

[Карта Go API](../quickstart.md) · [Хранение блокнотов](notebooks.md)

## Источники

- [internal/transport/http/router.go#L19-L56](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/transport/http/router.go#L19-L56)
- [internal/transport/http/security/bff.go#L18-L44](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/transport/http/security/bff.go#L18-L44)
- [internal/transport/http/security/bff.go#L47-L75](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/transport/http/security/bff.go#L47-L75)
- [internal/user/usecase/user.go#L34-L57](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/user/usecase/user.go#L34-L57)
- [internal/user/usecase/user.go#L84-L121](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/user/usecase/user.go#L84-L121)
- [internal/user/adapters/redis/session.lua#L81-L107](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/user/adapters/redis/session.lua#L81-L107)
- [internal/user/adapters/redis/session.go#L1-L25](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/user/adapters/redis/session.go#L1-L25)
- [internal/user/adapters/redis/session.go#L64-L84](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/user/adapters/redis/session.go#L64-L84)
- [internal/user/adapters/redis/session.lua#L1-L78](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/user/adapters/redis/session.lua#L1-L78)
- [internal/app/app.go#L37-L38](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/app/app.go#L37-L38)
