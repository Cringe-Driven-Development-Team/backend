---
type: guide
title: "Профиль, пароль и аватары"
description: "Изменение логина и пароля, поиск пользователей и публикация аватара через S3 и PostgreSQL."
tags: [users, profiles, avatar, s3]
verified:
  - by: openwiki/0.7.2
    at: 2026-11-20T18:28:51.294Z
sources:
  - id: openwiki-source-7f2f2eb724b9a17ee724de12
    resource: repo://internal/transport/http/middleware/avatar.go
  - id: openwiki-source-406983d5775b589a9813a316
    resource: repo://internal/user/adapters/avatar/codec.go
  - id: openwiki-source-b78e8a8dd68086e4702b2406
    resource: repo://internal/user/adapters/postgres/suggest.go
  - id: openwiki-source-7058e75a6d40ff7caa7c3db3
    resource: repo://internal/user/adapters/postgres/user.go
  - id: openwiki-source-a0aad76558c10a7db97b7b49
    resource: repo://internal/user/adapters/redis/limiter.go
  - id: openwiki-source-a612e519a1014bdb2d48da8a
    resource: repo://internal/user/adapters/s3/storage.go
  - id: openwiki-source-a30a6c6605ee1a9b1895e42e
    resource: repo://internal/user/delivery/http/profile.go
  - id: openwiki-source-7a3958146fc859019788b560
    resource: repo://internal/user/delivery/http/suggest.go
  - id: openwiki-source-ac561a30179aedee23f77bf6
    resource: repo://internal/user/usecase/avatar.go
  - id: openwiki-source-6efcd93307a3ac095092d4c5
    resource: repo://internal/user/usecase/profile.go
  - id: openwiki-source-7e9ef7bb1e139bd977d92279
    resource: repo://internal/user/usecase/suggest.go
generated: { by: "codex", at: "2026-11-20T18:28:51.294Z" }
---

# Профиль, пароль и аватары

## Профиль и поиск пользователей

Delivery использует ID из principal. Логин содержит от 3 до 32 латинских букв, цифр или символов подчёркивания. Конфликт уникальности преобразуется в HTTP 409. Для аватара delivery формирует ссылку из `S3_AVATARS_PUBLIC_URL` и `avatar_key`.

`SuggestUsers` требует входа, ищет по буквальному префиксу логина без учёта регистра и исключает запрашивающего пользователя. Ответ содержит ID и логины. Redis ограничивает поиск 60 запросами за минутное окно на пользователя. Превышение даёт 429.

## Пароль и отзыв сессий

При смене пароля проверяются текущий пароль и отличие от нового. Правило допускает от 8 до 72 печатных ASCII-символов без пробела. PostgreSQL обновляет хеш только при совпадении со старым хешем, после чего usecase вызывает RevokeAll в Redis. Ошибка отзыва возвращается после записи пароля и его не откатывает. Порядок выдачи сессии при конкурентной смене пароля описан на странице [доступа к API](auth-sessions.md).

## Проверка и публикация аватара

На PUT аватара middleware требует principal и multipart/form-data. На один роутер допускается одна загрузка одновременно, предел HTTP-тела составляет 6 МиБ. Delivery принимает один файл в поле `avatar` размером до 5 МиБ и проверяет отсутствие дополнительных частей и превышения лимита в эпилоге. Загруженный файл временный и удаляется по выходе из обработчика.

Codec проверяет соответствие заявленного JPEG или PNG сигнатуре и диапазон 64–4096 пикселей по каждой стороне до полного декодирования. Перекодирование удаляет метаданные, прозрачность PNG сводится на белый фон. Delivery возвращает 413 при превышении размера, 415 для неподдерживаемого MIME и 400 для недопустимого изображения.

Usecase загружает очищенный файл в S3 под новым UUID-ключом. PostgreSQL меняет `avatar_key` под блокировкой строки и возвращает прежний ключ после commit. Только тогда удаляется прежний объект. Ошибка удаления записывается в журнал и не отменяет обновление профиля. При ошибке commit новый объект сохраняется, поскольку ссылка могла уже измениться. S3 Put и Delete ограничены пятью секундами и одной попыткой.

`internal/user/usecase/profile_test.go` проверяет смену пароля и отказ Redis, `internal/user/adapters/avatar/codec_test.go` — формат и размеры, а `internal/user/adapters/postgres/suggest_integration_test.go` — префиксный поиск.

[Блокноты и права доступа](notebooks.md) · [Карта Go API](../quickstart.md)

## Источники

- [internal/user/usecase/profile.go#L12-L47](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/usecase/profile.go#L12-L47)
- [internal/user/adapters/postgres/user.go#L110-L120](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/adapters/postgres/user.go#L110-L120)
- [internal/transport/http/middleware/avatar.go#L23-L56](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/transport/http/middleware/avatar.go#L23-L56)
- [internal/user/delivery/http/profile.go#L80-L136](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/delivery/http/profile.go#L80-L136)
- [internal/user/adapters/avatar/codec.go#L47-L95](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/adapters/avatar/codec.go#L47-L95)
- [internal/user/adapters/avatar/codec.go#L106-L128](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/adapters/avatar/codec.go#L106-L128)
- [internal/user/adapters/postgres/user.go#L122-L152](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/adapters/postgres/user.go#L122-L152)
- [internal/user/usecase/avatar.go#L54-L83](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/usecase/avatar.go#L54-L83)
- [internal/user/delivery/http/suggest.go#L12-L39](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/delivery/http/suggest.go#L12-L39)
- [internal/user/usecase/suggest.go#L13-L31](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/usecase/suggest.go#L13-L31)
- [internal/user/adapters/postgres/suggest.go#L10-L27](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/adapters/postgres/suggest.go#L10-L27)
- [internal/user/adapters/redis/limiter.go#L24-L42](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/adapters/redis/limiter.go#L24-L42)
- [internal/user/usecase/profile.go#L10-L21](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/usecase/profile.go#L10-L21)
- [internal/user/adapters/postgres/user.go#L94-L108](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/adapters/postgres/user.go#L94-L108)
- [internal/user/delivery/http/profile.go#L32-L55](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/delivery/http/profile.go#L32-L55)
- [internal/user/adapters/s3/storage.go#L14-L52](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/adapters/s3/storage.go#L14-L52)
- [internal/user/delivery/http/user.go#L148-L155](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/delivery/http/user.go#L148-L155)
- [internal/config/config.go#L88-L96](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/config/config.go#L88-L96)
- [internal/user/delivery/http/profile.go#L139-L152](https://github.com/Cringe-Driven-Development-Team/backend/blob/8dd2f9cf82b2389d723c308c3deb5cc5f1db0b55/internal/user/delivery/http/profile.go#L139-L152)
