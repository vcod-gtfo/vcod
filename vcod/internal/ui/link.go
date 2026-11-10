package ui

import "strings"

// Link renders label as an OSC 8 terminal hyperlink to url. When escapes are off the url
// is printed after the label, so redirected output still carries it.
func Link(url, label string) string {
	if !colorEnabled {
		return label + "  " + url
	}
	return "\x1b]8;;" + url + "\x1b\\" + StyleLink.Render(label) + "\x1b]8;;\x1b\\"
}

// LinkLabel strips a Link back to its visible text.
func LinkLabel(s string) string {
	for {
		start := strings.Index(s, "\x1b]8;;")
		if start < 0 {
			return s
		}
		end := strings.Index(s[start:], "\x1b\\")
		if end < 0 {
			return s
		}
		s = s[:start] + s[start+end+2:]
	}
}
