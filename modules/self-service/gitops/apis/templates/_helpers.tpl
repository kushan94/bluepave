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

{{/* Whether AppDatabase is offered: the setting, else on when data-postgres is. */}}
{{- define "ss.appDatabase" -}}
{{- $apis := .Values.settings.apis | default dict -}}
{{- if hasKey $apis "appDatabase" -}}
{{- $apis.appDatabase -}}
{{- else -}}
{{- has "data-postgres" .Values.bluepave.modules -}}
{{- end -}}
{{- end -}}
