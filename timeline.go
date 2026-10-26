package main

// Timeline of a -alt message list and the events anchored to it.
//
// A list starts when its event fires; step i starts S_i ms later, S_i being the
// sum of the durations of the steps before it. When --fix-durations lengthens
// some durations, everything that was timed against the dialogue has to move
// with it:
//
//   - is-event-true-delay / is-event-false-delay / ...-msecs-delay "Event" N,
//     anywhere in the file (other events, ship and wing arrival / departure
//     cues, goals);
//   - "+Chained: N" of the event that follows the dialogue event in the file.
//
// A reference at time T keeps its place in the conversation: it moves by the
// shift of the step that was playing at T (T' = T + S'_k - S_k, k = last step
// started at or before T; a reference after the end moves by the whole change).
//
// Every such delay counts from the moment its event fires, so an event that
// itself moves because of a lengthened dialogue (A -> B -> cue) drags its own
// dependants along; each one only needs the change of the list its reference
// points into. Nothing is shifted twice.

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// altStep is one step of a conversation (a message and its alternatives); its
// duration is that of the first candidate.
type altStep struct {
	Old, New int64 // ms
	set      bool
}

// altList is the timeline of one send-message-list-alt / -chain-alt call.
type altList struct {
	Event string
	Steps []altStep
	Bad   bool // a duration could not be read: the timeline is unreliable
}

func (l *altList) oldTotal() (t int64) {
	for _, s := range l.Steps {
		t += s.Old
	}
	return
}

func (l *altList) changed() bool {
	for _, s := range l.Steps {
		if s.Old != s.New {
			return true
		}
	}
	return false
}

// shift maps a time t (ms from the start of the list) in the old timeline to
// the same point of the conversation in the new one. The second result is the
// 1-based number of the step that was playing (len+1 = after the end).
func (l *altList) shift(t int64) (int64, int) {
	var so, sn int64 // S_k and S'_k
	k := 0
	for i, s := range l.Steps {
		if so+s.Old > t {
			break
		}
		so += s.Old
		sn += s.New
		k = i + 1
	}
	return t + sn - so, k + 1
}

// depRef is one place that refers to an event by a delay.
type depRef struct {
	Source string // the event referred to
	Op     string
	Where  string // "Events: name", "Objects: ship", ...
	Event  string // set when the reference sits in an event (chain of events)
	Delay  string // as written
}

var reDep = regexp.MustCompile(`\(\s*(is-event-(?:true|false)-(?:msecs-)?delay)\s+"([^"]*)"\s+([^\s()"]+|\()`)

