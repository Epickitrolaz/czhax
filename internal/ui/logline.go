package ui

import (
	"regexp"
	"strings"
)

// ansiEscape matches CSI/OSC and two-byte escape sequences. Helper tools such as
// hcxdumptool render a full-screen status display with cursor moves and colour
// codes; rendering those through lipgloss makes the terminal execute them and
// scribbles over the whole TUI, so they are stripped before display.
var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]|\x1b\\][^\x07]*\x07|\x1b[()][A-Za-z0-9]|\x1b[@-_]")

// maxLogLineLen bounds one stored log line so a runaway tool cannot blow up the
// view buffer.
const maxLogLineLen = 160

// sanitizeLogLine turns raw child-process output into a single printable line:
// carriage-return redraws collapse to their last frame, ANSI escapes and control
// characters are removed, and the result is length-capped.
func sanitizeLogLine(s string) string {
	if idx := strings.LastIndexByte(s, '\r'); idx >= 0 {
		s = s[idx+1:]
	}
	s = ansiEscape.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f:
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if len(s) > maxLogLineLen {
		s = s[:maxLogLineLen]
	}
	return s
}
