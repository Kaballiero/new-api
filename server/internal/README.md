# server/internal — карта модулей

Бизнес backend разделён на самостоятельные модули. Сценарий читается как HTTP → usecase →
узкие порты → adapters. Правила — [AGENTS.md](AGENTS.md), схема backend —
[ARCHITECTURE.md](../ARCHITECTURE.md), статус — [REFACTOR_ROADMAP.md](../REFACTOR_ROADMAP.md).

## Модули

Число сценариев означает подпапки с http.go или usecase.go; это не число HTTP registrations.

| Модуль | Ответственность | Пакетов сценариев | README |
|---|---|---|---|
| `system` | setup, task registry/lease/history/cleanup, instances и runtime maintenance и performance metrics | 25 | [README](system/README.md) |
| `deployments` | управление io.net deployments, настройки и технические provider reads | 19 | [README](deployments/README.md) |
| `notifications` | доставка, event limiter, root и upstream model watchers | 3 | [README](notifications/README.md) |
| `settings` | общий options lifecycle, root editor, reload и публичная конфигурация; общий publication adapter и leaf pricing runtime | 11 | [README](settings/README.md) |
| `accounts` | GetAPI PAT initialization и аккаунты пользователей: создание и регистрация, профиль, группы, квота, привязки email, реферальный код и перевод реферальной квоты, ежедневный check-in, админское управление аккаунтами | 26 | [README](accounts/README.md) |
| `identity` | вход и доказательство личности: пароль, сессии и их отзыв, 2FA и резервные коды, passkey (WebAuthn), OAuth и WeChat, подтверждение email, восстановление пароля, повторная проверка для чувствительных действий | 72 | [README](identity/README.md) |
| `apikeys` | API-ключи пользователей для шлюза: создание, просмотр с маской, раскрытие, изменение, удаление, группы, статус, использование и gateway-аутентификация ключа и billing subscription compatibility | 16 | [README](apikeys/README.md) |
| `personal_access_tokens` | персональные токены доступа к панели управления (PAT): выпуск, статус, отзыв и проверка | 4 | [README](personal_access_tokens/README.md) |
| `authorization` | права администраторов (Casbin): каталог разрешений, проверка, переопределения для пользователя, загрузка политики | 11 | [README](authorization/README.md) |
| `catalog` | модели/вендоры, metadata sync, доступность моделей шлюза, presets и prefill groups | 26 | [README](catalog/README.md) |
| `pricing` | цены для клиентов: публичная и персональная страница цен, конфигурация цен и коэффициентов, сборка и публикация каталога цен из upstream | 27 | [README](pricing/README.md) |
| `channels` | администрирование каналов к провайдерам: CRUD, теги, ключи, баланс, проверка каналов, обновление моделей upstream, Codex-учётные данные, Ollama | 51 | [README](channels/README.md) |
| `billing` | request-local hold, funding wallet/subscription, token quota, settle/refund и notification eligibility; text/audio/realtime cost, tiered и FX; durable Task/Batch остаются снаружи | 12 | [README](billing/README.md) |
| `batch` | native Batch admission holds, execution, results, settlement/release, procurement и polling; provider wire остаётся в relay/batch | 12 | [README](batch/README.md) |
| `usage` | запись consumption/error; чтение логов, quota_data, flow, агрегаты и рейтинги; role visibility, metadata и billing usage compatibility | 18 | [README](usage/README.md) |
| `audit` | чтение событий и role projection; запись login/security/operation/PAT, metadata policy | 3 | [README](audit/README.md) |
| `redemption` | выпуск и управление кодами пополнения, атомарное погашение с начислением квоты | 9 | [README](redemption/README.md) |
| `subscriptions` | планы, user subscriptions, balance/provider checkout, Epay callbacks, order completion/expiry, reset maintenance | 25 | [README](subscriptions/README.md) |
| `payments` | top-up orders, checkout и provider webhooks, manual completion, payment availability/compliance и Pancake config | 22 | [README](payments/README.md) |
| `tasks` | durable async/Midjourney учёт, polling, terminal billing и media access | 27 | [README](tasks/README.md) |
| `taskplugins` | версии/registry sync, origin/query, observation и retrieve policy | 19 | [README](taskplugins/README.md) |

## Сборка и технические владельцы

- `app/` создаёт сценарии/adapters; `app/httpapi` держит HTTP once-сборку, audit/login callbacks и
  startup channel effects. Controller содержит только однострочные router bindings и wire OpenAPI declarations.
- `app/authentication`, `identitycore`, `authorizationcore` — leaf-сборки без parent app/middleware cycle.
- `adapter/gorm/<module>/record` содержит records/hooks/codecs, `adapter/gorm/<module>` — SQL.
- `adapter/cache/{accounts,apikeys}` владеет одним hash/fence/hydration state; `app/quotaruntime`
  связывает reserve orchestration billing с явными account/key capabilities.
- `adapter/gorm/counters` владеет пятью RAM очередями, `adapter/gorm/usage.Writer` — log writer и
  отдельным export cache; locking/dbclock/subscription cache имеют собственных технических владельцев.
- `adapter/settings/publication` владеет одним option publisher/group-ratio mutex; `adapter/pricing/state`
  — snapshot/revision/observations. `app/pricingruntime` связывает settings/pricing/catalog,
  сохраняя metadata anchor и same-tx pricing mutation.
- `adapter/taskplugins/aliases` — один TTL/generation view с channels SQL reader.
- `app/pricingfx` собирает FX refresh/replica; `app/systemmetrics` — metrics core/loop над runtime/SQL/Redis adapters.
- `contracttest/` содержит используемые DB/Redis/FX fixtures для обычных регрессий. Добавленная snapshot suite удалена по решению владельца; сохранены только унаследованные `origin/main` testdata. Покрытие сценариев расширяется постепенно (TECH_DEBT #99).

## Границы

Gateway ingress — `gateway/transport`, protocol execution — `relay/`, независимо собираемые wire DTO —
`relaykit/`. Business вызывает их только через consumer ports/adapters. Для pricing сохранён узкий
D1 constant/type alias leaf, описанный в AGENTS.md; direct gateway imports запрещены.

Production service и временных BRIDGE нет. Model содержит 47 migration-used table aliases,
DB bootstrap и прежние migrations/backfills; вспомогательные types/constants/errors используют владельцев.
Architecture allowlist пуст; import/purity/D1 и closure проверяются в architecture_test.go.
Технический долг бизнес-поведения сохраняется в TECH_DEBT.md; его устранение требует отдельного решения.
