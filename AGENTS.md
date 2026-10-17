# Backend: инструкции для агента

Разрабатываем аналог Google Colab: блокноты с блоками Python/R и текста.
Правила работы с задачами, ветками и PR — в [README.md](README.md).
Как завести и закрыть задачу — в [CONTRIBUTING.md](https://github.com/Cringe-Driven-Development-Team/.github/blob/main/CONTRIBUTING.md) организации;
в Codex и Claude Code используй командный скилл `cdd-tasks`.
Код/PR/issues — Cringe-Driven-Development-Team/backend; api-N, `Closes #N`,
review ментора, кроме настроенного исключения YarikMix (см. README).
Учебный repo — копия main без Actions и новых PR.

## Целевые продуктовые требования

- Регистрация и авторизация с валидацией; профиль пользователя.
- Список файлов пользователя; страница файла; создание, редактирование и сохранение
  отдельных блоков кода и текста.
- Исполнение файлов на R или Python с выводом результата.
- Шеринг файлов, поиск по файлу, комментарии к блокам кода и текста.
- Выделение квот на количество исполнений и ресурсы; статистика их использования;
  покупка подписок с квотой.
- Загрузка и скачивание ipynb, включая экспорт после исполнения.

## Стек и команды

- Go: версия в [go.mod](go.mod), зависимости через Go Modules (`go.mod`, `go.sum`).
- HTTP: `net/http`, Gorilla mux; PostgreSQL: `pgx/v5/pgxpool`.
- Локально: Docker Compose с Go API, PostgreSQL 18 и SeaweedFS (S3).
- `runtime-agent/`: отдельный Python 3.13 сервис systemd; Docker-контейнер на сессию
  Python/R на общей VPS. Сеть kernel отключена; SQLite и токен хранятся на хосте.

Нужны Go, C-компилятор для `-race`, Make, curl и Docker с Compose.
Настройки локального `.env` — в [.env.example](.env.example). Команды из корня:

```bash
go mod download       # скачать зависимости
go mod tidy -diff      # проверить согласованность go.mod/go.sum без изменения
make build            # собрать bin/server
make test             # тесты с -race, coverage.out и итоговое покрытие
make lint             # статический анализ
make docker-build     # собрать colab-backend:local
make doctor           # инструменты, настройки и GitHub-доступы; ничего не меняет
make check-fast       # gofmt и go vet; затем тесты изменённого пакета
make check            # tidy, сборка всех пакетов, race-тесты, lint перед PR
make up               # собрать и запустить API, PostgreSQL, SeaweedFS; ждать health
make health           # GET /health; make logs — последние логи; make down — остановка
make run              # то же окружение с логами в терминале
make agent-setup agent-lint agent-test agent-build # Python-агент: установка и проверки
make agent-images agent-integration # Docker-образы Python/R и интеграционные тесты
```

`make test` и CI используют `scripts/test.sh`: тестируются все пакеты с
`-race -count=1 -coverpkg=./...` (включая вызовы из тестов других пакетов).
Из `coverage.out` исключаются `internal/transport/http/api`, `cmd/apidog`,
`migrations` и `cmd/main`. Логика приложения в `internal/app` учитывается.
Последняя строка — итоговое покрытие; CI пишет ту же строку в summary.
Цель — ≥50%, автоматического порога нет. Отчёт: `go tool cover -func=coverage.out`.

Запускай клиент агента из корня backend с `go.mod`, включая отдельный worktree.
Настройка Codex/Claude Code, gopls и сценарий новой сессии — в
[docs/local-development.md](docs/local-development.md), раздел «Harness AI-агента».
Сначала вызови `go_workspace`, затем `go_search`;
после чтения Go-файла — `go_file_context`, перед изменением символа —
`go_symbol_references`, после изменений — `go_diagnostics` и тесты пакета.
Если MCP недоступен, явно укажи это; `rg`, `go vet` и тесты дают запасной путь.
Перед PR нужны `make check` и `make docker-build`; результаты и ограничения — в PR.

## Паттерны кода

В `cmd/` размещай только точки входа в приложение; допустим README с описанием
микросервиса. Тесты в `cmd/` запрещены: проверяемую логику и её тесты размещай
в `internal/` (сборка и запуск API — `internal/app`).

Карта кода: `cmd/main` → `internal/app` (сборка и запуск), `internal/config` — окружение.
`internal/transport/http` — роутер, `/health`, API-генерация, middleware,
security, cookies, principal и httperr; `internal/{user,notebook}/delivery/http`
→ `usecase` (сценарии, порты) → `domain`; `adapters/{postgres,s3,security,ipynb}`
реализуют порты. Usecase не импортирует HTTP или адаптеры; domain не знает о них.
`internal/tooling/apidog` — экспорт контракта; `migrations` — встроенные goose SQL.
`internal/transport/http/api/api.gen.go` не редактируй вручную. Контракт и `make generate` описаны в [docs/apidog.md](docs/apidog.md).

Сохраняй контекст запроса при передаче дальше; для операций БД из обработчика
используй `r.Context()`. Порядок: request ID → logging → recover → CORS →
CSRF/Authenticate → Validator. Общие middleware оборачивают весь роутер, чтобы
request ID был на всех ответах, включая CSRF-отказы, preflight, 404 и 405.

## Границы backend и эксплуатация

Общепроектный контекст, текущая архитектура и планы — в
[Контекст проекта](https://docs.cellestial.ru/project-context).
Факты о реализации проверяй по актуальному коду и конфигурации.

- Backend отвечает за Go API, данные и бизнес-логику; местный Compose — для разработки.
- Агент не создаёт VPS; язык выбирается при создании контейнерной сессии.
  Production-установка systemd и доступ Go API к агенту принадлежат `infra`.
- `infra`: Pulumi, Ansible Vault, production Compose и Caddyfile. Не переноси
  production-конфигурацию в backend и не коммить открытые секреты.
- Проект Compose на VPS — `cellestial`, каталог `/opt/cellestial`; существующие
  volumes Caddy сохраняются. Миграции goose выполняются при старте API.
- Файлы блокнотов хранятся в S3: нужен `S3_NOTEBOOKS_BUCKET` и параметры `AWS_*`.
  Локально Compose использует SeaweedFS, production — Selectel.
- `ci.yml` проверяет код и публикует образ, затем вызывает `cd.yml` через
  `workflow_call` с SHA-тегом из job `docker`. CD в environment `production`
  запускает `deploy-backend.yml` из `infra/main`. Inventory статический;
  Selectel credentials и Pulumi в CI не используются.
- Merge в `main` требует review ментора, кроме YarikMix; отдельного approval environment нет.
  Деплой только из `main`, без отмены выполняющейся выкатки.
- Откат — тот же playbook с прежним SHA-тегом, без автоматических миграций Down.
- API N+1 обслуживает клиента N: ручки и поля не удалять в том же релизе,
  где клиент перестал ими пользоваться; проверять на ревью Apidog.

Фактический [CI backend](.github/workflows/ci.yml) публикует
`ghcr.io/cringe-driven-development-team/backend`: теги `sha-<short>` и `main`,
без `latest`. Пакет приватный; job выкатки передаёт временный `GITHUB_TOKEN`
с `packages: read` через окружение Ansible. Постоянного GHCR-токена в Vault нет.
Выкатывается SHA-тег; строго неизменяемая ссылка — digest.
Deploy требует PRODUCTION_ENABLED=true; sync — COURSE_SYNC_ENABLED=true,
needs lint/test/docker, независимо от deploy. Повтор sync принимает только ci_run_id.
Pull старого GHCR path требует old sha-tag и прежний read credential.
Без registry используй проверенный сохранённый образ и manual override из
infra/ansible/README.md: pull_policy never, только API, без миграций Down.

## Коммиты

Формат: `<тип>: <описание>` или `<тип>(<область>): <описание>`.
Типы: `feat`, `fix`, `refactor`, `style`, `test`, `docs`, `chore`.

```text
docs: добавить инструкции для агента
refactor(auth): вынести проверку токена в middleware
```

Назначение типов — в [README](README.md#типы-коммитов).

## Документация

README содержит краткую, актуальную информацию о проекте и его использовании.
Порядок выкатки конкретной задачи, временные зависимости и условия слияния
описывай в задаче при необходимости, а не в README.
Подробную общепроектную документацию размещай в репозитории организации
[docs](https://github.com/Cringe-Driven-Development-Team/docs). Локальные инструкции
backend по запуску, API и CI размещай в `docs/`, оставляя README коротким навигатором.

## Актуальность документа

При изменении команд или архитектурных границ обновляй этот файл в том же изменении, размер этого документа не более 150 строк.
