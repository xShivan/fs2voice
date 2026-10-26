package main

// --fix-durations: raise the timings written into message SEXPs so that no
// voiced line is cut short by the next one.
//
//   - send-message-list-alt / send-message-chain-alt: (msg, duration) pairs;
//     the Lua sends msg and then waits |duration| ms. A negative duration
//     marks an alternative, so the sign is kept.
//   - send-message-list / send-message-chain: (sender, priority, msg, delay)
//     quads, where delay counts from the start of the previous message. A
//     message's audio therefore constrains the delay of the message after it.
//
// A checked number shorter than the audio is raised to the audio length rounded
// up to whole seconds plus one second. A time of <= 1 ms on the last step of a
// -alt list is an end marker (nothing follows) and is left alone.
//
// The events and ship cues anchored to a lengthened list are moved by
// timeline.go.

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// durEdit is one number changed in a mission file.
type durEdit struct {
	Event   string // +Name: of the event holding the SEXP
	Op      string // SEXP operator
	MsgName string // message whose audio sets the requirement
	Next    string // stock lists: the message whose delay is changed
	Wave    string // voice file used for the measurement (base name)
	AudioMs int64
	Old     string
	New     string
	Line    int // 1-based file line of the number
	Col     int // 0-based byte column of the number

	// Dependent edits (timeline.go): a delay that references the event whose
	// message list was lengthened.
	Dependent bool
	Where     string // section and name holding the reference
	Note      string // which part of the dialogue the delay was anchored to
}

// durWarn is a timing that could not be checked.
type durWarn struct {
	Event   string
	MsgName string
	Text    string
}

type durResult struct {
	File    string
	Edits   []durEdit
	Warns   []durWarn
	Missing int // timings skipped because the voice file is not recorded yet
	Lists   []*altList
	Refs    []depRef // every delay that refers to an event (for the chain report)
}

// durChecker measures voice files, caching results across missions.
type durChecker struct {
	voice voiceIndex
	cache map[string]durMeasure
}

type durMeasure struct {
	ms  int64
	err error
}

func newDurChecker(voice voiceIndex) *durChecker {
	return &durChecker{voice: voice, cache: map[string]durMeasure{}}
}

func (c *durChecker) measure(path string) (int64, error) {
	if r, ok := c.cache[path]; ok {
		return r.ms, r.err
	}
	ms, err := audioDuration(path)
	c.cache[path] = durMeasure{ms, err}
	return ms, err
}

// fixDurations finds every timing in m that is shorter than the audio of the
// message it belongs to. It does not modify the file; see applyEdits. For the
// -alt lists it also records each list's old and new timeline in res.Lists, so
// that timeline.go can move the events anchored to it.
func (c *durChecker) fixDurations(m *mission) durResult {
	res := durResult{File: m.File}
	for _, ev := range m.Events {
		walk(ev.Formula, func(n *node) {
			a := n.Items[1:]
			switch op := n.head(); op {
			case "send-message-list-alt", "send-message-chain-alt":
				start := 0
				if op == "send-message-chain-alt" {
					start = 1 // cancel event
				}
				c.checkAltList(m, &res, ev.Name, op, a[start:])
			case "send-message-list", "send-message-chain":
				start := 0
				if op == "send-message-chain" {
					start = 1 // cancel event
				}
				// The delay of the next quad must cover this quad's audio.
				before := len(res.Edits)
				for i := start; i+7 < len(a); i += 4 {
					if skipMsgName(a[i+2]) {
						continue
					}
					c.check(m, &res, ev.Name, op, strings.TrimSpace(a[i+2].Value), atom(a[i+6]), a[i+7], false)
				}
				if len(res.Edits) > before {
					res.Warns = append(res.Warns, durWarn{Event: ev.Name, Text: "stock message list lengthened - events anchored to it are not moved (only -alt lists are)"})
				}
			}
		})
	}
	return res
}

// checkAltList checks the (msg, duration) pairs of one -alt list and records
// its timeline. A negative duration is an alternative of the previous step.
func (c *durChecker) checkAltList(m *mission, res *durResult, evName, op string, pairs []*node) {
	type pair struct {
		msg, dur *node
		step     int
	}
	var ps []pair
	steps := 0
	for i := 0; i+1 < len(pairs); i += 2 {
		p := pair{msg: pairs[i], dur: pairs[i+1]}
		alt := !p.dur.isList() && strings.HasPrefix(strings.TrimSpace(p.dur.Value), "-") && steps > 0
		if !alt {
			steps++
		}
		p.step = steps - 1
		ps = append(ps, p)
	}
	lst := &altList{Event: evName, Steps: make([]altStep, steps)}
	for _, p := range ps {
		old, nw := int64(0), int64(0)
		ok := true
		if skipMsgName(p.msg) {
			// not a message: just read the number for the timeline
			v, err := strconv.ParseInt(strings.TrimSpace(atom(p.dur)), 10, 64)
			if err != nil {
				ok = false
			}
			if v < 0 {
				v = -v
			}
			old, nw = v, v
		} else {
			old, nw, ok = c.check(m, res, evName, op, strings.TrimSpace(p.msg.Value), "", p.dur, p.step == steps-1)
		}
		if !ok {
			lst.Bad = true
			continue
		}
		st := &lst.Steps[p.step]
		if !st.set { // the first candidate (the positive one) defines the step
			st.Old, st.New, st.set = old, nw, true
		}
	}
	if steps > 0 {
		res.Lists = append(res.Lists, lst)
	}
}

