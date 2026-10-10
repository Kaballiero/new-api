# OpenRouter Video

Один task-плагин для OpenRouter Video API. Использует существующие
`POST /v1/videos`, `GET /v1/videos/{id}` и `GET/HEAD /v1/videos/{id}/content`.
Канал OpenRouter имеет type `20`, base URL — `https://openrouter.ai/api`.
Добавление канала, моделей, alias и цен выполняется администратором отдельно.

## Возможности и метаданные

Снимок публичного каталога содержит 30 моделей. Длительности, размеры,
разрешения, пропорции, кадры, audio, seed и upscale проверяются по записи
выбранной модели. Поддержка каталога не доказывает доступность модели по ключу.
Матрица сочетаний параметров и ограничения содержимого входных медиа не
полностью представлены в публичном каталоге и дополнительно проверяются upstream.

JSON поддерживает `prompt`, `seconds` (mapping в `duration`), `size`,
`metadata` с параметрами OpenRouter, `input_reference`/`image`, `frame_images`
и `input_references`. Для multipart параметры модели передаются JSON-строкой
в `metadata`, изображение — единственным файлом `input_reference` до 20 MiB.
Явные `generate_audio: false` и `seed: 0` сохраняются. Неизвестные модели,
неподдерживаемые параметры и конфликтующие значения отвергаются до отправки.
Passthrough aliases, меняющие оплачиваемые входы, размеры или число результатов,
не допускаются; используйте канонические параметры.

Обновление снимка вне runtime hooks:

```sh
python3 plugins/tasks/openrouter-video/update-catalog.py
go test ./plugins ./pkg/jsplugin ./relay/channel/task/jsplugin ./relay
```

После обновления нужно проверить diff метаданных и тарифы. Снимок старше 30 дней
блокирует новые отправки; polling и скачивание существующих задач продолжаются.

## Клиентский резерв и финансовая сверка

Для новых заданий `billing_expr` определяет только начальный резерв. Выражение,
FX, выбранная группа и effective group ratio фиксируются перед paid POST.
Например, `tier("reserve", u("requested_seconds") * 0.1)` резервирует $0.10 за секунду
до group/FX multiplier; это пример резерва, а не опубликованная закупочная цена.
Окончательный клиентский расход вычисляется из подтверждённого OpenRouter
`usage.cost` и замороженного effective group ratio. Если cost выше резерва,
дополнительное списание может привести к отрицательному остатку кошелька/токена;
лимит подписки сохраняется, превышение оставляет операцию на ручную сверку.

При пропуске duration значение `0` означает «не задано»: выражение резерва
должно иметь явную ветку для этого случая. Плагин не подменяет значение
минимальной или максимальной длительностью из каталога.

Другие факты: `resolution`, `size`, `aspect_ratio`, `audio`, `images`, `videos`,
`audios`, `upscale_factor`, `creativity`, `jobs`. Для пропущенных enum используется
`default`, для пропущенного upscale factor — `0`.

OpenRouter документирует только `usage.cost` и `usage.is_byok`; они сохраняются
как отдельное приватное свидетельство закупочного расхода. Токены, фактические
секунды и megapixel-seconds из duration не выводятся. Тарифы по таким измеренным
единицам нельзя включать без дополнительного подтверждённого источника данных.

Плагин использует `submissionPolicy: "reconcile"`: durable intent сохраняется
до резервирования и единственного paid POST. Неопределённая отправка не
повторяется автоматически. Новые задания замораживают `settlement_mode:
openrouter_cost_v1`: `billing_expr` задаёт начальный резерв, а окончательный
расход равен подтверждённому `usage.cost` в USD × сохранённый effective group
ratio × QuotaPerUnit. Effective group ratio уже включает замороженный FX;
повторное применение FX не выполняется. Это правило действует для успеха и
оплаченного отказа; явный подтверждённый ноль возвращает весь резерв.
Отсутствие usage, BYOK или неизвестный BYOK, некорректный cost/billing snapshot,
timeout и потерянная задача сохраняют резерв и `reconciliation.required`.
Старые задания без settlement mode сохраняют прежний фиксированный тариф.

