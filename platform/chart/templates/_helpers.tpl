{{/* The platform repository Argo CD reads. */}}
{{- define "bluepave.repoURL" -}}
{{- .Values.gitops.repoURL | default (printf "https://github.com/%s/%s.git" .Values.spec.github.owner .Values.spec.github.platformRepo) -}}
{{- end -}}

{{/*
The module context: what every module's gitops chart receives as values under `bluepave`
(ADR-0002). It's part of the module API (v1alpha1): add fields freely, but don't rename or remove
them without a new API version.
*/}}
{{- define "bluepave.context" -}}
{{- $env := .Values.environment -}}
{{- $discovered := .Values.discovered | default dict -}}
{{- $profile := .Values.resolved.profileSpec -}}
{{- $hasSpot := false -}}
{{- range ($profile.cluster | default dict).userPools | default list -}}
{{- if .spot }}{{ $hasSpot = true }}{{ end -}}
{{- end -}}
platform: {{ .Values.metadata.name | quote }}
environment: {{ $env | quote }}
prefix: {{ .Values.spec.prefix | quote }}
region: {{ .Values.spec.azure.region | quote }}
# Apps and platform UIs are served under <name>.<environment>.<apex domain>.
domain: {{ printf "%s.%s" $env .Values.spec.dns.domain | quote }}
apexDomain: {{ .Values.spec.dns.domain | quote }}
github:
  owner: {{ .Values.spec.github.owner | quote }}
  platformRepo: {{ .Values.spec.github.platformRepo | quote }}
gitops:
  repoURL: {{ include "bluepave.repoURL" . | quote }}
  revision: {{ .Values.gitops.revision | quote }}
tenantId: {{ (($discovered.azure | default dict).tenantId) | default "" | quote }}
subscriptionId: {{ (($discovered.azure | default dict).subscriptionId) | default "" | quote }}
admins:
  group: {{ .Values.spec.admins.group | quote }}
  groupObjectId: {{ (($discovered.admins | default dict).groupObjectId) | default "" | quote }}
profile:
  name: {{ .Values.resolved.profile | quote }}
  {{- with $profile }}
  {{- toYaml . | nindent 2 }}
  {{- end }}
# Enabled module names, so a module can integrate with another one (e.g. add a route when
# edge-gateway is on) without reading its files.
modules:
  {{- range .Values.resolved.modules }}
  - {{ .name | quote }}
  {{- end }}
# The public Gateway (edge-gateway module) and the platform hostnames enabled modules serve
# (module.yaml spec.hostnames): a module routes <name>.<domain> through listener https-<name>.
gateway:
  name: public
  namespace: gateway
platformHosts:
  {{- range .Values.resolved.modules }}
  {{- $module := .name }}
  {{- range .hostnames }}
  - { name: {{ .name | quote }}, namespace: {{ .namespace | quote }}, module: {{ $module | quote }} }
  {{- end }}
  {{- end }}
# This environment's discovered IDs (.bluepave/discovered.yaml environments.<env>).
discovered:
  {{- toYaml (($discovered.environments | default dict) | dig $env dict) | nindent 2 }}
scheduling:
  # Run more than one replica of platform controllers: off on a cluster tier with no uptime SLA
  # (trial), where every vCPU counts.
  highAvailability: {{ ne ((($profile.cluster | default dict).tier) | default "Free") "Free" }}
  # Platform pods (controllers, the portal) may run on Spot nodes when the profile has them.
  tolerations:
    {{- if $hasSpot }}
    - key: kubernetes.azure.com/scalesetpriority
      operator: Equal
      value: spot
      effect: NoSchedule
    {{- else }} []
    {{- end }}
{{- end -}}
