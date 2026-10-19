# План реализации логирования backend

**Цель:** Связать безопасные JSON-логи HTTP, usecase, PostgreSQL и S3 одним `request_id` и управлять подробностью через `LOG_LEVEL`.

**Архитектура:** Нейтральный пакет `internal/observability` хранит ID в контексте и добавляет его через `slog.Handler`. Внешнее HTTP middleware измеряет ответ; маршрутизатор заполняет шаблон маршрута в общих данных запроса. Usecase и адаптеры пишут события через `slog.*Context`, не меняя публичный HTTP API.

**Технологии:** Go 1.27.1, стандартный `log/slog`, `net/http`, Gorilla mux, `pgx/v5`, AWS SDK v2; новых зависимостей нет.

**Спецификация:** [Дизайн логирования backend](../specs/2026-10-19-backend-logging-design.md).

## Общие ограничения

- Код менять в `Cringe-Driven-Development-Team/backend`, исходная точка — `09e91c89930f446fbdc2a1484868248bb8776651`; спецификация и план хранятся в `docs/superpowers/` этого же репозитория. Пути к файлам ниже указаны от корня backend.
- Сохранить существующий JSON-формат `slog`, HTTP-коды, тела ответов, cookie и семантику БД/S3. Не менять `internal/transport/http/api/api.gen.go` вручную.
- `LOG_LEVEL`: ровно `debug`, `info`, `warn`, `error`; по умолчанию `info`, неизвестное значение останавливает запуск. `Fatal` не вводить.
- Не логировать сырой `err.Error()`, query, заголовки, тела, пароль, токены, хеши, SQL/аргументы, имена блокнотов, текст ячеек, содержимое `.ipynb` или секреты S3.
- Сохранять `ctx` на всех слоях. Одно успешное изменение даёт одно событие usecase `Info` и одну итоговую запись HTTP; операция хранилища даёт `Debug`.

## На что смотреть при ревью

1. Заголовок `X-Request-ID` с переводом строки, пробелом или длиной >64 символов заменяется UUID; ни заголовок, ни его фрагменты не попадают в лог (задача 2).
2. Ответы до входа в маршрутизатор — CORS, CSRF, 404/405 и panic — всё равно получают ID, корректный статус и одну итоговую запись; маршрут для раннего отказа — `unmatched` (задача 2).
3. `ResponseWriter` без явного `WriteHeader` даёт статус 200; `Flush`, `Hijack`, `Push` и `ReadFrom` работают, когда их поддерживает исходный writer (задача 2).
4. Ошибка `pgconn.PgError` с секретом в `Detail` показывает только SQLSTATE; ошибка `Scan` и ошибка чтения S3 body входят в исход и длительность операции (задачи 5–6).
5. Ошибка проверки хеша пароля отличается безопасным кодом от обычного несовпадения; ни пароль, ни хеш, ни `err.Error()` не попадают в лог (задача 3).

---

### Задача 1: Контекст, JSON handler, уровень и запуск

**Файлы:**
- Создать: `internal/observability/request.go`, `handler.go`, `error.go`, `observability_test.go`
- Изменить: `internal/config/config.go`, `config_test.go`; `internal/app/run.go`; `cmd/main/main.go`; `.env.example`; `docs/local-development.md`

**Интерфейсы:**
- Результат: `observability.WithRequestID(ctx context.Context, id string) context.Context`; `observability.RequestID(ctx context.Context) string`; `observability.NewHandler(next slog.Handler) slog.Handler`.
- Результат: `observability.WithCode(err error, code string) error` (сохраняет `Unwrap`) и `observability.ErrorCode(err error) string` (читает только код обёртки, резервный код `internal_error`); `Config.LogLevel slog.Level`; `app.Run(cfg config.Config) error`. В `app` закрытая `startupError(stage string, err error) error` помечает известные этапы стабильным кодом.
- `NewHandler` обязан сохранять поведение `WithAttrs` и `WithGroup`; ID добавляется только к записи с контекстом. `ErrorCode` никогда не возвращает текст ошибки.

