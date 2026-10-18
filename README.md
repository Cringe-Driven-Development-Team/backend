# 2026_2_Cringe_Driven_Development

[![CI](https://github.com/Cringe-Driven-Development-Team/backend/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/Cringe-Driven-Development-Team/backend/actions/workflows/ci.yml)

Backend-репозиторий проекта «Colab» команды «Cringe Driven Development»

## Импорт 18 октября 2026

История разработки и история репозитория задач объединены через
[PR #24](https://github.com/Cringe-Driven-Development-Team/backend/pull/24)
с ревью backend-ментора. Рабочая ветка `api-3` сохранена с исходным SHA;
её изменения схемы не входят в импорт main.

В существующем клоне переключить основной remote и проверить адрес перед push:

```bash
git remote set-url origin https://github.com/Cringe-Driven-Development-Team/backend.git
git fetch origin
git remote -v
```

Рабочие PR, ревью и задачи ведутся в этом репозитории. После успешных
`lint`/`test`/`docker` main синхронизируется в
[учебный репозиторий](https://github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development):
туда передаются те же коммиты и файлы; Actions там выключены.
PR и ревью остаются здесь. Повторить только копирование можно через
`Course sync`, указав ID успешного main CI, без повторной выкатки.

Production использует отдельный SSH-ключ team/backend и приватный image из
этого репозитория. Выкатка разрешена при `PRODUCTION_ENABLED=true`;
синхронизация независимо управляется `COURSE_SYNC_ENABLED`.
Обычный откат и восстановление из сохранённого образа без доступа к старому
registry описаны в [инструкции infra](https://github.com/Cringe-Driven-Development-Team/infra/blob/main/ansible/README.md).
Порядок переключения и проверки — в
[runbook миграции](https://github.com/Cringe-Driven-Development-Team/.github/blob/main/docs/migrations/team-repositories-runbook.md).

## CI и Docker-образ

На каждом PR запускаются `lint` (golangci-lint), `test` (проверка зависимостей
через `go mod tidy -diff`, сборка и тесты с race detector) и `docker` (сборка образа
без публикации). Покрытие выводится в summary запуска; минимального порога нет.
Новый push в PR отменяет предыдущие проверки этого PR. Запуски `main` не отменяют
друг друга; выкатки сериализуются отдельно. Правила ревью и исключение для
`YarikMix` описаны ниже.

После push в `main`, если все проверки прошли, образ публикуется в
`ghcr.io/cringe-driven-development-team/backend`:

- `sha-<short>` — тег конкретного коммита для выбора версии при деплое;
- `main` — последняя успешно опубликованная сборка основной ветки.

Тег `latest` не публикуется. SHA-тег может быть перезаписан повторным запуском
для того же коммита; для строго неизменяемой ссылки используется digest образа.
После публикации `ci.yml` вызывает reusable workflow `cd.yml`, передавая SHA-тег
из job `docker`. CD запускает Ansible из ветки `main` репозитория `infra`
и выкатывает этот образ на единственную VPS Selectel.

Для локальной работы нужны Go версии из `go.mod`, компилятор C для `-race`,
Make и Docker с Compose. Команды:

```bash
make lint          # статический анализ
make test          # тесты с race detector, coverage.out и итоговое покрытие
make build         # бинарник bin/server
make docker-build  # образ colab-backend:local
make run           # API, PostgreSQL и SeaweedFS через Docker Compose
```

Первый локальный запуск (существующий `.env` не перезаписывайте):

```bash
cp -n .env.example .env
# В .env задайте JWT_SECRET и CSRF_SECRET: два разных случайных значения.
# Например, каждое получите командой: openssl rand -hex 32
make doctor
make up           # PostgreSQL + SeaweedFS + API, ожидание health до 120 секунд
make health       # {"status": "alive"}; по умолчанию http://localhost:8080/health
make logs         # последние 100 строк API, БД и S3
make down         # остановить; volumes с данными сохраняются
```

`make run` запускает то же окружение с логами в текущем терминале. Для другого
worktree задайте отдельный `COMPOSE_PROJECT_NAME` в его `.env` и свободные
`PORT`, `POSTGRES_PORT`, `S3_PORT` (по умолчанию 8080, 5432, 8333).
`POSTGRES_HOST=db` и `AWS_ENDPOINT_URL_S3=http://s3:8333` — внутренние адреса,
их порты при этом не меняются. При отказе `make up` смотрите `make logs` и
`docker compose ps -a`; неверные секреты исправьте в `.env`, затем повторите запуск.
Миграции goose применяются API при старте. `/health` показывает, что API отвечает;
это не проверка всех операций с БД/S3.

### Harness AI-агента

Начинайте с корня backend, содержащего `go.mod`, а не общей папки `cdd`.
Codex читает [AGENTS.md](AGENTS.md); Claude Code загружает его через
[CLAUDE.md](CLAUDE.md). Карта кода, границы изменений и команды находятся в AGENTS.
Для командного процесса нужен плагин `cdd-tasks` из
[agent-plugins](https://github.com/Cringe-Driven-Development-Team/agent-plugins).
В Claude Code marketplace и плагин объявлены в `.claude/settings.json`;
подтвердите установку в клиенте. В Codex установите плагин из того же репозитория
и убедитесь, что доступен скилл `cdd-tasks`. Перед работой читайте актуальный
[CONTRIBUTING организации](https://github.com/Cringe-Driven-Development-Team/.github/blob/main/CONTRIBUTING.md):
он имеет приоритет перед скиллом. Правила веток и PR — ниже в README.

Установите проверенную версию gopls с поддержкой headless MCP:

```bash
go install golang.org/x/tools/gopls@v0.20.0
export PATH="$(go env GOPATH)/bin:$PATH"  # сохраните в конфигурации своей shell
gopls version                           # v0.20.0
gopls mcp -help
```

В проекте уже есть `.codex/config.toml` для Codex и `.mcp.json` для Claude Code:
оба запускают `gopls mcp` через PATH, без абсолютного пути. Отдельный LSP-клиент
не нужен. Запускайте `codex` или `claude` из этого корня; для worktree — из его
корня. Доверие проекту и разрешение на запуск MCP подтверждаются в клиенте.
Codex загружает проектный config только в trusted-проекте; проверьте
`codex mcp get gopls` и `/mcp` в новой сессии. Проектная запись с тем же именем
перекрывает глобальную: не удаляйте `~/.codex/config.toml` и другие серверы.
В noninteractive `codex exec` запрос подтверждения MCP может завершиться ошибкой
`approval policy is never`. Для автоматической проверки разрешите на один запуск
нужные инструменты через `-c 'mcp_servers.gopls.tools.<tool>.approval_mode="approve"'`,
отдельно для `go_search`, `go_file_context`, `go_symbol_references`, `go_diagnostics`;
`go_workspace` проверяйте первым. Это не требуется для интерактивного подтверждения.
В Claude Code проверьте `claude mcp get gopls`, затем `/mcp`; локальная запись
`gopls` может иметь приоритет над проектной. Личные разрешения не коммитьте.
Источники: [Codex MCP](https://developers.openai.com/codex/mcp/),
[Claude Code MCP](https://code.claude.com/docs/en/mcp).

`make doctor` ничего не устанавливает и не печатает значения секретов. Он
проверяет Go в PATH относительно `go.mod` (с `GOTOOLCHAIN=local`, без
автоматического скачивания toolchain), CGO/C-компилятор для race, Make/curl/Git, gopls MCP,
Docker daemon/Compose, конфигурацию и секреты, авторизацию gh и права записи
в командном `Cringe-Driven-Development-Team/backend`, где теперь находятся и код, и задачи.
Права записи в учебную копию для разработки не нужны. Ошибки собираются вместе, код возврата ненулевой.
Установку клиента, trust, подключение MCP и наличие скилла проверяйте отдельно.
Apidog-токен нужен только для изменения контракта (`make generate`); Projects
нужен для работы с доской (`gh auth refresh -s project`). Сетевой отказ в
песочнице требует разрешения на сеть, а не обязательно нового токена gh.

Проверка в **новой сессии** после установки и принятия trust:

1. Попросите агента прочитать AGENTS.md, назвать точки входа и выполнить `make doctor`.
2. Через MCP вызовите `go_workspace`: ожидается модуль из текущего `go.mod`.
   Если модуль не найден, перезапустите клиент из корня backend.
3. `go_search` с `Handler.GetCurrentUser` должен найти
   `internal/user/delivery/user.go`; `go_file_context` — показать зависимости.
   `go_symbol_references` для `NewHandler` в этом файле должен найти сборку в
   `internal/app/app.go` и тесты; `go_diagnostics` для файла — вернуть диагностику.
   В аргументах путей используйте абсолютный путь **своего** checkout.
4. Выполните `make up`, `make health`, `make logs`, затем `make down`.
5. Во время разработки — `make check-fast` и `go test ./internal/<пакет>`;
   перед PR — `make check` и `make docker-build` (соответствуют lint/test/docker CI).
   Docker-проверка не публикует образ. Контракт меняйте по процессу Apidog ниже.
6. В PR запишите версии клиентов/gopls, результаты MCP, doctor, health и проверок.
   Недоступный клиент отмечайте как непроверенный; запуск MCP сам по себе
   не подтверждает интеграцию с обоими клиентами. Правила ревью и исключение YarikMix — в разделе работы с задачами ниже.

CORS для запросов с `credentials: "include"` настраивает Go API через
`CORS_ALLOWED_ORIGINS`. Если переменная отсутствует, разрешены
`https://cellestial.ru` и `http://localhost:5173`; явно пустое значение выключает
CORS. Заданный список заменяет значения по умолчанию: укажите все нужные origin
через запятые или пробелы, со схемой и без завершающего слэша. Другие порты,
`127.0.0.1` и dev-стенды добавляются явно; `*` не допускается.

Доверенный origin приложения для CSRF задаёт `APP_ORIGIN` (по умолчанию
`https://cellestial.ru`); он разрешён и при выключенном CORS.

Проверка preflight без авторизации (ожидается `204` с точным разрешённым origin,
`Access-Control-Allow-Credentials: true`, методами и заголовками):

```bash
curl -i -X OPTIONS http://localhost:8080/api/v1/users/me \
  -H 'Origin: http://localhost:5173' \
  -H 'Access-Control-Request-Method: GET' \
  -H 'Access-Control-Request-Headers: Content-Type, X-Request-ID, X-CSRF-Token'
```

В браузере фронт должен использовать `credentials: "include"`; разрешающие
CORS-заголовки приходят и на ошибках API. Политика cookie (`SameSite`/`Secure`)
настраивается отдельно и этим изменением не расширяется.

Пакет GHCR остаётся приватным. При первом publishing проверить связь package с
командным репозиторием и права Actions. CI публикует его с `GITHUB_TOKEN`; job выкатки получает `packages: read`
и передаёт собственный временный `GITHUB_TOKEN` в Ansible через окружение.
Постоянный PAT для production и отдельный GHCR secret не нужны. Для `main`
администратор включает обязательные проверки `lint`, `test`, `docker`.

Если публикация завершается с 403, передайте ментору ссылку на запуск для
проверки прав организации и пакета. Личные токены для обхода ограничений
не используются; CI работает с `GITHUB_TOKEN`.

## Рабочий репозиторий и синхронизация

Разработка и review ментора — в командном репозитории.
[Учебный репозиторий](https://github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development)
получает те же коммиты main; Actions там выключены, новые PR и reviews не копируются.
Sync после lint/test/docker включается `COURSE_SYNC_ENABLED=true`, не зависит от deploy.
Для повтора без deploy: Actions → Sync to course → main → `ci_run_id`
успешного push/main CI. Deploy в caller и CD требует `PRODUCTION_ENABLED=true`.
Отсутствующие flags выключают соответствующие операции. Production хранит
`DEPLOY_SSH_KEY`, `SSH_KNOWN_HOSTS`, `ANSIBLE_VAULT_PASSWORD`; contract-drift
использует repository secret `APIDOG_TOKEN`.
Порядок включения и восстановления задач —
[runbook организации](https://github.com/Cringe-Driven-Development-Team/.github/blob/main/docs/migrations/team-repositories-runbook.md).
Откат к старому registry требует полного
`ghcr.io/go-park-mail-ru/2026_2_cringe_driven_development:sha-<old>`
и credential с read-доступом к старому package; новый token может его не иметь.

## Авторизация

API принимает access-токен только из cookie `access_token`; заголовок
`Authorization` для авторизации не используется и в ответах не возвращается.
Login, register и refresh выдают три отдельные cookie:

| Cookie | Path | Срок жизни |
| --- | --- | --- |
| `access_token` | `/api/v1` | `ACCESS_TOKEN_TTL` (по умолчанию 15 минут) |
| `refresh_token` | `/api/v1/auth` | Оставшийся срок refresh-сессии (`REFRESH_TOKEN_TTL`, по умолчанию 30 дней) |
| `__Host-csrf` | `/` | `REFRESH_TOKEN_TTL` (по умолчанию 30 дней) |

Cookie `access_token` и `refresh_token` имеют `HttpOnly` и `SameSite=Lax`,
без `Domain`. Настройка `COOKIE_SECURE=true` включает `Secure` для обеих cookie;
в production с HTTPS она обязательна. Для локального HTTP используется `COOKIE_SECURE=false`.
Браузерный клиент отправляет запросы с `credentials: "include"` и не читает JWT.
При истечении access-токена закрытая ручка отвечает 401; клиент вызывает
`POST /api/v1/auth/refresh`, получает новые cookie и повторяет запрос.
Успешный logout удаляет cookie авторизации с исходными путями, отзывает refresh-сессию
и выдаёт новый анонимный CSRF-токен.

Для запуска нужен отдельный от `JWT_SECRET` обязательный `CSRF_SECRET`.

API выдаёт `__Host-csrf`, если её нет в запросе (кроме OPTIONS), в том числе на 401/403.
Для действующей access/refresh-сессии выдаётся подписанный токен; анонимная cookie
при наличии сессии заменяется подписанной. После отказа повторите запрос с новой cookie.
Cookie всегда `Secure`, без `HttpOnly`. На всех POST, PUT, PATCH и DELETE, включая
ручки авторизации, передавайте её свежее значение в `X-CSRF-Token`: токен меняется
при login/register/refresh/logout. Отказ — `403 csrf_invalid`, без запуска refresh.

Пример для локального API: curl сохраняет cookie в файл и отправляет их сам.
Используйте тестовую учётную запись; файл cookie содержит токены, не коммитьте его.

```bash
cookie_jar=$(mktemp)
csrf_token() { awk '$6 == "__Host-csrf" { print $7 }' "$cookie_jar"; }
# Получить CSRF-cookie (для гостя ожидается 401).
curl -si -c "$cookie_jar" http://localhost:8080/api/v1/users/me
curl --fail-with-body -i -b "$cookie_jar" -c "$cookie_jar" \
  -H "X-CSRF-Token: $(csrf_token)" -H 'Content-Type: application/json' \
  -d '{"login":"bob","password":"password123"}' \
  http://localhost:8080/api/v1/auth/login
curl --fail-with-body -b "$cookie_jar" http://localhost:8080/api/v1/users/me
curl --fail-with-body -i -b "$cookie_jar" -c "$cookie_jar" -X POST \
  -H "X-CSRF-Token: $(csrf_token)" http://localhost:8080/api/v1/auth/refresh
curl --fail-with-body -i -b "$cookie_jar" -c "$cookie_jar" -X POST \
  -H "X-CSRF-Token: $(csrf_token)" http://localhost:8080/api/v1/auth/logout
curl -i -b "$cookie_jar" http://localhost:8080/api/v1/users/me # ожидается 401
rm -f "$cookie_jar"
```

## Production-деплой

На VPS один проект Compose `cellestial` в `/opt/cellestial`: Caddy, Go API и
Postgres. Caddy проксирует `/api/v1/*` в API с сохранением пути; API и БД не публикуют
порты наружу. Production Compose, Caddyfile, Vault и playbook принадлежат `infra`;
местный `docker-compose.yml` остаётся окружением разработки. Пути клиента
вне `/api/v1/*` Caddy продолжает обслуживать из S3.

После успешных `lint`, `test`, `docker` на push в `main` CI вызывает
[CD](.github/workflows/cd.yml) через `workflow_call`, если `PRODUCTION_ENABLED=true`.
Job `deploy` использует environment `production`, разрешённый только из `main`.
Отдельного approval environment в действующей конфигурации нет: обязательное
ревью ментора проходит перед merge в `main`, кроме разрешённого исключения для `YarikMix`.

Подготовка командного CI:

- Приватный package `ghcr.io/cringe-driven-development-team/backend` связан с этим
  репозиторием; после публикации CI проверяет приватность и привязку через API.
- Отдельный public key командного CI хранится в
  `infra/ansible/files/authorized_keys/ci-team-backend.pub`, private key — только
  в environment secret `DEPLOY_SSH_KEY`. Public key принимается через infra PR
  и устанавливается существующим операторским ключом перед первым deploy.
- `SSH_KNOWN_HOSTS` содержит проверенный host key; `ANSIBLE_VAULT_PASSWORD` —
  пароль существующего Vault. Личный SSH-ключ оператора в CI не передаётся.
- Старый `ci.pub`, package и образ сохраняются для отката. Новый image default
  в infra принимается после успешной публикации нового package и остановки
  старых deploy jobs. Первое включение и откат описаны в
  [runbook миграции](https://github.com/Cringe-Driven-Development-Team/.github/blob/main/docs/migrations/team-repositories-runbook.md).

Первоначальная подготовка учебного CI (история до переноса):

- Пакет остаётся приватным и доступен `GITHUB_TOKEN` курсового репозитория.
  Ansible временно авторизует VPS для pull и делает logout даже при ошибке.
- Денис создаёт отдельный CI-ключ, коммитит `ci.pub` в infra и устанавливает ключ
  через `site.yml`. Проверенный host key берёт с доверенного подключения.
- Ярослав или Александр добавляет environment secrets `DEPLOY_SSH_KEY`,
  `SSH_KNOWN_HOSTS`, `ANSIBLE_VAULT_PASSWORD`. Личные SSH-ключи для CI не используются.
- Существующие `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID`, `TELEGRAM_TOPIC_ID` доступны
  job без дублирования. Результат и ссылка на запуск отправляются в основной топик.
- Сначала влить infra#22 и PR, закрывающий infra#30, затем установить `ci.pub`
  через `site.yml` и настроить environment. Backend PR мержится последним;
  job выкатки получает `deploy-backend.yml` из `infra/main`.

Деплой пишет пароль Vault и SSH-файлы во временный каталог runner, проверяет host key,
запускает `deploy-backend.yml` со статическим inventory и удаляет временные файлы.
В CI не нужны Selectel credentials, доступ к Pulumi state и `pulumi up`.

Только шаг с playbook получает `GHCR_USERNAME` из `github.actor` и `GHCR_TOKEN`
из `secrets.GITHUB_TOKEN`. Токен не передаётся через `-e`, не записывается в Vault
или `.env` API. Ansible выполняет login через stdin с `no_log`, скачивает API,
всегда делает logout и запускает уже скачанный образ. Отсутствие credentials
останавливает playbook до изменений VPS. Анонимный HTTP 401 для пакета ожидаем.

Playbook ждёт healthcheck `/health` внутри Compose и проверяет через HTTPS
`/api/v1/users/me`: без токена ожидается JSON с HTTP 401. Миграции встроены в образ и
выполняются при старте; отдельного шага миграций нет. Ошибка Telegram показывается
предупреждением и не подменяет результат деплоя.

Параметры S3 из `.env.example` роль `app` передаёт из infra vars и Vault.
Go API хранит файлы блокнотов в бакете `S3_NOTEBOOKS_BUCKET`, используя
`AWS_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` и `AWS_ENDPOINT_URL_S3`.
Локальный Compose использует SeaweedFS, production — Selectel. Параметры
аватарок передаются конфигурацией, но текущий API их ещё не читает.

Откат выполняется из infra тем же playbook с прежним опубликованным SHA-тегом,
см. [инструкцию Ansible](https://github.com/Cringe-Driven-Development-Team/infra/blob/main/ansible/README.md).
Миграции Down автоматически не запускаются: прежний бинарник должен быть совместим
с уже применённой схемой. SHA-тег может быть перезаписан повторной сборкой; перед
откатом сверить digest с исходным запуском публикации.

Клиент не перезагружает открытые вкладки автоматически. Go API N+1 должен
обслуживать клиента N: ручки и поля не удаляются в релизе, где клиент перестаёт их
использовать. Совместимость проверяется на ревью контракта в Apidog.

## Ссылки

- [Сервис cellestial.ru](https://cellestial.ru)
- [Доска задач](https://github.com/orgs/Cringe-Driven-Development-Team/projects/1)
- [Репозиторий фронтенда](https://github.com/Cringe-Driven-Development-Team/frontend)
- [Организация команды](https://github.com/Cringe-Driven-Development-Team)

## Участники команды

1. [Ерофей Гаранин](https://github.com/ManInTheCoat)
2. [Истратов Денис](https://github.com/iRedTea)
3. [Шпакова Дарья](https://github.com/GrayMouse9)
4. [Кунев Валентин](https://github.com/MrDuckVC)

## Менторы

- [Михалёв Ярослав](https://github.com/YarikMix) — _Frontend_
- [Батовкин Александр](https://github.com/blackHATred) — _Backend_
- [Ченцова Дарья](https://t.me/dewon_d) — _UX_

## Как работать с задачами

Код, PR и задачи ведём в
[Cringe-Driven-Development-Team/backend](https://github.com/Cringe-Driven-Development-Team/backend)
и на общей [доске](https://github.com/orgs/Cringe-Driven-Development-Team/projects/1) вместе с фронтовыми

1. **Завести задачу** в репозитории `backend`. Как завести и что писать в описании —
   в [гайдлайне организации](https://github.com/Cringe-Driven-Development-Team/.github/blob/main/CONTRIBUTING.md#как-завести)

2. **Взять задачу.** На доске выбрать карточку из `Ready`, поставить себя
   в `Assignees`, перевести в `In progress`

3. **Создать ветку.** Открыть задачу → в правой колонке `Development` →
   **`Create a branch`**. В `Repository destination` выбрать
   `Cringe-Driven-Development-Team/backend`,
   имя ветки заменить на `api-<номер задачи>`, например `api-12`.
   Затем `Create branch` и локально:

   ```bash
   git fetch
   git switch api-12
   ```

4. **Закоммитить** по шаблону `<тип>: <описание>`, типы — в таблице ниже.
   Область в скобках после типа указывать необязательно:

   ```
   feat: добавить эндпоинт регистрации
   fix: не отдавать 500 при пустом теле запроса
   refactor(auth): вынести проверку токена в middleware
   ```

5. **Открыть pull request** в `main`, когда код готов к ревью.
   Заголовок — по шаблону `API-<номер задачи>: <название задачи>`, например
   `API-12: Эндпоинт регистрации`. В описании — **обязательно** строка

   ```
   Closes #12
   ```

   Что писать, если задача затрагивает ещё один репозиторий, —
   в [гайдлайне](https://github.com/Cringe-Driven-Development-Team/.github/blob/main/CONTRIBUTING.md#как-закрыть).
   Убедиться, что в правой колонке PR в блоке `Development` указана задача

6. **Получить апрув** от [Александра](https://github.com/blackHATred), кроме исключения для `YarikMix` ниже.

7. **Влить в `main`** через `Merge pull request`.
   Задача закроется сама, карточка уедет в `Done`

> [!TIP]
> Ветку, созданную руками (`git switch -c api-12 origin/main`), с задачей свяжет
> та же строка `Closes …` — поэтому она обязательна всегда

## Ревью и исключение для YarikMix

По согласованию владельца `YarikMix` может мержить backend PR без Approve,
включая свои PR. Для остальных участников ревью Александра обязательно.
Исключение задано только для этого аккаунта в approval ruleset и действует
только через PR. Отдельный ruleset без bypass требует зелёные `lint`, `test`,
`docker`, актуальность ветки и разрешение обсуждений; force-push и удаление main
запрещены. Используется обычный merge commit без `--admin`.

## Типы коммитов

> [!NOTE]
> Коммиты ветки попадают в `main` как есть, поэтому от их качества зависит
> читаемость истории проекта. Сверху добавляется merge-коммит с названием
> pull request

| Тип | Когда используется |
|---|---|
| `feat` | новая функциональность |
| `fix` | исправление бага |
| `refactor` | код переписан, поведение не изменилось |
| `style` | форматирование и отступы, логика не тронута |
| `test` | тесты |
| `docs` | документация |
| `chore` | конфиги, зависимости, сборка, CI |

## Статусы на доске

| Статус | Что означает |
|---|---|
| `Backlog` | задача заведена, но не запланирована в спринт |
| `Ready` | взята в спринт, можно брать в работу |
| `In progress` | в работе |
| `In review` | открыт pull request, ждёт ревью |
| `Done` | влито в `main` |

Статусы доска двигает сама по событиям в pull request. Руками нужно только взять задачу в работу — остальное происходит автоматически

## Структура кода

- `cmd/main` и `cmd/apidog` — точки входа; логика экспорта контракта — `internal/tooling/apidog`.
- `internal/app` собирает зависимости и запускает API; `internal/config` читает окружение.
- `internal/transport/http` содержит роутер, `/health`, сгенерированный API,
  middleware, security, cookies, principal и преобразование ошибок.
- `internal/user` и `internal/notebook`: `domain` — типы предметной области,
  `usecase` — сценарии и необходимые им интерфейсы, `delivery/http` — обработчики,
  `adapters` — реализации для PostgreSQL, S3, криптографии и ipynb.
- Зависимости направлены внутрь: адаптеры реализуют интерфейсы usecase;
  domain не знает об HTTP и хранилищах. Сборка реализаций выполняется в app.

## Как менять контракт

Контракт API живёт в [Apidog](https://app.apidog.com/project/1382426) (проект `1382426`).
Источник истины — ветка `main` Apidog, опубликованная документация —
[vb78fyael1.apidog.io](https://vb78fyael1.apidog.io/). Файла `openapi.yaml` в репозитории нет:
`make generate` скачивает спецификацию по API и генерирует `internal/transport/http/api/api.gen.go`.
Сгенерированный код коммитится и руками не правится

### Личный токен

`make generate` ходит в Apidog с личным токеном каждого разработчика:

1. Apidog → аватар → **Account Settings → API Access Token** → **New**
2. Токен показывается один раз — сразу скопировать
3. Положить в локальный `.env` (он в `.gitignore`):

   ```bash
   APIDOG_TOKEN=<токен>
   ```


### Порядок правки

Ветка `main` в Apidog защищена: правки идут через sprint-ветку и Merge Request

1. Переключатель веток рядом с **APIs** → **New Sprint Branch**.
   Имя — как у ветки в git (`api-16`), источник — `main`
2. Ветка создаётся пустой. Чтобы изменить ручку или схему, её нужно забрать из `main`:
   **Pick from main**. Всё, что не забрано, ветка показывает из `main`
3. Внести правки. Ручки редактируются на вкладке **Design**: вкладка **Request** —
   отладочная, её настройки в контракт не попадают
4. **Merge to main** → выбрать ресурсы → **Create Merge Request**
5. Администратор ветки смотрит изменения и одобряет
6. После слияния — `make generate` без `APIDOG_BRANCH_ID` и PR с кодом

Пока Merge Request на ревью, код можно писать по sprint-ветке:

```bash
APIDOG_BRANCH_ID=<id ветки> make generate
```

В `main` репозитория попадает только код, сгенерированный из `main` Apidog: перед мержем PR
перегенерировать без `APIDOG_BRANCH_ID`

### ID sprint-ветки

Переключатель веток рядом с **APIs** → **Manage Sprint Branches**. ID — серое число
рядом с именем ветки (`# 1389936`). Там же видно, кто в `Branch Admins` и защищена ли ветка

### Что видно в Merge Request

Окно слияния показывает список ресурсов ветки с цветными точками: зелёная — новый,
оранжевая — изменённый, серая — без изменений (не сливается). Клик по изменённому
ресурсу показывает его рядом с версией из `main`.

Ручка с изменённым путём или методом считается новой: в Merge Request она появится как
новая, а старая — как удалённая, без сравнения «было — стало». Сравнить можно, открыв
ручку во вкладке **Preview** в `main` и в sprint-ветке

### Конфликты двух sprint-веток

Слияния по частям нет: изменённый ресурс (ручка или схема целиком) при слиянии
**перезаписывает** версию в `main`. Если две sprint-ветки поменяли один ресурс,
побеждает та, что влита позже, а правки первой молча теряются.

Поэтому перед одобрением Merge Request ревьюер сравнивает изменённые ресурсы с **текущим**
`main`: если там видны откаты чужих правок, ветку нужно обновить. История изменений ресурса
в `main` (**Change History**) позволяет посмотреть слияния и откатить неудачное

### Опубликованная документация

[vb78fyael1.apidog.io](https://vb78fyael1.apidog.io/) показывает только `main`
(**Publish Docs** → сайт → **Source Branch**) и обновляется сам сразу после слияния,
перепубликовывать не нужно. Правки из sprint-веток на сайте не видны, пока их не вольют в `main`.


### Подводные камни

- **Авторизация задаётся на самой ручке.** Auth, выставленный на папке, в Apidog работает,
  но в экспорт (а значит, в `security` и в сгенерированный код) не попадает.
  Для закрытой ручки: **Design → Auth → Security Scheme → схема cookie-авторизации**
  (`type: apiKey`, `in: cookie`, `name: access_token`)
- **Удаления через sprint-ветку не работают.** Удалить ресурс в ветке — значит просто
  перестать его переопределять: он продолжает приходить из `main` и в экспорт, и в
  `make generate`, а при слиянии не удаляется. Ненужные ручки и схемы удаляются
  в `main` отдельно, после слияния.
