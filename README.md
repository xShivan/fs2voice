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
