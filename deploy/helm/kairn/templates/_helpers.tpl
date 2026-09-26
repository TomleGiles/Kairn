{{/* Nom complet de la release. */}}
{{- define "kairn.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}{{ .Release.Name | trunc 63 | trimSuffix "-" }}{{- else -}}{{ printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" }}{{- end -}}
{{- end -}}

{{- define "kairn.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{- define "kairn.selector" -}}
app.kubernetes.io/name: {{ .root.Chart.Name }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{- define "kairn.image" -}}
{{- $tag := .root.Values.image.tag | default .root.Chart.AppVersion -}}
{{ printf "%s/%s:%s" .root.Values.image.registry .name $tag }}
{{- end -}}

{{- define "kairn.apiURL" -}}
{{- .Values.apiURL | default .Values.publicURL -}}
{{- end -}}

{{/* Référence à une clé du secret ; optional : la clé peut être absente. */}}
{{- define "kairn.secretEnv" -}}
- name: {{ .env }}
  valueFrom:
    secretKeyRef:
      name: {{ .root.Values.secrets.existingSecret }}
      key: {{ .key }}
      {{- if .optional }}
      optional: true
      {{- end }}
{{- end -}}

{{/* Environnement commun des services Go. */}}
{{- define "kairn.goEnv" -}}
{{- $fn := include "kairn.fullname" . -}}
- {name: KAIRN_MODE, value: production}
- {name: KAIRN_LOG_FORMAT, value: json}
- {name: KAIRN_LOG_LEVEL, value: {{ .Values.config.logLevel | quote }}}
- {name: KAIRN_PUBLIC_URL, value: {{ .Values.publicURL | quote }}}
- {name: KAIRN_API_URL, value: {{ include "kairn.apiURL" . | quote }}}
- {name: KAIRN_REGION, value: {{ .Values.config.region | quote }}}
- {name: KAIRN_CLICKHOUSE_ADDR, value: {{ .Values.clickhouse.url | quote }}}
- {name: KAIRN_CLICKHOUSE_DB, value: {{ .Values.clickhouse.database | quote }}}
- {name: KAIRN_CLICKHOUSE_USER, value: {{ .Values.clickhouse.user | quote }}}
{{- if eq .Values.mode "distributed" }}
- {name: KAIRN_NATS_URL, value: {{ .Values.nats.url | quote }}}
{{- end }}
{{- with .Values.redis.url }}
- {name: KAIRN_REDIS_URL, value: {{ . | quote }}}
{{- end }}
{{- if .Values.s3.endpoint }}
- {name: KAIRN_S3_ENDPOINT, value: {{ .Values.s3.endpoint | quote }}}
- {name: KAIRN_S3_BUCKET, value: {{ .Values.s3.bucket | quote }}}
- {name: KAIRN_S3_REGION, value: {{ .Values.s3.region | quote }}}
- {name: KAIRN_S3_ACCESS_KEY, value: {{ .Values.s3.accessKey | quote }}}
- {name: KAIRN_S3_SSL, value: {{ .Values.s3.useSSL | quote }}}
{{ include "kairn.secretEnv" (dict "root" . "env" "KAIRN_S3_SECRET_KEY" "key" "s3-secret-key") }}
{{- else }}
- {name: KAIRN_LOCAL_OBJECT_DIR, value: /data/objects}
{{- end }}
{{- if .Values.vault.addr }}
- {name: KAIRN_VAULT_ADDR, value: {{ .Values.vault.addr | quote }}}
- {name: KAIRN_VAULT_TRANSIT_KEY, value: {{ .Values.vault.transitKey | quote }}}
{{ include "kairn.secretEnv" (dict "root" . "env" "KAIRN_VAULT_TOKEN" "key" "vault-token") }}
{{- else }}
{{ include "kairn.secretEnv" (dict "root" . "env" "KAIRN_KEK" "key" "kek") }}
{{- end }}
{{- with .Values.config.oidc.issuer }}
- {name: KAIRN_OIDC_ISSUER, value: {{ . | quote }}}
- {name: KAIRN_OIDC_CLIENT_ID, value: {{ $.Values.config.oidc.clientId | quote }}}
{{ include "kairn.secretEnv" (dict "root" $ "env" "KAIRN_OIDC_CLIENT_SECRET" "key" "oidc-client-secret" "optional" true) }}
{{- end }}
{{- if .Values.ai.enabled }}
- {name: KAIRN_AI_URL, value: {{ printf "http://%s-ai:8091" $fn | quote }}}
{{- end }}
- {name: KAIRN_ANALYTICS_URL, value: {{ printf "http://%s-analytics:8090" $fn | quote }}}
{{- with .Values.config.smtp.addr }}
- {name: KAIRN_SMTP_ADDR, value: {{ . | quote }}}
- {name: KAIRN_SMTP_FROM, value: {{ $.Values.config.smtp.from | quote }}}
- {name: KAIRN_SMTP_USER, value: {{ $.Values.config.smtp.user | quote }}}
{{ include "kairn.secretEnv" (dict "root" $ "env" "KAIRN_SMTP_PASSWORD" "key" "smtp-password" "optional" true) }}
{{- end }}
{{- with .Values.config.otlpEndpoint }}
- {name: OTEL_EXPORTER_OTLP_ENDPOINT, value: {{ . | quote }}}
{{- end }}
- {name: KAIRN_DEMO_SEED, value: {{ .Values.config.demoSeed | quote }}}
- {name: KAIRN_TRUSTED_PROXIES, value: {{ join "," .Values.config.trustedProxies | quote }}}
- {name: KAIRN_PRICE_IMPORT, value: {{ .Values.config.priceImport | default (list "none") | join "," | quote }}}
{{ include "kairn.secretEnv" (dict "root" . "env" "KAIRN_POSTGRES_URL" "key" "postgres-url") }}
{{ include "kairn.secretEnv" (dict "root" . "env" "KAIRN_CLICKHOUSE_PASSWORD" "key" "clickhouse-password") }}
{{ include "kairn.secretEnv" (dict "root" . "env" "KAIRN_SESSION_SECRET" "key" "session-secret") }}
{{ include "kairn.secretEnv" (dict "root" . "env" "KAIRN_SERVICE_TOKEN" "key" "service-token") }}
{{ include "kairn.secretEnv" (dict "root" . "env" "KAIRN_STRIPE_SECRET_KEY" "key" "stripe-secret-key" "optional" true) }}
{{ include "kairn.secretEnv" (dict "root" . "env" "KAIRN_STRIPE_WEBHOOK_SECRET" "key" "stripe-webhook-secret" "optional" true) }}
{{- end -}}

{{/*
Deployment générique d'un composant.
  dict "root" $ "component" "api" "image" "api" "values" .Values.api "args" (list) "port" 8080
       "env" "go" | "none" | "extra" (list) "writable" (list "/tmp")
*/}}
{{- define "kairn.deployment" -}}
{{- $root := .root -}}
{{- $v := .values -}}
{{- $fn := include "kairn.fullname" $root -}}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ printf "%s-%s" $fn .component }}
  labels:
    {{- include "kairn.labels" $root | nindent 4 }}
    app.kubernetes.io/component: {{ .component }}
