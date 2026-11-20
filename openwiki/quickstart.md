---
type: guide
title: "Навигация по Go API"
description: "Карта Go API Cellestial, хранилищ, HTTP-границы, runtime и локальных проверок. Порядок чтения OpenWiki."
tags: [backend, architecture, navigation, openwiki]
verified:
  - by: openwiki/0.7.2
    at: 2026-11-20T18:28:51.294Z
sources:
  - id: openwiki-source-8037e2358a2c4f9b2c722a11
    resource: repo://AGENTS.md
  - id: openwiki-source-c35a1716a4af1d6a4899a1c3
    resource: repo://cmd/main/main.go
  - id: openwiki-source-86b6f7183f24ef4d377f93e1
    resource: repo://docs/openapi.md
  - id: openwiki-source-a009d602e55f848ea26930dd
    resource: repo://internal/app/app.go
  - id: openwiki-source-5450c45ea05d5ffd1d8ee804
    resource: repo://internal/app/run.go
  - id: openwiki-source-c1af176bb103419425143738
    resource: repo://internal/notebook/adapters/postgres/notebook.go
  - id: openwiki-source-55b426b919298d5817843eb0
    resource: repo://internal/transport/http/router.go
  - id: openwiki-source-012f2c78e3b1446dfc35803f
    resource: repo://Makefile
  - id: openwiki-source-aff45068e9f0fd7fda322f9c
    resource: repo://migrations/migrations.go
generated: { by: "codex", at: "2026-11-20T18:28:51.294Z" }
---

# Навигация по Go API

## Как читать OpenWiki

Начни с `openwiki_search`, затем вызови `openwiki_read` для нужной страницы и разделов, передав ID wiki из результата поиска. При достаточном ответе без противоречий на этом можно остановиться. Актуальность опубликованной wiki обеспечивает процесс обновления и публикации. К исходнику переходи при пробеле, противоречии, ошибке инструмента или для правки. Для простой правки известного файла можно сразу открыть код.

Без MCP открой эту страницу и подходящую Markdown-страницу из карты ниже. Правило остановки то же. Если wiki отсутствует, начни с документации репозитория и исходников. Для вопроса о нескольких репозиториях используй общий workspace и проверяй, к какой wiki относится результат.

`repo://` в Claims обозначает файл backend на `gitHead` из [checkpoint](.last-update.json). Страницы и Claims производны от исходников. Перед правкой сверяй актуальный код. Состояние прода требует отдельной проверки стенда.

## Куда идти по задаче

| Вопрос | Страница и путь к реализации |
| --- | --- |
| Как проверяются S2S, Bearer и TOTP? | [Доступ к API и Redis-сессии](architecture/auth-sessions.md), `internal/transport/http`, `internal/user/usecase` |
| Где хранятся ячейки, как меняется ревизия и кто может читать? | [Блокноты, ревизии и доступ](architecture/notebooks.md), `internal/notebook` |
| Как меняются логин, пароль и аватар? | [Профиль, пароль и аватары](architecture/profiles-avatars.md), `internal/user/usecase` |
| Как создаётся или удаляется VPS? | [Подготовка окружений](architecture/runtime-environments.md), `internal/runtime` |
| Как исполняются ячейки и сохраняются результаты? | [Runtime-сессии](architecture/runtime-sessions.md), `internal/runtimesession` |
| Где конфигурация, контракт и команды проверок? | [Запуск, HTTP-контракт и проверки](operations/startup-contract.md), `internal/config`, `Makefile` |

## Карта Go API

`cmd/main/main.go` читает конфигурацию из `internal/config` и передаёт управление в `internal/app`. Приложение собирает хранилища, HTTP-обработчики и воркеры. SQL из `migrations/` встроен и применяется через goose до запуска HTTP.

`internal/transport/http` задаёт request ID, журнал запросов, S2S, Bearer, ограничения тела и валидацию запроса. Браузерную cookie-сессию ведёт BFF во frontend. В `internal/user` и `internal/notebook` обработчики `delivery/http` вызывают сценарии `usecase` через интерфейсы. `domain` хранит типы и правила, `adapters` реализует взаимодействие с хранилищами и внешними сервисами.

PostgreSQL хранит пользователей, блокноты по ячейкам, окружения, runtime-сессии и результаты. Код и документ ipynb описаны в `internal/notebook/adapters/ipynb`. Redis хранит состояние авторизации и лимиты попыток, S3 — аватары. `internal/runtime` управляет подготовкой и удалением VPS, а `internal/runtimesession` связывает обработчики пользовательских сессий с очередью, журналом событий и Go-клиентом агента.

## Разработка и границы

В `Makefile` есть `make doctor`, `make up`, `make build`, `make check-fast`, `make check` и `make docker-build`. `make build` собирает `cmd/main`. Тесты находятся рядом с логикой в `internal/`, `make test` запускает `scripts/test.sh` с race и проверкой покрытия. Порядок локального запуска описан в `docs/local-development.md`.

Исходник контракта, который использует текущая сборка, находится в `spec/openapi.json`. Реализацию операций проверяй в delivery, usecase и роутере. Самостоятельный Python runtime-agent находится за границей корпуса этой wiki. Общепроектную карту и будущие функции ищи в `docs` команды, клиент и BFF — во `frontend`, выкладку — в `infra`.

## Источники

- [cmd/main/main.go#L12-L29](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/cmd/main/main.go#L12-L29)
- [internal/app/app.go#L58-L95](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/app/app.go#L58-L95)
- [internal/app/app.go#L97-L115](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/app/app.go#L97-L115)
- [internal/app/app.go#L129-L147](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/app/app.go#L129-L147)
- [internal/app/app.go#L97-L116](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/app/app.go#L97-L116)
- [internal/notebook/adapters/postgres/notebook.go#L91-L104](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/postgres/notebook.go#L91-L104)
- [internal/notebook/adapters/postgres/notebook.go#L146-L155](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/postgres/notebook.go#L146-L155)
- [AGENTS.md#L20-L29](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/AGENTS.md#L20-L29)
- [internal/app/app.go#L129-L152](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/app/app.go#L129-L152)
- [internal/app/run.go#L28-L49](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/app/run.go#L28-L49)
- [internal/app/app.go#L82-L96](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/app/app.go#L82-L96)
- [migrations/migrations.go#L15-L35](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/migrations/migrations.go#L15-L35)
- [Makefile#L58-L79](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/Makefile#L58-L79)
- [docs/openapi.md#L1-L9](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/docs/openapi.md#L1-L9)
- [internal/transport/http/router.go#L31-L64](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/transport/http/router.go#L31-L64)
- [AGENTS.md#L23-L26](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/AGENTS.md#L23-L26)
- [Makefile#L16-L29](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/Makefile#L16-L29)
- [Makefile#L31-L56](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/Makefile#L31-L56)
- [docs/local-development.md#L9-L15](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/docs/local-development.md#L9-L15)
- [internal/notebook/adapters/ipynb/document.go#L266-L290](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/ipynb/document.go#L266-L290)
- [openwiki/INSTRUCTIONS.md#L31-L37](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/openwiki/INSTRUCTIONS.md#L31-L37)
- [openwiki/INSTRUCTIONS.md#L39-L49](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/openwiki/INSTRUCTIONS.md#L39-L49)
