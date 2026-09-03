package main

import (
	"regexp"
	"strings"
)

// message is one #Messages entry.
type message struct {
	Name    string
	Text    string
	Persona string // +Persona: value, if present
	Wave    string // +Wave Name: value, verbatim (may be "" or a placeholder)
}

// shipInfo is the relevant subset of a #Objects entry.
type shipInfo struct {
	Callsign string
	Persona  string // +Persona Name:
}

// mission holds everything parsed from a single .fs2 file that the script
// generator needs.
type mission struct {
	File          string // base filename, e.g. "tvw-v01.fs2"
	CommandSender string // $Command Sender: override, or "" (=> "Command")
	Messages      map[string]*message
	Ships         map[string]*shipInfo
	Formulas      []*node // parsed $Formula: SEXP trees from #Events
}

var (
	reXstr      = regexp.MustCompile(`(?s)XSTR\s*\(\s*"(.*)"\s*,\s*-?\d+\s*\)`)
	reWhitespac = regexp.MustCompile(`\s+`)
)

// parseMission parses the raw bytes of a .fs2 file.
func parseMission(file string, raw []byte) *mission {
	m := &mission{
		File:     file,
		Messages: map[string]*message{},
		Ships:    map[string]*shipInfo{},
	}
	// Normalise line endings and split. FS2 files are ASCII / CP-1252; we keep
	// bytes as-is (the text we care about is ASCII).
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	lines := strings.Split(text, "\n")

	// First pass: slice the file into sections with a small state machine that
	// ignores '#' lines occurring inside a message's multi-line text block.
	section := ""
	inMultiText := false
	var msgLines, objLines, evtLines []string
	for _, ln := range lines {
		trimmed := strings.TrimRight(ln, " \t")
		if !inMultiText && strings.HasPrefix(trimmed, "#") {
			section = sectionName(trimmed)
			continue
		}
		switch {
		case strings.HasPrefix(strings.TrimSpace(ln), "$MessageNew:"),
			strings.HasPrefix(strings.TrimSpace(ln), "$Message:"):
			inMultiText = true
		case strings.TrimSpace(ln) == "$end_multi_text":
			inMultiText = false
		}
		switch section {
		case "Messages":
			msgLines = append(msgLines, ln)
		case "Objects":
			objLines = append(objLines, ln)
		case "Events":
			evtLines = append(evtLines, ln)
		}
	}

	m.parseMessages(msgLines)
	m.parseObjects(objLines)
	m.parseEvents(evtLines)
	return m
}

// sectionName maps a '#...' header line to a short key we care about.
func sectionName(headerLine string) string {
	h := strings.ToLower(strings.TrimSpace(headerLine))
	h = strings.TrimPrefix(h, "#")
	// Strip trailing comment (";! 113 total").
	if i := strings.IndexAny(h, ";"); i >= 0 {
		h = strings.TrimSpace(h[:i])
	}
	switch h {
	case "messages":
		return "Messages"
	case "objects":
		return "Objects"
	case "events":
		return "Events"
	}
	return "other"
}

func fieldValue(line, key string) (string, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, key) {
		return "", false
	}
	v := strings.TrimSpace(t[len(key):])
	return v, true
}

// stripInlineComment removes a trailing ";..." comment and surrounding space.
func stripInlineComment(s string) string {
	if i := strings.Index(s, ";"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func (m *mission) parseMessages(lines []string) {
	// Header block before the first "$Name:" may carry "$Command Sender:".
	var cur *message
	var textBuf []string
	collecting := false

	flush := func() {
		if cur == nil {
			return
		}
		if collecting {
			cur.Text = extractMessageText(strings.Join(textBuf, "\n"))
		}
		if cur.Name != "" {
			m.Messages[cur.Name] = cur
		}
		cur = nil
		textBuf = nil
		collecting = false
	}

	for _, ln := range lines {
		if v, ok := fieldValue(ln, "$Command Sender:"); ok && cur == nil {
			m.CommandSender = stripInlineComment(v)
			continue
		}
		if v, ok := fieldValue(ln, "$Name:"); ok {
			flush()
			cur = &message{Name: stripInlineComment(v)}
			continue
		}
		if cur == nil {
			continue
		}
		if collecting {
			if strings.TrimSpace(ln) == "$end_multi_text" {
				cur.Text = extractMessageText(strings.Join(textBuf, "\n"))
				collecting = false
				continue
			}
			textBuf = append(textBuf, ln)
			continue
		}
		if v, ok := fieldValue(ln, "$MessageNew:"); ok {
			textBuf = []string{v}
			collecting = true
			continue
		}
		if v, ok := fieldValue(ln, "$Message:"); ok {
			// Old single-line form; occasionally still terminated by
			// $end_multi_text, so start collecting too.
			textBuf = []string{v}
			collecting = true
			continue
		}
		if v, ok := fieldValue(ln, "+Persona:"); ok {
			cur.Persona = stripInlineComment(v)
			continue
		}
		if v, ok := fieldValue(ln, "+Wave Name:"); ok {
			cur.Wave = stripInlineComment(v)
			continue
		}
	}
	flush()
}

// extractMessageText unwraps XSTR("...", id) and normalises whitespace.
func extractMessageText(raw string) string {
	raw = strings.TrimSpace(raw)
	if mtch := reXstr.FindStringSubmatch(raw); mtch != nil {
		raw = mtch[1]
	}
	raw = strings.ReplaceAll(raw, "\n", " ")
	raw = reWhitespac.ReplaceAllString(raw, " ")
	return strings.TrimSpace(raw)
}

func (m *mission) parseObjects(lines []string) {
	var cur *shipInfo
	var curName string
	flush := func() {
		if cur != nil && curName != "" {
			m.Ships[curName] = cur
		}
		cur = nil
		curName = ""
	}
	for _, ln := range lines {
		if v, ok := fieldValue(ln, "$Name:"); ok {
			flush()
			curName = stripInlineComment(v)
			cur = &shipInfo{}
			continue
		}
		if cur == nil {
			continue
		}
		if v, ok := fieldValue(ln, "$Callsign:"); ok {
			cur.Callsign = stripInlineComment(v)
			continue
		}
		if v, ok := fieldValue(ln, "+Persona Name:"); ok {
			cur.Persona = stripInlineComment(v)
			continue
		}
	}
	flush()
}

// parseEvents collects each "$Formula:" S-expression from the #Events section by
// brace balancing (ignoring parens inside quoted strings), then parses it.
func (m *mission) parseEvents(lines []string) {
	i := 0
	for i < len(lines) {
		ln := lines[i]
		idx := strings.Index(ln, "$Formula:")
		if idx < 0 {
			i++
			continue
		}
		startLine := i + 1 // 1-based within the events slice; good enough for ordering
		rest := ln[idx+len("$Formula:"):]
		var b strings.Builder
		b.WriteString(rest)
		b.WriteByte('\n')
		depth, inStr := parenDelta(rest, 0, false)
		j := i + 1
		for depth > 0 && j < len(lines) {
			b.WriteString(lines[j])
			b.WriteByte('\n')
			depth, inStr = parenDelta(lines[j], depth, inStr)
			j++
		}
		forms := parseSexps(b.String(), startLine)
		m.Formulas = append(m.Formulas, forms...)
		i = j
	}
}

// parenDelta returns the running paren depth and in-string state after scanning s.
func parenDelta(s string, depth int, inStr bool) (int, bool) {
	for k := 0; k < len(s); k++ {
		c := s[k]
		if inStr {
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '(':
			depth++
		case ')':
			depth--
		case ';':
			return depth, inStr // rest of line is a comment
		}
	}
	return depth, inStr
}
