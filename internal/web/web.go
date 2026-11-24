// Package web holds Printty's embedded UI: HTML templates and static assets.
package web

import "embed"

//go:embed templates/*.html
var Templates embed.FS

//go:embed static
var Static embed.FS
