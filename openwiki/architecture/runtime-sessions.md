---
type: guide
title: "Runtime-сессии, команды и результаты"
description: "Пользовательские Python-сессии Go API, очередь исполнения, архив событий, проекция результатов и доступ к рабочим файлам."
tags: [runtime, notebooks, execution, persistence]
verified:
  - by: openwiki/0.7.2
    at: 2026-11-20T18:28:51.294Z
sources:
  - id: openwiki-source-472a92a4b9dff57077e8a12e
    resource: repo://internal/notebook/adapters/postgres/results.go
  - id: openwiki-source-48bf01205fdfca7cf0171d6d
    resource: repo://internal/runtimesession/adapters/postgres/batch.go
  - id: openwiki-source-04edcb13a308844a2fc36d1b
    resource: repo://internal/runtimesession/adapters/postgres/events.go
  - id: openwiki-source-a0b6c6658aa2e1d9bc9da3ab
    resource: repo://internal/runtimesession/adapters/postgres/executions.go
  - id: openwiki-source-7139a382e9f6eafb6310bbf2
    resource: repo://internal/runtimesession/adapters/postgres/kernel.go
  - id: openwiki-source-83e556320ac846778a36311f
    resource: repo://internal/runtimesession/adapters/postgres/store.go
  - id: openwiki-source-34a8cb6c89960426dd838490
    resource: repo://internal/runtimesession/delivery/http/files.go
  - id: openwiki-source-25818d23c61833b5bb951e95
    resource: repo://internal/runtimesession/domain/models.go
  - id: openwiki-source-1ffb7d9cf9148f95875bca19
    resource: repo://internal/runtimesession/usecase/commands.go
  - id: openwiki-source-392ddcda4317b0d8235e5d81
    resource: repo://internal/runtimesession/usecase/files.go
  - id: openwiki-source-3b23e5f2186df2894ba60293
    resource: repo://internal/runtimesession/usecase/session.go
  - id: openwiki-source-a0312b34e0f1afc9120baf12
    resource: repo://internal/runtimesession/usecase/stop_executions.go
  - id: openwiki-source-f2121fc2bf50c417da142756
    resource: repo://internal/runtimesession/usecase/worker.go
  - id: openwiki-source-55b426b919298d5817843eb0
    resource: repo://internal/transport/http/router.go
generated: { by: "codex", at: "2026-11-20T18:28:51.294Z" }
---

# Runtime-сессии, команды и результаты

## Запуск и владение

Создание runtime-сессии записывает в PostgreSQL намерение запустить Python для блокнота владельца. На владельца допускается одна активная сессия. Повтор `request_id` возвращает существующую запись при совпадающих блокноте и языке. Смена этих параметров с тем же ID даёт конфликт. Воркер по аренде отдельно запрашивает [окружение](runtime-environments.md) и переводит сессию в готовую после подтверждения агентом её ID, языка и готовности. До вызова создания окружения воркер сохраняет контрольную запись, чтобы восстановиться после неоднозначного ответа.

## Приём и отправка команд

Одиночное исполнение под блокировкой блокнота проверяет владельца, готовность сессии и свободное место в очереди. Backend читает исходник code-ячейки из PostgreSQL и сверяет SHA-256 с `source_hash`. В очередь сохраняются снимок исходника, его версия и номер попытки. Повтор execution ID с теми же параметрами возвращает запись. Пакет дополнительно проверяет порядок ячеек и принимает их атомарно, возвращая прежние ID при повторе неизменного запроса.

Воркер читает события агента и передаёт одну ожидающую команду с сохранённым ID. Переполнение очереди агента оставляет команду в очереди backend. Инспекция переменных проходит через очередь с отдельным типом результата. Interrupt и restart ограничены по времени и используют блокировку приёма команд. Отдельная остановка исполнений запрашивает остановку команд и дочитывает события с сохранением окружения.

## События и результат в блокноте

PostgreSQL архивирует события вместе с курсором агента и изменениями состояния исполнения. Публичное чтение использует отдельный курсор и проверяет владельца. Само чтение не продлевает активность сессии.

Отложенная проекция переносит вывод в [ячейку блокнота](notebooks.md), только если попытка последняя и версия исходника совпадает. Изменённый исходник или более новая попытка приводят к конфликту проекции, при этом архив исполнения остаётся доступен. Запись результата обновляет данные ячейки без смены ревизии редактирования. Сохранение результата и отметки проекции выполняется в одной транзакции.

