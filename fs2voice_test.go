package main

import (
	"os"
	"strings"
	"testing"
)

func loadSample(t *testing.T) []entry {
	t.Helper()
	raw, err := os.ReadFile("testdata/sample.fs2")
	if err != nil {
		t.Fatal(err)
	}
	m := parseMission("sample.fs2", raw)
	return buildEntries(m, true, true)
}

func find(entries []entry, msgName string) *entry {
	for i := range entries {
		if entries[i].MsgName == msgName {
			return &entries[i]
		}
	}
	return nil
}

func TestStandardSender(t *testing.T) {
	e := find(loadSample(t), "hello")
	if e == nil {
		t.Fatal("hello not found")
	}
	if e.Sender != "Alpha 2" {
		t.Errorf("sender = %q, want Alpha 2", e.Sender)
	}
	if e.Persona != "Vapor" {
		t.Errorf("persona = %q, want Vapor", e.Persona)
	}
	if e.Text != "Alpha wing, form up on me." {
		t.Errorf("text = %q", e.Text)
	}
	if e.Filename != "v99_m001.ogg" {
		t.Errorf("filename = %q, want v99_m001.ogg", e.Filename)
	}
}

func TestCommandSenderOverride(t *testing.T) {
	e := find(loadSample(t), "orders")
	if e == nil {
		t.Fatal("orders not found")
	}
	if e.Sender != "Steelward Control" {
		t.Errorf("sender = %q, want Steelward Control (from $Command Sender)", e.Sender)
	}
	if e.Filename != "TODO" {
		t.Errorf("filename = %q, want TODO (msg.wav placeholder)", e.Filename)
	}
	if e.Text != "Proceed to the nav point and hold position." {
		t.Errorf("multiline text not normalised: %q", e.Text)
	}
}

func TestPersonaFromMessageEntry(t *testing.T) {
	e := find(loadSample(t), "reply")
	if e == nil {
		t.Fatal("reply not found")
	}
	if e.Sender != "Alpha 3" {
		t.Errorf("sender = %q, want Alpha 3", e.Sender)
	}
	// +Persona: on the message wins over the ship's $Callsign (Brick).
	if e.Persona != "Large Ship" {
		t.Errorf("persona = %q, want Large Ship", e.Persona)
	}
}

func TestAltAliasResolution(t *testing.T) {
	entries := loadSample(t)

	if e := find(entries, "A2: on station"); e == nil || e.Sender != "Alpha 2" {
		t.Errorf("A2: on station sender = %v, want Alpha 2", e)
	}
	// COM -> #Command -> $Command Sender override.
	if e := find(entries, "COM: acknowledged"); e == nil || e.Sender != "Steelward Control" {
		t.Errorf("COM: acknowledged sender = %v, want Steelward Control", e)
	}
	if e := find(entries, "A2: fallback"); e == nil || e.Sender != "Alpha 2" {
		t.Errorf("A2: fallback sender = %v, want Alpha 2", e)
	}
}

func TestTrainingMsg(t *testing.T) {
	e := find(loadSample(t), "lesson1")
	if e == nil {
		t.Fatal("lesson1 not found")
	}
	if e.Sender != "Training" {
		t.Errorf("sender = %q, want Training", e.Sender)
	}
	if e.Filename != "v99_t01.ogg" {
		t.Errorf("filename = %q", e.Filename)
	}
}

func TestOrphans(t *testing.T) {
	e := find(loadSample(t), "unused_line")
	if e == nil {
		t.Fatal("unused_line not found")
	}
	if !e.Orphan {
		t.Error("unused_line should be flagged Orphan")
	}
}