// shiftDependents finds the delays that refer to an event whose message list
// was lengthened and adds the matching edits to res.
func shiftDependents(raw []byte, m *mission, res *durResult) {
	text := string(raw)

	// One timeline per event.
	lists := map[string]*altList{}
	multi := map[string]bool{}
	for _, l := range res.Lists {
		if l.Event == "" {
			continue
		}
		if prev, ok := lists[l.Event]; ok {
			multi[l.Event] = true
			if prev.oldTotal() >= l.oldTotal() {
				continue
			}
		}
		lists[l.Event] = l
	}
	anyChange := map[string]bool{}
	for _, l := range res.Lists {
		if l.changed() {
			anyChange[l.Event] = true
		}
	}
	for ev := range anyChange {
		l := lists[ev]
		for _, x := range res.Lists {
			if x.Event == ev && x.Bad {
				res.Warns = append(res.Warns, durWarn{Event: ev, Text: "a message list of this event has an unreadable timing - events anchored to it were not moved"})
				delete(lists, ev)
				l = nil
				break
			}
		}
		if l != nil && multi[ev] {
			res.Warns = append(res.Warns, durWarn{Event: ev, Text: "several message lists in one event - delays anchored to it follow the longest list"})
		}
	}

	lineStarts := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lineStarts = append(lineStarts, i+1)
		}
	}
	pos := func(off int) (line, col int) {
		i := sort.Search(len(lineStarts), func(i int) bool { return lineStarts[i] > off }) - 1
		return i + 1, off - lineStarts[i]
	}
	where := func(off int) (string, string) {
		head := text[:off]
		sec := "?"
		if i := strings.LastIndex(head, "\n#"); i >= 0 {
			end := strings.IndexAny(text[i+2:], "\r\n; \t")
			if end > 0 {
				sec = text[i+2 : i+2+end]
			}
		}
		if sec == "Events" {
			if i := strings.Index(text[off:], "+Name:"); i >= 0 {
				rest := text[off+i+len("+Name:"):]
				if e := strings.IndexAny(rest, "\r\n"); e >= 0 {
					rest = rest[:e]
				}
				n := strings.TrimSpace(rest)
				return sec + ": " + n, n
			}
			return sec, ""
		}
		if i := strings.LastIndex(head, "\n$Name:"); i >= 0 {
			rest := text[i+len("\n$Name:"):]
			if e := strings.IndexAny(rest, "\r\n"); e >= 0 {
				rest = rest[:e]
			}
			return sec + ": " + strings.TrimSpace(rest), ""
		}
		return sec, ""
	}

	for _, mt := range reDep.FindAllStringSubmatchIndex(text, -1) {
		op := text[mt[2]:mt[3]]
		src := text[mt[4]:mt[5]]
		tok := text[mt[6]:mt[7]]
		line, _ := pos(mt[6])
		wh, evName := where(mt[0])
		res.Refs = append(res.Refs, depRef{Source: src, Op: op, Where: wh, Event: evName, Delay: strings.Replace(tok, "(", "(expr)", 1)})

		l := lists[src]
		if l == nil || !l.changed() {
			continue
		}
		if evName == src && strings.HasPrefix(wh, "Events") {
			res.Warns = append(res.Warns, durWarn{Event: src, Text: fmt.Sprintf("%s refers to itself - not moved (line %d)", wh, line)})
			continue
		}
		msecs := strings.Contains(op, "msecs")
		move := func(off int, tok string) {
			v, err := strconv.ParseInt(tok, 10, 64)
			ln, cl := pos(off)
			if err != nil {
				res.Warns = append(res.Warns, durWarn{Event: src, Text: fmt.Sprintf("%s: delay %q is not a number - not moved (line %d)", wh, tok, ln)})
				return
			}
			ms := v
			if !msecs {
				ms = v * 1000
			}
			nms, step := l.shift(ms)
			nv := nms
			if !msecs {
				nv = ceilDiv(nms, 1000)
			}
			if nv == v {
				return
			}
			res.Edits = append(res.Edits, durEdit{
				Event: src, Op: op, Dependent: true, Where: wh,
				Old: tok, New: strconv.FormatInt(nv, 10), Line: ln, Col: cl,
				Note: stepNote(step, len(l.Steps)),
			})
		}
		if tok != "(" {
			move(mt[6], tok)
			continue
		}
		// ( rand A B ): both bounds move
		if rm := reRand.FindStringSubmatchIndex(text[mt[6]:]); rm != nil {
			move(mt[6]+rm[2], text[mt[6]+rm[2]:mt[6]+rm[3]])
			move(mt[6]+rm[4], text[mt[6]+rm[4]:mt[6]+rm[5]])
			continue
		}
		res.Warns = append(res.Warns, durWarn{Event: src, Text: fmt.Sprintf("%s: delay of %q is an expression - not moved (line %d)", wh, src, line)})
	}

	// +Chained: N counts from the previous event in the file.
	prev := map[int]string{}
	for _, ev := range m.Events {
		prev[ev.Rec] = ev.Name
	}
	seen := map[int]bool{}
	for _, ev := range m.Events {
		if ev.Chain == "" || ev.Rec == 0 || seen[ev.Rec] {
			continue
		}
		seen[ev.Rec] = true
		src := prev[ev.Rec-1]
		res.Refs = append(res.Refs, depRef{Source: src, Op: "+Chained", Where: "Events: " + ev.Name, Event: ev.Name, Delay: ev.Chain})
		l := lists[src]
		if l == nil || !l.changed() {
			continue
		}
		v, err := strconv.ParseInt(ev.Chain, 10, 64)
		if err != nil || v < 0 {
			continue
		}
		nms, step := l.shift(v * 1000)
		nv := ceilDiv(nms, 1000)
		if nv == v {
			continue
		}
		res.Edits = append(res.Edits, durEdit{
			Event: src, Op: "+Chained", Dependent: true, Where: "Events: " + ev.Name,
			Old: ev.Chain, New: strconv.FormatInt(nv, 10), Line: ev.ChainLine, Col: ev.ChainCol,
			Note: stepNote(step, len(l.Steps)),
		})
	}
}

func stepNote(step, n int) string {
	if step > n {
		return "after the end of the dialogue"
	}
	return fmt.Sprintf("during message %d of %d", step, n)
}

// renderChains lists, for every event whose dialogue was lengthened, the
// references to it, and below each one the references to that event in turn
// (they are relative, so they move along without a change of their own).
func renderChains(r durResult) string {
	byTarget := map[string][]depRef{}
	for _, d := range r.Refs {
		byTarget[d.Source] = append(byTarget[d.Source], d)
	}
	var roots []string
	seen := map[string]bool{}
	for _, e := range r.Edits {
		if !e.Dependent && !seen[e.Event] {
			seen[e.Event] = true
			roots = append(roots, e.Event)
		}
	}
	if len(roots) == 0 {
		return ""
	}
	var b strings.Builder
	var rec func(name string, depth int, path map[string]bool)
	rec = func(name string, depth int, path map[string]bool) {
		for _, d := range byTarget[name] {
			if d.Event == name {
				continue
			}
			fmt.Fprintf(&b, "%s%s  (%s %s)\n", strings.Repeat("  ", depth), d.Where, d.Op, d.Delay)
			if d.Event != "" && !path[d.Event] && depth < 6 {
				path[d.Event] = true
				rec(d.Event, depth+1, path)
				delete(path, d.Event)
			}
		}
	}
	fmt.Fprintf(&b, "%s\n", r.File)
	for _, ev := range roots {
		fmt.Fprintf(&b, "  [%s]\n", ev)
		rec(ev, 2, map[string]bool{ev: true})
	}
	b.WriteByte('\n')
	return b.String()
}

// renderDependents lists the delays moved because a dialogue got longer.
func renderDependents(r durResult) string {
	var b strings.Builder
	last := "\x00"
	for _, e := range r.Edits {
		if !e.Dependent {
			continue
		}
		if last == "\x00" {
			b.WriteString("  Delays moved with the dialogue:\n")
		}
		if e.Event != last {
			fmt.Fprintf(&b, "    after [%s]\n", e.Event)
			last = e.Event
		}
		fmt.Fprintf(&b, "      %s  %s  line %d  %s -> %s  (%s)\n", e.Where, e.Op, e.Line, e.Old, e.New, e.Note)
	}
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
	return b.String()
}

var reRand = regexp.MustCompile(`^\(\s*rand\s+(\d+)\s+(\d+)\s*\)`)
