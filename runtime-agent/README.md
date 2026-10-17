# Runtime agent

Один systemd-сервис на общей VPS управляет Docker-сессиями Python/R.
Клиент этого API — доверенный backend, который проверяет права пользователей.
Язык фиксируется при создании сессии. Каждая сессия имеет независимые память,
очередь, workspace и журнал событий; выполнение внутри сессии последовательное.
Сервис не обращается к Selectel, PostgreSQL или S3 и не изменяет ipynb.

## Разработка

Нужны Linux, Python 3.13 и Docker Engine с доступом служебного пользователя к socket.
Агент запускается от непривилегированного пользователя; доступ к Docker доверяется
только управляющему процессу. Запуск от root отклоняется.

Из корня backend:

```bash
make agent-setup agent-lint agent-test agent-build
make agent-images agent-integration
```

`AGENT_PYTHON` и `AGENT_VENV` позволяют выбрать интерпретатор и каталог virtualenv.
`make agent-test-jupyter` проверяет настоящий Python kernel без Docker.
Docker-тесты проверяют также R, две независимые сессии, отсутствие сети, лимиты,
OOM и восстановление после потери агента. CI запускает их на Linux с Docker.

Локальный запуск после сборки образов:

```bash
export RUNTIME_TOKEN=$(openssl rand -hex 32)
export STATE_DIR=$(mktemp -d /tmp/ca-state.XXXXXX)
export SOCKET_DIR=$(mktemp -d /tmp/ca-sock.XXXXXX)
runtime-agent/.venv/bin/cellestial-runtime-agent
```

Один worker обязателен. Эксклюзивная блокировка STATE_DIR предотвращает запуск
нескольких процессов с одним журналом. Каталоги состояния и socket должны быть
выделены этому агенту; SOCKET_DIR должен быть коротким из-за лимита Unix-сокетов.

## API

На всех HTTP-маршрутах (включая `/health`, `/openapi.json`) и WS handshake нужен
`Authorization: Bearer <RUNTIME_TOKEN>`. API по умолчанию слушает `127.0.0.1:8000`.
Схема HTTP доступна на защищённом `/openapi.json`; WS описан ниже.

```bash
session_id=$(python3 -c 'import uuid; print(uuid.uuid4())')
curl --fail-with-body http://127.0.0.1:8000/sessions \
  -H "Authorization: Bearer $RUNTIME_TOKEN" -H 'Content-Type: application/json' \
  -d "{\"session_id\":\"$session_id\",\"language\":\"python\"}"

# Дождаться state=READY через GET /sessions/{session_id}.
curl --fail-with-body "http://127.0.0.1:8000/sessions/$session_id/executions" \
  -H "Authorization: Bearer $RUNTIME_TOKEN" -H 'Content-Type: application/json' \
  -d '{"execution_id":"run-1","code":"x = 42\nprint(x)\nx * 2","timeout_seconds":60}'
curl --fail-with-body "http://127.0.0.1:8000/sessions/$session_id/events?after=0" \
  -H "Authorization: Bearer $RUNTIME_TOKEN"
```

`POST /sessions` принимает UUID `session_id` и язык `python|r`, возвращает `202`.
Повтор с тем же языком возвращает прежнюю сессию; другой язык — `409`.
Остановленная сессия не запускается повторно: создайте новый UUID.
`GET /health` показывает готовность агента и доступные слоты.

Пути ниже имеют префикс `/sessions/{session_id}`:

| Метод | Суффикс | Назначение |
| --- | --- | --- |
| GET | пустой | Состояние, язык, generation, активная команда, очередь, причина остановки |
| GET | `/health` | То же; HTTP 503, если сессия не принимает новые команды |
| POST | `/activity` | Продлить активность |
| POST | `/executions` | `{execution_id, code, timeout_seconds?}`, ответ 202 |
| GET | `/executions/{execution_id}` | `{execution_id, state, result}` |
| POST | `/variables` | `{execution_id}`, инспекция в общей очереди, ответ 202 |
| POST | `/kernel/interrupt` | Прервать активную команду |
| POST | `/kernel/restart` | Очистить память свободного kernel, сохранить workspace |
| POST | `/runtime/drain` | Запретить новые команды, отменить очередь, прервать активную |
| POST | `/runtime/stop` | Остановить сессию и удалить workspace; журнал остаётся |
| GET | `/events?after=0` | `{events: [...]}`, до 100 событий |
| WS | `/events/ws?after=0` | Replay и последующие события |

`execution_id`: строка длиной 1–128 символов (в URL используйте percent-encoding).
Код — до 1 048 576
символов, HTTP body — до 2 MiB. Таймаут исполнения: 1–86400 секунд, по умолчанию
300; таймаут инспекции — 5 секунд. Очередь — 100 ожидающих команд на сессию.
Недостаток слотов/места в очереди даёт `429`, недоступная сессия — `503`.