- [x] **Шаг 1: Написать падающие тесты.** В `observability_test.go`: JSON-запись `InfoContext(WithRequestID(..., "req-1"))` содержит один `request_id=req-1`; запись без ID не содержит поля; `WithAttrs` и `WithGroup` сохраняют ID; для `source := errors.New("SECRET")` вызов `ErrorCode(WithCode(source, "startup_config"))` даёт `startup_config`, а `errors.Is(WithCode(source, "startup_config"), source)` истинен; неизвестная ошибка даёт `internal_error`. В `config_test.go`: таблица `LOG_LEVEL` для пустого значения и четырёх допустимых строк даёт нужный `slog.Level`, `trace` возвращает ошибку без значения переменной.
- [x] **Шаг 2: Проверить красный результат:** `go test ./internal/observability ./internal/config`; ожидается ошибка сборки из-за отсутствующих интерфейсов/поля.
- [x] **Шаг 3: Реализовать пакет и конфигурацию.** Контекстный ключ оставить закрытым. Handler декорирует `Handle(ctx, record)` без изменения исходного `record`; `Enabled`, `WithAttrs`, `WithGroup` делегируют вложенному handler. Разбор уровня сделать в `config.LoadFrom`, строго по четырём строкам.
- [x] **Шаг 4: Проверить зелёный результат:** `go test ./internal/observability ./internal/config`; все тесты проходят.
- [x] **Шаг 5: Написать падающий тест запуска** в `internal/app` для `startupError(stage, err)`: этапы `database_connect`, `database_migrate`, `s3_client`, `http_listen` дают одноимённые безопасные коды через `observability.ErrorCode`; неизвестный этап даёт `internal_error`; `errors.Is` сохраняет исходную ошибку. JSON-запись с полученным кодом не содержит исходную строку с секретным сентинелом и фиктивный `request_id`.
- [x] **Шаг 6: Проверить красный результат:** `go test ./internal/app`; ожидается отсутствие безопасной классификации.
- [x] **Шаг 7: Подключить logger и безопасный запуск.** В `cmd/main` сначала установить JSON stdout handler с `HandlerOptions{Level: &slog.LevelVar}` и начальным `info`, затем загрузить config, выставить уровень и вызвать `app.Run(cfg)`; при ошибке сохранить exit code 1 и логировать только `observability.ErrorCode(err)`. Ошибки `app` оборачивать через `startupError` на границах startup-этапов; ошибку конфигурации помечать `startup_config`, без сырого текста. Предупреждения о невалидных duration сохраняют только имя ключа.
- [x] **Шаг 8: Обновить `.env.example` и `docs/local-development.md`**: `LOG_LEVEL=info`, допустимые значения, JSON-поля, `request_id` и запрет на чувствительные значения.
- [x] **Шаг 9: Проверить зелёный результат:** `go test ./internal/observability ./internal/config ./internal/app ./cmd/main`; ожидается PASS, `cmd/main` без тестов согласно `AGENTS.md`.

### Задача 2: Один HTTP итог и безопасный request ID

**Файлы:**
- Изменить: `internal/transport/http/middleware/common.go`; `internal/transport/http/router.go`; `internal/transport/http/httperr/httperr.go`; `internal/transport/http/router_test.go`
- Создать: `internal/transport/http/middleware/common_test.go`, `response_writer.go`, `response_writer_test.go`

**Интерфейсы:**
- Использует: `observability.WithRequestID`, `RequestID`, `ErrorCode`, контекстный обработчик из задачи 1.
- Результат: `middleware.RecordRoute(next http.Handler) http.Handler` для `r.Use`; закрытая структура `requestMeta` в контексте содержит шаблон маршрута, который внешний `Logging` читает после ответа.
- `RequestID` принимает только ASCII `[A-Za-z0-9._-]`, длина 1–64; иначе создаёт UUID. Событие HTTP: `http.request`, поля `method`, `route`, `status`, `duration`, `request_id`.

