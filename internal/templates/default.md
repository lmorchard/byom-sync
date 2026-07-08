---
title: "{{ .Title }}"
creator: "{{ .Creator }}"
date: "{{ .Date }}"
---

| Title | Artist | Album |
|-------|--------|-------|
{{ range .Tracks -}}
| {{ .Title }} | {{ .Artist }} | {{ .Album }} |
{{ end -}}
