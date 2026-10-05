{{/* The portal's namespace: its app's stage on the portal's environment (apps/portal.yaml). */}}
{{- define "portal.namespace" -}}portal-{{ .Values.settings.environment | default "dev" }}{{- end -}}

{{/* Whether this cluster runs the portal. */}}
{{- define "portal.here" -}}
{{- eq .Values.bluepave.environment (.Values.settings.environment | default "dev") -}}
{{- end -}}
