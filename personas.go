package main

import (
	"fmt"
	"sort"
	"strings"
)

// senderAgg accumulates, across every processed mission, which speakers appear
// where. The key is entry.Sender (an unresolved sender is bucketed as "—").
type senderAgg struct {
	lines    map[string]map[string]int      // sender -> missionID -> line count
	personas map[string]map[string]struct{} // sender -> set of personas seen
}

const unknownSender = "—"

func newSenderAgg() *senderAgg {
	return &senderAgg{
		lines:    map[string]map[string]int{},
		personas: map[string]map[string]struct{}{},
	}
}

func (a *senderAgg) add(missionID string, entries []entry) {
	for _, e := range entries {
		s := e.Sender
		if s == "" {
			s = unknownSender
		}
		if a.lines[s] == nil {
			a.lines[s] = map[string]int{}
		}
		a.lines[s][missionID]++

		// Only record personas that add information beyond the sender name
		// (mirrors the suppression in writeEntry).
		if e.Persona != "" && !strings.EqualFold(e.Persona, e.Sender) {
			if a.personas[s] == nil {
				a.personas[s] = map[string]struct{}{}
			}
			a.personas[s][e.Persona] = struct{}{}
		}
	}
}

// renderPersonaTable produces the Markdown report.
func renderPersonaTable(a *senderAgg) string {
	senders := make([]string, 0, len(a.lines))
	for s := range a.lines {
		senders = append(senders, s)
	}
	sort.Slice(senders, func(i, j int) bool {
		// "—" always sorts last.
		if (senders[i] == unknownSender) != (senders[j] == unknownSender) {
			return senders[j] == unknownSender
		}
		li, lj := strings.ToLower(senders[i]), strings.ToLower(senders[j])
		if li != lj {
			return li < lj
		}
		return senders[i] < senders[j]
	})

	var b strings.Builder
	b.WriteString("# Personas / message senders — fs2voice\n\n")
	b.WriteString("| Sender | Persona(s) | Missions | Lines |\n")
	b.WriteString("|---|---|---|---|\n")

	totalLines, totalMissions := 0, map[string]struct{}{}
	for _, s := range senders {
		missions := make([]string, 0, len(a.lines[s]))
		lineCount := 0
		for mid, c := range a.lines[s] {
			missions = append(missions, mid)
			lineCount += c
			totalMissions[mid] = struct{}{}
		}
		sort.Strings(missions)
		totalLines += lineCount

		fmt.Fprintf(&b, "| %s | %s | %s | %d |\n",
			mdCell(s), mdCell(joinSet(a.personas[s])), mdCell(strings.Join(missions, ", ")), lineCount)
	}

	fmt.Fprintf(&b, "\nTotal: %d senders, %d lines across %d missions.\n",
		len(senders), totalLines, len(totalMissions))
	return b.String()
}

// joinSet returns the set's members sorted and comma-joined, or "—" if empty.
func joinSet(set map[string]struct{}) string {
	if len(set) == 0 {
		return "—"
	}
	vals := make([]string, 0, len(set))
	for v := range set {
		vals = append(vals, v)
	}
	sort.Strings(vals)
	return strings.Join(vals, ", ")
}

// mdCell escapes the pipe character so table cells stay intact.
func mdCell(s string) string {
	if s == "" {
		return "—"
	}
	return strings.ReplaceAll(s, "|", `\|`)
}

// shortMissionID turns "tvw-v06.fs2" into "v06".
func shortMissionID(base string) string {
	base = strings.TrimSuffix(base, ".fs2")
	base = strings.TrimPrefix(base, "tvw-")
	return base
}
