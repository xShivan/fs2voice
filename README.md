# fs2voice

Voice-acting script generator for FreeSpace 2 missions (`*.fs2`)

It processes **both** message mechanisms:

* stock SEXPs: `send-message`, `send-message-list`, `send-message-chain`, `send-random-message`, `training-msg` - the sender is the first argument;
* the **"-alt"** system (Lua): `send-message-alt`, `send-message-list-alt`, `send-message-chain-alt` - the sender is encoded in the `#Messages` entry name as `"Source: Title"` and resolved through the `set-message-alias` / `set-freetext-message-alias` events.

For every mission a separate `.txt` file is produced, with blocks:

```
Sender:   Alpha 2
Persona:  Vapor            # optional line - only when known
Message:  Alpha wing, form up and move to the departure coordinates.
Filename: v01_m001.ogg     # from +Wave Name (extension -> .ogg) or TODO
Ref:      intro1           # the $Name of the #Messages entry (to locate it in FRED)
Note:     random variant   # optional: random variant / alternative / training message
```

Each file ends with an `--- ORPHANS ---` section listing `#Messages` entries that no event triggers (useful for coverage checks / trimming dead text).

### Briefings

Voiced briefing stages are exported into the same file. When a mission has any, the script is split into sections, in this order:

```
--- COMMAND BRIEFING ---   # #Command Briefing stages      (+Wave Filename)
--- BRIEFING ---           # #Briefing stages              ($voice)
--- MISSION MESSAGES ---   # the #Messages lines described above
--- DEBRIEFING ---         # #Debriefing_info stages       ($Voice)
--- ORPHANS ... ---
```

A briefing block uses `Sender: Command Briefing` / `Briefing` / `Debriefing`, and `Ref: cmd-brief stage 3`, `brief stage 2` (`brief team 2 stage 2` in multi-team missions) or `debrief stage 1`. A debriefing stage shown only under a condition gets `Note: when <event name>`, or `when <formula>` for anything more complex than `is-event-true(-delay)`.

Not exported: briefing icon descriptions and debriefing recommendation text (they are not voiced).

FS2 text markup is stripped from briefing text: colour tags (`$b`, `$f`, `$h`, `$W`, `$y`, …) are removed, and `$slash`, `$quote`, `$semicolon` become `/`, `"`, `;`. Line breaks are joined into one line, the same as for messages. A voice file of `none.wav` (or any `none.*`) is written as `TODO`.

Additionally a **`personas.md`** file is produced - a combined Markdown table of every sender and the missions they appear in:

| Sender | Persona(s) | Missions | Lines |
|---|---|---|---|
| Alpha 2 | Chaser, Vapor | v01, v02, v06, … | 293 |
| Command | - | v01, v02, … | 217 |
| - | … | … | … |

* rows keyed by the `Sender` field of the generated scripts (ORPHANS included);
* `Persona(s)` - distinct personas associated with that sender (empty if none);
* `Missions` - short mission IDs (`tvw-v06.fs2` → `v06`) with ≥1 line;
* `Lines` - total number of lines; the `—` row is lines with no recognised sender.

FS2 suffixed names (`Amethes#Steelward`, `Adrastea#1`) are collapsed to their display name (`Amethes`, `Adrastea`) - this also applies to the `Sender:` field in the scripts.

## Build

```
go build -o fs2voice.exe .
```

## Usage

```
fs2voice --dir <missions-dir> [--out <dir>] [options]
```

| Flag | Meaning |
|---|---|
| `--dir` | directory with `*.fs2` files (required) |
| `--out` | output directory (default `fs2voice-out`) |
| `--mission tvw-v06.fs2` | limit to a single mission |
| `--no-dedup` | do not merge repeats of the same message within a mission |
| `--no-orphans` | skip the ORPHANS section |
| `--no-personas` | do not generate the combined `personas.md` table |

Example:

```
fs2voice --dir "C:\FreeSpace\FS2\yourmod-999.0.0-DevEnv\yourmod\data\missions" --out out
```

Automatically skipped: `*.bak`, `z_old_*.fs2`.

## Fixing message durations

### Voice directory

