package main

import (
	"fmt"
	"regexp"
	"strings"
)

// briefStage is one voiced stage of a command briefing, briefing or debriefing.
type briefStage struct {
	Text string
	Wave string // $voice: / $Voice: / +Wave Filename: value, verbatim
	Cond string // debriefing only: human-readable stage condition, or ""
}

// Entry sections, in the order they appear in the rendered script.
const (
	secCmdBriefing = "cbrief"
	secBriefing    = "brief"
	secMessages    = ""
	secDebriefing  = "debrief"
)

var sectionTitles = map[string]string{
	secCmdBriefing: "COMMAND BRIEFING",
	secBriefing:    "BRIEFING",
	secMessages:    "MISSION MESSAGES",
	secDebriefing:  "DEBRIEFING",
}

var (
	reColorTag = regexp.MustCompile(`\$[A-Za-z](\s+|$)`)
	markupRepl = strings.NewReplacer("$slash", "/", "$quote", `"`, "$semicolon", ";")
)

// stripMarkup removes FS2 briefing text markup: colour tags ("$b word") and the
// $slash / $quote / $semicolon escapes.
func stripMarkup(s string) string {
	s = markupRepl.Replace(s)
	s = reColorTag.ReplaceAllString(s, "")
	s = reWhitespac.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// firstField returns the value of the first key that prefixes line.
func firstField(line string, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := fieldValue(line, k); ok {
			return v, true
		}
	}
	return "", false
}

// multiText collects a text block that starts with first (the remainder of the
// key line) and ends at $end_multi_text. It returns the cleaned text and the
// index of the terminating line.
func multiText(lines []string, i int, first string) (string, int) {
	buf := []string{first}
	j := i + 1
	for ; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "$end_multi_text" {
			break
		}
		buf = append(buf, lines[j])
	}
	return stripMarkup(extractMessageText(strings.Join(buf, "\n"))), j
}

func (m *mission) parseCmdBriefing(lines []string) {
	for i := 0; i < len(lines); i++ {
		ln := lines[i]
		if v, ok := fieldValue(ln, "$Stage Text:"); ok {
			var st briefStage
			st.Text, i = multiText(lines, i, v)
			m.CmdBriefing = append(m.CmdBriefing, st)
			continue
		}
		if v, ok := fieldValue(ln, "+Wave Filename:"); ok && len(m.CmdBriefing) > 0 {
			m.CmdBriefing[len(m.CmdBriefing)-1].Wave = stripInlineComment(v)
		}
	}
}

func (m *mission) parseBriefing(lines []string) {
	var cur *briefStage
	inIcon := false
	for i := 0; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		switch {
		case t == "$start_briefing":
			m.Briefings = append(m.Briefings, nil)
			cur = nil
			continue
		case t == "$start_stage":
			if len(m.Briefings) == 0 {
				m.Briefings = append(m.Briefings, nil)
			}
			b := &m.Briefings[len(m.Briefings)-1]
			*b = append(*b, briefStage{})
			cur = &(*b)[len(*b)-1]
			inIcon = false
			continue
		case t == "$end_stage":
			cur = nil
			continue
		case t == "$start_icon":
			inIcon = true
			continue
		case t == "$end_icon":
			inIcon = false
			continue
		}
		if cur == nil {
			continue
		}
		if v, ok := fieldValue(t, "$multi_text"); ok {
			// Icon descriptions share the tag but are not voiced.
			text, j := multiText(lines, i, v)
			if !inIcon {
				cur.Text = text
			}
			i = j
			continue
		}
		if v, ok := firstField(t, "$voice:", "$Voice:"); ok {
			cur.Wave = stripInlineComment(v)
		}
	}
}

func (m *mission) parseDebriefing(lines []string) {
	cur := -1
	for i := 0; i < len(lines); i++ {
		ln := lines[i]
		if idx := strings.Index(ln, "$Formula:"); idx >= 0 {
			rest := ln[idx+len("$Formula:"):]
			var b strings.Builder
			b.WriteString(rest)
			b.WriteByte('\n')
			depth, inStr := parenDelta(rest, 0, false)
			for depth > 0 && i+1 < len(lines) {
				i++
				b.WriteString(lines[i])
				b.WriteByte('\n')
				depth, inStr = parenDelta(lines[i], depth, inStr)
			}
			m.Debriefing = append(m.Debriefing, briefStage{Cond: describeCond(parseSexps(b.String(), 1))})
			cur = len(m.Debriefing) - 1
			continue
		}
		if cur < 0 {
			continue
		}
		if v, ok := firstField(ln, "$Multi text", "$multi_text"); ok {
			m.Debriefing[cur].Text, i = multiText(lines, i, v)
			continue
		}
		if v, ok := fieldValue(ln, "$Recommendation text:"); ok {
			_, i = multiText(lines, i, v) // not voiced
			continue
		}
		if v, ok := firstField(ln, "$Voice:", "$voice:"); ok {
			m.Debriefing[cur].Wave = stripInlineComment(v)
		}
	}
}

// describeCond renders a debriefing stage formula for the Note: field: the
// event name for the common is-event-true(-delay) form, "" for ( true ), and
// the flattened SEXP otherwise.
func describeCond(forms []*node) string {
	if len(forms) == 0 {
		return ""
	}
	f := forms[0]
	switch f.head() {
	case "true":
		return ""
	case "is-event-true", "is-event-true-delay":
		if len(f.Items) > 1 && !f.Items[1].isList() {
			return f.Items[1].Value
		}
	}
	return flattenSexp(f)
}

func flattenSexp(n *node) string {
	if !n.isList() {
		if n.Quoted {
			return `"` + n.Value + `"`
		}
		return n.Value
	}
	parts := make([]string, len(n.Items))
	for i, c := range n.Items {
		parts[i] = flattenSexp(c)
	}
	return "( " + strings.Join(parts, " ") + " )"
}

// briefingEntries turns the parsed briefings into script entries: pre goes
// before the mission messages, post (the debriefing) after them.
func briefingEntries(m *mission) (pre, post []entry) {
	for i, st := range m.CmdBriefing {
		pre = append(pre, stageEntry(st, secCmdBriefing, "Command Briefing", fmt.Sprintf("cmd-brief stage %d", i+1)))
	}
	for t, stages := range m.Briefings {
		for i, st := range stages {
			ref := fmt.Sprintf("brief stage %d", i+1)
			if len(m.Briefings) > 1 {
				ref = fmt.Sprintf("brief team %d stage %d", t+1, i+1)
			}
			pre = append(pre, stageEntry(st, secBriefing, "Briefing", ref))
		}
	}
	for i, st := range m.Debriefing {
		e := stageEntry(st, secDebriefing, "Debriefing", fmt.Sprintf("debrief stage %d", i+1))
		if st.Cond != "" {
			e.Note = "when " + st.Cond
		}
		post = append(post, e)
	}
	return pre, post
}

func stageEntry(st briefStage, section, sender, ref string) entry {
	text := st.Text
	if text == "" {
		text = "(empty text)"
	}
	return entry{
		Sender:   sender,
		Text:     text,
		Filename: waveToFilename(st.Wave),
		MsgName:  ref,
		Section:  section,
	}
}
