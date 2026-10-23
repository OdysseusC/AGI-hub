package app

import (
	_ "embed"
	"strings"
)

// Keep the complete dashboard in the loopback page: no CDN, build step or
// additional unauthenticated asset routes are needed.
//
//go:embed web/index.html
var dashboardHTML string

//go:embed web/dashboard.css
var dashboardCSS string

//go:embed web/dashboard.js
var dashboardJS string

var dashboard = strings.NewReplacer(
	"/* HUB_STYLES */", dashboardCSS,
	"/* HUB_SCRIPT */", dashboardJS,
).Replace(dashboardHTML)