Только победитель terminal status CAS выполняет финансовый переход. Target и
pending сохраняются до перехода; main DB транзакция синхронно меняет wallet
или subscription, token, used counters и task quota независимо от batch mode.
Затем применяются только additive guarded Redis deltas и обязательный
consume/refund log; равная резерву сумма не создаёт денежный log. Finalized
устанавливается лишь после подтверждения всех шагов. Неопределённый результат
оставляет durable pending и автоматически не повторяется.

Main DB, Redis и отдельно настроенная log DB не образуют общей транзакции.
Существующие batch reservation/cache механизмы также остаются отдельными:
авария до сброса начальной batch очереди, холодный cache и неоднозначный ответ
Redis требуют ручной сверки; абсолютный DB balance в горячий cache не пишется.

Сверка выполняется по приватным `Task.PrivateData.Reconciliation`, upstream ID,
первичному reservation snapshot и финансовым записям кошелька/подписки, токена,
счётчиков и consume/refund logs. Автоматического инструмента ручного завершения
незавершённых операций пока нет. Для unattended production это открытый блокер:
необходимо согласовать и проверить операционную процедуру сверки. Не сбрасывайте
pending-маркеры и не повторяйте POST без доказательства исхода операции.

## Пример существующего API

```sh
curl "$GETAPI_BASE/v1/videos" \
  -H "Authorization: Bearer $GETAPI_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"model":"x-ai/grok-imagine-video","prompt":"Ocean waves","seconds":1,"metadata":{"resolution":"480p"}}'

curl "$GETAPI_BASE/v1/videos/$TASK_ID" \
  -H "Authorization: Bearer $GETAPI_TOKEN"

curl "$GETAPI_BASE/v1/videos/$TASK_ID/content" \
  -H "Authorization: Bearer $GETAPI_TOKEN" -o result.mp4
```

Для image-to-video добавьте `"input_reference":"https://…/image.png"`
для поддерживающей first frame модели. Клиентский alias остаётся в публичной
задаче; upstream получает имя из mapping канала. Используется выбранный при
отправке ключ даже после ротации канала. Content URL строится на origin канала;
`unsigned_urls` и `polling_url` не используются как адреса для передачи ключа.

Источники: [Video API](https://openrouter.ai/docs/guides/overview/multimodal/video-generation),
[video models](https://openrouter.ai/api/v1/videos/models),
[общий каталог](https://openrouter.ai/api/v1/models?output_modalities=video),
[официальная OpenAPI](https://openrouter.ai/docs/openapi/openapi.yaml).

## Проверенные реальные сценарии (2026-10-09)

Через существующий NewAPI API скачаны и полностью декодированы три MP4:

| Модель | Сценарий | Фактический результат | OpenRouter cost |
|---|---|---|---|
| x-ai/grok-imagine-video | text-to-video через alias, 480p/1s | 848×480, 1.041667s, H264+AAC | $0.05 |
| x-ai/grok-imagine-video | PNG first frame, 480p/1s; перезапуск NewAPI после receipt | 544×544, 1.041667s, H264+AAC | $0.052 |
| google/veo-3.1-lite | 720p/4s, generate_audio=false | 1280×720, 4s, H264 без audio | $0.12 |

Для каждой задачи terminal `usage.cost` совпал с `generation.total_cost`;
`is_byok=false`. Клиентское списание и consume log выполнены однократно,
изоляция пользователей и content GET/HEAD проверены. Это подтверждает только
указанные сценарии. Реальные paid failures/refunds не проверялись.

Сумма задач — $0.222. Прирост общего счётчика аккаунта — $0.222447500;
разница $0.000447500 осталась необъяснённой. В лимите проверок $5 учтён весь
прирост (остаток $4.777552500); полная сверка общего счётчика не заявляется.
