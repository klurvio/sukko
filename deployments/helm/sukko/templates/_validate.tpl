{{- define "sukko.validate" -}}
{{- /*
  Infrastructure validation guards.
  Catch mode/infrastructure mismatches at helm template time
  with actionable error messages.
*/ -}}

{{- /* === Kafka / Redpanda === */ -}}
{{- $messageBackend := index .Values "ws-server" "messageBackend" | default "direct" -}}
{{- $kafkaBrokers := "" -}}
{{- if index .Values "ws-server" "kafka" -}}
  {{- $kafkaBrokers = index .Values "ws-server" "kafka" "brokers" | default "" -}}
{{- end -}}
{{- $redpandaEnabled := .Values.redpanda.enabled | default false -}}

{{- /* Guard 1: kafka mode without infrastructure */ -}}
{{- if and (eq $messageBackend "kafka") (not $redpandaEnabled) (not $kafkaBrokers) -}}
  {{- fail "\n[CONFIG ERROR] messageBackend is 'kafka' but no Kafka infrastructure is configured.\nEither set redpanda.enabled: true (in-cluster) or set ws-server.kafka.brokers (external)." -}}
{{- end -}}

{{- /* Guard 2: redpanda + external brokers conflict */ -}}
{{- if and $redpandaEnabled $kafkaBrokers -}}
  {{- fail "\n[CONFIG ERROR] Both redpanda.enabled and kafka.brokers are set.\nRemove redpanda.enabled (use external) or remove kafka.brokers (use in-cluster)." -}}
{{- end -}}

{{- /* Guard 3: redpanda enabled but mode doesn't use it */ -}}
{{- if and $redpandaEnabled (ne $messageBackend "kafka") -}}
  {{- fail (printf "\n[CONFIG ERROR] redpanda.enabled is true but messageBackend is '%s'.\nRedpanda will be deployed but unused. Set messageBackend: kafka to use it, or remove redpanda.enabled." $messageBackend) -}}
{{- end -}}

{{- /* === Valkey === */ -}}
{{- $broadcastType := index .Values "ws-server" "broadcastType" | default "nats" -}}
{{- $valkeyAddrs := "" -}}
{{- if index .Values "ws-server" "valkey" -}}
  {{- $valkeyAddrs = index .Values "ws-server" "valkey" "addrs" | default "" -}}
{{- end -}}
{{- $valkeyEnabled := .Values.valkey.enabled | default false -}}

{{- /* Guard 4: valkey mode without infrastructure */ -}}
{{- if and (eq $broadcastType "valkey") (not $valkeyEnabled) (not $valkeyAddrs) -}}
  {{- fail "\n[CONFIG ERROR] broadcastType is 'valkey' but no Valkey infrastructure is configured.\nEither set valkey.enabled: true (in-cluster) or set ws-server.valkey.addrs (external)." -}}
{{- end -}}

{{- /* Guard 5: valkey enabled + external addrs conflict */ -}}
{{- if and $valkeyEnabled $valkeyAddrs -}}
  {{- fail "\n[CONFIG ERROR] Both valkey.enabled and valkey.addrs are set.\nRemove valkey.enabled (use external) or remove valkey.addrs (use in-cluster)." -}}
{{- end -}}

{{- /* Guard 6: valkey enabled but mode doesn't use it */ -}}
{{- if and $valkeyEnabled (ne $broadcastType "valkey") -}}
  {{- fail (printf "\n[CONFIG ERROR] valkey.enabled is true but broadcastType is '%s'.\nValkey will be deployed but unused. Set broadcastType: valkey to use it, or remove valkey.enabled." $broadcastType) -}}
{{- end -}}

