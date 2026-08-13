package miniapp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// extractVideoAudio pulls the audio track out of a shared video with ffmpeg so it
// can ride the existing ASR path (a meeting recorded as .mp4, a screen capture).
// UNTRUSTED bytes, so it runs like the document converters: the bytes go to a
// throwaway temp dir, the command takes only literal args (no tainted string
// reaches a shell), and a timeout bounds it. Output is 16 kHz mono WAV — the form
// the ASR sidecar wants.
func extractVideoAudio(ctx context.Context, video []byte) ([]byte, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, fmt.Errorf("ffmpeg 미설치")
	}
	dir, err := os.MkdirTemp("", "deneb-vid-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "in"), video, 0o600); err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	// Literal args, run inside the temp dir. -vn drops the video stream; -nostdin/-y
	// keep ffmpeg non-interactive.
	cmd := exec.CommandContext(runCtx, "ffmpeg", //nolint:gosec // G204 — literal args, temp dir, no shell
		"-nostdin", "-y", "-i", "in", "-vn", "-ac", "1", "-ar", "16000", "-f", "wav", "out.wav")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg 오디오 추출 실패: %w", err)
	}
	out, err := os.ReadFile(filepath.Join(dir, "out.wav"))
	if err != nil {
		return nil, fmt.Errorf("ffmpeg 오디오 출력 없음: %w", err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("동영상에 오디오 트랙이 없음")
	}
	return out, nil
}