- [x] **Шаг 1: Написать падающие HTTP-тесты** в `router_test.go` и `common_test.go`: валидный ID сохраняется; пустой/невалидный заменяется UUID; для `/health`, маршрута с параметром, 401/403, 404/405, panic и раннего CORS-ответа есть ровно одна запись `http.request`, совпадающий ID и правильный статус; query и секретный raw path отсутствуют; неизвестный маршрут — `unmatched`.
- [x] **Шаг 2: Написать падающие writer-тесты** в `response_writer_test.go`: неявный 200; явный 201/500; `ReadFrom` считает записанные байты; `Flush`, `Hijack`, `Push` доступны и делегируются, если их поддерживает базовый writer.
- [x] **Шаг 3: Проверить красный результат:** `go test ./internal/transport/http/...`; ожидается отсутствие статуса/маршрута/безопасного ID и провал тестов writer.
- [x] **Шаг 4: Реализовать middleware и writer.** Сохранить порядок `RequestID → Logging → Recover → CORS → CSRF/Authenticate → Validator`; `Logging` создаёт общие данные запроса, `RecordRoute` в `mux.Router.Use` заполняет шаблон через `mux.CurrentRoute(r).GetPathTemplate()`. Для раннего отказа оставить `unmatched`; writer фиксирует первый статус и делегирует доступные возможности стандартного сервера.
- [x] **Шаг 5: Убрать сырую ошибку из `httperr.Internal`**: `slog.ErrorContext` пишет стабильное событие `http.internal_error` и `error_code=observability.ErrorCode(err)`; API-ответ остаётся прежним. Ожидаемые 4xx не получают `Error`.
- [x] **Шаг 6: Проверить зелёный результат:** `go test ./internal/transport/http/...`; ожидается PASS. Проверить отдельно лог 500 с секретом в `err.Error()`: секрета в JSON нет.

### Задача 3: Значимые события пользователя и сессии

**Файлы:**
- Изменить: `internal/user/usecase/user.go`, `user_test.go`, `ports_test.go`

**Интерфейсы:**
- Использует: `slog.InfoContext(ctx, ...)`, безопасные коды из задачи 1.
- Результат: `Info` только после успешного завершения `Register`, `Login`, `Refresh`, `Logout`: события `user.register`, `user.login`, `session.refresh`, `session.logout`, поле `outcome=success`; внутренний числовой `user_id` допустим. Ожидаемые ошибки авторизации не пишутся на `Error`.

- [x] **Шаг 1: Написать падающие тесты**: по одному успеху каждого метода даёт ровно одно соответствующее `Info` с ID из контекста и без login/password/token/hash; `ErrInvalidCredentials` и `ErrInvalidRefresh` не дают `Error`; ошибка `PasswordHasher.Compare` даёт отличимую безопасную категорию, но не текст ошибки или пароль.
- [x] **Шаг 2: Проверить красный результат:** `go test ./internal/user/usecase`; ожидается отсутствие событий.
- [x] **Шаг 3: Добавить `slog.*Context` в точки завершения usecase.** Не логировать промежуточный успех `startSession`, если внешняя операция ещё может завершиться ошибкой. Сбой сравнения пароля классифицировать стабильным кодом `password_compare_failed`; неверный пароль остаётся обычной 4xx веткой.
- [x] **Шаг 4: Проверить зелёный результат:** `go test ./internal/user/usecase`; ожидается PASS.

### Задача 4: Значимые события блокнота и ячеек

**Файлы:**
- Изменить: `internal/notebook/usecase/notebook.go`, `notebook_test.go`, `ports_test.go`

**Интерфейсы:**
- Использует: `slog.InfoContext(ctx, ...)` и ID из задачи 1.
- Результат: по одному `Info` после успешных `Create`, `CreateCell`, `DeleteCell`: `notebook.create`, `notebook.cell_create`, `notebook.cell_delete`, `outcome=success`. `List` и `Get` не создают `Info` usecase.

- [x] **Шаг 1: Написать падающие тесты**: каждое успешное изменение даёт одну запись с правильным событием и ID; ошибка S3/БД/кодека не создаёт ложного успешного события; имя, ключ файла и содержимое ячейки отсутствуют.
- [x] **Шаг 2: Проверить красный результат:** `go test ./internal/notebook/usecase`; ожидается отсутствие событий.
- [x] **Шаг 3: Добавить события после успешного завершения операций.** Не писать `notebook.cell_update`: такой HTTP-операции в текущем API нет.
- [x] **Шаг 4: Проверить зелёный результат:** `go test ./internal/notebook/usecase`; ожидается PASS.

### Задача 5: Диагностика PostgreSQL без SQL и аргументов

**Файлы:**
- Изменить: `internal/user/adapters/postgres/user.go`; `internal/notebook/adapters/postgres/notebook.go`
- Создать: `internal/user/adapters/postgres/user_test.go`; `internal/notebook/adapters/postgres/notebook_test.go`; `internal/observability/storage.go`, `storage_test.go`

**Интерфейсы:**
- Результат: `observability.LogStorage(ctx context.Context, operation string, started time.Time, err error)` — один `Debug` с `operation`, `duration`, `outcome=success|error|not_found`; для `*pgconn.PgError` добавить только `sqlstate`, без `Detail` и сообщения.
- Не менять существующие интерфейсы `Querier`/`DB`, SQL-запросы и аргументы.

