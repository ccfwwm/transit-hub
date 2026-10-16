package monitor_media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"time"
)

func contains(v []string, s string) bool {
	for _, x := range v {
		if x == s {
			return true
		}
	}
	return false
}

type numeric float64

func number(s string) (numeric, error) {
	n, e := strconv.ParseFloat(s, 64)
	if e != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, fmt.Errorf("invalid number")
	}
	return numeric(n), nil
}

var integer = regexp.MustCompile(`^[1-9][0-9]*$`)

func requestSeconds(v any) (int, error) {
	s := stringValue(v)
	if !integer.MatchString(s) {
		return 0, fail(400, "invalid_seconds", "Duration must be a positive integer")
	}
	n, e := strconv.Atoi(s)
	if e != nil || n > 3600 {
		return 0, fail(400, "invalid_seconds", "Duration exceeds validator bounds")
	}
	return n, nil
}
func orderedValues(v any) ([]any, error) {
	if v == nil {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fail(400, "invalid_references", "References must be ordered arrays")
	}
	return list, nil
}
func refString(v any) (string, string, error) {
	if s, ok := v.(string); ok {
		return s, "string", nil
	}
	if o, ok := v.(map[string]any); ok {
		s, ok := o["url"].(string)
		if !ok || s == "" {
			return "", "", fail(400, "invalid_reference", "Reference object requires url")
		}
		for k := range o {
			if k != "url" && k != "duration_seconds" {
				return "", "", fail(400, "invalid_reference", "Unknown reference object field")
			}
		}
		return s, "object", nil
	}
	return "", "", fail(400, "invalid_reference", "URL string or object required")
}
func validateArtifact(ctx context.Context, path string, a *Artifact) error {
	if a.Type == "image" {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		config, format, err := image.DecodeConfig(file)
		if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 100000000 {
			return fmt.Errorf("invalid image artifact")
		}
		// Decoding catches truncated files whose header alone is valid.
		if _, err = file.Seek(0, 0); err != nil {
			return err
		}
		if _, _, err = image.Decode(file); err != nil {
			return fmt.Errorf("incomplete image artifact")
		}
		a.MIME = "image/" + format
		return nil
	}
	d, err := probeMedia(ctx, path, a.Type)
	if err != nil {
		return err
	}
	a.Duration = &d
	return nil
}
func probeMedia(ctx context.Context, path, kind string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", "file,pipe", "-show_entries", "format=duration:stream=codec_type", "-of", "json", path)
	var output limitedBuffer
	command.Stdout = &output
	command.Stderr = &limitedBuffer{}
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("media duration unavailable")
	}
	var probe struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Type string `json:"codec_type"`
		} `json:"streams"`
	}
	if err := json.NewDecoder(bytes.NewBufferString(output.String())).Decode(&probe); err != nil {
		return "", fmt.Errorf("invalid media probe")
	}
	if kind != "" {
		found := false
		for _, s := range probe.Streams {
			if s.Type == kind {
				found = true
			}
		}
		if !found {
			return "", fmt.Errorf("missing media stream")
		}
	}
	value := probe.Format.Duration
	n, err := strconv.ParseFloat(value, 64)
	if err != nil || n <= 0 {
		return "", fmt.Errorf("invalid artifact duration")
	}
	return value, nil
}

type limitedBuffer struct{ raw []byte }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if len(b.raw) < 8192 {
		remaining := 8192 - len(b.raw)
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.raw = append(b.raw, p...)
	}
	return n, nil
}
func (b *limitedBuffer) String() string { return string(b.raw) }
