{{/* The app's stages on this cluster (stage.environment, default dev). */}}
{{- define "onboarding.stages" -}}
{{- $env := .Values.bluepave.environment -}}
{{- $out := list -}}
{{- range .Values.spec.stages -}}
{{- if eq (.environment | default "dev") $env }}{{ $out = append $out . }}{{ end -}}
{{- end -}}
{{- dict "stages" $out | toYaml -}}
{{- end -}}

{{- define "onboarding.namespace" -}}
{{- printf "%s-%s" .app .stage.name -}}
{{- end -}}

{{/* The registry the golden path pushes to (registry module output), or "" before `bluepave up`. */}}
{{- define "onboarding.registry" -}}
{{- (index .Values.bluepave.discovered "registry" | default dict).containerRegistryLoginServer | default "" -}}
{{- end -}}

{{/* Kargo expressions are written ${{ ... }}; Helm would read the inner braces, so emit them. */}}
{{- define "kargo.open" -}}${{ "{{" }}{{- end -}}
