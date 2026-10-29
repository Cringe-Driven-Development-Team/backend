# Runtime provisioning: VPS пользовательских сессий

Модуль `internal/runtime` создаёт, настраивает и удаляет отдельную VPS в Selectel
для каждой пользовательской сессии. Агент (`runtime-agent/`) на VPS сам VPS не
создаёт. Потребитель (#13) работает только через `EnvironmentService`:
`RequestCreate`, `Get`, `RequestDelete`.

## Границы с infra

Backend отвечает за создание, настройку и удаление runtime-VPS: воркер вызывает
OpenStack API и запускает `ansible-playbook`. В infra ([infra#89](https://github.com/Cringe-Driven-Development-Team/infra/issues/89))
сопровождаются:

- роли Ansible `runtime_host`, `runtime_agent` и `site.yml`. Infra копирует их на
  VPS2 и монтирует read-only в контейнер Go API в `/app/deploy/runtime`;
- реализация сетевой политики и firewall ([runtime-agent-network.md](runtime-agent-network.md)),
  которую воркер применяет при настройке каждой VPS;
- production-секреты и файлы (`clouds`-учётка, SSH-ключ, CA и клиентский сертификат).

В образ backend входят wheel агента, `requirements.txt`, unit `cellestial-agent.service`
(`/app/agent`), `ansible-core` и `/app/runtime-smoke`. Каталог артефактов можно
переопределить через `RUNTIME_ARTIFACTS_DIR` (по умолчанию `/app`).

## Как это работает

`RequestCreate` атомарно сохраняет окружение и задание в PostgreSQL и сразу
возвращается. Воркер внутри процесса API берёт задания с lease (две реплики не
берут одно задание), продлевает lease и выполняет шаги. Каждый полученный ID
облачного ресурса сохраняется сразу; ресурсы помечаются метаданными
`cellestial_runtime=1`, `cellestial_env=<RUNTIME_ENV_NAME>`, `session_id`,
`operation_id`, поэтому после падения воркера ресурс находится по ним и дубликата нет.

```text
QUEUED → CREATING → CONFIGURING → VERIFYING → READY
              (порт, том, сервер)  (Ansible)  (GET /health агента)
READY → DELETING → DELETED
```

| Состояние | Что происходит |
| --- | --- |
| `QUEUED` | окружение и задание сохранены |
| `CREATING` | порт, загрузочный том, сервер; host key ed25519 задаётся через cloud-init |
| `CONFIGURING` | воркер запускает playbook с лимитом `RUNTIME_ANSIBLE_TIMEOUT` |
| `VERIFYING` | опрос `/health` по mTLS до `RUNTIME_STARTUP_TIMEOUT`; нужны своя сессия и язык |
| `READY` | агент готов; `Endpoint` = `https://<private_ip>:8443` |
| `DELETING` | drain/остановка агента (его недоступность не блокирует), затем сервер, том, порт |
| `DELETED` | пользовательское удаление завершено |
| `CLEANUP_PENDING` | очистка не завершена или ожидает повтора |
| `FAILED` | создание не удалось, ресурсы удалены |

Итоги:

- Ошибка или таймаут на CREATING/CONFIGURING/VERIFYING (в том числе чужие
  `session_id`/язык или 503 от агента) пишет `last_error`, переводит окружение в
  `CLEANUP_PENDING`, затем ресурсы удаляются. После подтверждённой очистки итог
  `FAILED`, а не `DELETED`.
- `RequestDelete` в любой момент создания останавливает его: окружение не
  становится `READY`, ресурсы удаляются, итог `DELETED`.
- Постоянная ошибка облака при удалении (квота, 403) не зацикливает воркер:
  `CLEANUP_PENDING` с причиной в `cleanup_result`. Такие окружения
  повторяет reconcile каждые `RUNTIME_RECONCILE_INTERVAL`; он же удаляет сирот —
  ресурсы окружения с неизвестной БД или уже завершённой сессией.
- Лимит `RUNTIME_MAX_ACTIVE` ограничивает число активных окружений
  (`ErrCapacity`); `RUNTIME_MAX_CONCURRENT_OPS` — число одновременных операций воркера.

Секреты (токен агента, host key, клиентские ключи) хранятся зашифрованными ключом
`RUNTIME_SECRETS_KEY`, не пишутся в логи, ошибки и `Environment`; задачи Ansible с
ними идут с `no_log: true`.

## Настройки `RUNTIME_*`

Читаются при `RUNTIME_ENABLED=true`; иначе модуль выключен, воркер не стартует,
сервис отвечает `ErrDisabled`.

| Переменная | Умолчание | Назначение |
| --- | --- | --- |
| `RUNTIME_ENABLED` | `false` | включает модуль и воркер |
| `RUNTIME_PROVIDER` | нет | `openstack` или `fake` |
| `RUNTIME_SECRETS_KEY` | нет | base64 ровно 32 байт, ключ шифрования секретов |
| `RUNTIME_ENV_NAME` | `production` | значение метки `cellestial_env` |
| `RUNTIME_WORK_DIR` | `/var/lib/cellestial/runtime` | рабочий каталог воркера |
| `RUNTIME_ROOT_DISK_GIB` | `20` | размер загрузочного тома |
| `RUNTIME_KERNEL_DNS` | `1.1.1.1` | IPv4 DNS для контейнера kernel |
| `RUNTIME_PYTHON_IMAGE`, `RUNTIME_R_IMAGE` | digest из образа (локально `cellestial-kernel-*:local`) | образы kernel |
| `RUNTIME_AGENT_VERSION` | SHA сборки (`dev`) | версия агента |
| `RUNTIME_STARTUP_TIMEOUT` | `20m` | предел создания до `READY` |
| `RUNTIME_ANSIBLE_TIMEOUT` | `15m` | предел playbook |
| `RUNTIME_DRAIN_TIMEOUT` | `30s` | ожидание drain агента при удалении |
| `RUNTIME_DELETE_TIMEOUT` | `10m` | предел удаления |
| `RUNTIME_RECONCILE_INTERVAL` | `5m` | период reconcile |
| `RUNTIME_MAX_CONCURRENT_OPS` | `2` | одновременные операции |
| `RUNTIME_MAX_ACTIVE` | `3` | активные окружения |
| `RUNTIME_ARTIFACTS_DIR` | `/app` | каталог `agent/` и `deploy/runtime/` |

Только для `openstack` (все обязательны):

| Переменная | Назначение |
| --- | --- |
| `RUNTIME_OS_AUTH_URL`, `RUNTIME_OS_USERNAME`, `RUNTIME_OS_PASSWORD` | доступ к Keystone |
| `RUNTIME_OS_USER_DOMAIN_NAME`, `RUNTIME_OS_PROJECT_ID`, `RUNTIME_OS_REGION` | домен, проект, регион |
| `RUNTIME_AVAILABILITY_ZONE`, `RUNTIME_NETWORK_ID`, `RUNTIME_SUBNET_ID`, `RUNTIME_SECURITY_GROUP_ID` | зона и сеть |
| `RUNTIME_IMAGE_ID`, `RUNTIME_FLAVOR_ID`, `RUNTIME_VOLUME_TYPE` | образ, флейвор, тип диска |
| `RUNTIME_SSH_KEY_NAME`, `RUNTIME_SSH_PRIVATE_KEY_FILE`, `RUNTIME_SSH_USER` (`root`) | SSH для Ansible |
| `RUNTIME_TLS_CA_CERT_FILE`, `RUNTIME_TLS_CA_KEY_FILE` | CA, выпускающий сертификаты агентов |
| `RUNTIME_TLS_CLIENT_CERT_FILE`, `RUNTIME_TLS_CLIENT_KEY_FILE` | клиентский сертификат backend для mTLS |
| `RUNTIME_BLOCKED_PUBLIC_IPS` | IP инфраструктуры через запятую, закрываемые для kernel |

Образы kernel: `ghcr.io/cringe-driven-development-team/cellestial-kernel-{python,r}`,
публичные пакеты. CI публикует их и вшивает digest в образ backend как значения
по умолчанию `RUNTIME_PYTHON_IMAGE` и `RUNTIME_R_IMAGE`.

## Fake-режим

`RUNTIME_ENABLED=true`, `RUNTIME_PROVIDER=fake` и `RUNTIME_SECRETS_KEY`. Облако,
настройка VPS и агент заменены in-memory реализациями (`adapters/fake`), CA
одноразовый, облачные настройки не нужны. БД остаётся реальной. Подходит для
локальной разработки потребителя без Selectel.

## Smoke до мержа

`cmd/runtime-smoke` создаёт сессии в настоящем облаке, выполняет `print(42)` в агенте,
удаляет их и проверяет, что не осталось ресурсов; печатает JSON-отчёт, код выхода
не 0 при ошибке. Образ с бинарём `/app/runtime-smoke` для PR собирает `workflow_dispatch`
с тегом `pr-<branch>-<sha>`.

1. Запустить `ci.yml` вручную (`workflow_dispatch`) на ветке PR.
2. На VPS2 указать в compose образ `pr-<branch>-<sha>` и выполнить:

   ```bash
   docker compose run --rm backend /app/runtime-smoke --language python
   docker compose run --rm backend /app/runtime-smoke --language r --parallel 3
   ```

3. Проверить сбой воркера: `--kill-worker-after volume` и `--kill-worker-after server`
   отменяют воркер smoke после сохранения ID; создание должен продолжить другой
   воркер без дублей. Эти режимы требуют `--parallel 1` или выключенного
   (`RUNTIME_ENABLED=false`) воркера API: очередь заданий общая.
4. Вернуть прод на тег `main`.

Миграции `runtime_*` при запуске PR-образа применяются к боевой БД (принято владельцем).
Флаги: `--language python|r`, `--parallel N`, `--timeout` (30m).

## Ручная очистка

Если ресурсы остались, найди их по меткам `cellestial_runtime=1` и
`cellestial_env=<RUNTIME_ENV_NAME>` (серверы и тома — metadata, порты — теги и
description) и сверь `session_id` с таблицей `runtime_environments`:

```bash
ENV=production
openstack server list --long -c ID -c Name -c Properties | grep "cellestial_env='$ENV'"
openstack volume list --long -c ID -c Name -c Properties | grep "cellestial_env='$ENV'"
openstack port list --tags cellestial_runtime=1,cellestial_env=$ENV
openstack server delete <id>; openstack volume delete <id>; openstack port delete <id>
```

Удаляй по порядку: сервер, том, порт. Не трогай ресурсы сессий в состояниях
`QUEUED`..`READY`. Обычно достаточно дождаться reconcile.
