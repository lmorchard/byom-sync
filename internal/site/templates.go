package site

import "embed"

//go:embed templates/*.html assets/*
var embedded embed.FS
