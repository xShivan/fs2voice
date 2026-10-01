package main

import (
	"sort"
	"strings"
)

// entry is one line of the voice-acting script.
type entry struct {
	Sender   string
	Persona  string
	Text     string
	Filename string
	MsgName  string
	Note     string // "random variant", "alternative ...", "training message", or ""
	SrcLine  int
	Orphan   bool
	Section  string // secCmdBriefing / secBriefing / secMessages / secDebriefing
}

type resolver struct {
	m       *mission
	aliases map[string]string
}

// messageSexps are the operators that cause a line to be voiced.
var messageSexps = map[string]bool{
	"send-message":           true,
	"send-message-list":      true,
	"send-message-chain":     true,
	"send-random-message":    true,
	"send-message-alt":       true,
	"send-message-list-alt":  true,
	"send-message-chain-alt": true,
	"training-msg":           true,
}

func newResolver(m *mission) *resolver {
	r := &resolver{m: m, aliases: map[string]string{}}
	for _, f := range m.Formulas {
		walk(f, func(n *node) {
			switch n.head() {
			case "set-message-alias", "set-freetext-message-alias":
				a := n.Items[1:]
				if len(a) >= 2 && !a[0].isList() && !a[1].isList() {
					r.aliases[strings.TrimSpace(a[0].Value)] = strings.TrimSpace(a[1].Value)
				}
			}
		})
	}
	return r
}

// buildEntries walks the parsed formulas and produces the ordered, de-duplicated
// script for the mission, followed by orphan #Messages entries.
func buildEntries(m *mission, dedup, includeOrphans bool) []entry {
	r := newResolver(m)
	var raw []entry
	used := map[string]bool{}

	for _, f := range m.Formulas {
		walk(f, func(n *node) {
			if !messageSexps[n.head()] {
				return
			}
			for _, e := range r.fromCall(n) {
				used[e.MsgName] = true
				raw = append(raw, e)
			}
		})
	}

	sort.SliceStable(raw, func(i, j int) bool { return raw[i].SrcLine < raw[j].SrcLine })

	var out []entry
	at := map[string]int{} // MsgName -> index in out
	for _, e := range raw {
		if dedup && e.MsgName != "" {
			if idx, ok := at[e.MsgName]; ok {
				// Keep the occurrence with the more informative sender:
				// a standard send-message names the speaker explicitly,
				// whereas a malformed "-alt" reference may not.
				if out[idx].Sender == "" && e.Sender != "" {
					note := out[idx].Note
					out[idx] = e
					if out[idx].Note == "" {
						out[idx].Note = note
					}
				}
				continue
			}
			at[e.MsgName] = len(out)
		}
		out = append(out, e)
	}

	pre, post := briefingEntries(m)
	out = append(append(pre, out...), post...)

	if includeOrphans {
		var names []string
		for name := range m.Messages {
			if !used[name] {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			out = append(out, r.orphanEntry(m.Messages[name]))
		}
	}
	return out
}

// fromCall expands a single message SEXP call into script entries.
func (r *resolver) fromCall(n *node) []entry {
	a := n.Items[1:]
	switch n.head() {
	case "send-message":
		if len(a) < 3 {
			return nil
		}
		return []entry{r.standard(atom(a[0]), a[2])}

	case "training-msg":
		if len(a) < 1 || skipMsgName(a[0]) {
			return nil
		}
		e := r.standard("Training", a[0])
		e.Note = "training message"
		return []entry{e}

	case "send-message-list":
		return r.quads(a, 0, "")

	case "send-message-chain":
		return r.quads(a, 1, "") // a[0] is the cancel event

	case "send-random-message":
		if len(a) < 3 {
			return nil
		}
		sender := atom(a[0])
		var out []entry
		for _, msg := range a[2:] {
			e := r.standard(sender, msg)
			e.Note = "random variant"
			out = append(out, e)
		}
		return out

	case "send-message-alt":
		// a[0] = shuffle bool; then (msgname, condition) pairs.
		var out []entry
		for i := 1; i < len(a); i += 2 {
			if a[i].isList() || skipMsgName(a[i]) {
				continue
			}
			e := r.alt(a[i])
			e.Note = "alternative (chosen by conditions)"
			out = append(out, e)
		}
		return out

	case "send-message-list-alt":
		return r.altPairs(a, 0)

	case "send-message-chain-alt":
		return r.altPairs(a, 1) // a[0] is the cancel event
	}
	return nil
}

// quads expands repeating (sender, priority, msgname, delay) groups.
func (r *resolver) quads(a []*node, start int, _ string) []entry {
	var out []entry
	for i := start; i+2 < len(a); i += 4 {
		name := a[i+2]
		if skipMsgName(name) {
			continue
		}
		out = append(out, r.standard(atom(a[i]), name))
	}
	return out
}

// altPairs expands repeating (msgname, duration) groups; a negative duration
// marks the line as an alternative to the previous one.
func (r *resolver) altPairs(a []*node, start int) []entry {
	var out []entry
	for i := start; i+1 < len(a); i += 2 {
		name := a[i]
		if name.isList() || skipMsgName(name) {
			continue
		}
		e := r.alt(name)
		if dur := atom(a[i+1]); strings.HasPrefix(strings.TrimSpace(dur), "-") {
			e.Note = "alternative line"
		}
		out = append(out, e)
	}
	return out
}

func (r *resolver) standard(senderRaw string, msgNode *node) entry {
	name := strings.TrimSpace(msgNode.Value)
	sender := r.resolveSpecial(senderRaw)
	msg := r.m.Messages[name]
	return entry{
		Sender:   sender,
		Persona:  r.persona(msg, sender),
		Text:     textOf(msg),
		Filename: waveToFilename(waveOf(msg)),
		MsgName:  name,
		SrcLine:  msgNode.Line,
	}
}

func (r *resolver) alt(msgNode *node) entry {
	name := strings.TrimSpace(msgNode.Value)
	sender := r.resolveAltSender(name)
	msg := r.m.Messages[name]
	return entry{
		Sender:   sender,
		Persona:  r.persona(msg, sender),
		Text:     textOf(msg),
		Filename: waveToFilename(waveOf(msg)),
		MsgName:  name,
		SrcLine:  msgNode.Line,
	}
}

func (r *resolver) orphanEntry(msg *message) entry {
	sender := ""
	if strings.Contains(msg.Name, ":") {
		sender = r.resolveAltSender(msg.Name)
	}
	return entry{
		Sender:   sender,
		Persona:  r.persona(msg, sender),
		Text:     textOf(msg),
		Filename: waveToFilename(msg.Wave),
		MsgName:  msg.Name,
		Orphan:   true,
	}
}

// resolveSpecial turns "#Command"/"#Support"/... into a display name, strips the
// FS2 "name#suffix" display-name convention (e.g. "Amethes#Steelward" -> "Amethes"),
// and otherwise passes ship/wing names through unchanged.
func (r *resolver) resolveSpecial(raw string) string {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "#") {
		return displayName(s)
	}
	name := strings.TrimPrefix(s, "#")
	if strings.EqualFold(name, "Command") {
		if r.m.CommandSender != "" {
			return displayName(r.m.CommandSender)
		}
		return "Command"
	}
	return displayName(name)
}

