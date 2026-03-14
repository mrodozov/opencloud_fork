// Package video provides utilities for extracting keyframes from video files
// using ffmpeg. ffmpeg and ffprobe must be installed on the target device.
package video

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultMaxKeyframes is used when the caller passes maxFrames <= 0.
const DefaultMaxKeyframes = 8

// ExtractKeyframes writes videoData to a temp file, uses ffmpeg to extract up
// to maxFrames evenly-spaced I-frames, and returns each frame as a JPEG byte
// slice. Temporary files are cleaned up before returning.
func ExtractKeyframes(videoData []byte, maxFrames int) ([][]byte, error) {
	if maxFrames <= 0 {
		maxFrames = DefaultMaxKeyframes
	}

	// write video to a temp file so ffmpeg can seek it
	tmp, err := os.CreateTemp("", "vision-video-*")
	if err != nil {
		return nil, fmt.Errorf("keyframe: creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(videoData); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("keyframe: writing temp file: %w", err)
	}
	tmp.Close()

	// probe duration so we can compute the sampling interval
	duration, err := probeDuration(tmpPath)
	if err != nil || duration <= 0 {
		// fall back: grab first maxFrames I-frames regardless of position
		return extractIFrames(tmpPath, maxFrames)
	}

	return extractEvenly(tmpPath, duration, maxFrames)
}

// probeDuration returns the video duration in seconds via ffprobe.
func probeDuration(path string) (float64, error) {
	out, err := exec.Command(
		"ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	).Output()
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(out))
	return strconv.ParseFloat(s, 64)
}

// extractEvenly uses an fps filter to extract maxFrames frames spaced evenly
// across the full duration of the video.
func extractEvenly(path string, duration float64, maxFrames int) ([][]byte, error) {
	fps := float64(maxFrames) / duration
	if fps > 1 {
		fps = 1 // never exceed 1 fps to avoid flooding on short clips
	}

	outDir, err := os.MkdirTemp("", "vision-frames-*")
	if err != nil {
		return nil, fmt.Errorf("keyframe: creating output dir: %w", err)
	}
	defer os.RemoveAll(outDir)

	pattern := filepath.Join(outDir, "frame_%04d.jpg")
	cmd := exec.Command(
		"ffmpeg",
		"-i", path,
		"-vf", fmt.Sprintf("fps=%.6f", fps),
		"-q:v", "2",    // JPEG quality (2 = near-lossless)
		"-frames:v", strconv.Itoa(maxFrames),
		"-f", "image2",
		pattern,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("keyframe: ffmpeg: %w (stderr: %s)", err, stderr.String())
	}

	return readFrameFiles(outDir)
}

// extractIFrames extracts up to maxFrames I-frames (keyframes in the codec
// sense) using a select filter. Used as a fallback when duration is unknown.
func extractIFrames(path string, maxFrames int) ([][]byte, error) {
	outDir, err := os.MkdirTemp("", "vision-iframes-*")
	if err != nil {
		return nil, fmt.Errorf("keyframe: creating output dir: %w", err)
	}
	defer os.RemoveAll(outDir)

	pattern := filepath.Join(outDir, "frame_%04d.jpg")
	cmd := exec.Command(
		"ffmpeg",
		"-i", path,
		"-vf", `select=eq(pict_type\,I)`,
		"-vsync", "vfr",
		"-q:v", "2",
		"-frames:v", strconv.Itoa(maxFrames),
		"-f", "image2",
		pattern,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("keyframe: ffmpeg (I-frame): %w (stderr: %s)", err, stderr.String())
	}

	return readFrameFiles(outDir)
}

// readFrameFiles reads all JPEG files from dir, sorted by name, and returns
// their contents as byte slices.
func readFrameFiles(dir string) ([][]byte, error) {
	entries, err := filepath.Glob(filepath.Join(dir, "frame_*.jpg"))
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("keyframe: ffmpeg produced no output frames")
	}

	frames := make([][]byte, 0, len(entries))
	for _, e := range entries {
		data, err := os.ReadFile(e)
		if err != nil {
			continue
		}
		frames = append(frames, data)
	}
	return frames, nil
}
