# Авторизация: BFF и совместимость


Режим выбирается при запуске через `API_AUTH_MODE`. По умолчанию `legacy`,
чтобы обычный deploy backend сохранял работу текущего браузерного клиента.
`bff` включает принятый серверный контракт Apidog. Режимы не смешиваются в одном
процессе; переключение не требует миграций БД.

## Серверный режим BFF

При `API_AUTH_MODE=bff`:

| Запрос | Успешный ответ |
| --- | --- |
| `POST /api/v1/auth/register`, JSON `Credentials` | `201 {user, tokens}` |
| `POST /api/v1/auth/login`, JSON `Credentials` | `200 {user, tokens}` |
| `POST /api/v1/auth/refresh`, JSON `{refresh_token}` | `200 TokenPair` |
| `POST /api/v1/auth/logout`, JSON `{refresh_token}`, Bearer | `204` без тела |

`TokenPair` содержит `access_token`, `refresh_token`, `access_expires_in` и
`refresh_expires_in`. Сроки — целые секунды оставшейся жизни выданных токенов,
округлённые вверх. Access соответствует подписанному JWT `exp`; refresh — сроку
сессии в хранилище. Refresh выполняет ротацию, старый токен становится недействителен.
Тела refresh/logout обязательны; отсутствующее или пустое поле — `400 validation_error`,
невалидный refresh — `401 unauthorized`.

Все ручки данных и logout требуют `Authorization: Bearer <access_token>`.
Access из cookie игнорируется, fallback на cookie отсутствует. Register, login и
refresh не требуют access. Ответы не устанавливают cookie; CORS и CSRF не
подключаются. `CSRF_SECRET`, `COOKIE_SECURE`, `APP_ORIGIN`, `CORS_ALLOWED_ORIGINS`
в BFF-режиме не используются. TTL обоих токенов должен быть не меньше секунды.

### Проверка сервиса и ротация

`BFF_S2S_REQUIRED=true` требует `API_AUTH_MODE=bff` и непустой `BFF_API_KEY`.
Каждый запрос к `/api/v1`, включая register/login/refresh и OPTIONS, должен иметь
ровно один `X-BFF-Key`. Проверка идёт до Bearer, разбора JSON и валидации параметров.
Отказ: `403 {"code":"s2s_forbidden","message":"service access forbidden"}`.
BFF должен трактовать его как инфраструктурную ошибку: без refresh и удаления сессии.
Ключи и токены не записываются в логи.

Go принимает `BFF_API_KEY` и, если задан, `BFF_API_KEY_PREVIOUS`. Для ротации:
сначала разрешить новый ключ как текущий, сохранив старый как предыдущий; затем
перевести BFF на новый; после проверки удалить предыдущий. Конфигурация применяется
при перезапуске. Предыдущий ключ без текущего запрещён.

На одной VPS допустимо `BFF_S2S_REQUIRED=false`: используется только Bearer,
а изоляцию Go обеспечивает сеть Compose. `GET /health` не требует ни ключа,
ни Bearer. Доступ извне к нему и API ограничивает infra.

### Переключение и откат

После merge backend остаётся в `legacy`. Согласованное включение BFF и Caddy,
ограничение сетевого доступа к Go и включение S2S выполняются с infra#58/infra#59.
Проверить health, login, данные, refresh и logout через BFF. Проверку того, что
ошибка S2S не очищает сессию BFF, выполнить совместно с frontend#2.
При откате маршрутизации вернуть `API_AUTH_MODE=legacy` и
`BFF_S2S_REQUIRED=false`, восстановив настройки cookie/CSRF/CORS. SQL Down не нужен.

## Временный браузерный режим legacy

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

Для запуска в `legacy` нужен отдельный от `JWT_SECRET` обязательный `CSRF_SECRET`.

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

## CORS и origin

CORS для запросовс `credentials: "include"` настраивает Go API через
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
