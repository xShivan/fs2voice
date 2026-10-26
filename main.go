// fs2voice generates a unified voice-acting script from FreeSpace 2 mission
// files. It understands both the stock send-message / send-message-list family
// and the mod's Lua "-alt" family (send-message-alt / -list-alt / -chain-alt),
// where the speaker is encoded in the message name and resolved through
// set-message-alias / set-freetext-message-alias events.
//
// Command briefing, briefing and debriefing stages are exported too, in their
// own sections of the same file.
//
// Output: one plain-text file per mission, blocks of
//
//	Sender:   <ship / wing / Command>
//	Persona:  <persona or callsign>      (only when known)
//	Message:  <message text>
//	Filename: <name.ogg or TODO>
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	dir := flag.String("dir", "", "directory containing *.fs2 mission files (required)")
	out := flag.String("out", "fs2voice-out", "output directory for the generated files")
	only := flag.String("mission", "", "limit to a single mission (file name, e.g. tvw-v01.fs2)")
	noDedup := flag.Bool("no-dedup", false, "do not merge repeated messages within a mission")
	noOrphans := flag.Bool("no-orphans", false, "skip the ORPHANS section")
	noPersonas := flag.Bool("no-personas", false, "do not generate the combined personas.md table")
	fixDur := flag.Bool("fix-durations", false, "raise message timings shorter than their voice files, then exit")
	voiceDir := flag.String("voice", "", "voice files directory for --fix-durations (default: $FS2VOICE_VOICE, \"voice\" in fs2voice.json, or auto-detected)")
	dryRun := flag.Bool("dry-run", false, "with --fix-durations: only report, do not modify mission files")
	flag.Parse()

	cfg, err := loadConfig(exeDir())
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	set := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if !set["dir"] && cfg.Dir != "" {
		*dir = cfg.Dir
	}
	if !set["out"] && cfg.Out != "" {
		*out = cfg.Out
	}

	if *dir == "" {
		fmt.Fprintln(os.Stderr, "error: --dir is required")
		flag.Usage()
		os.Exit(2)
	}

	files, err := listMissions(*dir, *only)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "no *.fs2 files found in", *dir)
		os.Exit(1)
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	if *fixDur {
		vd, source, err := resolveVoiceDir(*voiceDir, *dir, os.Getenv(voiceEnvVar), cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		fmt.Printf("Voice files: %s (%s)\n", vd, source)
		if err := runFixDurations(files, vd, *out, *dryRun); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	agg := newSenderAgg()
	total := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skipping %s: %v\n", f, err)
			continue
		}
		base := filepath.Base(f)
		m := parseMission(base, raw)
		entries := buildEntries(m, !*noDedup, !*noOrphans)
		agg.add(shortMissionID(base), entries)

		outName := strings.TrimSuffix(base, filepath.Ext(base)) + ".txt"
		outPath := filepath.Join(*out, outName)
		if err := os.WriteFile(outPath, []byte(renderScript(m, entries)), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing %s: %v\n", outPath, err)
			continue
		}

		voiced, briefs, orphans := 0, 0, 0
		for _, e := range entries {
			if e.Section != secMessages {
				briefs++
			} else if e.Orphan {
				orphans++
			} else {
				voiced++
			}
		}
		total += voiced + briefs
		fmt.Printf("%-20s %3d messages, %3d briefing, %3d orphan  -> %s\n", base, voiced, briefs, orphans, outPath)
	}
	fmt.Printf("\nTotal: %d voiced lines across %d missions.\n", total, len(files))

	if !*noPersonas {
		pPath := filepath.Join(*out, "personas.md")
		if err := os.WriteFile(pPath, []byte(renderPersonaTable(agg)), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing %s: %v\n", pPath, err)
		} else {
			fmt.Printf("%-20s %3d senders            -> %s\n", "sender table", len(agg.lines), pPath)
		}
	}
}

// runFixDurations applies --fix-durations to every mission and writes the
// report to stdout and to <out>/durations-report.txt.
func runFixDurations(files []string, voiceDir, out string, dryRun bool) error {
	voice, err := indexVoiceDir(voiceDir)
	if err != nil {
		return err
	}
	if len(voice) == 0 {
		return fmt.Errorf("no .ogg/.wav files found in %s", voiceDir)
	}
	chk := newDurChecker(voice)

	var results []durResult
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skipping %s: %v\n", f, err)
			continue
		}
		m := parseMission(filepath.Base(f), raw)
		res := chk.fixDurations(m)
		shiftDependents(raw, m, &res)
		results = append(results, res)
		if dryRun || len(res.Edits) == 0 {
			continue
		}
		fixed, err := applyEdits(raw, res.Edits)
		if err != nil {
			return fmt.Errorf("%s: %w (file left unchanged)", f, err)
		}
		info, err := os.Stat(f)
		if err != nil {
			return err
		}
		if err := os.WriteFile(f, fixed, info.Mode().Perm()); err != nil {
			return err
		}
	}

	report := renderDurationReport(results, dryRun)
	fmt.Print(report)
	rPath := filepath.Join(out, "durations-report.txt")
	if err := os.WriteFile(rPath, []byte(report), 0o644); err != nil {
		return err
	}
	fmt.Printf("\nReport written to %s\n", rPath)
	return nil
}

// listMissions returns the .fs2 files to process, skipping backups and the
// superseded z_old_* files.
func listMissions(dir, only string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.EqualFold(filepath.Ext(name), ".fs2") {
			continue
		}
		if strings.HasPrefix(name, "z_old_") || strings.Contains(strings.ToLower(name), ".bak") {
			continue
		}
		if only != "" && !strings.EqualFold(name, only) {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	sort.Strings(out)
	return out, nil
}