Идемпотентность scoped по `(session_id, execution_id)`: тот же запрос возвращает
сохранённое состояние даже после остановки; другое содержимое — `409`.
Вызывающий сервис сохраняет ID **до** отправки и повторяет его после сетевого сбоя.

Состояния сессий: `STARTING`, `READY`, `DRAINING`, `STOPPED`, `FAILED`.
Состояния команд: `QUEUED`, `RUNNING`, `SUCCEEDED`, `FAILED`, `INTERRUPTED`,
`TIMED_OUT`, `CANCELLED`, `UNKNOWN`. `UNKNOWN` означает отсутствие достоверного
результата; такие команды автоматически не повторяются.

## События и жизненный цикл

Каждое событие имеет `seq`, `session_id`, `execution_id` (или null), `kind`,
`timestamp`. Для `kernel_message` дополнительно передаются `channel`, `msg_type`,
`content`, `metadata`, `parent_msg_id`, `generation`, при необходимости
`buffers_base64`. Сохраняются stream, MIME-вывод, display_id, clear/update и comm.
UI widgets и формирование ipynb реализуются вызывающим сервисом.

`seq` возрастает, может иметь пропуски и не сбрасывается при restart kernel.
Backend фиксирует событие и свой курсор одной транзакцией, дедуплицирует по
`(session_id, seq)`. Heartbeat WS каждые 15 секунд содержит `{kind:"heartbeat",
after:...}` без seq. Медленный WS-клиент отключается после 10 секунд ожидания
отправки; повторное подключение с последним курсором восстанавливает поток.

Поздний вывод сохраняет связь с исходным исполнением. Вывод с неизвестным parent
передаётся как `unattributed_output`. Лимит сериализованных выводов — 10 MiB на
команду, включая поздние сообщения; один `output_truncated` сообщает об отсечении.
Для unattributed_output действует отдельный общий лимит на поколение kernel.
Kernel продолжает исполняться после отсечения вывода. Транспорт принимает кадры
до 32 MiB; нарушение протокола или потеря канала завершает сессию.

Инспекция возвращает `{variables:[{name,type,preview}]}`: до 200 переменных,
preview до 200 символов у простых значений, иначе null. Служебная инспекция
не добавляет запись в историю и не передаёт свой stdout как вывод пользовательской
ячейки. Ошибка инспекции отражается в результате команды.

Через 15 минут простоя сессия останавливается с `reason=idle_timeout`.
Новые команды, управляющие действия и `/activity` обновляют активность;
чтение результатов и WS heartbeat — нет. Активное исполнение и очередь защищены
от idle timeout; после завершения очередь получает полный новый интервал.

При timeout/interrupt агент ждёт подтверждения до 5 секунд, затем завершает
сессию; неподтверждённое исполнение получает UNKNOWN. Потеря kernel/канала
также останавливает сессию, неотправленные команды становятся CANCELLED.
При перезапуске агента оставшиеся его контейнеры удаляются, незавершённые команды
становятся UNKNOWN, прежние сессии — STOPPED с `reason=agent_restarted`.
Неудачная очистка контейнера сохраняет занятый слот до успешного повтора.

## Поставка

`deploy/cellestial-agent.service` и `deploy/agent.env.example` — шаблоны для infra.
Infra создаёт пользователя `cellestial-agent` с доступом к Docker, устанавливает
Python-пакет и requirements.txt в `/opt/cellestial-agent/venv`, загружает образы,
кладёт защищённый env-файл в `/etc/cellestial-agent/agent.env` и устанавливает unit.
Контейнеры запускаются с UID/GID агента, но отдельными PID/mount/network namespaces.
Журнал и главный токен никогда не монтируются в контейнеры.

Настройки: `HOST`, `PORT`, `RUNTIME_TOKEN`, `STATE_DIR`, `SOCKET_DIR`,
`MAX_SESSIONS` (1), `IDLE_TIMEOUT_SECONDS` (900), `MAX_OUTPUT_BYTES` (10485760),
`PYTHON_IMAGE`, `R_IMAGE`. Infra выбирает MAX_SESSIONS с резервом ресурсов для
Go API и БД. Каждый контейнер ограничен 1 CPU, 1 GiB RAM, 128 процессами;
workspace временный (до 256 MiB), интернет отключён.

Журналы сохраняются до удаления каталога состояния оператором; автоматического
удаления истории и протокола подтверждения доставки в этой версии нет.
Токен агента даёт доступ ко всем сессиям; его получает только доверенный backend.
В production доступность API, доставка образов и установка unit настраиваются
в infra. Этот репозиторий не выполняет автоматическую выкатку агента.
