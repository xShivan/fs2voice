package main

// Where the voice files are.
// The mod is split into packages (tvwp_core holds the missions, tvwp_voice the audio), so the audio is no longer next to the missions.
// The voice directory is resolved in this order: --voice, the FS2VOICE_VOICE variable, "voice" in fs2voice.json next to the executable, auto-detection.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	configFileName = "fs2voice.json"
	voiceEnvVar    = "FS2VOICE_VOICE"
)

// config is the content of fs2voice.json.
// Relative paths are taken relative to the directory of the file.
type config struct {
	Dir   string `json:"dir"`
	Out   string `json:"out"`
	Voice string `json:"voice"`
}

// loadConfig reads fs2voice.json from dir.
// A missing file is not an error and gives an empty config.
func loadConfig(dir string) (config, error) {
	var c config
	path := filepath.Join(dir, configFileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return config{}, fmt.Errorf("%s: %w", path, err)
	}
	for _, p := range []*string{&c.Dir, &c.Out, &c.Voice} {
		if *p != "" && !filepath.IsAbs(*p) {
			*p = filepath.Join(dir, *p)
		}
	}
	return c, nil
}

// exeDir is the directory of the running executable, or "" if unknown.
func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// resolveVoiceDir picks the voice directory and says where the choice came from.
func resolveVoiceDir(flagVoice, missionsDir, envVoice string, cfg config) (dir, source string, err error) {
	switch {
	case flagVoice != "":
		dir, source = flagVoice, "--voice"
	case envVoice != "":
		dir, source = envVoice, voiceEnvVar
	case cfg.Voice != "":
		dir, source = cfg.Voice, configFileName
	default:
		dir, err = detectVoiceDir(missionsDir)
		if err != nil {
			return "", "", err
		}
		return dir, "auto-detected", nil
	}
	if st, statErr := os.Stat(dir); statErr != nil || !st.IsDir() {
		return "", "", fmt.Errorf("voice directory %s (from %s) does not exist", dir, source)
	}
	return dir, source, nil
}

// detectVoiceDir looks for the voice files near the missions.
// It tries the old layout (<missions>/../voice) first, then goes up from the missions directory and takes the data/voice of a sibling package such as tvwp_voice.
func detectVoiceDir(missionsDir string) (string, error) {
	abs, err := filepath.Abs(missionsDir)
	if err != nil {
		return "", err
	}
	if legacy := filepath.Join(abs, "..", "voice"); isDir(legacy) {
		return legacy, nil
	}
	for up, d := 0, abs; up < 5; up++ {
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
		found, _ := filepath.Glob(filepath.Join(d, "*", "data", "voice"))
		var cands []string
		for _, f := range found {
			if isDir(f) {
				cands = append(cands, f)
			}
		}
		if len(cands) == 1 {
			return cands[0], nil
		}
		if len(cands) > 1 {
			var named []string
			for _, c := range cands {
				pkg := filepath.Base(filepath.Dir(filepath.Dir(c)))
				if strings.Contains(strings.ToLower(pkg), "voice") {
					named = append(named, c)
				}
			}
			if len(named) == 1 {
				return named[0], nil
			}
			return "", fmt.Errorf("several voice directories found, choose one with --voice:\n  %s", strings.Join(cands, "\n  "))
		}
	}
	return "", fmt.Errorf("voice directory not found near %s\nset it with --voice <dir>, the %s variable, or \"voice\" in %s next to the executable", missionsDir, voiceEnvVar, configFileName)
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