// check compares the number in durNode against the audio length of msgName and
// queues an edit when it is shorter. It returns the number's absolute value
// before and after the edit; ok is false when the number could not be read.
// endMarker marks the last step of a conversation: a time of <= 1 ms there
// means "nothing follows" and is kept.
func (c *durChecker) check(m *mission, res *durResult, evName, op, msgName, next string, durNode *node, endMarker bool) (oldAbs, newAbs int64, ok bool) {
	warn := func(format string, args ...any) {
		res.Warns = append(res.Warns, durWarn{Event: evName, MsgName: msgName, Text: fmt.Sprintf(format, args...)})
	}
	if durNode.isList() {
		warn("timing is an expression, not a number - skipped")
		return 0, 0, false
	}
	old := strings.TrimSpace(durNode.Value)
	v, err := strconv.ParseInt(old, 10, 64)
	if err != nil {
		warn("timing %q is not a number - skipped", old)
		return 0, 0, false
	}
	neg := v < 0
	if neg {
		v = -v
	}
	oldAbs, newAbs, ok = v, v, true

	msg := m.Messages[msgName]
	if msg == nil || waveToFilename(msg.Wave) == "TODO" {
		return // nothing recorded for this line
	}
	path := c.voice.lookup(msg.Wave)
	if path == "" {
		res.Missing++
		return
	}
	ms, err := c.measure(path)
	if err != nil {
		warn("%s: %v", msg.Wave, err)
		return
	}
	if v >= ms || (endMarker && v <= 1) {
		return
	}
	nv := ceilDiv(ms, 1000)*1000 + 1000 // audio rounded up + 1 s
	newAbs = nv
	if neg {
		nv = -nv
	}
	col := durNode.Col
	if durNode.Quoted {
		col++
	}
	res.Edits = append(res.Edits, durEdit{
		Event:   evName,
		Op:      op,
		MsgName: msgName,
		Next:    next,
		Wave:    baseName(path),
		AudioMs: ms,
		Old:     durNode.Value,
		New:     strconv.FormatInt(nv, 10),
		Line:    durNode.Line,
		Col:     col,
	})
	return
}

func baseName(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[i+1:]
	}
	return path
}

// applyEdits rewrites the numbers in raw in place, leaving every other byte
// (CRLF line ends, CP-1252 text) untouched. It fails without changing anything
// if a number is not where the parser saw it.
func applyEdits(raw []byte, edits []durEdit) ([]byte, error) {
	lines := strings.Split(string(raw), "\n")
	sorted := append([]durEdit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Line != sorted[j].Line {
			return sorted[i].Line < sorted[j].Line
		}
		return sorted[i].Col > sorted[j].Col // right to left within a line
	})
	for _, e := range sorted {
		if e.Line < 1 || e.Line > len(lines) {
			return nil, fmt.Errorf("line %d out of range", e.Line)
		}
		ln := lines[e.Line-1]
		end := e.Col + len(e.Old)
		if e.Col < 0 || end > len(ln) || ln[e.Col:end] != e.Old {
			return nil, fmt.Errorf("line %d col %d: expected %q", e.Line, e.Col+1, e.Old)
		}
		lines[e.Line-1] = ln[:e.Col] + e.New + ln[end:]
	}
	return []byte(strings.Join(lines, "\n")), nil
}

// renderDurationReport lists the changes per mission and event, then warnings.
func renderDurationReport(results []durResult, dryRun bool) string {
	var b strings.Builder
	verb := "updated"
	if dryRun {
		b.WriteString("# DRY RUN - mission files were not modified\n\n")
		verb = "would be updated"
	}
	edits, missions, warns, missing := 0, 0, 0, 0
	chainsHeader := false
	for _, r := range results {
		warns += len(r.Warns)
		missing += r.Missing
		if len(r.Edits) == 0 {
			continue
		}
		missions++
		edits += len(r.Edits)
		fmt.Fprintln(&b, r.File)
		lastEv := "\x00"
		for _, e := range r.Edits {
			if e.Dependent {
				continue
			}
			if e.Event != lastEv {
				fmt.Fprintf(&b, "  [%s]\n", e.Event)
				lastEv = e.Event
			}
			msg := fmt.Sprintf("%q", e.MsgName)
			if e.Next != "" {
				msg += fmt.Sprintf(" -> delay of next %q", e.Next)
			}
			fmt.Fprintf(&b, "    %s  %s  line %d\n", e.Op, msg, e.Line)
			fmt.Fprintf(&b, "      %s  %.2fs   %s -> %s\n", e.Wave, float64(e.AudioMs)/1000, e.Old, e.New)
		}
		b.WriteByte('\n')
		b.WriteString(renderDependents(r))
	}
	for _, r := range results {
		if c := renderChains(r); c != "" {
			if !chainsHeader {
				b.WriteString("Dependency chains (delays are relative, so each dependant moves along with its own source):\n")
				chainsHeader = true
			}
			b.WriteString(c)
		}
	}
	if warns > 0 {
		b.WriteString("Warnings:\n")
		for _, r := range results {
			for _, w := range r.Warns {
				fmt.Fprintf(&b, "  %s [%s] %q: %s\n", r.File, w.Event, w.MsgName, w.Text)
			}
		}
		b.WriteByte('\n')
	}
	if missing > 0 {
		b.WriteString("Not checked - voice file not in the voice dir yet:\n")
		for _, r := range results {
			if r.Missing > 0 {
				fmt.Fprintf(&b, "  %-20s %3d\n", r.File, r.Missing)
			}
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "Summary: %d values %s in %d missions (%d warnings, %d not checked).\n", edits, verb, missions, warns, missing)
	return b.String()
}
