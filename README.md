# Backend Colab

[![CI](https://github.com/Cringe-Driven-Development-Team/backend/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/Cringe-Driven-Development-Team/backend/actions/workflows/ci.yml)

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

## Код и документация

- `cmd/main` — точка входа; `internal/app` собирает зависимости и маршруты.
- `internal/{user,notebook}` — доставка, use case и хранилища; `internal/auth` и `internal/middleware` — авторизация и HTTP-политики.
- `migrations/` — SQL-миграции goose; `internal/api/api.gen.go` — код, сгенерированный из Apidog, вручную не редактируйте.
- [Авторизация, CSRF и CORS](docs/authentication.md) — cookie, заголовки и примеры запросов.
- [Контракт API в Apidog](docs/apidog.md) — ветки, Merge Request и `make generate`.
- [CI, образ и деплой](docs/release.md) — проверки, синхронизация учебной копии и выпуск. Production-конфигурация принадлежит [infra](https://github.com/Cringe-Driven-Development-Team/infra).
- [AGENTS.md](AGENTS.md) — локальные правила для агента; Claude Code подключает их через [CLAUDE.md](CLAUDE.md).

История переноса в командную организацию и порядок восстановления находятся в [runbook миграции](https://github.com/Cringe-Driven-Development-Team/.github/blob/main/docs/migrations/team-repositories-runbook.md). Состав команды — в [профиле организации](https://github.com/Cringe-Driven-Development-Team/.github/tree/main/profile).

## Как работать с задачами

Код, задачи, PR и ревью ведутся в [командном backend](https://github.com/Cringe-Driven-Development-Team/backend); [учебный репозиторий](https://github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development) автоматически получает те же коммиты `main`. Как создать, назначить и закрыть задачу, описано в [CONTRIBUTING организации](https://github.com/Cringe-Driven-Development-Team/.github/blob/main/CONTRIBUTING.md). Доска — [Scrumban](https://github.com/orgs/Cringe-Driven-Development-Team/projects/1).

Для задачи `#N` создайте ветку `api-N`, затем PR в `main` с заголовком `API-N: <название>` и строкой `Closes #N` в описании. Перед PR выполните `make check` и `make docker-build`; обязательные проверки GitHub — `lint`, `test`, `docker`. Merge — отдельным merge-коммитом.

Backend PR требует review [Александра](https://github.com/blackHATred). По согласованному исключению аккаунт `YarikMix` может влить backend PR без Approve, включая свой; PR, зелёные проверки, актуальная ветка и разрешённые обсуждения остаются обязательными. Исключение относится только к approval ruleset, не к CI/integrity. Подробности реализации правила смотрите в GitHub rulesets.

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
