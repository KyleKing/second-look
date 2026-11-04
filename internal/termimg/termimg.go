// Package termimg draws an image on the real terminal through the inline
// graphics protocol it speaks. Kitty is asked for first, then iTerm2, then
// sixel, because that is the order of what the terminals this runs on answer.
// A terminal that speaks none of them reports ProtoNone and the caller says so
// rather than emitting escape sequences a dumb terminal would print.
package termimg

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // register gif for image.Decode
	_ "image/jpeg" // register jpeg for image.Decode
	"image/png"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-sixel"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register webp for image.Decode
)

// Protocol is the inline graphics protocol a terminal answered for.
type Protocol int

// The protocols a terminal can answer for, in the order Detect tries them:
// none, then kitty, iTerm2, and sixel.
const (
	ProtoNone Protocol = iota
	ProtoKitty
	ProtoIterm
	ProtoSixel
)

// errNotAnImage names a file image.Decode does not know, which for the files
// attach: accepts is a video — previewable nowhere a terminal draws.
var errNotAnImage = errors.New("not a still image the terminal can draw")

// errNoProtocol is a Render asked to draw for a terminal that answered no
// protocol at all.
var errNoProtocol = errors.New("no inline image protocol")

// Detect asks the environment which protocol the terminal speaks, in the order
// they are tried. It cannot be answered more rigorously than this without a
// device-attribute round trip, which a process that has already lost the
// terminal to a child cannot do from inside the pane.
func Detect() Protocol {
	return DetectEnv(os.Getenv)
}

// DetectEnv is Detect against an environment a test can build.
func DetectEnv(env func(string) string) Protocol {
	term, prog := strings.ToLower(env("TERM")), env("TERM_PROGRAM")

	switch {
	case env("KITTY_WINDOW_ID") != "", strings.Contains(term, "kitty"),
		prog == "WezTerm", prog == "ghostty":
		return ProtoKitty
	case prog == "iTerm.app", prog == "mintty", prog == "rio", prog == "vscode":
		return ProtoIterm
	case strings.Contains(term, "foot"), strings.Contains(term, "contour"),
		strings.Contains(term, "mlterm"), strings.Contains(term, "yaft"):
		return ProtoSixel
	}

	return ProtoNone
}

// InTMUX reports whether the run is inside a tmux, where a graphics sequence
// has to be wrapped in a DCS passthrough to reach the outer terminal.
func InTMUX() bool { return os.Getenv("TMUX") != "" }

// Passthrough wraps one terminal escape so tmux forwards it to the terminal
// outside it. Inside the wrapper every ESC doubles, and a tmux without
// allow-passthrough drops the whole thing, which reads as nothing drawn rather
// than as an error.
func Passthrough(seq []byte) []byte {
	out := bytes.NewBufferString("\x1bPtmux;")
	out.Write(bytes.ReplaceAll(seq, []byte("\x1b"), []byte("\x1b\x1b")))
	out.WriteString("\x1b\\")

	return out.Bytes()
}

// load reads an image file and answers it decoded once plus as PNG bytes, the
// one encoding every protocol here carries. A gif keeps only its first frame:
// the protocols send one image per sequence either way.
func load(path string) (image.Image, []byte, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the path is the caller's own image ref
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, errNotAnImage)
	}

	var pngs bytes.Buffer
	if err := png.Encode(&pngs, img); err != nil {
		return nil, nil, fmt.Errorf("encoding %s as PNG: %w", path, err)
	}

	return img, pngs.Bytes(), nil
}

// Render writes the escape sequence drawing path inside a cols by rows box,
// using the protocol the caller detected. The PNG form is sent where one is
// taken and the decoded image where a protocol wants pixels, so the file is
// decoded once either way.
func Render(w io.Writer, proto Protocol, path string, cols, rows int) error {
	img, pngs, err := load(path)
	if err != nil {
		return err
	}

	var seq []byte

	switch proto {
	case ProtoKitty:
		seq = kitty(pngs, cols, rows)
	case ProtoIterm:
		seq = iterm(pngs, cols, rows)
	case ProtoSixel:
		var buf bytes.Buffer
		if err := sixel.NewEncoder(&buf).Encode(fit(img, cols, rows)); err != nil {
			return fmt.Errorf("encoding %s as sixel: %w", path, err)
		}

		seq = buf.Bytes()
	case ProtoNone:
		return errNoProtocol
	}

	if InTMUX() {
		seq = Passthrough(seq)
	}

	if _, err := w.Write(seq); err != nil {
		return fmt.Errorf("writing the image: %w", err)
	}

	return nil
}

// Delete erases what Render left up, for the protocols whose images float over
// the text instead of scrolling with it. Only kitty draws that way; the others
// print the image as part of the stream and it scrolls off on its own.
func Delete(w io.Writer, proto Protocol) {
	if proto != ProtoKitty {
		return
	}

	seq := []byte("\x1b_Ga=d,d=a,q=2\x1b\\")
	if InTMUX() {
		seq = Passthrough(seq)
	}

	//nolint:errcheck // the delete is best-effort; a dead tty fails the redraw too
	_, _ = w.Write(seq)
}

// Kitty emits the image as chunked base64 PNG data displayed in place. C and r
// bound the box in cells and kitty fits inside it keeping the aspect, so the
// image's own dimensions are never needed.
func kitty(pngs []byte, cols, rows int) []byte {
	const chunk = 4096

	b64 := base64.StdEncoding.EncodeToString(pngs)

	var out bytes.Buffer

	for i := 0; i < len(b64); i += chunk {
		end := min(i+chunk, len(b64))

		switch {
		case i == 0 && end == len(b64):
			fmt.Fprintf(&out, "\x1b_Ga=T,f=100,t=d,q=2,c=%d,r=%d;%s\x1b\\", cols, rows, b64)
		case i == 0:
			fmt.Fprintf(&out, "\x1b_Ga=T,f=100,t=d,q=2,c=%d,r=%d,m=1;%s\x1b\\", cols, rows, b64[i:end])
		case end == len(b64):
			fmt.Fprintf(&out, "\x1b_Gm=0;%s\x1b\\", b64[i:end])
		default:
			fmt.Fprintf(&out, "\x1b_Gm=1;%s\x1b\\", b64[i:end])
		}
	}

	return out.Bytes()
}

// iterm emits the OSC 1337 form, which takes the PNG bytes as one base64 blob
// and the box as cell counts preserveAspectRatio fits inside.
func iterm(pngs []byte, cols, rows int) []byte {
	return []byte(fmt.Sprintf(
		"\x1b]1337;File=inline=1;width=%d;height=%d;preserveAspectRatio=1:%s\a",
		cols, rows, base64.StdEncoding.EncodeToString(pngs)))
}

// fit scales an image into a cell box for sixel, which wants pixels and gets
// none of the box-fitting the other two do themselves. Cell pixels are read as
// twice the width when nothing better is known, the shape most terminals are.
func fit(img image.Image, cols, rows int) image.Image {
	const cell, tall = 9, 2

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	boxW, boxH := cols*cell, rows*cell*tall

	scale := min(float64(boxW)/float64(w), float64(boxH)/float64(h), 1.0)
	if scale >= 1.0 {
		return img
	}

	dw, dh := max(int(float64(w)*scale), 1), max(int(float64(h)*scale), 1)
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)

	return dst
}
