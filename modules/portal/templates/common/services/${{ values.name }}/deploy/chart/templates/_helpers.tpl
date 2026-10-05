{{- define "app.labels" -}}
app.kubernetes.io/part-of: {{ .Chart.Name }}
app.kubernetes.io/component: ${{ values.component }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "app.selector" -}}
app.kubernetes.io/part-of: {{ .Chart.Name }}
app.kubernetes.io/component: ${{ values.component }}
{{- end }}
