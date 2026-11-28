# Выпуск, выкатка и откат backend

Опубликованный immutable GitHub Release означает «выпуск подготовлен».
Успешный production Deployment означает «выпуск выкачен и прошёл проверки».
CI и CD имеют отдельные Actions runs; успешный CI не подтверждает состояние VPS.
Production Compose/Vault/SSH принадлежат infra, доставка уведомлений —
`.github/services/webhook-alerts` (.github#59). Прямой отправки Telegram из backend нет.

## Подготовка репозитория

В Settings → General → Releases включить **Enable release immutability** до первого
выпуска. Администратор может выполнить:

```sh
gh api -X PUT repos/Cringe-Driven-Development-Team/backend/immutable-releases
gh api repos/Cringe-Driven-Development-Team/backend/immutable-releases
```

Для этих endpoints нужны Administration write/read; `GITHUB_TOKEN` CI их не имеет.
CI проверяет `immutable: true` у опубликованного выпуска; при отсутствии этого
признака не запускает CD. Уже опубликованный mutable Release автоматически не
заменяется. Нельзя считать локальные тесты подтверждением настройки репозитория.

GHCR package `ghcr.io/cringe-driven-development-team/backend` должен быть Public
и привязан к backend. CI проверяет настройки package и анонимный pull точного
`linux/amd64` digest с пустым Docker config. Публикация требует `packages: write`,
Release — `contents: write`; CD/Ansible GHCR credentials не получают.

## Автоматическая подготовка выпуска

CI классифицирует исходники по дереву Git проверяемого SHA. Полный набор
`cmd/identity/main.go`, `cmd/workspace/main.go`, `cmd/environments/main.go`
выбирает выпуск микросервисов. Неполный набор точек входа блокирует выпуск.

### Микросервисы и manifest v2

Job Service checks запускается из CI для PR, push в `main` и ручной проверки.
Матрица собирает три образа из одного SHA, проверяет их упаковку и OCI labels.
Отдельный job проверяет воспроизводимость OpenAPI и Protobuf и прогоняет HTTP/gRPC
smoke с тестовым mTLS, PostgreSQL и Redis.

Каждый образ содержит запускаемый сервис и `migrate`. Образ Identity также
содержит `identity-admin`. В Environments входят provisioning tools,
wheel агента с checksum и оба systemd unit-файла. Локально на исходниках
трёх сервисов упаковку можно проверить так:

```bash
for service in identity workspace environments; do
  docker build -f build/services.Dockerfile --target "$service" -t "cellestial-$service:local" .
  bash scripts/check-service-image.sh "$service" "cellestial-$service:local"
done
```

После trusted push в `main` публикация v2 ждёт Service checks, lint, unit/integration
tests, контракты и проверку реального Python kernel. Для каждого сервиса
используется отдельный GHCR package `cellestial-identity`, `cellestial-workspace`
или `cellestial-environments`. При первом выпуске GHCR может создать их как
закрытые. В настройках каждого package привяжите его к репозиторию backend,
разрешите workflow запись и установите Public, затем повторите CI.
Путь выпуска проверяет анонимный pull по digest с пустым Docker config.

Тег выпуска остаётся `release-<полный backend SHA>`. Для образов создаются теги
`sha-<полный backend SHA>`. При повторном CI доступный тег используется как есть,
а образ проверяется по digest, исходному SHA и OCI labels. Версии runtime-agent и
kernel проверяются по конфигурации образа Environments. Идентичность протокола
строится из дерева файлов Protobuf и серверных контрактов OpenAPI.
Релиз v2 содержит:

- `release.json` с `format_version: 2`, `source_sha`, `ci_url`,
  `protocol_revision` и картой `services`. Для каждого сервиса указаны digest-ref,
  исходный SHA, платформа, image ID, версия схемы, имя, размер и SHA-256 архива.
  Environments дополнительно фиксирует версию агента и kernel digest-ref.
- `identity-image.tar.gz`, `workspace-image.tar.gz` и `environments-image.tar.gz`.
  Перед загрузкой каждого архива CI сверяет image ID после `docker save` и
  `docker load`.
- `SHA256SUMS` для трёх архивов и точных байтов manifest.

CI сериализует выпуск одного SHA. Draft получает manifest первым. Повтор
скачивает загруженные файлы и добавляет только отсутствующие. Восстановленный
архив должен совпасть с метаданными draft. Перед публикацией все файлы
скачиваются обратно и проверяются, затем CI подтверждает immutability выпуска.
Для первого переключения полный выпуск v2 явно выбирает оператор в CD.

Для проверки и загрузки сохранённых образов:

```sh
gh release download <tag> -R Cringe-Driven-Development-Team/backend
sha256sum -c SHA256SUMS
for service in identity workspace environments; do
  docker load -i "$service-image.tar.gz"
done
```

Храните immutable release assets и digest из текущего production inventory,
подтверждённых целей отката и работающих runtime-VPS. В эту проверку входят
версии агентского артефакта и Python kernel. Перед очисткой кандидатов сверяйте
ссылки по manifest, истории Deployment и инвентарю runtime-VPS.

### Выпуск монолита до переключения

Production Release `release-<полный 40-hex SHA>` для монолита публикуется
после push в `main` и успешных обязательных проверок. PR и ручной CI
пропускают публикацию backend production-выпуска.
Образ имеет теги `sha-<SHA>` и `main`, без `latest`; источник выкатки — digest из manifest.
Архив получается через pull опубликованного digest → docker save → gzip, без второй сборки.
Перед публикацией архив загружается через docker load с проверкой image ID.

Release сначала создаётся draft. После загрузки и повторной проверки файлов
публикуется immutable Release с generated changelog, PR/compare ссылками, SHA,
ссылкой на исходный CI run и командой скачивания по digest. Файлы:

- `backend-image.tar.gz`: Docker-образ для самостоятельной загрузки;
- `release.json`: `format_version: 1`, `source_sha`, `image_ref`, `ci_url`,
  `build.platform`, `build.image_id`, `archive.name`, `archive.size`, `archive.sha256`;
- `SHA256SUMS`: SHA256 архива и точных байтов `release.json`.

Автоматические GitHub source archives не являются сборочными артефактами.
Скачивание файлов: Release → Assets либо `gh release download <tag> -R
Cringe-Driven-Development-Team/backend`. Затем:

```sh
sha256sum -c SHA256SUMS
docker load -i backend-image.tar.gz
```

При повторе CI опубликованный выпуск полностью проверяется и используется без
пересборки backend или изменения файлов. Публикации одного SHA сериализованы.
Если загрузка оборвалась, draft с `release.json` фиксирует digest: повтор берёт
именно его, восстанавливает архив и проверяет соответствие checkpoint перед
добавлением отсутствующих файлов. Несогласованные файлы не перезаписываются.
Если checkpoint ещё не загружен и опубликованного выпуска нет, повтор может собрать
образ заново; завершённый Release никогда не пересобирается.

## Самостоятельный CD и повторное применение infra

После публикации выпуска монолита CI вызывает `workflow_dispatch` workflow `CD` на `main`, передавая
конкретный `release_tag`. Вызывающая job имеет только `contents: read`, `actions: write`.
На `release: published` автоматизация не полагается: публикация через `GITHUB_TOKEN`
не запускает следующий workflow. Ошибка dispatch — отдельная ошибка CI job;
Release остаётся готовым, его можно выкатить вручную без пересборки.

Ручной путь: Actions → **CD** → **Run workflow** → branch `main` → обязательный
`release_tag`, например `release-0123456789012345678901234567890123456789`.
У CD собственный run, история и Summary. Разрешены текущий подтверждённый выпуск
и новый выпуск, происходящий от текущего. Старую версию выбирают через Rollback.
Внутри общей production concurrency-секции CD заново читает Deployments: запоздавший
запуск предка не откатывает более новую успешную выкатку. Расходящаяся история
и неизвестное последнее состояние отклоняются, требуют сверки истории и состояния.

До доступа к SSH проверяются публикация/immutability, точный tag и его commit,
manifest, контрольные суммы скачанных файлов, разрешённый backend package/digest
и анонимный pull. Draft, отсутствующий выпуск, несогласованные SHA/digest/checksum
отклоняются с причиной в логе. Dispatch HEAD не определяет образ.

Каждый CD получает актуальную `infra/main`, пишет её SHA в Summary и передаёт
`backend_image_ref=ghcr.io/cringe-driven-development-team/backend@sha256:<64 hex>`
в `infra/ansible/deploy-backend.yml`. Штатная выкатка скачивает образ из GHCR;
архив на VPS не загружается. Повторный CD текущего Release снова запускает playbook,
применяя свежие переменные, включая `runtime_extra_env`. Infra шаблонизирует их
в `.env` и Compose, а Compose пересоздаёт API при изменении окружения.
Совпадение digest не является условием пропуска playbook.

### CD и Rollback микросервисов

После появления микросервисов в `main` CD принимает manifest v2 и параметр
`services`. Значение `all` выбирает полный комплект. Для выборочной
выкладки укажи имена через запятую, например `identity,workspace`.
Соседние сервисы остаются на наблюдаемых digest. Выборочный выпуск проходит
preflight при совпадении ревизии HTTP/gRPC контрактов с соседями. Если ревизия
поменялась, требуется полный набор.

Первое переключение запускают через CD с `first_cutover=true` и
`services=all` после подготовки резервной копии и проверки runtime-VPS
по инфраструктурному runbook. Откат первой миграции возвращает данные и
запуск монолита по этому runbook. Штатный Rollback выбирает более ранний
подтверждённый набор или версию выбранных сервисов. Если требуется понизить
версию схемы, восстановление выполняется по runbook.

Для согласованного первого запуска без старых записей выберите
`services=all` и `clean_start=true`, оставив `first_cutover=false`.
Используйте выпуск с поддержкой миграций от владельца заранее созданной схемы.
Ansible создаст три схемы в отдельной PostgreSQL базе на VPS2 и использует
отдельный индекс Redis. Для запуска требуются подтверждённый `cutover.json`
и проверенные копии PostgreSQL и Redis. После проверки первого запуска
повторите полный CD с обоими выключенными флагами, чтобы включить воркеры.
Подготовка и возврат описаны в [инфраструктурном runbook](https://github.com/Cringe-Driven-Development-Team/infra/blob/main/RUNBOOK.md#первый-запуск-с-пустыми-таблицами).

Для CD и Rollback действует одна очередь `backend-production`. Перед
переключением проверяются доступность выбранных digest, их OCI provenance и
соответствие набора фактическому состоянию VPS. Каждая попытка сохраняет
`deployment-result.json` в отдельный GitHub artifact и после его загрузки
обновляет Deployment. В Summary указаны выбранные и наблюдавшиеся версии,
проверки каждого сервиса и ссылки на сравнение SHA. Потеря наблюдения или
незавершённая попытка требуют сверки с VPS перед следующей операцией.

### Как найти текущий подтверждённый Release

Открыть backend → Environments → production → последний **успешный Deployment**.
Перейти по его log URL в CD/Rollback run и прочитать Summary: target SHA,
`release-<SHA>`, image digest и infra SHA. Этот tag указать в CD для повторной выкатки.
Последний по дате опубликованный Release и текущий HEAD не подтверждают выкатку.
История GitHub также не утверждает текущее состояние VPS после потери доступа.

## Rollback и обязательные проверки

Actions → **Rollback** → **Run workflow** на `main`. Необязательное поле `sha` —
полный SHA ранее успешно выкаченной версии. Без поля: после успешного последнего
запуска выбирается предыдущий успешный другой digest; после failure/error — последний
успешный. Неизвестное или незавершённое последнее состояние запрещает автоматический
выбор; явная цель тоже требует успешного подтверждения и однозначного digest.

Цель должна иметь опубликованный immutable Release с тем же digest. Старые записи
CI #102 остаются свидетельствами истории, но не заменяют отсутствующий Release.
Импорт старых выпусков не выполняется. Откат не пересобирает образ и не откатывает БД.
CD, повторная выкатка и Rollback используют одну группу `backend-production`,
не отменяют выполняющуюся операцию; выбор цели происходит внутри этой группы.

Сохраняются `PRODUCTION_ENABLED=true`, environment `production`, доверенные workflow
на `main`, SSH host-key verification и удаление временных SSH/Vault файлов.
Deployment получает `in_progress`. Для успешной выкладки и отката Ansible
должен завершиться с кодом 0. Для каждого выбранного сервиса должны совпасть
запрошенный и наблюдаемый digest, а проверки `image` и `ready` должны пройти.
Для монолита локальная готовность берётся из Docker health запущенного контейнера.
Summary показывает образ и готовность. История допускает дополнительное поле
`smoke` в старых артефактах. Ошибка регистрации статуса видна отдельно.

Для работы CD при недоступном BFF требуется ревизия infra с контрактом
[infra#215](https://github.com/Cringe-Driven-Development-Team/infra/issues/215)
для обоих playbook. При интеграции закрепи её полный SHA в `cd.yml`.

## Webhook-контракт и интеграционная проверка

Payload сохраняет `action`, `image_ref`, `run`, `notification.run_attempt`,
`notification.base_sha`, `notification.report_url`; добавлены `release_tag`, `infra_sha`.
CD: workflow `cd.yml`, event `workflow_dispatch`, job `deploy`; Rollback сохраняет
`rollback.yml`, `workflow_dispatch`, job `rollback / deploy`. Имена шагов
`Switch backend` и `Health check` сохраняются. Target SHA независим от dispatch HEAD.

Распознавание самостоятельного CD принадлежит
[consumer webhook](https://github.com/Cringe-Driven-Development-Team/.github/tree/main/services/webhook-alerts)
(.github#59). Consumer должен разрешать источник `cd.yml` и job `deploy`, сохраняя
проверки ветки, репозитория, workflow ID, попытки и доказательств выполнения.
Общий fixture `backend-cd-contract.json` проверяет CD с HEAD, отличным от target.
Для интеграционной приёмки нужен выкаченный consumer с этой поддержкой.
Telegram sender не меняется.

Локально: `make release-check`, `actionlint .github/workflows/{ci,cd,rollback}.yml`,
`make check`, `make docker-build`. Consumer проверяется `bun test` в
`services/webhook-alerts` без сетевой отправки уведомлений. На интеграционной приёмке
приложить ссылки на настоящий CI run, Release и отдельный CD run; проверить повтор
текущего Release с изменением infra при том же digest и всю цепочку Deployment → webhook.
Пробный production Rollback ради локальной приёмки не требуется.

Course-sync независим от CD, управляется `COURSE_SYNC_ENABLED=true` и повторяется
по `ci_run_id` без повторной выкатки.
