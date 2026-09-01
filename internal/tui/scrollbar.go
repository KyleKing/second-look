package tui

// The scrollbar's two glyphs. Both are drawn, so where the frame sits reads off
// the length of the thumb as well as its position, and a terminal with no color
// still shows it.
const (
	scrollTrack = "│"
	scrollThumb = "┃"
)

// scrollbar is the column on the right edge saying how much of the content the
// frame is showing and where in it. Content that fits gets none: a full-height
// thumb says the same thing as no bar at all and costs a column to say it.
func scrollbar(height, total, offset int) []string {
	if height < 1 || total <= height {
		return nil
	}

	size := max(1, height*height/total)
	// The thumb reaches the bottom exactly when the last line is on screen, so
	// the position is measured against how far the offset can travel rather
	// than against the content.
	travel := max(1, total-height)
	at := min((height-size)*offset/travel, height-size)

	out := make([]string, height)
	for i := range out {
		out[i] = scrollTrack
		if i >= at && i < at+size {
			out[i] = scrollThumb
		}
	}

	return out
}

// alongside puts the bar on the right edge of lines already rendered, padding
// each to the same column so the bar is straight whatever the lines carry.
func alongside(lines []string, bar []string, s styles, width int) []string {
	if bar == nil {
		return lines
	}

	for i := range lines {
		glyph := s.subtitle.Render(bar[i])
		if bar[i] == scrollThumb {
			glyph = s.rail.Render(bar[i])
		}

		lines[i] = pad(lines[i], width-1) + glyph
	}

	return lines
}

// trackWidth is the column the bar occupies, subtracted from what a row is cut
// to so the two never collide. Content that fits keeps the column: a bar that
// says nothing is not worth a column of every row.
const trackWidth = 1

func bodyWidth(width int, bar []string) int {
	if bar == nil {
		return width - 1
	}

	return width - 1 - trackWidth
}