spec:
  {{- if not (and $v.autoscaling $v.autoscaling.enabled) }}
  replicas: {{ .replicas | default $v.replicas | default 1 }}
  {{- end }}
  {{- with .strategy }}
  strategy: {type: {{ . }}}
  {{- end }}
  selector:
    matchLabels:
      {{- include "kairn.selector" (dict "root" $root "component" .component) | nindent 6 }}
  template:
    metadata:
      labels:
        {{- include "kairn.selector" (dict "root" $root "component" .component) | nindent 8 }}
      annotations:
        {{- with $root.Values.podAnnotations }}{{ toYaml . | nindent 8 }}{{ end }}
    spec:
      serviceAccountName: {{ $fn }}
      automountServiceAccountToken: false
      {{- with $root.Values.imagePullSecrets }}
      imagePullSecrets: {{ toYaml . | nindent 8 }}
      {{- end }}
      securityContext: {{ toYaml $root.Values.podSecurityContext | nindent 8 }}
      containers:
        - name: {{ .component }}
          image: {{ include "kairn.image" (dict "root" $root "name" .image) }}
          imagePullPolicy: {{ $root.Values.image.pullPolicy }}
          {{- with .args }}
          args: {{ toJson . }}
          {{- end }}
          securityContext: {{ toYaml $root.Values.securityContext | nindent 12 }}
          env:
            {{- if eq .env "go" }}
            {{- include "kairn.goEnv" $root | nindent 12 }}
            {{- end }}
            {{- with .extraEnv }}
            {{- toYaml . | nindent 12 }}
            {{- end }}
          {{- if .port }}
          ports:
            - {name: http, containerPort: {{ .port }}}
          readinessProbe:
            httpGet: {path: {{ .readyPath | default "/readyz" }}, port: http}
            periodSeconds: 10
            failureThreshold: 3
          livenessProbe:
            httpGet: {path: /healthz, port: http}
            initialDelaySeconds: 10
            periodSeconds: 20
          {{- end }}
          resources: {{ toYaml $v.resources | nindent 12 }}
          volumeMounts:
            - {name: tmp, mountPath: /tmp}
            {{- range $i, $p := .writable }}
            - {name: {{ printf "w%d" $i }}, mountPath: {{ $p }}}
            {{- end }}
      volumes:
        - {name: tmp, emptyDir: {}}
        {{- range $i, $p := .writable }}
        - {name: {{ printf "w%d" $i }}, emptyDir: {}}
        {{- end }}
      {{- with $root.Values.nodeSelector }}
      nodeSelector: {{ toYaml . | nindent 8 }}
      {{- end }}
      {{- with $root.Values.tolerations }}
      tolerations: {{ toYaml . | nindent 8 }}
      {{- end }}
      affinity:
        {{- if $root.Values.affinity }}
        {{- toYaml $root.Values.affinity | nindent 8 }}
        {{- else }}
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              podAffinityTerm:
                topologyKey: kubernetes.io/hostname
                labelSelector:
                  matchLabels:
                    {{- include "kairn.selector" (dict "root" $root "component" .component) | nindent 20 }}
        {{- end }}
{{- end -}}

{{/* Service ClusterIP d'un composant. */}}
{{- define "kairn.service" -}}
apiVersion: v1
kind: Service
metadata:
  name: {{ printf "%s-%s" (include "kairn.fullname" .root) .component }}
  labels:
    {{- include "kairn.labels" .root | nindent 4 }}
    app.kubernetes.io/component: {{ .component }}
spec:
  selector:
    {{- include "kairn.selector" (dict "root" .root "component" .component) | nindent 4 }}
  ports:
    - {name: http, port: {{ .port }}, targetPort: http}
{{- end -}}

{{/* PodDisruptionBudget (au moins un pod disponible). */}}
{{- define "kairn.pdb" -}}
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: {{ printf "%s-%s" (include "kairn.fullname" .root) .component }}
  labels:
    {{- include "kairn.labels" .root | nindent 4 }}
spec:
  minAvailable: 1
  selector:
    matchLabels:
      {{- include "kairn.selector" (dict "root" .root "component" .component) | nindent 6 }}
{{- end -}}
