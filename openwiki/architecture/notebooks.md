---
type: guide
title: "Блокноты: PostgreSQL, S3 и ipynb"
description: "Владение блокнотами, порядок операций с S3 и PostgreSQL, ограничения кодека."
tags: [notebooks, persistence, ipynb]
verified:
  - by: openwiki/0.7.2
    at: 2026-10-31T14:34:07.758Z
sources:
  - id: openwiki-source-517011d1027a07f39186a00d
    resource: repo://internal/notebook/adapters/ipynb/ipynb.go
  - id: openwiki-source-c1af176bb103419425143738
    resource: repo://internal/notebook/adapters/postgres/notebook.go
  - id: openwiki-source-7f9be0e2b42665c2af6d9c69
    resource: repo://internal/notebook/adapters/s3/files.go
  - id: openwiki-source-0be68ea43b41f70debccebfc
    resource: repo://internal/notebook/usecase/notebook.go
generated: { by: "codex", at: "2026-10-31T14:34:07.758Z" }
---

# Блокноты: PostgreSQL, S3 и ipynb

## Владение и слои

HTTP delivery берёт owner ID из principal, передаёт его в usecase и переводит доменные ошибки в ответы. PostgreSQL выбирает блокнот одновременно по ID и owner ID; чужой блокнот выглядит отсутствующим. Get получает метаданные прежде чтения S3. List ограничен базой данных и не загружает содержимое файлов. Как principal появляется в запросе, описано в [авторизации](auth-sessions.md).

Usecase зависит от портов Metadata, Files и NotebookCodec. При сборке приложения это PostgreSQL, S3 и ipynb.Codec. Контракт HTTP принадлежит Apidog; эта страница объясняет реализацию, не заменяя спецификацию.

## Порядок записи и отказ

Create обрезает края имени, отвергает пустое имя, создаёт один code-блок и записывает ipynb по UUID-ключу. Только после успешного Put появляется строка метаданных. При ошибке записи метаданных нет компенсационного удаления S3-объекта, поэтому файл может остаться без строки.

CreateCell проверяет лимит и позицию вставки; без позиции добавляет блок в конец. DeleteCell проверяет индекс. Обе операции проходят через modify: адаптер начинает SQL-транзакцию, получает строку владельца под SELECT FOR UPDATE, затем callback читает файл, меняет блоки, сериализует и перезаписывает S3. После callback обновляются cells_count и updated_at и выполняется commit. Ошибки возвращаются вызывающему коду, незавершённая SQL-транзакция откатывается.

Блокировка сериализует изменения одной строки, но не делает S3 и PostgreSQL общей транзакцией: после успешного Put сбой UPDATE/commit может оставить файл изменённым. Это граница существующего кода, а не обещание атомарности двух хранилищ.

## Файлы и преобразование

S3-адаптер использует AWS default config и path-style адресацию; значения подключения берутся из конфигурации, здесь не публикуются. Get и Put ограничены пятисекундным context timeout. Put заменяет объект по тому же ключу и ставит MIME `application/x-ipynb+json`.

Кодек читает source как строку либо склеивает массив строк; принимает code и markdown, неизвестные типы отвергает. Build создаёт nbformat 4.5. Code-блоки получают пустой outputs и null execution_count; metadata не сохраняется как произвольный объект. Следовательно, этот путь нельзя описывать как без потерь сохраняющий результаты исполнения произвольного ipynb. Наличие такого кодека не доказывает интеграцию Go API с отдельным runtime-agent.

## Где проверять изменения

`internal/notebook/usecase/notebook_test.go` и `notebook_flow_test.go` проверяют порядок вызовов и ошибки; `adapters/postgres/notebook_test.go` — транзакции; `adapters/ipynb/ipynb_test.go` — формат. Input-security тесты сохраняют текстовые данные без HTML-экранирования в хранилище: безопасный вывод остаётся отдельной границей клиента.

[Карта Go API](../quickstart.md) · [Запуск и контракт](../operations/startup-contract.md)

## Источники

- [internal/notebook/usecase/notebook.go#L27-L77](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/notebook/usecase/notebook.go#L27-L77)
- [internal/notebook/adapters/postgres/notebook.go#L28-L80](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/notebook/adapters/postgres/notebook.go#L28-L80)
- [internal/notebook/usecase/notebook.go#L32-L60](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/notebook/usecase/notebook.go#L32-L60)
- [internal/notebook/usecase/notebook.go#L117-L140](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/notebook/usecase/notebook.go#L117-L140)
- [internal/notebook/adapters/postgres/notebook.go#L85-L116](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/notebook/adapters/postgres/notebook.go#L85-L116)
- [internal/notebook/adapters/ipynb/ipynb.go#L33-L93](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/notebook/adapters/ipynb/ipynb.go#L33-L93)
- [internal/notebook/adapters/s3/files.go#L32-L88](https://github.com/Cringe-Driven-Development-Team/backend/blob/30d62561c9e2c067764239e4b68dd477c9abb982/internal/notebook/adapters/s3/files.go#L32-L88)
