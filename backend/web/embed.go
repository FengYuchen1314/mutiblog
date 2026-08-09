package web

import "embed"

// Admin contains the Vite build emitted into web/admin by the frontend build.
//go:embed admin/*
var Admin embed.FS
