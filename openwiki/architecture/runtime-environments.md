---
type: guide
title: "Подготовка runtime-окружений"
description: "Подключение Go-адаптеров, очередь подготовки и удаления VPS, тёплый пул и восстановление."
tags: [runtime, provisioning, worker, persistence]
verified:
  - by: openwiki/0.7.2
    at: 2026-11-20T18:28:51.294Z
sources:
  - id: openwiki-source-a009d602e55f848ea26930dd
    resource: repo://internal/app/app.go
  - id: openwiki-source-5450c45ea05d5ffd1d8ee804
    resource: repo://internal/app/run.go
  - id: openwiki-source-08f73a6cbd452c1a6a0a9454
    resource: repo://internal/runtime/adapters/openstack/provider.go
  - id: openwiki-source-80527b1b02b4c10f5f035479
    resource: repo://internal/runtime/adapters/postgres/environments.go
  - id: openwiki-source-f08e86c5293b0d4d83463492
    resource: repo://internal/runtime/adapters/postgres/jobs.go
  - id: openwiki-source-1449920d53cd166b139c4a16
    resource: repo://internal/runtime/adapters/postgres/pool.go
  - id: openwiki-source-6b926fb184beddf2ccbf8041
    resource: repo://internal/runtime/module.go
  - id: openwiki-source-5caedd671d86311b3df2dd43
    resource: repo://internal/runtime/usecase/create.go
  - id: openwiki-source-600f480336937ddaa0150e0d
    resource: repo://internal/runtime/usecase/delete.go
  - id: openwiki-source-2e110c4857fbc508dbc6588d
    resource: repo://internal/runtime/usecase/pool.go
  - id: openwiki-source-7c5b2622482ae158ab24f724
    resource: repo://internal/runtime/usecase/reconcile.go
  - id: openwiki-source-cdaba5d803745f298794e37d
    resource: repo://internal/runtime/usecase/service.go
  - id: openwiki-source-080f23788565faaa3a6f0817
    resource: repo://internal/runtime/usecase/worker.go
  - id: openwiki-source-c70cc4c1a5f9170884c5d0e1
    resource: repo://internal/runtimesession/adapters/environment/client.go
generated: { by: "codex", at: "2026-11-20T18:28:51.294Z" }
---

# Подготовка runtime-окружений

## Подключение к приложению

`internal/app/app.go` собирает модуль runtime, запускаемый вместе с Go API. Воркер [пользовательских runtime-сессий](runtime-sessions.md) запрашивает создание, состояние и удаление окружения через адаптер `internal/runtimesession/adapters/environment/client.go`. Адаптер передаёт состояние и безопасный код причины. Конфигурация выбирает `fake` или `openstack`. В первом случае подставляются тестовые адаптеры, во втором собираются облачный провайдер, Ansible и Go-клиент агента. `cmd/runtime-smoke` служит отдельной ручной точкой входа.

## Запрос и аренда задания

`RequestCreate` проверяет SessionID, язык и профиль, затем записывает окружение `QUEUED` с целью `READY` вместе с заданием create. PostgreSQL проверяет лимит под advisory-блокировкой. Повтор с тем же SessionID возвращает найденное окружение при совпадающих языке и профиле. Несовпадение даёт конфликт.

Очередь выбирает готовое задание через `FOR UPDATE SKIP LOCKED`. Воркер ограничивает число параллельных операций и продлевает аренду. Потеря аренды отменяет текущие шаги, а перевод задания в delete прерывает создание.

## Подготовка и назначение

При создании новой VPS окружение проходит `QUEUED → CREATING → CONFIGURING → VERIFYING → READY`. Воркер обеспечивает порт, том и сервер, сохраняет полученные ID и передаёт настройку хоста в Ansible. Ключ хоста и токен хранятся запечатанными. В `VERIFYING` воркер вызывает Assign и ожидает успешный HTTP-ответ с совпадающими сессией, языком и признаком Ready. Общий срок подготовки отсчитывается от создания записи. Временные ошибки возвращают задание в очередь с задержкой, постоянная ошибка или превышение срока на этапе подготовки переводит окружение к очистке.

