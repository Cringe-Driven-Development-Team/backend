# CI, публикация и деплой backend

Текущую архитектуру продукта описывает [документация команды](https://docs.cellestial.ru/project-context). Здесь — только правила сборки и выпуска backend. Исходники автоматизации: [ci.yml](../.github/workflows/ci.yml), [cd.yml](../.github/workflows/cd.yml) и [infra/ansible](https://github.com/Cringe-Driven-Development-Team/infra/tree/main/ansible).

## Проверки и образ

На каждом PR запускаются `lint` (golangci-lint), `test` (`go mod tidy -diff`, сборка и тесты с race detector) и `docker` (сборка образа без публикации). Покрытие выводится в summary; минимального порога нет. Новый push в PR отменяет предыдущие проверки этого PR. Запуски `main` не отменяют друг друга; выкатки сериализуются отдельно.

После успешных проверок push в `main` публикует приватный образ `ghcr.io/cringe-driven-development-team/backend` с тегами `sha-<short>` и `main`. Тег `latest` не публикуется. SHA-тег может быть перезаписан повторным запуском для того же коммита; для строго неизменяемой ссылки используйте digest.

CI публикует образ через `GITHUB_TOKEN`. Job выкатки получает `packages: read` и передаёт свой временный `GITHUB_TOKEN` в Ansible через окружение. Постоянный PAT для production не нужен. При 403 передайте ментору ссылку на запуск для проверки прав package и организации; личные токены для обхода не используйте.

## Учебная копия

Рабочие PR, задачи и ревью находятся в [командном backend](https://github.com/Cringe-Driven-Development-Team/backend). [Учебный репозиторий](https://github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development) получает те же коммиты `main`; Actions и новые PR там выключены.

После `lint`/`test`/`docker` синхронизация запускается при `COURSE_SYNC_ENABLED=true` независимо от деплоя. Для повтора без деплоя используйте Actions → Sync to course → `main` и передайте `ci_run_id` успешного push/main CI. Подробный порядок восстановления — в [runbook организации](https://github.com/Cringe-Driven-Development-Team/.github/blob/main/docs/migrations/team-repositories-runbook.md).

## Production

После успешных проверок `ci.yml` вызывает `cd.yml` через `workflow_call` и передаёт SHA-тег из job `docker`. Выкатка запускается при `PRODUCTION_ENABLED=true` из `main` в environment `production`. CD запускает `deploy-backend.yml` из `infra/main`; production Compose, Caddyfile, Vault и серверная конфигурация принадлежат репозиторию [infra](https://github.com/Cringe-Driven-Development-Team/infra). Местный `docker-compose.yml` служит только для разработки.

`DEPLOY_SSH_KEY`, `SSH_KNOWN_HOSTS` и `ANSIBLE_VAULT_PASSWORD` хранятся как secrets environment. Для командного CI установлен отдельный public key `infra/ansible/files/authorized_keys/ci-team-backend.pub`; личные SSH-ключи и Selectel credentials в CI не передаются. Ansible временно авторизуется в GHCR, скачивает образ и всегда выполняет logout.

Playbook ждёт `/health` внутри Compose и проверяет через HTTPS `/api/v1/users/me`: без токена ожидается JSON с HTTP 401. Миграции goose выполняются API при старте; отдельного шага миграций нет. Ошибка Telegram показывается предупреждением и не подменяет результат деплоя.

Параметры S3 роль `app` передаёт из infra vars и Vault. API использует `S3_NOTEBOOKS_BUCKET`, `AWS_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` и `AWS_ENDPOINT_URL_S3`. Локально используется SeaweedFS, в production — Selectel.

Откат выполняется из infra прежним опубликованным SHA-тегом по [инструкции Ansible](https://github.com/Cringe-Driven-Development-Team/infra/blob/main/ansible/README.md). Миграции Down автоматически не запускаются: старый бинарник должен быть совместим с уже применённой схемой. Перед откатом сверяйте digest образа с исходным запуском публикации.

## Выпуск перехода на Redis

Job `test` поднимает настоящий Redis 8 и проверяет TTL, атомарную ротацию,
отзыв, гонки, потерю ответа и восстановление данных. Изолированный restart-тест
проверяет AOF. Перед первым выпуском остановите старый API и следуйте
[runbook](redis-sessions.md): старые refresh-сессии очищаются, новый JWT требует
sid/версии. Откат на stateless API требует смены JWT-ключа и удаления старого
verification key; одного очищения refresh недостаточно.
