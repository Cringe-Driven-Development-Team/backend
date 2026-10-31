---
type: guide
title: "Навигация по Go API"
description: "Карта backend: сборка приложения, авторизация, хранение блокнотов и владельцы соседних систем."
tags: [backend, navigation, architecture]
verified:
  - by: openwiki/0.7.2
    at: 2026-10-31T14:34:07.758Z
sources:
  - id: openwiki-source-8037e2358a2c4f9b2c722a11
    resource: repo://AGENTS.md
  - id: openwiki-source-c35a1716a4af1d6a4899a1c3
    resource: repo://cmd/main/main.go
  - id: openwiki-source-a009d602e55f848ea26930dd
    resource: repo://internal/app/app.go
  - id: openwiki-source-012f2c78e3b1446dfc35803f
    resource: repo://Makefile
generated: { by: "codex", at: "2026-10-31T14:34:07.758Z" }
---

# Навигация по Go API

## Что описывает wiki

Это производная карта Go API backend на `30d62561c9e2c067764239e4b68dd477c9abb982`. Она описывает код, а не проверенное поведение прода. `repo://` в Claims обозначает файл этого backend на `gitHead` из `.last-update.json`. Страницы и Claims не заменяют исходник: перед изменением сверяй актуальный код.

Проверка свежести из корня клона: `bun scripts/openwiki-check.ts --against <полный актуальный SHA backend/main>`. При stale используй wiki как указатель. Checkpoint одной wiki не подтверждает соседние репозитории или выкладку.

## Куда идти по задаче

| Вопрос | Страница и отправная точка |
| --- | --- |
| Почему запрос отклонён до обработчика? | [BFF, авторизация и Redis](architecture/auth-sessions.md): transport/http/router.go, security/bff.go |
| Что происходит при refresh/logout или отказе Redis? | [Жизненный цикл сессии](architecture/auth-sessions.md): user/usecase и adapters/redis |
| Где лежат блоки и как проверяется владелец? | [Блокноты и ipynb](architecture/notebooks.md): notebook/usecase и adapters |
| Как запускается API, миграции и проверки? | [Запуск и контракт](operations/startup-contract.md): internal/app, config, migrations, Makefile |
| Кто определяет HTTP-запросы и ответы? | [Apidog и генерация](operations/startup-contract.md), docs/apidog.md |

## Карта слоёв

`cmd/main` читает конфигурацию и запускает `internal/app`. App связывает PostgreSQL, Redis, S3, usecase и HTTP-router. `internal/config` отвечает за окружение и его проверки, `migrations` — за SQL, встроенный в приложение и применяемый при запуске.

HTTP идёт через `internal/transport/http`: общие middleware, S2S, Bearer, Validator и серверный фасад. Затем `internal/user` или `internal/notebook`: delivery/http переводит транспортные типы и ошибки, usecase выполняет сценарий через порты, domain содержит данные, adapters реализуют работу с внешними системами. Users и notebook metadata хранятся в PostgreSQL, авторитетное состояние сессий — в Redis, файлы блокнотов — в S3 через ipynb-кодек. Проверку access нельзя свести к подписи JWT: она зависит от состояния сессии.

Расширяя сценарий, начни с его usecase и портов, затем проверь delivery, adapter и соседние тесты. Для изменения HTTP-контракта первоисточник — Apidog; generated API не служит самостоятельным доказательством и не редактируется вручную.

## Разработка и границы

Команды: `go mod download`, `make doctor`, `make build`, `make up`, `make test`, `make check`, `make docker-build`. Подробности и требования инструментов находятся в AGENTS.md, Makefile и docs/local-development.md. Тесты размещены рядом с логикой в internal; scripts/test.sh включает race и coverage. Локальный Compose — среда разработки.

Общепроектная карта и проверенное состояние продукта принадлежат docs; клиент и BFF — frontend; production-выкладка — infra. Внутренности пакетов `@cdd-team/*` ищут в frontend-packages по карте docs. Для межрепозиторного вопроса используй общий локальный OpenWiki workspace и сохраняй ID wiki из результатов поиска. Без MCP читай quickstart соответствующего клона и сверяй его собственный checkpoint.

Будущие функции ищут в модулях docs. Самостоятельный Python runtime-agent не входит в корпус этой wiki; его наличие не является свидетельством интеграции исполнения в Go API. Для утверждений о проде нужна отдельная проверка стенда.

## Источники

- [cmd/main/main.go#L12-L29](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/cmd/main/main.go#L12-L29)
- [internal/app/app.go#L29-L94](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/app/app.go#L29-L94)
- [internal/app/app.go#L63-L84](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/app/app.go#L63-L84)
- [AGENTS.md#L3-L17](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/AGENTS.md#L3-L17)
- [Makefile#L16-L20](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/Makefile#L16-L20)