The audio can live in another package than the missions (for example `tvwp_core/data/missions` and `tvwp_voice/data/voice`), so the directory is resolved in this order:

1. `--voice <dir>`;
2. the `FS2VOICE_VOICE` environment variable;
3. `"voice"` in `fs2voice.json` next to `fs2voice.exe` (relative paths are relative to that file; `"dir"` and `"out"` may be set there too and act as defaults for `--dir` / `--out`);
4. auto-detection: `<missions>/../voice` (old layout), otherwise the `data/voice` of a sibling package found by going up from `--dir` (several candidates: the package whose name contains `voice`, else an error listing them).

The chosen directory and where it came from are printed at the start (`Voice files: ... (auto-detected)`). See `fs2voice.example.json`; `fs2voice.json` itself is git-ignored.


```
fs2voice --dir <missions-dir> --fix-durations [--voice <dir>] [--dry-run] [--out <dir>]
```

Measures the voice file of every message (`+Wave Name:`, looked up by base name in the voice directory (see below), recursively; `.ogg` Vorbis/Opus and `.wav`). Any timing shorter than the audio is raised to the audio length rounded **up to whole seconds, plus 1 s**, and the mission files are rewritten in place. A time of <= 1 ms on the last step of a `-alt` list is an end marker and is kept. Only numbers change; every other byte is left alone.

**Dependent timings.** Events and ship cues anchored to a lengthened `-alt` list are moved by the same amount, keeping their place in the dialogue (a delay that fell during message k moves by the growth of the messages before k; a delay after the end moves by the whole growth): `is-event-true-delay`, `is-event-false-delay`, `...-msecs-delay` (also the bounds of `( rand a b )`) anywhere in the file - events, `#Objects` / `#Wings` arrival and departure cues, goals - and `+Chained: N` of the event following the dialogue event. Delays are relative to the moment their event fires, so a chain A -> B -> cue needs no extra work: B moves with A, its own dependants only follow B's list. The report lists the moved delays and a "Dependency chains" tree. Stock (non-`-alt`) lists are lengthened but their dependants are not moved (a warning is printed).

| SEXP | Number checked |
|---|---|
| `send-message-list-alt`, `send-message-chain-alt` | the message's own duration; the sign is kept (`-5000` stays an alternative) |
| `send-message-list`, `send-message-chain` | the **delay of the next message**, which counts from the start of this one (so the last message of a list is not checked) |

`send-message`, `send-message-alt`, `send-random-message` and `training-msg` carry no timing and are not touched.

The report goes to stdout and `<out>/durations-report.txt`: it lists each change by mission and event, with message, voice file, audio length and `old -> new`, followed by warnings (a timing that is not a plain number, an unreadable file) and the number of lines per mission whose voice file does not exist yet. `--dry-run` writes only the report. Running the command a second time reports 0 changes.

| Flag | Meaning |
|---|---|
| `--fix-durations` | run this mode instead of generating scripts |
| `--voice` | voice files directory (default: see "Voice directory") |
| `--dry-run` | report only, do not modify `.fs2` files |

## How the sender is determined

1. Stock SEXP → the `sender` argument verbatim.
2. "-alt" SEXP → the part of the message name before the first `:` → alias from the mission map (`set-message-alias` + `set-freetext-message-alias`); no alias → the prefix verbatim. A name without `:` → unknown sender (`—`), to be filled in by hand.
3. `#Command` → the `$Command Sender:` value from the `#Messages` header, otherwise `Command`. Other `#Xxx` → `Xxx`.
4. `training-msg` → `Training`.
5. Suffix `name#anything` → `name` (FS2 display-name convention).

The `Persona:` field = `+Persona:` on the message entry, otherwise the `+Persona Name:` / `$Callsign:` of the ship whose name matches the sender (from `#Objects`).

## Limitations

* Line order within a mission = order the SEXP appears in the file. Events fire conditionally, so this approximates mission flow, not strict chronology.
* Variants (`send-random-message`, `-alt` alternatives) are all listed, each with a `Note:` - every one needs to be recorded.
* `msg.wav` / `none` placeholders → `Filename: TODO` (names are not generated).

## Tests

```
go test ./...
```
