---
type: guide
title: "Запуск, HTTP-контракт и проверки"
description: "Сборка Go API, миграции, остановка, граница Apidog и команды проверки."
tags: [operations, api, testing]
verified:
  - by: openwiki/0.7.2
    at: 2026-10-31T14:34:07.758Z
sources:
  - id: openwiki-source-1d795ecce182369dda32d227
    resource: repo://docs/apidog.md
  - id: openwiki-source-a009d602e55f848ea26930dd
    resource: repo://internal/app/app.go
  - id: openwiki-source-5450c45ea05d5ffd1d8ee804
    resource: repo://internal/app/run.go
  - id: openwiki-source-a8910515ddd14810ad43f5c1
    resource: repo://internal/config/config.go
  - id: openwiki-source-3d8ec5b2c80c2b1c7077f0ce
    resource: repo://internal/config/redis.go
  - id: openwiki-source-55b426b919298d5817843eb0
    resource: repo://internal/transport/http/router.go
  - id: openwiki-source-012f2c78e3b1446dfc35803f
    resource: repo://Makefile
  - id: openwiki-source-aff45068e9f0fd7fda322f9c
    resource: repo://migrations/migrations.go
generated: { by: "codex", at: "2026-10-31T14:34:07.758Z" }
---

# Запуск, HTTP-контракт и проверки

## Запуск и отказ до приёма запросов

`cmd/main` настраивает структурный logger, читает config и вызывает app.Run. Конфигурация требует BFF_API_KEY, JWT_SECRET и S3_NOTEBOOKS_BUCKET; Redis host/port/db и timeout валидируются отдельно. Значения секретов и адреса здесь не публикуются: настройку проверяют в `internal/config` и локальной конфигурации окружения.

`newServer` создаёт PostgreSQL pool и Redis client, проверяет PostgreSQL, применяет встроенные goose-миграции и проверяет Redis. Затем собирает SessionStore, security-адаптеры, пользовательский usecase, S3 client, notebook usecase и роутер. Ошибки раннего запуска закрывают ресурсы и не запускают HTTP listener. Создание клиента S3 не является проверкой доступности бакета: явного пробного запроса к нему в newServer нет.

Run ждёт SIGINT/SIGTERM либо ошибку listener. При сигнале даёт HTTP Shutdown десять секунд; при ошибке Shutdown закрывает сервер принудительно. Redis client и PostgreSQL pool освобождаются при выходе. `/health` возвращает alive; обработчик не делает live-проверок PostgreSQL, Redis или S3, поэтому этот ответ не равен готовности всех зависимостей.

## Кто владеет HTTP-контрактом

Канонический контракт принадлежит Apidog. `make generate` запускает cmd/apidog, затем oapi-codegen с `internal/transport/http/api/cfg.yaml`. Сгенерированный `api.gen.go` не редактируют руками и не используют как независимый источник этой wiki. Фактическую реализацию ищут в delivery, usecase и router; изменение контракта проходит через процесс из docs/apidog.md. Контракт и реализация должны проверяться вместе, но наличие пункта в Apidog не доказывает реализованный сценарий.

## Локальный цикл

Версию Go берут из go.mod, зависимости — через `go mod download`. `make doctor` проверяет инструменты; `make build` собирает bin/server. `make up` запускает Compose с ожиданием health, `make logs` читает логи, `make down` останавливает окружение. Compose здесь предназначен для разработки; production-конфигурация принадлежит infra.

`make check-fast` проверяет shell-синтаксис, gofmt и go vet. `make check` дополняет это tidy diff, сборкой, тестами и lint. `make test` вызывает scripts/test.sh с race и coverage. `make docker-build` собирает образ по build/main.Dockerfile. Для изменения auth начни с transport/http и user; для хранения — notebook. Это команды проекта, а не заявление, что каждая из них выполнялась при создании wiki.

Проверки runtime-agent отдельные: Python-сервис не входит в этот корпус. Из его наличия нельзя выводить исполнение Go API на kernel. Развёртывание проверяют по infra и конкретному стенду; main, CI и образ сами по себе не являются свидетельством поведения прода.

[Карта Go API](../quickstart.md) · [Auth и Redis](../architecture/auth-sessions.md) · [Блокноты](../architecture/notebooks.md)

## Источники

- [internal/app/app.go#L29-L94](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/app/app.go#L29-L94)
- [migrations/migrations.go#L15-L34](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/migrations/migrations.go#L15-L34)
- [internal/app/run.go#L26-L52](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/app/run.go#L26-L52)
- [internal/transport/http/router.go#L34-L39](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/transport/http/router.go#L34-L39)
- [docs/apidog.md#L1-L7](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/docs/apidog.md#L1-L7)
- [Makefile#L55-L59](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/Makefile#L55-L59)
- [Makefile#L25-L35](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/Makefile#L25-L35)
- [internal/config/config.go#L72-L91](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/config/config.go#L72-L91)
- [internal/config/redis.go#L11-L44](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/config/redis.go#L11-L44)
