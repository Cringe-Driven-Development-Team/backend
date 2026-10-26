# Авторизация через BFF

Go API обслуживает серверные запросы BFF. Для запуска обязательны `JWT_SECRET`,
`BFF_API_KEY` и `S3_NOTEBOOKS_BUCKET`. `BFF_API_KEY` должен содержать непустой
общий секрет Go и BFF; пустое значение или только пробелы останавливают запуск.

| Запрос | Успешный ответ |
| --- | --- |
| `POST /api/v1/auth/register`, JSON `Credentials` | `201 {user, tokens}` |
| `POST /api/v1/auth/login`, JSON `Credentials` | `200 {user, tokens}` |
| `POST /api/v1/auth/refresh`, JSON `{refresh_token}` | `200 TokenPair` |
| `POST /api/v1/auth/logout`, JSON `{refresh_token}`, Bearer | `204` без тела |

`TokenPair` содержит `access_token`, `refresh_token`, `access_expires_in` и
`refresh_expires_in`. Сроки — целые секунды оставшейся жизни токенов, округлённые
вверх. Access соответствует JWT `exp`, refresh — сроку сессии в хранилище.
`ACCESS_TOKEN_TTL` (15 минут по умолчанию) и `REFRESH_TOKEN_TTL` (30 дней)
должны быть не меньше секунды. Refresh заменяет токен; старый повторно не принимается.

Тела refresh/logout обязательны. Отсутствующее или пустое поле — `400 validation_error`,
невалидный refresh — `401 unauthorized`. Данные и logout требуют
`Authorization: Bearer <access_token>`. Register, login и refresh не требуют access.
API читает токены из JSON и Bearer, игнорирует браузерные cookie и не выдаёт
`Set-Cookie`. CORS и CSRF в Go API отсутствуют.

## Проверка BFF

Каждый запрос к `/api/v1` и его подпутям, включая register/login/refresh и OPTIONS,
требует ровно один `X-BFF-Key`, равный `BFF_API_KEY`. Проверка выполняется до Bearer,
разбора JSON и валидации параметров. SHA-256 ключей сравниваются постоянным временем.
Отсутствующий, неправильный или повторяющийся заголовок получает
`403 {"code":"s2s_forbidden","message":"service access forbidden"}`.
BFF должен обрабатывать этот ответ как инфраструктурную ошибку, сохраняя сессию.
Ключи, пароли и токены не записываются в логи.

Принимается единственный ключ. Для замены синхронно обновите общий секрет Go и BFF
и перезапустите сервисы; одновременный приём старого и нового ключей отсутствует.
Та же проверка обязательна на одной VPS.

`GET /health` не требует ключа и Bearer, чтобы контейнер мог проверять себя по
`http://127.0.0.1:8080/health`. Публичную доступность Go API и health ограничивает
сеть и firewall в infra; локальный Compose публикует порт для разработки.

Порядок выпуска и согласование BFF/infra описаны в backend#41. Проверка сохранения
сессии BFF при `403 s2s_forbidden` требует работающего BFF и выполняется совместно
с frontend#2. Миграций схемы БД для нового контракта нет.
