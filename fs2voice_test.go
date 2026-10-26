package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
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
	forms := parseSexps(`( when ( and ( true ) ( > ( mission-time ) 5 ) ) ( send-message "A" "Normal" "m" ) )`, 1, 0)
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

// --- --fix-durations ----------------------------------------------------------

// oggPage builds a minimal Ogg page holding one packet.
func oggPage(granule int64, packet []byte) []byte {
	p := []byte("OggS")
	p = append(p, 0, 0)
	p = binary.LittleEndian.AppendUint64(p, uint64(granule))
	p = append(p, make([]byte, 12)...) // serial, sequence, CRC (not checked)
	p = append(p, 1, byte(len(packet)))
	return append(p, packet...)
}

func writeVorbis(t *testing.T, path string, rate uint32, samples int64) {
	t.Helper()
	id := append([]byte("\x01vorbis"), 0, 0, 0, 0, 1)
	id = binary.LittleEndian.AppendUint32(id, rate)
	id = append(id, make([]byte, 14)...)
	data := append(oggPage(0, id), oggPage(samples, []byte("audio"))...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeWav(t *testing.T, path string, byteRate uint32, dataSize int) {
	t.Helper()
	b := []byte("RIFF\x00\x00\x00\x00WAVEfmt ")
	b = binary.LittleEndian.AppendUint32(b, 16)
	b = append(b, 1, 0, 1, 0) // PCM, mono
	b = binary.LittleEndian.AppendUint32(b, byteRate)
	b = binary.LittleEndian.AppendUint32(b, byteRate)
	b = append(b, 1, 0, 8, 0)
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(dataSize))
	b = append(b, make([]byte, dataSize)...)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAudioDuration(t *testing.T) {
	dir := t.TempDir()
	ogg := filepath.Join(dir, "a.ogg")
	writeVorbis(t, ogg, 44100, 44100*5+1) // just over 5 s
	if ms, err := audioDuration(ogg); err != nil || ms != 5001 {
		t.Errorf("ogg: %d, %v; want 5001", ms, err)
	}
	wav := filepath.Join(dir, "b.wav")
	writeWav(t, wav, 11025, 11025*3/2) // 1.5 s
	if ms, err := audioDuration(wav); err != nil || ms != 1500 {
		t.Errorf("wav: %d, %v; want 1500", ms, err)
	}
}

const durMission = `#Events

$Formula: ( when ( true ) ( send-message-list-alt "A: one" 5000 "A: two" -3000 "A: three" 9000 ) )
+Name: Alt list
+Repeat Count: 1

$Formula: ( when
   ( true )
   ( send-message-list
      "#Command" "Normal" "one" 0
      "#Command" "Normal" "two" 2000
      "#Command" "Normal" "three" 1000
   )
)
+Name: Stock list

#Messages

$Name: A: one
$MessageNew: XSTR("1", -1)
$end_multi_text
+Wave Name: one.wav

$Name: A: two
$MessageNew: XSTR("2", -1)
$end_multi_text
+Wave Name: two.ogg

$Name: A: three
$MessageNew: XSTR("3", -1)
$end_multi_text
+Wave Name: three.ogg

$Name: one
$MessageNew: XSTR("1", -1)
$end_multi_text
+Wave Name: one.ogg

$Name: two
$MessageNew: XSTR("2", -1)
$end_multi_text
+Wave Name: two.ogg

$Name: three
$MessageNew: XSTR("3", -1)
$end_multi_text
+Wave Name: missing.ogg

#End
`

func TestFixDurations(t *testing.T) {
	dir := t.TempDir()
	writeVorbis(t, filepath.Join(dir, "one.ogg"), 22050, 22050*6+100) // 6.0x s -> 8000
	writeVorbis(t, filepath.Join(dir, "two.ogg"), 22050, 22050*3)     // 3.0 s, "A: two" 3000 is enough; stock delay -> 4000
	writeVorbis(t, filepath.Join(dir, "three.ogg"), 22050, 22050*8)   // 8.0 s, 9000 is enough
	voice, err := indexVoiceDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	raw := []byte(strings.ReplaceAll(durMission, "\n", "\r\n"))
	m := parseMission("d.fs2", raw)
	res := newDurChecker(voice).fixDurations(m)

	// "A: one" (one.wav -> one.ogg): 5000 -> 8000 (audio rounded up + 1 s); "A: two" exactly 3 s: kept.
	// Stock: "one" (6.x s) raises the delay of "two" 2000 -> 8000; "two" (3 s)
	// raises the delay of "three" 1000 -> 3000.
	if len(res.Edits) != 3 {
		t.Fatalf("edits = %+v, want 3", res.Edits)
	}
	if e := res.Edits[0]; e.Event != "Alt list" || e.MsgName != "A: one" || e.Old != "5000" || e.New != "8000" {
		t.Errorf("edit 0 = %+v", e)
	}
	if e := res.Edits[1]; e.Event != "Stock list" || e.MsgName != "one" || e.Next != "two" || e.New != "8000" {
		t.Errorf("edit 1 = %+v", e)
	}
	if e := res.Edits[2]; e.MsgName != "two" || e.Next != "three" || e.Old != "1000" || e.New != "4000" {
		t.Errorf("edit 2 = %+v", e)
	}
	if res.Missing != 0 {
		t.Errorf("missing = %d, want 0 (last stock message is not checked)", res.Missing)
	}

	out, err := applyEdits(raw, res.Edits)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.NewReplacer(
		`"A: one" 5000`, `"A: one" 8000`,
		`"two" 2000`, `"two" 8000`,
		`"three" 1000`, `"three" 4000`,
	).Replace(string(raw))
	if string(out) != want {
		t.Errorf("rewritten file differs:\n%s", out)
	}

	// Idempotent: a second pass finds nothing.
	if again := newDurChecker(voice).fixDurations(parseMission("d.fs2", out)); len(again.Edits) != 0 {
		t.Errorf("second pass edits = %+v", again.Edits)
	}
}

func TestFixDurationsNegativeAlternative(t *testing.T) {
	dir := t.TempDir()
	writeVorbis(t, filepath.Join(dir, "two.ogg"), 1000, 4500) // 4.5 s -> 5000
	voice, _ := indexVoiceDir(dir)
	src := strings.Replace(durMission, `"A: one" 5000`, `"A: one" -1`, 1)
	m := parseMission("d.fs2", []byte(src))
	res := newDurChecker(voice).fixDurations(m)
	if len(res.Edits) == 0 || res.Edits[0].MsgName != "A: two" || res.Edits[0].New != "-6000" {
		t.Errorf("edits = %+v, want A: two -3000 -> -6000 first", res.Edits)
	}
	if res.Missing == 0 {
		t.Error("missing one.ogg / three.ogg not counted")
	}
}

const shiftMission = `#Events

$Formula: ( when ( true ) ( send-message-list-alt "A: one" 5000 "A: two" 6000 "A: three" 1 ) )
+Name: Dialogue
+Repeat Count: 1

$Formula: ( when ( is-event-true-delay "Dialogue" 3 ) ( do-nothing ) )
+Name: Early
+Chained: 6
+Repeat Count: 1

$Formula: ( when ( is-event-true-delay "Dialogue" 7 ) ( do-nothing ) )
+Name: Middle
+Repeat Count: 1


$Formula: ( when ( is-event-true-delay "Dialogue" 12 ) ( do-nothing ) )
+Name: After
+Repeat Count: 1

$Formula: ( when ( is-event-true-msecs-delay "Dialogue" 5000 ) ( do-nothing ) )
+Name: Msecs
+Repeat Count: 1

$Formula: ( when ( is-event-true-delay "After" 2 ) ( do-nothing ) )
+Name: Downstream
+Repeat Count: 1

#Objects

$Name: Ship
$Arrival Cue: ( is-event-true-delay "Dialogue" 11 )

#Messages

$Name: A: one
$MessageNew: XSTR("1", -1)
$end_multi_text
+Wave Name: one.ogg

$Name: A: two
$MessageNew: XSTR("2", -1)
$end_multi_text
+Wave Name: two.ogg

$Name: A: three
$MessageNew: XSTR("3", -1)
$end_multi_text
+Wave Name: three.ogg

#End
`

func TestShiftDependents(t *testing.T) {
	dir := t.TempDir()
	writeVorbis(t, filepath.Join(dir, "one.ogg"), 22050, 22050*8)   // 8 s vs 5000 -> 9000 (+4 s)
	writeVorbis(t, filepath.Join(dir, "two.ogg"), 22050, 22050*3)   // 3 s vs 6000: kept
	writeVorbis(t, filepath.Join(dir, "three.ogg"), 22050, 22050*9) // last step, time 1: kept
	voice, _ := indexVoiceDir(dir)

	raw := []byte(strings.ReplaceAll(shiftMission, "\n", "\r\n"))
	m := parseMission("s.fs2", raw)
	res := newDurChecker(voice).fixDurations(m)
	shiftDependents(raw, m, &res)
	out, err := applyEdits(raw, res.Edits)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	// 3 s lies inside message 1 (S_1 = 0): unchanged. 7 s and 11 s are in
	// message 2 / after it and move by 4 s; 12 s is after the end.
	checks := map[string]bool{
		`( is-event-true-delay "Dialogue" 3 )`:                true,
		`( is-event-true-delay "Dialogue" 11 ) ( do`:          true,
		`( is-event-true-delay "Dialogue" 16 ) ( do`:          true,
		`( is-event-true-msecs-delay "Dialogue" 9000 )`:       true,
		`$Arrival Cue: ( is-event-true-delay "Dialogue" 15 )`: true,
		`( is-event-true-delay "After" 2 )`:                   true,
		"+Chained: 10":                                        true,
	}
	for s := range checks {
		if !strings.Contains(got, s) {
			t.Errorf("missing %q in:\n%s", s, got)
		}
	}
	if strings.Contains(got, "\n") && !strings.Contains(got, "\r\n") {
		t.Error("CRLF lost")
	}

	// Second pass: nothing to do.
	m2 := parseMission("s.fs2", out)
	again := newDurChecker(voice).fixDurations(m2)
	shiftDependents(out, m2, &again)
	if len(again.Edits) != 0 {
		t.Errorf("second pass edits = %+v", again.Edits)
	}
}

func mkVoiceLayout(t *testing.T) (root, missions, voice string) {
	t.Helper()
	root = t.TempDir()
	missions = filepath.Join(root, "tvwp_core", "data", "missions")
	voice = filepath.Join(root, "tvwp_voice", "data", "voice")
	for _, d := range []string{missions, filepath.Join(voice, "special"), filepath.Join(root, "tvwp_music", "data", "music")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return
}

func TestResolveVoiceDirAutoDetect(t *testing.T) {
	_, missions, voice := mkVoiceLayout(t)
	got, src, err := resolveVoiceDir("", missions, "", config{})
	if err != nil || got != voice || src != "auto-detected" {
		t.Errorf("got %q, %q, %v; want %q auto-detected", got, src, err, voice)
	}
}

func TestResolveVoiceDirLegacyLayout(t *testing.T) {
	root := t.TempDir()
	missions := filepath.Join(root, "data", "missions")
	voice := filepath.Join(root, "data", "voice")
	for _, d := range []string{missions, voice} {
		os.MkdirAll(d, 0o755)
	}
	got, _, err := resolveVoiceDir("", missions, "", config{})
	if err != nil || filepath.Clean(got) != filepath.Clean(voice) {
		t.Errorf("got %q, %v; want %q", got, err, voice)
	}
}

func TestResolveVoiceDirPriority(t *testing.T) {
	_, missions, _ := mkVoiceLayout(t)
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()
	cfg := config{Voice: c}
	if got, src, _ := resolveVoiceDir(a, missions, b, cfg); got != a || src != "--voice" {
		t.Errorf("flag: %q, %q", got, src)
	}
	if got, src, _ := resolveVoiceDir("", missions, b, cfg); got != b || src != voiceEnvVar {
		t.Errorf("env: %q, %q", got, src)
	}
	if got, src, _ := resolveVoiceDir("", missions, "", cfg); got != c || src != configFileName {
		t.Errorf("config: %q, %q", got, src)
	}
}

func TestResolveVoiceDirErrors(t *testing.T) {
	root := t.TempDir()
	missions := filepath.Join(root, "x", "data", "missions")
	os.MkdirAll(missions, 0o755)
	if _, _, err := resolveVoiceDir("", missions, "", config{}); err == nil || !strings.Contains(err.Error(), "--voice") {
		t.Errorf("not found: err = %v, want a hint about --voice", err)
	}
	if _, _, err := resolveVoiceDir(filepath.Join(root, "nope"), missions, "", config{}); err == nil {
		t.Error("missing --voice dir accepted")
	}
	// Two unrelated packages with data/voice: ambiguous.
	_, m2, _ := mkVoiceLayout(t)
	os.MkdirAll(filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(m2))), "other", "data", "voice"), 0o755)
	// tvwp_voice is chosen because its name contains "voice".
	if got, _, err := resolveVoiceDir("", m2, "", config{}); err != nil || !strings.Contains(got, "tvwp_voice") {
		t.Errorf("name preference: %q, %v", got, err)
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	if c, err := loadConfig(dir); err != nil || c != (config{}) {
		t.Errorf("missing file: %+v, %v", c, err)
	}
	os.WriteFile(filepath.Join(dir, configFileName), []byte("{\"voice\": \"../audio\", \"dir\": \"C:/abs/missions\"}"), 0o644)
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "../audio"); c.Voice != want {
		t.Errorf("voice = %q, want %q (relative to the config file)", c.Voice, want)
	}
	if c.Dir != "C:/abs/missions" {
		t.Errorf("dir = %q", c.Dir)
	}
	os.WriteFile(filepath.Join(dir, configFileName), []byte(`{oops`), 0o644)
	if _, err := loadConfig(dir); err == nil || !strings.Contains(err.Error(), configFileName) {
		t.Errorf("bad json: %v", err)
	}
}
