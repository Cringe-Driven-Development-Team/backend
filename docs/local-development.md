# Локальная разработка backend

Запуск Identity, Workspace, Environments и gRPC-агента в этой версии
описан в [руководстве по сервисам](microservices.md). Для переноса данных
используйте [порядок миграции монолита](microservices-migration.md).
Команды Compose и запуск `cmd/main` ниже относятся к выпущенной версии
монолита до переноса схем.

## Запуск и проверки

Нужны Go из `go.mod`, C-компилятор для `-race`, Make, Docker Compose и Node.js
с npm для проверки контракта. Для агента нужен Python 3.13.

1. Скопируйте `.env.example` в `.env`. Заполните JWT и BFF secrets разными
   случайными значениями. Для TOTP и runtime используйте разные ключи согласно
   примеру. Go вне Compose запускайте с `RUNTIME_PROVIDER=fake`.
2. Выполните `make doctor` и `make up`. Compose поднимает Go API, PostgreSQL,
   Redis и S3-совместимый SeaweedFS. `make health` проверяет готовность,
   `make logs` показывает логи, `make down` останавливает сервисы.
3. Для отдельной сборки используйте `make build`. Перед проверкой полного diff
   запустите `make check-fast` и тесты изменённых пакетов.

## OnlineConf

1. Подготовьте PostgreSQL, Redis, S3 и TLS для отдельного Identity по
   [руководству](identity-grpc.md). Примените миграции от владельца схемы и
   выдайте app-роли Identity право `SELECT` на `identity.onlineconf_admins`.
   Запустите полный `go run ./cmd/identity`. Для проверки роли достаточно
   Identity и его хранилищ, продуктовый API и Workspace можно оставить выключенными.
2. Выпустите для OnlineConf клиентский сертификат с DNS SAN
   `onlineconf.internal.cellestial.ru` и EKU ClientAuth от CA, которому доверяет
   Identity. Сертификат сервера Identity должен содержать DNS SAN
   `identity.internal.cellestial.ru`. В отдельном терминале укажите пути к
   клиентским TLS-файлам. Подготовьте отдельную схему OnlineConf. Примените
   миграции с подключением владельца через `DATABASE_URL`:

   ```sh
   go run ./cmd/migrate --service onlineconf up
   ```

   Выдайте прикладной роли `USAGE` схемы `onlineconf`, права
   `SELECT, INSERT, UPDATE, DELETE` на `onlineconf.keys` и `SELECT` на
   `onlineconf.goose_db_version`. Используйте подключение этой роли при запуске:

   ```sh
   ONLINECONF_DATABASE_URL="$ONLINECONF_DATABASE_URL" \
     ONLINECONF_HTTP_ADDR=127.0.0.1:8085 IDENTITY_GRPC_ADDR=127.0.0.1:9090 \
     TLS_CERT_FILE=/path/to/onlineconf.crt TLS_KEY_FILE=/path/to/onlineconf.key \
     TLS_CA_FILE=/path/to/ca.crt BFF_API_KEY="$BFF_API_KEY" go run ./cmd/onlineconf
   ```

3. Проверьте `GET http://127.0.0.1:8085/health` для проверки процесса.
   Настройте одинаковый непустой `BFF_API_KEY` у OnlineConf и вызывающего BFF.
   Для локального запроса передайте его в `X-BFF-Key`. Получите
   пользовательский access token через Identity и передайте его в
   `Authorization: Bearer …` на `GET http://127.0.0.1:8085/admin/access`.
   Без правильного ключа ответ `403 s2s_forbidden`. С правильным ключом
   без действующей сессии ответ `401 unauthorized`, без роли `403 forbidden`,
   с ролью `204`. Ошибки доступа возвращаются в JSON.
   BFF добавляет ключ на сервере и хранит его вне браузера.
   При недоступном Identity административный запрос с токеном получает `503`.

С теми же заголовками проверьте `GET /admin/keys` и CRUD по [контракту](../spec/openapi.json).
Публичный BFF направляет `/api/v1/onlineconf/admin/...` в `/admin/...`.
Чтобы проверить сохранность данных, создайте ключ, перезапустите OnlineConf
с тем же подключением к БД и прочитайте карточку. Identity должен разрешать
сертификату OnlineConf и проверку роли, и обращения к каталогу пользователей.

