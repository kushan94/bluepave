{{/* An Argo CD Application installing an upstream chart. */}}
{{- define "ss.application" -}}
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: {{ .name }}
  namespace: argocd
  annotations:
    argocd.argoproj.io/compare-options: ServerSideDiff=true
spec:
  project: platform
  source:
    repoURL: {{ .upstream.repoURL | quote }}
    chart: {{ .upstream.chart | quote }}
    targetRevision: {{ .upstream.version | quote }}
    helm:
      releaseName: {{ .name }}
      valuesObject:
        {{- toYaml .values | nindent 8 }}
  destination:
    server: https://kubernetes.default.svc
    namespace: {{ .namespace }}
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
      - ServerSideApply=true
    retry:
      limit: 10
      backoff:
        duration: 30s
        factor: 2
        maxDuration: 10m
{{- end -}}

{{/* This module's discovered outputs. */}}
{{- define "ss.ids" -}}
{{- index .Values.bluepave.discovered "self-service" | default dict | toYaml -}}
{{- end -}}

{{/* Whether AppStorage is offered: the setting, else on where the profile allows public access. */}}
{{- define "ss.appStorage" -}}
{{- $apis := .Values.settings.apis | default dict -}}
{{- if hasKey $apis "appStorage" -}}
{{- $apis.appStorage -}}
{{- else -}}
{{- .Values.bluepave.profile.network.allowPublicNetworkAccess -}}
{{- end -}}
{{- end -}}

{{/* Whether AppCache is offered: the setting, else on where the profile's cache is in-cluster. */}}
{{- define "ss.appCache" -}}
{{- $apis := .Values.settings.apis | default dict -}}
{{- if hasKey $apis "appCache" -}}
{{- $apis.appCache -}}
{{- else -}}
{{- eq ((.Values.bluepave.profile.data | default dict).cache | default "inCluster") "inCluster" -}}
{{- end -}}
{{- end -}}
