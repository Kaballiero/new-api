# Gateway token usage

Владелец подсчёта и оценки request/text/audio/image/realtime tokens и выбора canonical provider billing usage. Использует RelayInfo/relaykit facts, прежние common saturation helpers и pkg/media; экономику settlement не дублирует.

`InitTokenEncoders` остаётся явным startup вызовом в прежнем порядке: HTTP initialization → token encoders. Default encoder, model map/lock, lazy lookup и fallback unchanged. Provider estimation weights, media short-circuits и audio saturation сохранены. Billing и Batch получают прежние canonical usage facts.
