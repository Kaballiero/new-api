# Channel probe gateway adapter

Синтетический запрос к provider adapter: endpoint/model selection, request builder,
stream parsing, usage extraction и test quota projection. NewEngine реализует channels/domain.Probe;
record resolver сохраняет исходную загруженную запись, key и error mapping. AutomaticStream и
ResolveProbeUserID обслуживают прежнюю сборку channel management.

Channel failure effects выполняет gateway/transport.ProcessChannelError. Это техническая
orchestration поверх прежних channel/usage owners; бизнес не импортирует transport. Ни root relay,
ни нижние gateway leaves не импортируют probe adapter. Реальные settings, SQL, billing и provider
protocol implementations не дублируются; request/response/error/log bytes сохраняются.
