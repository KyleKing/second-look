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

// scrollMark is a row worth knowing the position of that the frame is not
// showing. The track is where it goes: a second column would cost every row a
// cell to say something only a handful of rows have to say.
const scrollMark = "•"

// mark puts rows onto the track by where they sit in the content. The thumb
// wins a collision, since a row inside the frame is one the reader can see.
func mark(bar []string, total int, rows []int) []string {
	if len(bar) == 0 || total < 1 {
		return bar
	}

	for _, at := range rows {
		i := at * len(bar) / total
		if i < 0 || i >= len(bar) || bar[i] != scrollTrack {
			continue
		}

		bar[i] = scrollMark
	}

	return bar
}

// alongside puts the bar on the right edge of lines already rendered, padding
// each to the same column so the bar is straight whatever the lines carry.
func alongside(lines, bar []string, s styles, width int) []string {
	if bar == nil {
		return lines
	}

	for i := range lines {
		glyph := s.subtitle.Render(bar[i])

		switch bar[i] {
		case scrollThumb:
			glyph = s.rail.Render(bar[i])
		case scrollMark:
			glyph = s.file.Render(bar[i])
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