- [x] **Шаг 1: Написать падающие тесты helper**: успешная операция пишет `Debug` с длительностью и ID; `pgconn.PgError{Code:"23505", Detail:"SECRET"}` пишет `sqlstate=23505`, но не `SECRET`; обычная ошибка даёт только безопасный `error_code`.
- [x] **Шаг 2: Написать падающие тесты адаптеров** с fake `pgx.Row`/`Querier`/`DB`: `CreateUser`, `UserByLogin`, `NotebookRepository.Create`, `List`, `Get`, `Modify` пишут по одному событию на вызов; ошибка `Scan`, `CollectRows`, `Begin` или `Commit` меняет `outcome` на `error`, а возвращаемая ошибка сохраняется.
- [x] **Шаг 3: Проверить красный результат:** `go test ./internal/observability ./internal/user/adapters/postgres ./internal/notebook/adapters/postgres`; ожидается отсутствие storage событий.
- [x] **Шаг 4: Реализовать `LogStorage` и вызывать его через `defer` в публичных методах адаптеров.** Для `QueryRow` измерять до конца `Scan`; для `Query` — до конца `CollectRows`; для `Modify` — до конца `Commit`/ошибки. Названия операций стабильны: `user.create`, `user.by_login`, `user.by_id`, `session.create`, `session.delete`, `session.by_token`, `notebook.create`, `notebook.list`, `notebook.get`, `notebook.modify`.
- [x] **Шаг 5: Проверить зелёный результат:** та же команда `go test`; ожидается PASS и отсутствие SQL, аргументов, login, token hash и `PgError.Detail` в JSON.

### Задача 6: Диагностика S3 и итоговая проверка

**Файлы:**
- Изменить: `internal/notebook/adapters/s3/files.go`, `files_test.go`; при необходимости `internal/transport/http/router_test.go` для сквозного сценария

**Интерфейсы:**
- Использует: `observability.LogStorage` из задачи 5.
- Результат: `notebook.get_object` и `notebook.put_object` на `Debug`, с `duration`, `outcome`, `request_id` и безопасной категорией ошибки; ни bucket/key, ни содержимое объекта не записываются.

- [x] **Шаг 1: Написать падающие тесты S3**: успешные `Get`/`Put`, `NoSuchKey`, ошибка `PutObject` и ошибка `io.ReadAll` дают по одному событию на вызов; ID совпадает с контекстом; секретные bucket/key/body/текст SDK-ошибки отсутствуют.
- [x] **Шаг 2: Проверить красный результат:** `go test ./internal/notebook/adapters/s3`; ожидается отсутствие событий.
- [x] **Шаг 3: Добавить `LogStorage` после полного завершения `Get`/`Put`.** Для `Get` включить чтение body во время операции; возвращаемую ошибку и `ErrFileNotFound` сохранить без изменений.
- [x] **Шаг 4: Проверить зелёный результат:** `go test ./internal/notebook/adapters/s3`; ожидается PASS.
- [x] **Шаг 5: Проверить сквозную корреляцию** тестом HTTP → usecase → подставными репозиторием и S3: одинаковый ID в `http.request`, успешном usecase-событии и storage-событии при `LOG_LEVEL=debug`; при `info` Debug отсутствует, HTTP-ответ тот же.
- [x] **Шаг 6: Запустить финальные проверки** из корня backend: `go test -race ./...`, `make check`, `make docker-build`. Ожидается PASS; если Docker недоступен, записать конкретную причину, не объявлять сборку проверенной.
- [x] **Шаг 7: Просмотреть diff и результаты проверок.** Сверить пять пунктов раздела «На что смотреть при ревью», отсутствие новых зависимостей и ручных изменений сгенерированного API; передать пользователю результат и команды для самостоятельного коммита/push/PR при необходимости.

## Самопроверка

Покрытие спеки: Задача 1 — JSON, `LOG_LEVEL`, запуск; Задача 2 — ID, статус и маршрут HTTP и 500; Задачи 3–4 — успешные действия и безопасная авторизация; Задачи 5–6 — PostgreSQL/S3 и SQLSTATE; Задача 6 — сквозная корреляция и проверки. Все пять пунктов раздела «На что смотреть при ревью» закреплены в тестовых шагах своих заданий. Публичный HTTP API и семантика хранилищ остаются прежними.