// displayName applies the FS2 rule that a '#' (not in the first position) and
// everything after it is a disambiguation suffix, not part of the shown name.
func displayName(s string) string {
	if i := strings.Index(s, "#"); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// resolveAltSender derives the speaker for an "-alt" message from the part of
// its name before the first ':', resolved through the mission's alias map.
func (r *resolver) resolveAltSender(msgName string) string {
	i := strings.Index(msgName, ":")
	if i < 0 {
		// Not in "Source: Title" form — the speaker is undeterminable here
		// (in-engine the Lua would warn). Leave it for a standard call or a
		// human to fill in.
		return ""
	}
	prefix := strings.TrimSpace(msgName[:i])
	target := prefix
	if v, ok := r.aliases[prefix]; ok {
		target = v
	}
	return r.resolveSpecial(target)
}

func (r *resolver) persona(msg *message, sender string) string {
	if msg != nil && msg.Persona != "" {
		return msg.Persona
	}
	if sh, ok := r.m.Ships[sender]; ok {
		if sh.Persona != "" {
			return sh.Persona
		}
		if sh.Callsign != "" {
			return sh.Callsign
		}
	}
	return ""
}

// --- small helpers -------------------------------------------------------------

func atom(n *node) string {
	if n == nil || n.isList() {
		return ""
	}
	return n.Value
}

func skipMsgName(n *node) bool {
	if n.isList() {
		return true
	}
	v := strings.TrimSpace(n.Value)
	return v == "" || v == "-1"
}

func textOf(m *message) string {
	if m == nil {
		return "(no matching #Messages entry)"
	}
	if m.Text == "" {
		return "(empty text)"
	}
	return m.Text
}

func waveOf(m *message) string {
	if m == nil {
		return ""
	}
	return m.Wave
}

func waveToFilename(w string) string {
	w = strings.TrimSpace(w)
	lw := strings.ToLower(w)
	if w == "" || lw == "none" || lw == "msg.wav" || lw == "message.wav" || lw == "msg.ogg" ||
		strings.HasPrefix(lw, "none.") {
		return "TODO"
	}
	if i := strings.LastIndex(w, "."); i >= 0 {
		w = w[:i]
	}
	return w + ".ogg"
}
