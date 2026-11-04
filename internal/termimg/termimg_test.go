package termimg_test

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/termimg"
)

func TestDetectEnv(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		env  map[string]string
		want termimg.Protocol
	}{
		"kitty term":   {map[string]string{"TERM": "xterm-kitty"}, termimg.ProtoKitty},
		"kitty window": {map[string]string{"KITTY_WINDOW_ID": "3"}, termimg.ProtoKitty},
		"wezterm":      {map[string]string{"TERM_PROGRAM": "WezTerm"}, termimg.ProtoKitty},
		"ghostty":      {map[string]string{"TERM_PROGRAM": "ghostty"}, termimg.ProtoKitty},
		"iterm":        {map[string]string{"TERM_PROGRAM": "iTerm.app"}, termimg.ProtoIterm},
		"foot":         {map[string]string{"TERM": "foot"}, termimg.ProtoSixel},
		"dumb":         {map[string]string{"TERM": "xterm-256color"}, termimg.ProtoNone},
		"empty":        {map[string]string{}, termimg.ProtoNone},
		"kitty beats iterm": {
			map[string]string{"TERM_PROGRAM": "WezTerm", "TERM": "xterm-256color"},
			termimg.ProtoKitty,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := termimg.DetectEnv(func(key string) string { return tt.env[key] })
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// aPNG is a real decodable image, since every protocol's encoder reads the
// file for itself.
func aPNG(t *testing.T) string {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 4, 4))

	path := filepath.Join(t.TempDir(), "x.png")

	file, err := os.Create(path) //nolint:gosec // a path the test itself just made
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestRenderKitty(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	if err := termimg.Render(&out, termimg.ProtoKitty, aPNG(t), 80, 20); err != nil {
		t.Fatalf("rendering: %v", err)
	}

	got := out.String()
	if !strings.HasPrefix(got, "\x1b_Ga=T,f=100,t=d,q=2,c=80,r=20") {
		t.Errorf("the kitty header is wrong: %.60q", got)
	}
	if !strings.HasSuffix(got, "\x1b\\") {
		t.Errorf("the kitty trailer is missing: %.60q", got)
	}
}

func TestRenderIterm(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	if err := termimg.Render(&out, termimg.ProtoIterm, aPNG(t), 80, 20); err != nil {
		t.Fatalf("rendering: %v", err)
	}

	got := out.String()
	if !strings.HasPrefix(got, "\x1b]1337;File=inline=1;width=80;height=20;preserveAspectRatio=1:") {
		t.Errorf("the iterm header is wrong: %.80q", got)
	}
	if !strings.HasSuffix(got, "\a") {
		t.Errorf("the iterm terminator is missing: %.80q", got)
	}
}

func TestRenderSixel(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	if err := termimg.Render(&out, termimg.ProtoSixel, aPNG(t), 80, 20); err != nil {
		t.Fatalf("rendering: %v", err)
	}

	if !strings.HasPrefix(out.String(), "\x1bP") {
		t.Errorf("sixel output does not open with DCS: %.40q", out.String())
	}
}

func TestRenderRefusesNonImage(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(path, []byte("not a real video"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := termimg.Render(&bytes.Buffer{}, termimg.ProtoKitty, path, 80, 20)
	if err == nil || !strings.Contains(err.Error(), "not a still image") {
		t.Errorf("wanted a refusal naming the kind, got %v", err)
	}
}

func TestPassthroughDoublesEscapes(t *testing.T) {
	t.Parallel()

	got := termimg.Passthrough([]byte("\x1b_Ga=T;x\x1b\\"))

	if !bytes.HasPrefix(got, []byte("\x1bPtmux;")) {
		t.Errorf("the passthrough opener is missing: %q", got)
	}
	if !strings.Contains(string(got), "\x1b\x1b_G") {
		t.Errorf("the inner escape was not doubled: %q", got)
	}
	if !bytes.HasSuffix(got, []byte("\x1b\\")) {
		t.Errorf("the passthrough closer is missing: %q", got)
	}
}
