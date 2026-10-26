# Развёртывания моделей

Модуль владеет управлением контейнерными развёртываниями io.net: доступностью настройки,
списками и поиском, созданием, изменением, переименованием, продлением и завершением,
оборудованием, локациями, оценкой цены, контейнерами и чтением логов.

## Сценарии и входы

Все management routes `/api/deployments` используют существующий AdminAuth (session/PAT).
`get_deployment_settings` читает поздние настройки; `test_deployment_connection` проверяет
явный или сохранённый ключ независимо от enabled. Два test-connection alias route сохраняются.

- `list_deployments`, `search_deployments`: provider pagination, поиск только в полученной странице,
  page-local status counts в list, provider total при пустом keyword.
- `get_deployment`, `create_deployment`, `update_deployment`, `rename_deployment`,
  `extend_deployment`, `terminate_deployment`: detail/commands; rename сначала проверяет имя,
  затем выполняет update на том же provider client.
- `list_deployment_hardware`, `list_deployment_locations`, `get_available_replicas`,
  `estimate_deployment_price`, `check_deployment_name`: technical reads и прежние projections.
- `get_deployment_logs`, `list_deployment_containers`, `get_deployment_container`: raw logs и
  container/events projection. Логи возвращаются обычной JSON-строкой, не stream/SSE.

## Зависимости и сборка

`internal/app/deployments.go` собирает handlers один раз. Настройки читаются под прежней
блокировкой на каждом запросе; обычный SDK client получает raw key, test-connection — trimmed key.
`adapter/http/deployments` преобразует plain facts и scenario DTO в единственный `pkg/ionet` SDK;
public API используется для locations/logs, enterprise — для остальных операций.
SDK client создаётся на операцию, без нового retry, cancellation или transport policy.

List/search, rename и extend используют самостоятельные usecases. Прямые technical reads
выполняются HTTP handlers через узкие provider ports. Общие deployment/container facts,
проекция списка и business errors находятся в domain; single-scenario wire types — в dto.go.
Копирование полей сохраняет nil slices/maps и explicit zero без JSON round trips.

## Границы

Локальных deployment records, quota debit, customer settlement и фонового scheduler нет.
Авторизация принадлежит middleware, настройки options — settings, SDK transport — pkg/ionet.
`controller/deployment.go` содержит delegates для существующих router registrations; их можно
удалить после прямой сборки handlers в router. Прежние HTTP200 error envelopes и provider error
chains сохраняются, включая отдельную legacy direct type assertion test-connection.
