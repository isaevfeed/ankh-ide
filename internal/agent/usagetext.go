package agent

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	ansiRe    = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[()][A-Za-z0-9]|\x1b[=>78]`)
	sessionRe = regexp.MustCompile(`(?is)current session(.{0,600}?)(\d{1,3}(?:\.\d+)?)\s*%\s*used`)
	weekRe    = regexp.MustCompile(`(?is)current week(.{0,600}?)(\d{1,3}(?:\.\d+)?)\s*%\s*used`)
	resetsRe  = regexp.MustCompile(`(?i)^\s*resets\s+(.{1,60}?)(?:\s{2,}|current|$)`)
)

// StripANSI removes terminal escape sequences; cursor moves become spaces so
// words do not glue together.
func StripANSI(s string) string {
	s = ansiRe.ReplaceAllString(s, " ")
	return strings.Map(func(r rune) rune {
		if r == '\r' {
			return '\n'
		}
		if r < 32 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
}

// ParseUsageText reads the output of Claude Code's /usage. It returns nil
// when no percentages are found.
func ParseUsageText(raw string) *Usage {
	t := StripANSI(raw)
	u := &Usage{Source: "usage"}
	if w := parseWindow(t, sessionRe); w != nil {
		u.FiveHour = w
	}
	if w := parseWindow(t, weekRe); w != nil {
		u.SevenDay = w
	}
	if u.FiveHour == nil && u.SevenDay == nil {
		return nil
	}
	return u
}

func parseWindow(t string, re *regexp.Regexp) *Window {
	loc := re.FindStringSubmatchIndex(t)
	if loc == nil {
		return nil
	}
	pct, err := strconv.ParseFloat(t[loc[4]:loc[5]], 64)
	if err != nil {
		return nil
	}
	w := &Window{UsedPercentage: pct}
	rest := t[loc[1]:]
	if i := strings.Index(strings.ToLower(rest), "resets"); i >= 0 && i < 200 {
		if m := resetsRe.FindStringSubmatch(rest[i:]); m != nil {
			w.ResetsText = strings.Join(strings.Fields(m[1]), " ")
		}
	}
	return w
}
