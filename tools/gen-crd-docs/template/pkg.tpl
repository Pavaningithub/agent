{{ define "packages" }}

{{ with .packages}}
---
aliases:
- /docs/agent/latest/operator/crd/
title: Custom Resource Definition Reference
description: Learn about the Grafana Agent API
weight: 500
---
# Custom Resource Definition Reference
{{ end}} 

{{ range .packages }}
{{ with (index .GoPackages 0 )}}
{{ with .DocComments }}
{{ . }}
{{ end }} 
{{ end }} 

## Resource Types:
{{ range (visibleTypes (sortedTypes .Types)) }} 
{{ if isExportedType . -}}
* [{{ typeDisplayName . }}]({{ linkForType . }}) 
{{- end }} 
{{ end }}

{{ range (visibleTypes (sortedTypes .Types))}} 
{{ template "type" . }} 
{{ end }}
{{ end }}
{{ end }}
