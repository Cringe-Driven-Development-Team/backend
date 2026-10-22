# Backend Colab

Go API учебного проекта «Colab». Общепроектный контекст, текущую архитектуру и планы ищите на [сайте документации](https://docs.cellestial.ru/project-context). Этот README — точка входа в backend: запуск, код и рабочие правила.

## Быстрый старт

Нужны Go версии из [go.mod](go.mod), C-компилятор для тестов с `-race`, Make и Docker с Compose. Локальный Compose запускает API, PostgreSQL и SeaweedFS (S3). Секреты задаются только в `.env`, файл не коммитьте.

```bash
cp -n .env.example .env
# Задайте в .env разные JWT_SECRET и CSRF_SECRET.
make doctor
make up
make health  # ожидается {"status": "alive"}
make down    # volumes сохраняются
```

`make up` ждёт healthcheck до 120 секунд. При отказе используйте `make logs` и `docker compose ps -a`. `/health` проверяет ответ API, но не все операции с БД и S3. Полный список команд, настройки портов и окружения — в [локальной разработке](docs/local-development.md).

## Агент исполнения Python/R

[runtime-agent](runtime-agent/README.md) — Python 3.13 сервис systemd на выделенной
VPS пользовательской сессии. Запускает один контейнер с одним Python или R kernel,
принимает команды через внутренний HTTP/WebSocket API и хранит журнал SQLite.
UUID сессии и язык задаются при запуске; лимиты — 1 CPU / 1 GiB RAM, простой — 15 минут.
Контейнер без host mounts; связь агента с Jupyter — закрытый TCP без публикации порта.
Доступ kernel к интернету ограничивает firewall infra по [контракту сети](docs/runtime-agent-network.md).

Проверки: `make agent-setup agent-lint agent-test agent-build`;
с Docker — `make agent-images agent-integration`; локальный Python kernel —
`make agent-test-jupyter`. Сетевая приёмка на отдельной VPS — `make agent-network-check`.
Production-установка, firewall и пул VPS принадлежат `infra`; интеграция с Go API,
пользователями и ipynb выполняется отдельно.

## Код и документация

- `cmd/main` — точка входа; `internal/app` собирает зависимости, `internal/config` читает окружение.
- `internal/transport/http` — маршруты, авторизация и HTTP-политики; `internal/{user,notebook}` — domain, usecase, HTTP-обработчики и адаптеры хранения.
- `migrations/` — SQL-миграции goose; `internal/transport/http/api/api.gen.go` — код, сгенерированный из Apidog, вручную не редактируйте.
- [Авторизация, CSRF и CORS](docs/authentication.md) — cookie, заголовки и примеры запросов.
- [Контракт API в Apidog](docs/apidog.md) — ветки, Merge Request и `make generate`.
- [CI, образ и деплой](docs/release.md) — проверки и выпуск. Production-конфигурация принадлежит [infra](https://github.com/Cringe-Driven-Development-Team/infra).
- [AGENTS.md](AGENTS.md) — локальные правила для агента; Claude Code подключает их через [CLAUDE.md](CLAUDE.md).

Состав команды — в [профиле организации](https://github.com/Cringe-Driven-Development-Team/.github/tree/main/profile).

## Как работать с задачами

Задачи команды собраны на [доске Scrumban](https://github.com/orgs/Cringe-Driven-Development-Team/projects/1).
Правила оформления задач и ревью — в [CONTRIBUTING команды](https://github.com/Cringe-Driven-Development-Team/.github/blob/main/CONTRIBUTING.md).
Перед передачей изменений выполните `make check` и `make docker-build`.

## Типы коммитов

Формат: `<тип>: <описание>` или `<тип>(<область>): <описание>`. Коммиты ветки сохраняются в истории как есть.

| Тип | Когда использовать |
| --- | --- |
| `feat` | новая функциональность |
| `fix` | исправление бага |
| `refactor` | изменение кода без изменения поведения |
| `style` | форматирование |
| `test` | тесты |
| `docs` | документация |
| `chore` | конфиги, зависимости, сборка и CI |
