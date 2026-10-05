{{- define "anvil.labels" -}}
app.kubernetes.io/part-of: anvil
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end }}

{{- define "anvil.selector" -}}
app.kubernetes.io/part-of: anvil
app.kubernetes.io/component: {{ . }}
{{- end }}

{{/* Whether a component is deployed: once Kargo has written its image (repository and digest). */}}
{{- define "anvil.deployed" -}}
{{- $img := index .root.Values.images .name -}}
{{- if and $img.repository $img.digest }}true{{ end -}}
{{- end }}

{{/* Image reference pinned by digest: what runs is exactly what the golden path scanned and signed. */}}
{{- define "anvil.image" -}}
{{- $img := index .root.Values.images .name -}}
{{ $img.repository }}@{{ $img.digest }}
{{- end }}
