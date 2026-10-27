# server/internal — карта модулей

Бизнес-логика backend, разложенная по самостоятельным модулям. Внутри модуля каждый сценарий — отдельный пакет, который читается сверху вниз: `http.go` → `usecase.go` → узкие интерфейсы → адаптеры. Правила структуры — [AGENTS.md](AGENTS.md). Общая схема backend — [ARCHITECTURE.md](../ARCHITECTURE.md), порядок работ — [REFACTOR_ROADMAP.md](../REFACTOR_ROADMAP.md).

Шлюз к AI-моделям (`relay/`, `relaykit/`, relay-цепочка `middleware/`) сюда не входит: это заменяемая часть, бизнес-модули её не импортируют.

## Модули

| Модуль | Ответственность | Сценариев | README |
|---|---|---|---|
| `system` | setup, task registry/lease/history/cleanup, instances и runtime maintenance и performance metrics | 25 | [README](system/README.md) |
| `deployments` | управление io.net deployments, настройки и технические provider reads | 18 | [README](deployments/README.md) |
| `notifications` | доставка, event limiter, root и upstream model watchers | 3 | [README](notifications/README.md) |
| `settings` | общий options lifecycle, root editor, reload и публичная конфигурация; общий publication adapter и leaf pricing runtime | 11 | [README](settings/README.md) |
| `accounts` | аккаунты пользователей: создание и регистрация, профиль, группы, квота, привязки email, реферальный код и перевод реферальной квоты, ежедневный check-in, админское управление аккаунтами | 24 | [README](accounts/README.md) |
| `identity` | вход и доказательство личности: пароль, сессии и их отзыв, 2FA и резервные коды, passkey (WebAuthn), OAuth и WeChat, подтверждение email, восстановление пароля, повторная проверка для чувствительных действий | 66 | [README](identity/README.md) |
| `apikeys` | API-ключи пользователей для шлюза: создание, просмотр с маской, раскрытие, изменение, удаление, группы, статус, использование и gateway-аутентификация ключа | 16 | [README](apikeys/README.md) |
| `personal_access_tokens` | персональные токены доступа к панели управления (PAT): выпуск, статус, отзыв и проверка | 4 | [README](personal_access_tokens/README.md) |
| `authorization` | права администраторов (Casbin): каталог разрешений, проверка, переопределения для пользователя, загрузка политики | 11 | [README](authorization/README.md) |
| `catalog` | модели/вендоры, metadata sync, доступность моделей шлюза, presets и prefill groups | 25 | [README](catalog/README.md) |
| `pricing` | цены для клиентов: публичная и персональная страница цен, конфигурация цен и коэффициентов, сборка и публикация каталога цен из upstream | 26 | [README](pricing/README.md) |
| `channels` | администрирование каналов к провайдерам: CRUD, теги, ключи, баланс, проверка каналов, обновление моделей upstream, Codex-учётные данные, Ollama | 51 | [README](channels/README.md) |
| `billing` | request-local hold, funding wallet/subscription, token quota, settle/refund и notification eligibility; text/audio/realtime cost, tiered и FX; durable Task/Batch остаются снаружи | 11 | [README](billing/README.md) |
| `batch` | native Batch admission holds, execution, results, settlement/release, procurement и polling; provider wire остаётся в relay/batch | 10 | [README](batch/README.md) |
| `usage` | запись consumption/error; чтение логов, quota_data, flow, агрегаты и рейтинги; role visibility и metadata | 3 | [README](usage/README.md) |
| `audit` | чтение событий и role projection; запись login/security/operation/PAT, metadata policy | 3 | [README](audit/README.md) |
| `redemption` | выпуск и управление кодами пополнения, атомарное погашение с начислением квоты | 9 | [README](redemption/README.md) |

| `subscriptions` | планы, user subscriptions, balance/provider checkout, Epay callbacks, order completion/expiry, reset maintenance | 25 registrations | [README](subscriptions/README.md) |
| `payments` | top-up orders, checkout и provider webhooks, manual completion, payment availability/compliance и Pancake config | 22 | [README](payments/README.md) |

README каждого модуля появляется в коммите, который чистит или переносит этот модуль.

## Служебные части

| Папка | Что внутри |
|---|---|
| `app/` | сборка зависимостей модулей и HTTP-обработчики для `controller/`; `app/authentication` — сборка проверок credentials без обратного импорта middleware; `app/billing` — сборка lifecycle/cost без gateway types; независимые `app/notifications` и `app/channelruntime` — доставка и канал/root integration |
| `adapter/gorm/<module>` | хранение через GORM |
| `adapter/settings/<module>` | чтение настроек |
| `adapter/casbin`, `adapter/identity`, `adapter/channels`, `adapter/pricing`, `adapter/catalog` | технические реализации интерфейсов модулей |
| `contracttest/` | заготовка контрактных HTTP-тестов |
| `architecture_test.go` | проверка правил импортов; текущий долг — `architecture_known_violations_test.go` |

## Ещё не перенесено

Шлюз (`relay/`, `controller/relay.go`) остаётся снаружи internal; его static model snapshot и wire rendering принадлежат `relay/modelcatalog` и `relay/modelview`. Deployments, catalog HTTP и notifications перенесены. Settings publisher/mutex принадлежат `adapter/settings/publication`, pricing state — `adapter/pricing/state`,
общая сборка — leaf `app/pricingruntime`. Metadata mutex/anchor/transaction находятся в catalog adapter;
same-tx pricing edits передаются явной capability. Catalog domain чистый; codecs/version hashing — adapters.
System metrics core/loop собирается в leaf app/systemmetrics; aggregation/SQL/Redis находятся в adapters. Pricing FX refresh/replica lifecycle собирается в leaf app/pricingfx; RelayInfo mapping принадлежит gateway/metrics.

- `tasks` — durable async/Midjourney учёт, terminal billing, polling и media access; core сборка
  `app/tasks`, gateway/runtime — `app/taskruntime`, records/SQL — `adapter/gorm/tasks`.


`taskplugins` — управление версиями/слоями task-плагинов, sync pass, origin/query selection,
observation admission/timeout/backoff и retrieve policy. Сборка `app/taskplugins`, outer
`app/taskpluginsruntime`, storage `adapter/gorm/taskplugins`, compiler/settings
`adapter/taskplugins`. Публичный task projection/provenance — gateway `relay/taskview`.
Durable tasks и background writes остаются tasks; provider VM/wire machine остаются gateway.
Единый alias view находится в `adapter/taskplugins/aliases`, SQL reader — channels adapter.
Channel records/cache/abilities принадлежат channels adapter; model сохраняет строгие aliases.