При остановке сессии воркер пытается остановить агент и сохранить доступный журнал. Незавершённые попытки с недостоверным исходом получают состояние `UNKNOWN`. Пользовательский слот освобождается после остановки и попыток архивации, а воркер продолжает следить за удалением окружения.

## Файлы рабочего каталога

Диспетчер Workspace работает после S2S и Bearer. Он проверяет HTTP-метод, операцию и размер запроса, затем usecase проверяет владельца и состояние `READY` перед каждым обращением. Допустимые запросы передаются Go-клиенту агента, ответ идёт потоком с `private, no-store`. Закрытое рабочее пространство даёт 410.

`internal/app/runtime_integration_test.go` охватывает HTTP-поток с fake-окружением. `internal/runtimesession/adapters/postgres/owner_test.go` проверяет конкурентный запуск, а `projection_test.go` — отложенную проекцию и защиту от записи результата поверх новой попытки.

[Подготовка окружений](runtime-environments.md) · [Доступ к API](auth-sessions.md) · [Карта Go API](../quickstart.md)

## Источники

- [internal/runtimesession/domain/models.go#L60-L76](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/domain/models.go#L60-L76)
- [internal/runtimesession/usecase/session.go#L33-L41](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/usecase/session.go#L33-L41)
- [internal/runtimesession/adapters/postgres/store.go#L107-L154](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/adapters/postgres/store.go#L107-L154)
- [internal/runtimesession/usecase/worker.go#L116-L161](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/usecase/worker.go#L116-L161)
- [internal/runtimesession/usecase/worker.go#L160-L188](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/usecase/worker.go#L160-L188)
- [internal/runtimesession/adapters/postgres/executions.go#L113-L154](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/adapters/postgres/executions.go#L113-L154)
- [internal/runtimesession/adapters/postgres/executions.go#L155-L193](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/adapters/postgres/executions.go#L155-L193)
- [internal/runtimesession/adapters/postgres/batch.go#L46-L94](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/adapters/postgres/batch.go#L46-L94)
- [internal/runtimesession/adapters/postgres/batch.go#L94-L136](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/adapters/postgres/batch.go#L94-L136)
- [internal/runtimesession/adapters/postgres/batch.go#L138-L156](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/adapters/postgres/batch.go#L138-L156)
- [internal/runtimesession/usecase/worker.go#L354-L388](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/usecase/worker.go#L354-L388)
- [internal/runtimesession/usecase/commands.go#L30-L66](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/usecase/commands.go#L30-L66)
- [internal/runtimesession/usecase/stop_executions.go#L12-L47](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/usecase/stop_executions.go#L12-L47)
- [internal/runtimesession/adapters/postgres/kernel.go#L14-L49](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/adapters/postgres/kernel.go#L14-L49)
- [internal/runtimesession/adapters/postgres/events.go#L16-L41](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/adapters/postgres/events.go#L16-L41)
- [internal/runtimesession/adapters/postgres/events.go#L52-L98](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/adapters/postgres/events.go#L52-L98)
- [internal/runtimesession/adapters/postgres/events.go#L100-L131](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/adapters/postgres/events.go#L100-L131)
- [internal/runtimesession/adapters/postgres/events.go#L152-L200](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/adapters/postgres/events.go#L152-L200)
- [internal/runtimesession/adapters/postgres/events.go#L203-L226](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/adapters/postgres/events.go#L203-L226)
- [internal/notebook/adapters/postgres/results.go#L65-L109](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/postgres/results.go#L65-L109)
- [internal/runtimesession/usecase/worker.go#L421-L428](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/usecase/worker.go#L421-L428)
- [internal/runtimesession/usecase/worker.go#L463-L521](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/usecase/worker.go#L463-L521)
- [internal/runtimesession/adapters/postgres/executions.go#L202-L230](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/adapters/postgres/executions.go#L202-L230)
- [internal/transport/http/router.go#L43-L63](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/transport/http/router.go#L43-L63)
- [internal/runtimesession/delivery/http/files.go#L16-L65](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/delivery/http/files.go#L16-L65)
- [internal/runtimesession/delivery/http/files.go#L66-L107](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/delivery/http/files.go#L66-L107)
- [internal/runtimesession/usecase/files.go#L15-L35](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/usecase/files.go#L15-L35)
- [internal/app/app.go#L129-L135](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/app/app.go#L129-L135)
- [internal/app/runtime_integration_test.go#L44](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/app/runtime_integration_test.go#L44)
- [internal/runtimesession/adapters/postgres/owner_test.go#L16](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/adapters/postgres/owner_test.go#L16)
- [internal/runtimesession/adapters/postgres/projection_test.go#L17](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/adapters/postgres/projection_test.go#L17)
