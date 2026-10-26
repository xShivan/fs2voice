package main

// Audio length measurement for the voice files a mission references, using
// only the container headers: Ogg (Vorbis / Opus) and RIFF WAVE.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// audioDuration returns the playing time of an .ogg or .wav file in
// milliseconds, rounded up.
func audioDuration(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	switch {
	case bytes.HasPrefix(data, []byte("OggS")):
		return oggDuration(data)
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE":
		return wavDuration(data)
	}
	return 0, errors.New("unrecognised audio format (expected Ogg or WAV)")
}

// oggDuration reads the sample rate from the first (identification) packet and
// the total sample count from the granule position of the last page.
func oggDuration(data []byte) (int64, error) {
	payload, err := firstOggPacket(data)
	if err != nil {
		return 0, err
	}
	var rate, skip int64
	switch {
	case len(payload) >= 16 && payload[0] == 1 && string(payload[1:7]) == "vorbis":
		rate = int64(binary.LittleEndian.Uint32(payload[12:16]))
	case len(payload) >= 12 && string(payload[0:8]) == "OpusHead":
		rate = 48000 // Opus granule positions always count 48 kHz samples
		skip = int64(binary.LittleEndian.Uint16(payload[10:12]))
	default:
		return 0, errors.New("unsupported Ogg codec (expected Vorbis or Opus)")
	}
	if rate <= 0 {
		return 0, errors.New("invalid sample rate")
	}

	// Find the last page whose granule position is set (-1 means "no packet
	// ends on this page").
	for i := bytes.LastIndex(data, []byte("OggS")); i >= 0; i = bytes.LastIndex(data[:i], []byte("OggS")) {
		if i+14 > len(data) || data[i+4] != 0 {
			continue // truncated, or "OggS" inside packet data
		}
		g := int64(binary.LittleEndian.Uint64(data[i+6 : i+14]))
		if g < 0 {
			continue
		}
		return ceilDiv(max(g-skip, 0)*1000, rate), nil
	}
	return 0, errors.New("no Ogg page with a granule position")
}

// firstOggPacket returns the payload of the first page (the codec's
// identification header always sits alone on it).
func firstOggPacket(data []byte) ([]byte, error) {
	if len(data) < 27 {
		return nil, errors.New("truncated Ogg page")
	}
	nseg := int(data[26])
	if len(data) < 27+nseg {
		return nil, errors.New("truncated Ogg page")
	}
	size := 0
	for _, s := range data[27 : 27+nseg] {
		size += int(s)
	}
	start := 27 + nseg
	if len(data) < start+size {
		return nil, errors.New("truncated Ogg page")
	}
	return data[start : start+size], nil
}

// wavDuration divides the size of the "data" chunk by the byte rate from "fmt ".
func wavDuration(data []byte) (int64, error) {
	var byteRate, dataSize int64
	haveData := false
	for p := 12; p+8 <= len(data); {
		id := string(data[p : p+4])
		size := int64(binary.LittleEndian.Uint32(data[p+4 : p+8]))
		body := p + 8
		switch id {
		case "fmt ":
			if body+12 > len(data) {
				return 0, errors.New("truncated fmt chunk")
			}
			byteRate = int64(binary.LittleEndian.Uint32(data[body+8 : body+12]))
		case "data":
			dataSize = size
			if rest := int64(len(data) - body); dataSize > rest {
				dataSize = rest // tolerate a wrong size in a truncated file
			}
			haveData = true
		}
		next := int64(body) + size + size%2 // chunks are word-aligned
		if next > int64(len(data)) {
			break
		}
		p = int(next)
	}
	if byteRate <= 0 || !haveData {
		return 0, errors.New("WAV file without fmt/data chunk")
	}
	return ceilDiv(dataSize*1000, byteRate), nil
}

func ceilDiv(a, b int64) int64 { return (a + b - 1) / b }

// voiceIndex maps a voice file's lower-case base name (no extension) to its path.
type voiceIndex map[string]string

// indexVoiceDir collects every .ogg / .wav file under dir. The engine accepts
// either extension for a +Wave Name, so lookups ignore it; .ogg wins a tie.
func indexVoiceDir(dir string) (voiceIndex, error) {
	idx := voiceIndex{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".ogg" && ext != ".wav" {
			return nil
		}
		key := strings.ToLower(strings.TrimSuffix(d.Name(), filepath.Ext(d.Name())))
		if prev, ok := idx[key]; ok && strings.EqualFold(filepath.Ext(prev), ".ogg") {
			return nil
		}
		idx[key] = path
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning voice dir: %w", err)
	}
	return idx, nil
}

// lookup returns the file for a +Wave Name value, or "" if there is none.
func (v voiceIndex) lookup(wave string) string {
	w := strings.TrimSpace(wave)
	if w == "" {
		return ""
	}
	w = strings.TrimSuffix(w, filepath.Ext(w))
	return v[strings.ToLower(w)]
}