{{- /* === PostgreSQL === */ -}}
{{- $databaseDriver := index .Values "provisioning" "databaseDriver" | default "sqlite" -}}
{{- $extDBUrl := "" -}}
{{- $extDBSecret := "" -}}
{{- if index .Values "provisioning" "externalDatabase" -}}
  {{- $extDBUrl = index .Values "provisioning" "externalDatabase" "url" | default "" -}}
  {{- $extDBSecret = index .Values "provisioning" "externalDatabase" "existingSecret" | default "" -}}
{{- end -}}
{{- $pgEnabled := false -}}
{{- if .Values.postgresql -}}
  {{- $pgEnabled = .Values.postgresql.enabled | default false -}}
{{- end -}}
{{- $hasExternalDB := or $extDBUrl $extDBSecret -}}

{{- /* Guard 7: postgres mode without infrastructure */ -}}
{{- if and (eq $databaseDriver "postgres") (not $pgEnabled) (not $hasExternalDB) -}}
  {{- fail "\n[CONFIG ERROR] databaseDriver is 'postgres' but no PostgreSQL infrastructure is configured.\nEither set postgresql.enabled: true (in-cluster) or set provisioning.externalDatabase.url (external)." -}}
{{- end -}}

{{- /* Guard 8: postgresql enabled + external db conflict */ -}}
{{- if and $pgEnabled $hasExternalDB -}}
  {{- fail "\n[CONFIG ERROR] Both postgresql.enabled and externalDatabase are set.\nRemove postgresql.enabled (use external) or remove externalDatabase (use in-cluster)." -}}
{{- end -}}

{{- /* Guard 9: postgresql enabled but mode doesn't use it */ -}}
{{- if and $pgEnabled (ne $databaseDriver "postgres") -}}
  {{- fail (printf "\n[CONFIG ERROR] postgresql.enabled is true but databaseDriver is '%s'.\nPostgreSQL will be deployed but unused. Set databaseDriver: postgres to use it, or remove postgresql.enabled." $databaseDriver) -}}
{{- end -}}

{{- /* === Reverse External Guards (address set, mode doesn't use it) === */ -}}

{{- /* Guard 10: external kafka address without kafka mode */ -}}
{{- if and $kafkaBrokers (ne $messageBackend "kafka") -}}
  {{- fail (printf "\n[CONFIG ERROR] kafka.brokers is set but messageBackend is '%s'.\nThe external Kafka address will be ignored. Set messageBackend: kafka to use it, or remove kafka.brokers." $messageBackend) -}}
{{- end -}}

{{- /* Guard 11: external valkey address without valkey mode */ -}}
{{- if and $valkeyAddrs (ne $broadcastType "valkey") -}}
  {{- fail (printf "\n[CONFIG ERROR] valkey.addrs is set but broadcastType is '%s'.\nThe external Valkey address will be ignored. Set broadcastType: valkey to use it, or remove valkey.addrs." $broadcastType) -}}
{{- end -}}

{{- /* Guard 12: external database without postgres mode */ -}}
{{- if and $hasExternalDB (ne $databaseDriver "postgres") -}}
  {{- fail (printf "\n[CONFIG ERROR] externalDatabase is configured but databaseDriver is '%s'.\nThe external database will be ignored and provisioning will use SQLite. Set databaseDriver: postgres to use it, or remove externalDatabase." $databaseDriver) -}}
{{- end -}}

{{- /* === Deprecation Guards (catch old renamed/removed keys) === */ -}}

{{- /* Guard 13: old broadcast.type key still present */ -}}
{{- if index .Values "ws-server" "broadcast" -}}
  {{- fail "\n[CONFIG ERROR] ws-server.broadcast.type has been renamed to ws-server.broadcastType.\nUpdate your values file." -}}
{{- end -}}

{{- /* Guard 14: old global.postgresql.enabled key still present */ -}}
{{- if .Values.global.postgresql -}}
  {{- fail "\n[CONFIG ERROR] global.postgresql.enabled is no longer supported.\nSet provisioning.databaseDriver: postgres instead." -}}
{{- end -}}

{{- end -}}