func TestRenderShape(t *testing.T) {
	m := &mission{File: "x.fs2"}
	out := renderScript(m, []entry{{
		Sender: "Alpha 2", Persona: "Vapor",
		Text: "Hi.", Filename: "a.ogg",
	}})
	for _, want := range []string{"Sender:   Alpha 2", "Persona:  Vapor", "Message:  Hi.", "Filename: a.ogg"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}

func TestPersonaTable(t *testing.T) {
	agg := newSenderAgg()
	agg.add("sample", loadSample(t))

	if _, ok := agg.lines["Alpha 2"]; !ok {
		t.Fatal("Alpha 2 missing from aggregate")
	}
	if agg.lines["Alpha 2"]["sample"] < 2 {
		t.Errorf("Alpha 2 line count = %d, want >= 2", agg.lines["Alpha 2"]["sample"])
	}
	if _, ok := agg.personas["Alpha 2"]["Vapor"]; !ok {
		t.Errorf("Alpha 2 personas = %v, want to contain Vapor", agg.personas["Alpha 2"])
	}
	// "#Command" + "$Command Sender: Steelward Control" -> sender "Steelward Control".
	if _, ok := agg.lines["Steelward Control"]; !ok {
		t.Errorf("expected sender 'Steelward Control', have %v", keys(agg.lines))
	}

	md := renderPersonaTable(agg)
	for _, want := range []string{"| Sender | Persona(s) | Missions | Lines |", "| Alpha 2 |", "Total:"} {
		if !strings.Contains(md, want) {
			t.Errorf("table missing %q\n%s", want, md)
		}
	}
}

func keys(m map[string]map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestShortMissionID(t *testing.T) {
	cases := map[string]string{
		"tvw-v06.fs2":       "v06",
		"tvw-v11-v2.fs2":    "v11-v2",
		"tvw-v14-outro.fs2": "v14-outro",
	}
	for in, want := range cases {
		if got := shortMissionID(in); got != want {
			t.Errorf("shortMissionID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDisplayName(t *testing.T) {
	cases := map[string]string{
		"Amethes#Steelward": "Amethes",
		"Adrastea#1":        "Adrastea",
		"Alpha 2":           "Alpha 2",
		"#Command":          "#Command", // leading '#' is not a suffix
	}
	for in, want := range cases {
		if got := displayName(in); got != want {
			t.Errorf("displayName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSexpParserNested(t *testing.T) {
	forms := parseSexps(`( when ( and ( true ) ( > ( mission-time ) 5 ) ) ( send-message "A" "Normal" "m" ) )`, 1)
	if len(forms) != 1 {
		t.Fatalf("got %d forms", len(forms))
	}
	var got string
	walk(forms[0], func(n *node) {
		if n.head() == "send-message" {
			got = n.Items[3].Value
		}
	})
	if got != "m" {
		t.Errorf("nested send-message message arg = %q, want m", got)
	}
}

func TestBriefingSections(t *testing.T) {
	entries := loadSample(t)
	want := []struct{ section, sender, ref, text, file, note string }{
		{secCmdBriefing, "Command Briefing", "cmd-brief stage 1", "Welcome aboard Date: 11/04/2333", "v99_cb01.ogg", ""},
		{secCmdBriefing, "Command Briefing", "cmd-brief stage 2", "Second stage. #not a section header", "TODO", ""},
		{secBriefing, "Briefing", "brief stage 1", "Welcome to Steelward, pilots.", "v99_b01.ogg", ""},
		{secBriefing, "Briefing", "brief stage 2", "Stay sharp.", "v99_b02.ogg", ""},
		{secDebriefing, "Debriefing", "debrief stage 1", "Good work. Dismissed.", "v99_db01.ogg", "when DB success"},
		{secDebriefing, "Debriefing", "debrief stage 2", "Always shown.", "TODO", ""},
	}
	var got []entry
	for _, e := range entries {
		if e.Section != secMessages {
			got = append(got, e)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("got %d briefing entries, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.Section != w.section || g.Sender != w.sender || g.MsgName != w.ref ||
			g.Text != w.text || g.Filename != w.file || g.Note != w.note {
			t.Errorf("entry %d = %+v, want %+v", i, g, w)
		}
	}
	// Order: briefings first, debriefing after the mission messages, before orphans.
	if entries[0].Section != secCmdBriefing {
		t.Errorf("first entry section = %q, want command briefing", entries[0].Section)
	}
	lastDebrief, firstOrphan := -1, -1
	for i, e := range entries {
		if e.Section == secDebriefing {
			lastDebrief = i
		}
		if e.Orphan && firstOrphan < 0 {
			firstOrphan = i
		}
	}
	if firstOrphan >= 0 && lastDebrief > firstOrphan {
		t.Errorf("debriefing (idx %d) after orphans (idx %d)", lastDebrief, firstOrphan)
	}
	// Messages parsed after the briefing sections are unaffected.
	if find(entries, "hello") == nil {
		t.Error("hello message lost after adding briefing sections")
	}
}

func TestRenderBriefingHeaders(t *testing.T) {
	raw, err := os.ReadFile("testdata/sample.fs2")
	if err != nil {
		t.Fatal(err)
	}
	m := parseMission("sample.fs2", raw)
	out := renderScript(m, buildEntries(m, true, true))
	last := -1
	for _, h := range []string{"--- COMMAND BRIEFING ---", "--- BRIEFING ---", "--- MISSION MESSAGES ---", "--- DEBRIEFING ---", "--- ORPHANS"} {
		i := strings.Index(out, h)
		if i < 0 || i < last {
			t.Errorf("header %q missing or out of order\n%s", h, out)
		}
		last = i
	}
	for _, bad := range []string{"not voiced", "$b", "$slash"} {
		if strings.Contains(out, bad) {
			t.Errorf("output unexpectedly contains %q", bad)
		}
	}
}

func TestStripMarkup(t *testing.T) {
	cases := map[string]string{
		"$b * $b GTCa $b Adrastea":  "* GTCa Adrastea",
		"11$slash04 $quotehi$quote": `11/04 "hi"`,
		"a$semicolon b $W":          "a; b",
		"cost $100":                 "cost $100",
	}
	for in, want := range cases {
		if got := stripMarkup(in); got != want {
			t.Errorf("stripMarkup(%q) = %q, want %q", in, got, want)
		}
	}
}
