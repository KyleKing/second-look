package termimg_test

import (
	"bytes"
	"encoding/base64"
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

// noisy is an image whose PNG runs past one kitty chunk, since noise does not
// compress.
func noisy(t *testing.T) string {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	seed := uint32(1)

	for i := range img.Pix {
		seed = seed*1664525 + 1013904223
		img.Pix[i] = byte(seed >> 24)
	}

	path := filepath.Join(t.TempDir(), "noise.png")

	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// pngOf is what every protocol carries for path: the image re-encoded as PNG.
func pngOf(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path) //nolint:gosec // a path the test itself just made
	if err != nil {
		t.Fatal(err)
	}

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}

	return base64.StdEncoding.EncodeToString(b.Bytes())
}

// kittyChunks splits kitty output into each escape's control data and payload.
func kittyChunks(t *testing.T, out string) ([]string, string) {
	t.Helper()

	var (
		controls []string
		payload  strings.Builder
	)

	for _, seq := range strings.SplitAfter(out, "\x1b\\") {
		if seq == "" {
			continue
		}

		body, ok := strings.CutPrefix(strings.TrimSuffix(seq, "\x1b\\"), "\x1b_G")
		control, data, found := strings.Cut(body, ";")

		if !ok || !found {
			t.Fatalf("%.40q is not one kitty escape", seq)
		}

		controls = append(controls, control)
		payload.WriteString(data)
	}

	return controls, payload.String()
}

func TestRender(t *testing.T) {
	t.Parallel()

	t.Run("kitty in one escape", renderKittyInOneEscape)

	t.Run("kitty in chunks", renderKittyInChunks)

	t.Run("iterm", renderIterm)

	t.Run("sixel", renderSixel)

	t.Run("refusals", renderRefusals)
}

func renderKittyInOneEscape(t *testing.T) {
	t.Helper()
	t.Parallel()

	path := aPNG(t)

	var out bytes.Buffer
	if err := termimg.Render(&out, termimg.ProtoKitty, path, 80, 20); err != nil {
		t.Fatalf("rendering: %v", err)
	}

	if want := "\x1b_Ga=T,f=100,t=d,q=2,c=80,r=20;" + pngOf(t, path) + "\x1b\\"; out.String() != want {
		t.Errorf("kitty wrote %.80q, want %.80q", out.String(), want)
	}
}

func renderKittyInChunks(t *testing.T) {
	t.Helper()
	t.Parallel()

	path := noisy(t)

	var out bytes.Buffer
	if err := termimg.Render(&out, termimg.ProtoKitty, path, 40, 10); err != nil {
		t.Fatalf("rendering: %v", err)
	}

	controls, payload := kittyChunks(t, out.String())
	if payload != pngOf(t, path) {
		t.Error("the chunks do not join into the image")
	}

	if len(controls) < 3 {
		t.Fatalf("a %d-byte payload went out in %d chunk(s)", len(payload), len(controls))
	}

	for i, c := range controls {
		want := "m=1"

		switch i {
		case 0:
			want = "a=T,f=100,t=d,q=2,c=40,r=10,m=1"
		case len(controls) - 1:
			want = "m=0"
		}

		if c != want {
			t.Errorf("chunk %d says %q, want %q", i, c, want)
		}
	}
}

func renderIterm(t *testing.T) {
	t.Helper()
	t.Parallel()

	path := aPNG(t)

	var out bytes.Buffer
	if err := termimg.Render(&out, termimg.ProtoIterm, path, 80, 20); err != nil {
		t.Fatalf("rendering: %v", err)
	}

	want := "\x1b]1337;File=inline=1;width=80;height=20;preserveAspectRatio=1:" + pngOf(t, path) + "\a"
	if out.String() != want {
		t.Errorf("iterm wrote %.80q, want %.80q", out.String(), want)
	}
}

func renderSixel(t *testing.T) {
	t.Helper()
	t.Parallel()

	var out bytes.Buffer
	if err := termimg.Render(&out, termimg.ProtoSixel, aPNG(t), 80, 20); err != nil {
		t.Fatalf("rendering: %v", err)
	}

	if !strings.HasPrefix(out.String(), "\x1bP") || !strings.HasSuffix(out.String(), "\x1b\\") {
		t.Errorf("sixel output is not one DCS: %.40q", out.String())
	}
}

func renderRefusals(t *testing.T) {
	t.Helper()
	t.Parallel()

	clip := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(clip, []byte("not a real video"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		proto termimg.Protocol
		path  string
		want  string
	}{
		{termimg.ProtoKitty, clip, "not a still image"},
		{termimg.ProtoKitty, filepath.Join(t.TempDir(), "gone.png"), "reading "},
		{termimg.ProtoNone, aPNG(t), ""},
	} {
		var out bytes.Buffer

		err := termimg.Render(&out, tc.proto, tc.path, 80, 20)
		if err == nil || !strings.Contains(err.Error(), tc.want) || out.Len() != 0 {
			t.Errorf("%v of %s answered %v and wrote %d bytes", tc.proto, filepath.Base(tc.path), err, out.Len())
		}
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
