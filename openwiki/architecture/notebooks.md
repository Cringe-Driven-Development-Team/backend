---
type: guide
title: "Блокноты: ячейки, ревизии и доступ"
description: "Хранение в PostgreSQL, проверка ревизии при редактировании, список, режимы чтения и независимые копии."
tags: [notebooks, persistence, ipynb, sharing]
verified:
  - by: openwiki/0.7.2
    at: 2026-11-20T18:28:51.294Z
sources:
  - id: openwiki-source-2b59619f074aeb893d96a0f9
    resource: repo://internal/notebook/adapters/ipynb/document.go
  - id: openwiki-source-517011d1027a07f39186a00d
    resource: repo://internal/notebook/adapters/ipynb/ipynb.go
  - id: openwiki-source-d33dbe50e3c0c2adf4ebf2d1
    resource: repo://internal/notebook/adapters/postgres/access.go
  - id: openwiki-source-c96f532e1f832ed854414a3f
    resource: repo://internal/notebook/adapters/postgres/delete.go
  - id: openwiki-source-18ae44dd0a97a67b3227a5d2
    resource: repo://internal/notebook/adapters/postgres/editing.go
  - id: openwiki-source-ab643930eec2d443e83ca4b5
    resource: repo://internal/notebook/adapters/postgres/list.go
  - id: openwiki-source-c1af176bb103419425143738
    resource: repo://internal/notebook/adapters/postgres/notebook.go
  - id: openwiki-source-eb33162e41e8867fc6790896
    resource: repo://internal/notebook/adapters/postgres/sharing.go
  - id: openwiki-source-b1d926fa00f4ede267bc3709
    resource: repo://internal/notebook/delivery/http/editing.go
  - id: openwiki-source-e3559343d5d14b0e14440834
    resource: repo://internal/notebook/domain/sharing.go
  - id: openwiki-source-d414ea889014eeb39ea9591f
    resource: repo://internal/notebook/usecase/list.go
  - id: openwiki-source-0be68ea43b41f70debccebfc
    resource: repo://internal/notebook/usecase/notebook.go
  - id: openwiki-source-6c7fe7a1d6a90b854b04a6af
    resource: repo://internal/transport/http/middleware/notebook_cache.go
  - id: openwiki-source-55b426b919298d5817843eb0
    resource: repo://internal/transport/http/router.go
  - id: openwiki-source-14e8f7fac0b244c1e1c65190
    resource: repo://migrations/20261117225455_add_notebook_sharing.sql
generated: { by: "codex", at: "2026-11-20T18:28:51.294Z" }
---

# Блокноты: ячейки, ревизии и доступ

## Хранение и формат

HTTP-обработчики получают посетителя из контекста и вызывают `NotebookUsecase`. Репозиторий PostgreSQL хранит заголовок документа в `notebooks`, порядок и исходники в `notebook_cells`, результаты и вложения в `notebook_cell_data`. Создание нормализует имя и пишет документ nbformat 4.5 с одной пустой code-ячейкой в одной транзакции.

Кодек ipynb принимает `source` строкой или массивом строк. `Parse` читает code и markdown, `Build` собирает новый nbformat 4.5 с пустыми outputs и null execution_count для code. Для полного документа `DecodeDocument` и `EncodeDocument` работают также с метаданными, вложениями, результатами и дополнительными JSON-полями. Перед записью репозиторий нормализует поля и считает размер сериализованного документа.

## Редактирование

Content возвращает ячейки и непрозрачную UUID-ревизию в поле `hash` и заголовке `ETag`. Редактирование принимает один сильный `If-Match`. Репозиторий блокирует строку по ID блокнота и владельца, затем сверяет ревизию. Устаревшая ревизия даёт 409. При изменении обновляются затронутые строки и новая ревизия в одной транзакции, а повтор прежнего текста или перемещение на текущую позицию сохраняет прежнюю ревизию. Смена текста создаёт новую версию исходника, у code-ячейки сбрасываются результаты и счётчик исполнения.

Удаление под блокировкой владельца проверяет активное состояние runtime, незавершённую очистку, аренду и ожидающие проекции. Когда проверки пройдены, завершённая история runtime удаляется вместе с блокнотом в одной транзакции.

## Доступ и список

