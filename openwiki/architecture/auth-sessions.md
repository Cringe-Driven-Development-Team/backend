---
type: guide
title: "Доступ к API и Redis-сессии"
description: "Порядок S2S и Bearer, вход с TOTP, ротация refresh и отзыв авторизации."
tags: [auth, redis, security]
verified:
  - by: openwiki/0.7.2
    at: 2026-11-20T18:28:51.294Z
sources:
  - id: openwiki-source-a009d602e55f848ea26930dd
    resource: repo://internal/app/app.go
  - id: openwiki-source-55b426b919298d5817843eb0
    resource: repo://internal/transport/http/router.go
  - id: openwiki-source-39d8294d108f5e15812daead
    resource: repo://internal/transport/http/security/bff.go
  - id: openwiki-source-a0aad76558c10a7db97b7b49
    resource: repo://internal/user/adapters/redis/limiter.go
  - id: openwiki-source-dbc18a3bac0b60994e4038e4
    resource: repo://internal/user/adapters/redis/session.go
  - id: openwiki-source-c91b579389d2163426bf78c6
    resource: repo://internal/user/adapters/redis/session.lua
  - id: openwiki-source-b20e27401d8bdee4383801d7
    resource: repo://internal/user/usecase/twofactor.go
  - id: openwiki-source-1f5edfaa55907e3a11216234
    resource: repo://internal/user/usecase/user.go
generated: { by: "codex", at: "2026-11-20T18:28:51.294Z" }
---

# Доступ к API и Redis-сессии

## Граница BFF и пользовательский доступ

`NewRouter` требует непустой `BFF_API_KEY`. Request ID и журнал запросов охватывают весь роутер, включая ранние отказы, 404 и 405. На `/api/v1` и вложенных путях S2S проверяет единственное значение `X-BFF-Key` и при отказе возвращает 403 до Bearer, ограничений тела и OpenAPI Validator. `GET /health` доступен отдельно.

Backend извлекает пользовательскую сессию из `Authorization`. Middleware сначала проверяет токен, затем вызывает `ValidateAccess` и только после этого помещает principal в контекст. Cookie-сессию ведёт BFF во frontend. Для двух GET-маршрутов чтения [блокнота](notebooks.md) запрос без заголовка может быть гостевым, а присутствующий недействительный Bearer даёт 401.

## Регистрация и вход

Register хеширует пароль и записывает пользователя в PostgreSQL до создания сессии. Пользователь остаётся в базе, если Redis после этой записи вернул ошибку. Register и Login фиксируют версию авторизации перед чтением актуальных учётных данных. Redis отклоняет создание сессии, если после чтения версии произошёл отзыв. Новая сессия получает SID, а хранилище получает хеш refresh-токена.

Настройка TOTP сохраняет зашифрованный секрет в состоянии ожидания, подтверждение включает второй фактор. Вход с включённым TOTP требует код после проверки пароля под блокировкой учётной записи. Принятый временной шаг нельзя использовать повторно. Отключение требует текущий пароль и очищает секрет. Redis ограничивает попытки входа и операций TOTP.

## Ротация и отзыв

Refresh читает сессию по хешу, проверяет срок и передаёт замену в Rotate. Lua перепроверяет записи, сохраняет SID и версию, заменяет refresh-индекс и выбирает больший из старого и нового срока access. Logout сверяет refresh с claims и удаляет обе записи сессии. RevokeAll меняет версию пользователя, после чего прежние токены не проходят проверку.

Redis используется как standalone primary для состояния сессий. Lua проверяет версию и сроки по Redis TIME. Чтение не продлевает TTL. Операции ограничены таймаутом, а повторы клиента отключены. Если мутация вернула таймаут, запись могла уже состояться.

Сценарии покрывают `internal/user/usecase/auth_race_test.go`, `internal/transport/http/session_integration_test.go`, `internal/transport/http/twofactor_integration_test.go` и `internal/user/adapters/redis/unknown_outcome_test.go`.

[Профиль и аватары](profiles-avatars.md) · [Запуск и HTTP-контракт](../operations/startup-contract.md) · [Карта Go API](../quickstart.md)

## Источники

- [internal/transport/http/router.go#L31-L63](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/transport/http/router.go#L31-L63)
- [internal/transport/http/security/bff.go#L17-L40](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/transport/http/security/bff.go#L17-L40)
- [internal/transport/http/security/bff.go#L42-L92](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/transport/http/security/bff.go#L42-L92)
- [internal/user/usecase/user.go#L38-L76](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/usecase/user.go#L38-L76)
- [internal/user/usecase/user.go#L129-L169](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/usecase/user.go#L129-L169)
- [internal/user/adapters/redis/session.lua#L80-L107](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/adapters/redis/session.lua#L80-L107)
- [internal/app/app.go#L71-L73](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/app/app.go#L71-L73)
- [internal/user/adapters/redis/session.go#L20-L35](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/adapters/redis/session.go#L20-L35)
- [internal/user/adapters/redis/session.go#L80-L99](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/adapters/redis/session.go#L80-L99)
- [internal/user/adapters/redis/session.lua#L1-L78](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/adapters/redis/session.lua#L1-L78)
- [internal/user/usecase/user.go#L54-L76](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/usecase/user.go#L54-L76)
- [internal/user/usecase/user.go#L91-L118](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/usecase/user.go#L91-L118)
- [internal/user/usecase/user.go#L181-L206](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/usecase/user.go#L181-L206)
- [internal/user/adapters/redis/session.lua#L69-L77](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/adapters/redis/session.lua#L69-L77)
- [internal/user/usecase/twofactor.go#L31-L85](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/usecase/twofactor.go#L31-L85)
- [internal/user/usecase/twofactor.go#L124-L142](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/usecase/twofactor.go#L124-L142)
- [internal/user/usecase/user.go#L98-L118](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/usecase/user.go#L98-L118)
- [internal/app/app.go#L97-L105](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/app/app.go#L97-L105)
- [internal/user/usecase/user.go#L79-L98](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/usecase/user.go#L79-L98)
- [internal/user/usecase/twofactor.go#L24-L34](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/usecase/twofactor.go#L24-L34)
- [internal/user/usecase/twofactor.go#L68-L108](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/usecase/twofactor.go#L68-L108)
- [internal/user/adapters/redis/limiter.go#L25-L42](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/adapters/redis/limiter.go#L25-L42)