Для продуктового чтения OnlineConf использует общий снимок в Redis.
Все экземпляры OnlineConf должны получать одинаковые `REDIS_HOST`,
`REDIS_PORT`, `REDIS_PASSWORD` и `REDIS_DB`. При запуске вне Compose задайте
доступный с хоста адрес Redis. Фоновый worker перечитывает PostgreSQL при
старте, после создания, изменения или удаления ключа и каждые 15 секунд.
Ответ об успешной административной записи опирается на COMMIT PostgreSQL.
Если Redis недоступен после записи, очередная сверка подхватит изменение.
Снимок доступен для продуктового чтения не дольше 60 секунд с последней
успешной проверки PostgreSQL.

Для проверки кеша запустите `go test -race ./internal/app/onlineconf -count=1`
с `SERVICE_TEST_DATABASE_URL` и `REDIS_TEST_ADDR`, указывающими на одноразовые
PostgreSQL и Redis.

OnlineConf вычисляет состояние флага для ID пользователя по сохранённой записи.
Выключенный флаг всегда даёт `false`. Для включённого флага сначала применяется
персональное исключение, затем персональное включение. Для остальных ID флаг
включён, если bucket меньше `rollout_percent`. Bucket — остаток от деления
на 100 первых восьми байт SHA-256, прочитанных как беззнаковое число в порядке
big-endian. На вход хеша подаются полное имя флага, байт `0x00` и десятичный ID.
При увеличении процента уже включённые пользователи сохраняют доступ.
Проверки правил без БД и HTTP запускаются командой
`go test ./internal/app/onlineconf -run 'TestFlag'`.

Назначение роли `onlineconf_admin` выполняет оператор БД Identity. В `psql`
установите переменную `user_id` в ID нужного пользователя и выполните:

```sql
INSERT INTO identity.onlineconf_admins (user_id)
VALUES (:'user_id') ON CONFLICT DO NOTHING;
```

Для отзыва выполните
`DELETE FROM identity.onlineconf_admins WHERE user_id = :'user_id';`.
Изменение действует на следующий запрос с тем же токеном.

## Логи

- Go пишет access log и добавляет `request_id` из контекста. Вызовы Go к агенту
  передают `X-Request-ID`. Агент проверяет заголовок или генерирует собственный ID.
- `LOG_LEVEL=debug` включает сводки `storage.operation` в Go. PostgreSQL,
  Redis и S3 инструментированы на границе драйвера. Агент пишет access log и SQLite-сводки в JSON. Для SQLite задайте
  `LOG_LEVEL=debug` в окружении агента. Поля содержат тип хранилища, операцию, длительность и исход.
- Логи не включают SQL, аргументы, токены, ключи объектов и тела запросов.
  Для фоновых операций используйте существующие идентификаторы сессии или lease,
  если HTTP-контекст уже завершён.

## Harness AI-агента

Запускайте клиент в корне checkout с `go.mod`. Используйте gopls MCP для Go
и Serena для `runtime-agent/`. Проверяйте callers перед изменением портов,
а после правок запускайте диагностику и профильные тесты. Для Pyright
используйте интерпретатор из `runtime-agent/.venv`.

## Генерация тестовых моков

`bash scripts/generate-mocks.sh` вызывает закреплённую в `go.mod` версию mockgen.
После изменения портов повторите генерацию и проверьте воспроизводимость diff.

## Redis для авторизации

[Сессии, ключи и восстановление Redis](redis-sessions.md).

После проверки лимита попыток вход с неизвестным логином выполняет
bcrypt-сравнение с фиктивным хешем той же стоимости, что и новые хеши паролей.
Для неизвестного логина и неверного пароля API возвращает одинаковую ошибку.

## S3 для аватаров и приватных файлов