Чтение разрешено владельцу, любому посетителю в режиме `public` и указанным пользователям в режиме `restricted`. Метаданные и ячейки читаются в одном снимке PostgreSQL. Недоступный и отсутствующий блокноты дают 404. Middleware выставляет `no-store` на чтении и настройках доступа, включая ранние ошибки. Правки и управление доступом принадлежат владельцу. Замена списка читателей и режима атомарна. Копирование требует входа, проверяет право после блокировки исходника и записывает отдельный приватный документ на имя запрашивающего пользователя.

Список читает сводки и счётчики по владельцу, сортирует по `updated_at DESC, id DESC` и поддерживает поиск по имени. Курсор привязан к владельцу и поисковому запросу. Usecase помечает запись, для которой у пользователя есть активная [runtime-сессия](runtime-sessions.md).

Сценарии PostgreSQL проверяют матрицу доступа, отзыв доступа перед копированием и отсутствие частично записанной копии. См. `internal/notebook/adapters/postgres/sharing_integration_test.go`, `clone_concurrency_test.go` и `large_integration_test.go`.

[Доступ к API](auth-sessions.md) · [Карта Go API](../quickstart.md)

## Источники

- [internal/notebook/usecase/notebook.go#L55-L72](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/usecase/notebook.go#L55-L72)
- [internal/notebook/adapters/postgres/access.go#L12-L46](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/postgres/access.go#L12-L46)
- [internal/notebook/adapters/postgres/list.go#L14-L66](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/postgres/list.go#L14-L66)
- [internal/notebook/usecase/notebook.go#L24-L52](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/usecase/notebook.go#L24-L52)
- [internal/notebook/adapters/postgres/notebook.go#L71-L104](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/postgres/notebook.go#L71-L104)
- [internal/notebook/adapters/postgres/notebook.go#L146-L155](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/postgres/notebook.go#L146-L155)
- [internal/notebook/adapters/postgres/editing.go#L40-L98](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/postgres/editing.go#L40-L98)
- [internal/notebook/adapters/postgres/editing.go#L153-L206](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/postgres/editing.go#L153-L206)
- [internal/notebook/adapters/ipynb/ipynb.go#L34-L108](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/ipynb/ipynb.go#L34-L108)
- [internal/notebook/adapters/postgres/notebook.go#L137-L143](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/postgres/notebook.go#L137-L143)
- [internal/notebook/adapters/postgres/editing.go#L40-L57](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/postgres/editing.go#L40-L57)
- [internal/notebook/adapters/postgres/editing.go#L99-L149](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/postgres/editing.go#L99-L149)
- [internal/notebook/delivery/http/editing.go#L17-L92](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/delivery/http/editing.go#L17-L92)
- [internal/notebook/adapters/ipynb/document.go#L27-L128](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/ipynb/document.go#L27-L128)
- [internal/notebook/adapters/ipynb/document.go#L266-L290](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/ipynb/document.go#L266-L290)
- [internal/notebook/adapters/postgres/notebook.go#L107-L155](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/postgres/notebook.go#L107-L155)
- [internal/notebook/domain/sharing.go#L23-L35](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/domain/sharing.go#L23-L35)
- [internal/notebook/adapters/postgres/sharing.go#L12-L74](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/postgres/sharing.go#L12-L74)
- [internal/notebook/adapters/postgres/access.go#L49-L94](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/postgres/access.go#L49-L94)
- [internal/notebook/adapters/postgres/notebook.go#L91-L104](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/postgres/notebook.go#L91-L104)
- [migrations/20261117225455_add_notebook_sharing.sql#L1-L10](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/migrations/20261117225455_add_notebook_sharing.sql#L1-L10)
- [internal/notebook/adapters/postgres/list.go#L24-L57](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/postgres/list.go#L24-L57)
- [internal/notebook/usecase/list.go#L10-L52](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/usecase/list.go#L10-L52)
- [internal/transport/http/middleware/notebook_cache.go#L8-L23](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/transport/http/middleware/notebook_cache.go#L8-L23)
- [internal/transport/http/router.go#L61-L64](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/transport/http/router.go#L61-L64)
- [internal/notebook/adapters/postgres/delete.go#L11-L79](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/notebook/adapters/postgres/delete.go#L11-L79)
