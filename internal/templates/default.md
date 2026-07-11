---
title: "{{ .Title }}"
creator: "{{ .Creator }}"
date: "{{ .Date }}"
updated: "{{ .Updated }}"
---

| Title | Artist | Album | Added |
|-------|--------|-------|-------|
{{ range .Tracks -}}
| {{ if .SpotifyURL }}[{{ .Title }}]({{ .SpotifyURL }}){{ else }}{{ .Title }}{{ end }} | {{ .Artist }} | {{ .Album }} | {{ .AddedAt }} |
{{ end -}}
