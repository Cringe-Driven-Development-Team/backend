---
type: guide
title: "Запуск, HTTP-контракт и проверки"
description: "Инициализация Go API, настройки, проверка контракта и передача выпуска по digest."
tags: [startup, openapi, testing, release]
verified:
  - by: openwiki/0.7.2
    at: 2026-11-20T18:28:51.294Z
sources:
  - id: openwiki-source-164e2da859b5277df81c7d94
    resource: repo://.github/workflows/ci.yml
  - id: openwiki-source-0a2b44962b4b8ac526bb9ca5
    resource: repo://build/main.Dockerfile
  - id: openwiki-source-c35a1716a4af1d6a4899a1c3
    resource: repo://cmd/main/main.go
  - id: openwiki-source-86b6f7183f24ef4d377f93e1
    resource: repo://docs/openapi.md
  - id: openwiki-source-3da928d96895d057d35011cd
    resource: repo://docs/release.md
  - id: openwiki-source-a009d602e55f848ea26930dd
    resource: repo://internal/app/app.go
  - id: openwiki-source-5450c45ea05d5ffd1d8ee804
    resource: repo://internal/app/run.go
  - id: openwiki-source-a8910515ddd14810ad43f5c1
    resource: repo://internal/config/config.go
  - id: openwiki-source-3d8ec5b2c80c2b1c7077f0ce
    resource: repo://internal/config/redis.go
  - id: openwiki-source-9c745fdd65f057e1eb39cdbd
    resource: repo://internal/config/runtime.go
  - id: openwiki-source-eaf813a1f64e000964a02a75
    resource: repo://internal/observability/handler.go
  - id: openwiki-source-8b07da6a54c57d952fe486f6
    resource: repo://internal/storage/s3client/client.go
  - id: openwiki-source-55b426b919298d5817843eb0
    resource: repo://internal/transport/http/router.go
  - id: openwiki-source-012f2c78e3b1446dfc35803f
    resource: repo://Makefile
  - id: openwiki-source-aff45068e9f0fd7fda322f9c
    resource: repo://migrations/migrations.go
  - id: openwiki-source-2161525089da862e880baf7a
    resource: repo://scripts/test.sh
generated: { by: "codex", at: "2026-11-20T18:28:51.294Z" }
---

# Запуск, HTTP-контракт и проверки

## Запуск приложения

`cmd/main/main.go` настраивает структурный JSON-логгер, читает конфигурацию и вызывает `app.Run`. `internal/config/config.go` требует `BFF_API_KEY`, `JWT_SECRET`, `TOTP_ENCRYPTION_KEY`, бакет аватаров и допустимый публичный URL. Настройки Redis и обязательный runtime-провайдер проверяются отдельно в `internal/config/redis.go` и `internal/config/runtime.go`.

При запуске приложение проверяет PostgreSQL, применяет встроенные goose-миграции и проверяет Redis до создания HTTP-сервера. S3-клиент создаётся из конфигурации SDK без пробного запроса к бакету. При ошибке запуска уже открытые зависимости закрываются. Приложение передаёт request ID в структурные логи через контекст.

`Run` запускает воркеры [окружений](../architecture/runtime-environments.md) и [сессий](../architecture/runtime-sessions.md) рядом с HTTP listener. При SIGINT или SIGTERM сервер получает до десяти секунд на Shutdown. При выходе приложение отменяет и дожидается воркеров перед закрытием Redis и PostgreSQL. `GET /health` отвечает `alive` без обращения к хранилищам.

## Контракт и обработчики

Текущий исходник HTTP-контракта — `spec/openapi.json`. `make contract-lint` проверяет его Redocly, `make generate` затем запускает oapi-codegen по `internal/transport/http/api/cfg.yaml`. `make contract-check` генерирует во временный файл и сравнивает результат с рабочим Go-файлом. Ссылки в этой wiki ведут к Makefile и руководству, которые входят в корпус.

