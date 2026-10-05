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

{{/* The app's repository: source.repoURL, else the platform repository. */}}
{{- define "onboarding.repoURL" -}}
{{- .Values.spec.source.repoURL | default (printf "https://github.com/%s/%s" .Values.bluepave.github.owner .Values.bluepave.github.platformRepo) -}}
{{- end -}}