При положительном размере тёплого пула воркер отдельно готовит VPS и ждёт, пока агент сообщит о готовности принять назначение. `ClaimPool` передаёт одну готовую VPS нужного образа и версии агента ровно одному окружению в транзакции. Это сразу переводит окружение в `VERIFYING`. При пустом пуле или неудачном назначении воркер переходит к новой VPS в рамках исходного срока.

## Удаление и восстановление

Удаление пытается завершить работу агента, затем очищает сервер, том и порт. Недоступность агента не блокирует очистку. Обычное успешное удаление даёт `DELETED`, очистка после ошибки подготовки — `FAILED`. Периодическая сверка планирует повтор для `CLEANUP_PENDING` и проверяет владельцев облачных ресурсов. Проход по сессиям пропускает ресурсы без SessionID, а ресурсы пула сверяются отдельно по PoolID. Просроченные аренды заданий подхватывает Claim.

Границу с агентом подтверждает Go-клиент с mTLS и без прокси из окружения. Выполнение playbook и выбор файлов настройки видны в Go-адаптерах. Подготовку, потерю аренды, очистку и использование пула покрывают `internal/runtime/usecase/worker_test.go` и `pool_test.go`.

[Runtime-сессии](runtime-sessions.md) · [Запуск и конфигурация](../operations/startup-contract.md) · [Карта Go API](../quickstart.md)

## Источники

- [internal/app/app.go#L111-L115](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/app/app.go#L111-L115)
- [internal/app/app.go#L129-L152](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/app/app.go#L129-L152)
- [internal/app/run.go#L28-L49](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/app/run.go#L28-L49)
- [internal/runtimesession/adapters/environment/client.go#L36-L60](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtimesession/adapters/environment/client.go#L36-L60)
- [internal/runtime/usecase/service.go#L40-L69](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/usecase/service.go#L40-L69)
- [internal/runtime/adapters/postgres/environments.go#L89-L128](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/adapters/postgres/environments.go#L89-L128)
- [internal/runtime/usecase/create.go#L15-L58](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/usecase/create.go#L15-L58)
- [internal/runtime/usecase/create.go#L225-L248](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/usecase/create.go#L225-L248)
- [internal/runtime/usecase/create.go#L267-L288](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/usecase/create.go#L267-L288)
- [internal/runtime/usecase/reconcile.go#L14-L37](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/usecase/reconcile.go#L14-L37)
- [internal/runtime/usecase/reconcile.go#L72-L109](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/usecase/reconcile.go#L72-L109)
- [internal/runtime/usecase/reconcile.go#L111-L143](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/usecase/reconcile.go#L111-L143)
- [internal/runtime/adapters/openstack/provider.go#L431-L451](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/adapters/openstack/provider.go#L431-L451)
- [internal/runtime/usecase/pool.go#L19-L58](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/usecase/pool.go#L19-L58)
- [internal/runtime/usecase/pool.go#L91-L116](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/usecase/pool.go#L91-L116)
- [internal/runtime/adapters/postgres/pool.go#L92-L122](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/adapters/postgres/pool.go#L92-L122)
- [internal/runtime/usecase/create.go#L23-L45](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/usecase/create.go#L23-L45)
- [internal/runtime/module.go#L90-L115](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/module.go#L90-L115)
- [internal/runtime/module.go#L125-L142](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/module.go#L125-L142)
- [internal/runtime/adapters/postgres/jobs.go#L13-L63](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/adapters/postgres/jobs.go#L13-L63)
- [internal/runtime/usecase/worker.go#L150-L205](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/usecase/worker.go#L150-L205)
- [internal/runtime/usecase/delete.go#L19-L123](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/usecase/delete.go#L19-L123)
- [internal/runtime/usecase/create.go#L91-L133](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/usecase/create.go#L91-L133)
- [internal/runtime/usecase/create.go#L290-L309](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/usecase/create.go#L290-L309)
- [internal/runtime/adapters/agent/client.go#L44-L66](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/adapters/agent/client.go#L44-L66)
- [internal/runtime/adapters/ansible/runner.go#L150-L165](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/runtime/adapters/ansible/runner.go#L150-L165)
- [cmd/runtime-smoke/main.go#L23](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/cmd/runtime-smoke/main.go#L23)