Фактический порядок middleware задаёт `internal/transport/http/router.go`, прикладные ответы определяются в delivery и usecase. Границу [BFF и Bearer](../architecture/auth-sessions.md) проверяют по этим обработчикам.

## Локальная разработка и тесты

Версию Go задаёт `go.mod`. `make doctor` проверяет инструменты, `make build` собирает API. `make up` поднимает Compose с ожиданием health, `make logs` показывает логи, `make down` останавливает окружение.

`make check-fast` проверяет синтаксис shell-скриптов, gofmt и go vet. `make check` также включает проверки release-скриптов и контракта, tidy, сборку, тесты и линтер. `make test` запускает race-тесты и проверяет покрытие не ниже 60%. `make docker-build` собирает образ, в котором Dockerfile упаковывает API, runtime-smoke, артефакты агента и инструменты Ansible/SSH. API запускается от отдельного непривилегированного пользователя.

## Граница выкладки

Конфигурация CI связывает сборку образа с lint, test, contract и kernel-images. При push в main исходного репозитория сборка готовит immutable Release с digest образа. При `PRODUCTION_ENABLED=true` отдельная задача CI передаёт CD тег подготовленного Release. Course-sync имеет собственное условие. CD валидирует Release и передаёт digest в playbook из infra. Состояние конкретной выкладки проверяют по её отдельному запуску и стенду.

[Карта Go API](../quickstart.md)

## Источники

- [internal/app/app.go#L71-L109](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/app/app.go#L71-L109)
- [migrations/migrations.go#L15-L35](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/migrations/migrations.go#L15-L35)
- [internal/storage/s3client/client.go#L15-L25](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/storage/s3client/client.go#L15-L25)
- [internal/app/run.go#L28-L49](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/app/run.go#L28-L49)
- [internal/app/run.go#L50-L81](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/app/run.go#L50-L81)
- [Makefile#L58-L79](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/Makefile#L58-L79)
- [docs/openapi.md#L1-L23](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/docs/openapi.md#L1-L23)
- [Makefile#L16-L36](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/Makefile#L16-L36)
- [scripts/test.sh#L7-L30](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/scripts/test.sh#L7-L30)
- [scripts/test.sh#L40-L60](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/scripts/test.sh#L40-L60)
- [internal/config/config.go#L74-L105](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/config/config.go#L74-L105)
- [internal/config/redis.go#L11-L44](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/config/redis.go#L11-L44)
- [internal/config/runtime.go#L99-L117](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/config/runtime.go#L99-L117)
- [.github/workflows/ci.yml#L182-L200](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/.github/workflows/ci.yml#L182-L200)
- [.github/workflows/ci.yml#L251-L260](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/.github/workflows/ci.yml#L251-L260)
- [.github/workflows/ci.yml#L278-L308](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/.github/workflows/ci.yml#L278-L308)
- [docs/release.md#L29-L44](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/docs/release.md#L29-L44)
- [docs/release.md#L79-L87](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/docs/release.md#L79-L87)
- [build/main.Dockerfile#L1-L14](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/build/main.Dockerfile#L1-L14)
- [build/main.Dockerfile#L16-L34](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/build/main.Dockerfile#L16-L34)
- [cmd/main/main.go#L12-L29](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/cmd/main/main.go#L12-L29)
- [internal/observability/handler.go#L19-L26](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/observability/handler.go#L19-L26)
- [internal/transport/http/router.go#L35-L40](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/transport/http/router.go#L35-L40)
- [internal/transport/http/router.go#L47-L64](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/transport/http/router.go#L47-L64)
- [Makefile#L38-L56](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/Makefile#L38-L56)
- [go.mod#L3](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/go.mod#L3)
- [docs/local-development.md#L9-L15](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/docs/local-development.md#L9-L15)
- [.github/workflows/cd.yml#L62-L86](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/.github/workflows/cd.yml#L62-L86)
