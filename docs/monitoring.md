# Метрики backend для Prometheus

Контракт [backend#119](https://github.com/Cringe-Driven-Development-Team/backend/issues/119)
используется после перехода на [сервисы #123](microservices.md). Конфигурация
VPS4, Grafana, OpenStack discovery и exporters принадлежит
[infra#164](https://github.com/Cringe-Driven-Development-Team/infra/issues/164).

## Соответствие техническим требованиям

- **Метрики запросов во всех сервисах.** Identity и Workspace публикуют
  количество запросов, коды ответа и гистограммы длительности по шаблону URL
  и HTTP-методу. Environments измеряет прикладные gRPC-вызовы по сервису,
  RPC-методу и коду завершения. Runtime-agent публикует метрики gRPC и
  административного HTTP. Каждый процесс опрашивается отдельно. Для внешнего
  трафика предусмотрены цели BFF и Caddy, для вспомогательных сервисов —
  их собственные endpoints.
- **Ресурсы каждой машины.** Источник метрик CPU, памяти и файловых систем —
  `node_exporter` на VPS1–VPS4 и каждой живой runtime-VPS, включая свободный
  пул. Runtime-agent отдельно публикует потребление и лимиты контейнера
  kernel, занятый объём и квоту workspace.
- **Единый дашборд.** Дашборд `Cellestial — Services & Infrastructure`
  предназначен для совместного просмотра метрик запросов и ресурсов машин. HTTP-панели используют разрезы по маршруту, методу и коду ответа,
  gRPC-панели — по сервису, RPC-методу и коду завершения. Панели ресурсов
  группируются по машине. Provisioning дашборда и подключение источников
  описаны в [infra#164](https://github.com/Cringe-Driven-Development-Team/infra/issues/164).

## Цели сбора

| Цель | Адрес и назначение |
| --- | --- |
| Identity, Workspace, Environments | `GET /metrics` на отдельном приватном `ADMIN_ADDR` каждого процесса. В контейнере по умолчанию `:9091`, адреса/порты на VPS2 задаёт inventory. |
| Runtime-agent | `GET /metrics` по HTTPS с mTLS один раз на каждый живой runtime-VPS, включая свободный пул. |
| BFF | Приватная цель на каждый экземпляр BFF. Сбор и build info реализует владелец BFF. |
| Caddy | Собирать метрики reverse proxy отдельно для работающих экземпляров на VPS1 и VPS4. |
| `webhook-alerts`, `githubbot` | Отдельная приватная цель на каждый работающий экземпляр. Входы и схемы метрик согласовать с владельцами `.github`. |
| `node_exporter` | Приватный `:9100/metrics` на VPS1, VPS2, VPS3, VPS4 и каждой runtime-VPS. |

Scrape добавляет `environment`, `service` и `instance` на уровне target.
Используйте отдельный стабильный идентификатор для каждого постоянного
экземпляра. Для runtime-agent и его node_exporter задавайте `instance`
равным OpenStack server UUID, приватный IP используйте в `__address__`.
Фильтр облачного inventory использует `cellestial_runtime=1` и
`cellestial_env=<environment>`. Инфраструктура задаёт `host_role`, источник
scrape и классификацию внешних HTTP-входов. Не публикуйте metadata владельца VPS
и значения идентификаторов сессии. `VERSION` задаётся при выкладке и публикуется
в `identity_build_info`, `workspace_build_info`, `environments_build_info`
и `runtime_agent_build_info`.

### HTTPS runtime-agent

Агент включает отдельный scrape listener при наличии всех четырёх настроек.

| Настройка | Назначение |
| --- | --- |
| `METRICS_ADDR` | Приватный bind `host:port`, отличный от `ADMIN_ADDR` и `GRPC_ADDR`. |
| `METRICS_TLS_CERT_FILE` | Сертификат HTTPS-входа с ServerAuth и SAN, проверяемым Prometheus. |
| `METRICS_TLS_KEY_FILE` | Приватный ключ HTTPS-входа, доступный пользователю агента. |
| `METRICS_TLS_CA_FILE` | Отдельный CA для клиентского сертификата Prometheus с ClientAuth. |

При установке systemd unit настройки можно передать в
`/etc/cellestial-agent/monitoring.env`. Prometheus получает собственные
сертификат и ключ, а также CA серверного сертификата. При discovery по IP
согласуйте серверный SAN и проверяемое имя. Не отключайте проверку peer.
На прежнем admin listener агента доступны `/livez` и `/readyz`. Управляющий
gRPC проверяет независимый CA и service identity. На сетевой границе
разрешайте scrape только с VPS4. Метрики, Docker socket и сертификаты
должны быть недоступны пользовательскому kernel.

## Формат метрик

| Семейство | Labels поверх scrape labels |
| --- | --- |
| `cellestial_http_requests_total`, `cellestial_http_request_duration_seconds`, `cellestial_http_stream_errors_total` | `method`, `route`, `status_code`. Ошибка после отправки заголовков учитывается в отдельном счётчике с фактическим статусом. |
| `cellestial_http_client_requests_total`, `cellestial_http_client_request_duration_seconds` | `dependency` (`s3`, `openstack`, `jupyter`, `onlineconf`), `operation`, `method`, `status_code`. `0` означает транспортную ошибку до получения ответа. |
| `cellestial_http_client_response_body_duration_seconds` | Те же HTTP client labels и `outcome`. Отдельное время от заголовков до EOF, ошибки чтения или закрытия тела. `closed` означает закрытие без наблюдавшегося EOF и само по себе не является ошибкой. Для Jupyter учитывается чтение JSON-ответов, WebSocket остаётся в метриках HTTP handshake. |
| `cellestial_grpc_calls_total`, `cellestial_grpc_call_duration_seconds`, `cellestial_grpc_stream_duration_seconds` | `grpc_service`, `grpc_method`, `grpc_code`, `rpc_type=unary\|stream`, `rpc_side=client\|server`. Код отмены `Canceled`, успех `OK`. Клиент считает логический вызов целиком, сервер учитывает каждый дошедший вызов. |
| `cellestial_grpc_active_streams`, `cellestial_grpc_stream_messages_total`, `cellestial_grpc_stream_message_bytes_total` | `grpc_service`, `grpc_method`, `rpc_type`, `rpc_side`. Счётчики сообщений и несжатых protobuf bytes также имеют `direction=sent\|received` относительно процесса. |
| `cellestial_runtime_target_info` | `server_id`, `state=creating\|warm\|active\|cleanup\|deleting\|failed`. Назначенную VPS показывает только запись окружения. Запись пула в состоянии `CLAIMED` исключена. |

Workspace измеряет чтение настроек OnlineConf с метками `dependency="onlineconf"`
и `operation="get_settings"`.

HTTP использует шаблон вроде `/api/v1/notebooks/{id}/content`; неизвестный
путь получает `unmatched`, нестандартный метод — `other`. Для gRPC
`grpc_service` и `grpc_method` соответствуют двум частям полного
`/package.Service/Method`. Request ID для корреляции находится в логах.
Незарегистрированные gRPC-методы учитываются только в серверном счётчике
как `grpc_service=unmatched`, `grpc_method=unmatched`, `rpc_type=unknown`
и `grpc_code=Unimplemented`. TLS-отказы до HTTP/gRPC dispatch видны
на уровне TLS, прокси и scrape.

Границы HTTP и unary в секундах одинаковы для Go и Python.
`0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600`.
Границы времени жизни stream в секундах.
`0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600, 1800, 3600, 7200`.

Для процессных панелей используйте `process_cpu_seconds_total`,
`process_resident_memory_bytes`, `process_open_fds`, `go_goroutines` и
`runtime_agent_event_loop_lag_seconds`. Общие process показатели Linux
публикуются и Go, и Python. Для агента доступны `runtime_agent_kernel_state`
и `runtime_agent_kernel_queue_length`.

Хостовый сбор отдаёт `runtime_agent_kernel_cpu_seconds_total`,
`runtime_agent_kernel_cpu_limit_cores`, `runtime_agent_kernel_memory_usage_bytes`,
`runtime_agent_kernel_memory_limit_bytes` и `runtime_agent_kernel_oom_kills_total`.
CPU и OOM накапливаются в процессе агента между заменами kernel. OOM
считывается из доступного cgroup v2, с fallback на Docker `OOMKilled`.
Workspace отдельно публикует `runtime_agent_workspace_used_bytes` и
`runtime_agent_workspace_quota_bytes` с `storage=tmpfs|disk`. Снимок
обновляется с паузой 5 секунд между вызовами Docker. Недоступные usage/limit
отсутствуют, причину отличают `runtime_agent_kernel_metrics_available` и
`runtime_agent_workspace_metrics_available`. Root disk измеряет node_exporter.

Время фонового сбора измеряет
`runtime_agent_resource_collection_duration_seconds` по источникам `docker`
(включая чтение cgroup) и `workspace`. Сбои увеличивают
`runtime_agent_resource_collection_errors_total`. Возраст снимка вычисляется
как `time() - runtime_agent_resource_collection_last_success_timestamp_seconds`.
До первого успешного замера timestamp отсутствует. Ожидание создания
контейнера учитывается во времени опроса и само по себе не считается сбоем.

## Агрегация и проверка

Для hits используйте `sum(increase(counter[<диапазон>]))`, для RPS
`sum(rate(counter[<окно>]))`. Разрез 4xx и 5xx строится по
`status_code=~"4.."` и `status_code=~"5.."`, доли — относительно тех же
фильтров и окна без фильтра статуса. Для gRPC показывайте каждый `grpc_code`
в разрезе `rpc_side` и отдельно длительность жизни stream. Исключайте
`/livez`, `/readyz` и `/metrics` из прикладных totals, а пользовательские
hits считайте по выбранному внешнему слою.

Среднее — сумма `rate(..._sum[5m])`, делённая на сумму
`rate(..._count[5m])`. Например, p95 HTTP по сервису и маршруту внутри
одного окружения можно вычислить так.

```promql
histogram_quantile(0.95,
  sum by (environment, service, route, method, le) (
    rate(cellestial_http_request_duration_seconds_bucket{route!~"/livez|/readyz|/metrics"}[5m])
  )
)
```

Отдельно показывайте `up`, последнее успешное время сбора и ошибку.
Время последнего успеха можно получить через
`max_over_time(timestamp(up == 1)[24h:15s])`, если scrape идёт каждые 15 секунд.
Prometheus targets API возвращает время последней попытки и `lastError`. Для runtime сопоставляйте
`server_id` из `cellestial_runtime_target_info` с `instance` обнаруженных
целей. Проверяйте `cellestial_runtime_inventory_available` и успешность
scrape Environments перед интерпретацией отсутствующих VPS. Для состояний
`creating`, `cleanup` и `deleting` применяйте правила жизненного цикла
infra. Отсутствие прикладного счётчика у здоровой цели может означать
отсутствие трафика.

`make services-test` использует тестовые PostgreSQL и Redis, fake cloud и
fake kernel. Для реального scrape в том же тесте задайте
`MONITORING_PROMETHEUS_BIN` абсолютным путём к Prometheus с `promtool` в
том же каталоге. Тест использует одноразовый TSDB, проверяет конфигурацию
и rules через promtool, четыре targets, mTLS и контрольные метрики в PromQL.
После остановки exporter тест проверяет `up=0` и наличие последнего успешного
scrape при повторном открытии TSDB. Ресурсы настоящего kernel проверяются через `make agent-integration`.
Проверку OpenStack, всех хостов, сетевых правил и дашборда завершает общий
стенд [infra#164](https://github.com/Cringe-Driven-Development-Team/infra/issues/164).
