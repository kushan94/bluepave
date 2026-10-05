{{/* The label app onboarding puts on every app namespace; its value is the app's name. */}}
{{- define "kyverno.appLabel" -}}platform.bluepave.dev/app{{- end -}}

{{/* The environment's registry login server (registry module output), or "" before `bluepave up`. */}}
{{- define "kyverno.registry" -}}
{{- (index .Values.bluepave.discovered "registry" | default dict).containerRegistryLoginServer | default "" -}}
{{- end -}}

{{/*
Certificate identities trusted to sign app images: the platform repository's golden-path workflow
(build-app.yml) at main or a release tag. The certificate names the workflow, not the calling app
repository, so one identity covers every app (ADR-0001).
*/}}
{{- define "kyverno.signerSubject" -}}
{{- $gh := .Values.bluepave.github -}}
{{- $refs := .Values.settings.signerRefRegExp | default "refs/(heads/main|tags/v[0-9]+(\\.[0-9]+)*)" -}}
^https://github\.com/{{ regexQuoteMeta $gh.owner }}/{{ regexQuoteMeta $gh.platformRepo }}/\.github/workflows/build-app\.yml@{{ $refs }}$
{{- end -}}
