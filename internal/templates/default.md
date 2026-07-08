---
title: "{{ .Title }}"
creator: "{{ .Creator }}"
date: "{{ .Date }}"
---

| Title | Artist | Album | Added |
|-------|--------|-------|-------|
{{ range .Tracks -}}
| {{ .Title }} | {{ .Artist }} | {{ .Album }} | {{ .AddedAt }} |
{{ end -}}
