{{/* An Argo CD Application installing an upstream chart. */}}
{{- define "edge.application" -}}
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
      # The Gateway API and Envoy Gateway CRDs exceed the client-side apply annotation limit.
      - ServerSideApply=true
    retry:
      limit: 10
      backoff:
        duration: 30s
        factor: 2
        maxDuration: 10m
{{- end -}}
