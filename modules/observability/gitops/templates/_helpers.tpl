{{/* An Argo CD Application installing an upstream chart into the observability namespace. */}}
{{- define "obs.application" -}}
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: {{ .name }}
  namespace: argocd
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
    namespace: observability
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
