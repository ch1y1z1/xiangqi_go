package ui

import native "github.com/egoist/mygo/ui"

func outlineIcon(shapes string) *native.SVG {
	return native.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`))
}

var (
	iconSidebar  = outlineIcon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16"/>`)
	iconTools    = outlineIcon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M15 4v16"/>`)
	iconNew      = outlineIcon(`<path d="M12 5v14M5 12h14"/>`)
	iconSettings = outlineIcon(`<path d="M4 7h7m6 0h3M4 17h3m6 0h7"/><circle cx="14" cy="7" r="3"/><circle cx="10" cy="17" r="3"/>`)
	iconImage    = outlineIcon(`<rect x="3" y="3" width="18" height="18" rx="3"/><circle cx="8" cy="8" r="1.5"/><path d="m3 16 5-5 4 4 3-3 6 6"/>`)
	iconBoard    = outlineIcon(`<rect x="4" y="3" width="16" height="18" rx="2"/><path d="M9 3v18m6-18v18M4 9h16M4 15h16"/>`)
)
