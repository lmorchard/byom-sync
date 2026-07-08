---
title: "{{ .Title }}"
creator: "{{ .Creator }}"
date: "{{ .Date }}"
---

| Title | Artist | Album | Added |
|-------|--------|-------|-------|
{{ range .Tracks -}}
| {{ if .SpotifyURL }}[{{ .Title }}]({{ .SpotifyURL }}){{ else }}{{ .Title }}{{ end }} | {{ .Artist }} | {{ .Album }} | {{ .AddedAt }} |
{{ end -}}