Локально `s3-init` настраивает бакет и публичное чтение аватаров. Go использует
`S3_AVATARS_BUCKET`, `S3_AVATARS_PUBLIC_URL` и настройки `AWS_*`.
Production S3/CDN настраивает infra. [Загрузка аватара](user-profile.md#загрузка-аватара).

SeaweedFS также создаёт приватный бакет `S3_PRIVATE_FILES_BUCKET`.
У него должно быть отдельное имя. Для проверки с настройками Compose по умолчанию
из корня backend запустите:

```sh
docker compose up -d s3 s3-init
docker compose wait s3-init
AWS_REGION=us-east-1 AWS_ACCESS_KEY_ID=devkey AWS_SECRET_ACCESS_KEY=devsecret \
  AWS_ENDPOINT_URL_S3=http://127.0.0.1:8333 \
  S3_PRIVATE_FILES_BUCKET=cellestial-private-files-local S3_PRIVATE_FILES_SMOKE=1 \
  go test ./internal/storage/s3client -run '^TestPrivateFilesS3$' -count=1 -v
```

Подставьте значения своего Compose, если меняли порт, бакет или ключи.
Проверка загружает JPEG и текстовый файл через подписанный PUT, сверяет размер
через HEAD и скачивает оригиналы через подписанный GET. Ссылки действуют две минуты.
Тест проверяет заголовок `Content-Disposition: attachment`, отказ 403 для
анонимного GET и отдельное JPEG-превью с длинной стороной 512 пикселей.
Тест печатает ключи объектов под `smoke/private-files/`. Их можно убрать ключом
оператора.

После выкладки backend и правил CSP/CORS из [infra#195](https://github.com/Cringe-Driven-Development-Team/infra/pull/195)
проверьте PUT со страницы `https://cellestial.ru` по свежей ссылке, выданной Go.
Отправляйте запрос с `credentials: "omit"` и `Content-Type: application/octet-stream`.
В инструментах браузера проверьте, что preflight разрешает PUT и этот заголовок,
загрузка возвращает 2xx, а origin ссылки — `https://cellestial-private-files-0.s3.ru-7.storage.selcloud.ru`.
Для отчёта запишите origin и статусы ответов.

## Агент исполнения Python

`make agent-setup agent-lint agent-test agent-build` проверяет код и собирает пакет.
`make agent-images agent-integration` требует Docker и проверяет запуск kernel.
`make agent-test-jupyter` проверяет транспорт Jupyter отдельно.
[Сессии и исполнение](runtime-sessions.md) · [Подготовка VPS](runtime-provisioning.md).

## Проверки перед PR

- `make check` проверяет release-скрипты, форматирование, vet, зависимости,
  сборку, race-тесты, линтер и OpenAPI. `make docker-build` проверяет образ API.
- `make test` формирует `coverage.out`. Порог 60% сравнивается до округления.
  Сгенерированные Go-файлы, `cmd/main` и `migrations` исключены из метрики.
  Отчёт можно открыть через `go tool cover -func=coverage.out`.
- CI предоставляет PostgreSQL и Redis. Для локальных интеграционных тестов
  задайте `TEST_POSTGRES_DSN`, `RUNTIME_TEST_DATABASE_URL`, `REDIS_TEST_ADDR`
  на выделенных тестовых сервисах. Сценарий восстановления Redis также требует
  `REDIS_RECOVERY_TESTS=1` и доступный `redis-cli`.
- Для изменений агента выполните его проверки и приложите их результаты.

## Как работать с задачами

Рабочая ветка — `api-N` в репозитории backend. Правила ревью, задачи и доска
описаны в [CONTRIBUTING команды](https://github.com/Cringe-Driven-Development-Team/.github/blob/main/CONTRIBUTING.md).
Правила именования и открытия PR указаны в [AGENTS.md](../AGENTS.md#git-и-задачи).

## Типы коммитов

`feat` добавляет функциональность, `fix` исправляет поведение, `refactor`
упрощает реализацию, `style` меняет форматирование, `test` добавляет проверки,
`docs` меняет документацию, `chore` меняет сборку, конфигурацию или зависимости.

## Код и документация

Сверяйте руководство с кодом и контрактом в том же изменении. Общие границы
компонентов ведите в командном docs. Локальные команды и runbooks ведите здесь.

## Файлы runtime

Для файлового интерфейса нужен дисковый WORKSPACE_PATH из infra #161. Подготовка
каталогов, контракт и дополнительные проверки описаны в
[руководстве workspace](workspace-files.md).
