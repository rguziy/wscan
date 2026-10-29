package render

import (
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/rguziy/wscan/internal/wifi"
)

// RenderRecommendation writes channel recommendations: for every band the
// least loaded channels with their frequencies, so nobody has to look up
// which frequency a channel number stands for.
func RenderRecommendation(w io.Writer, rec *wifi.Recommendation, o Options) error {
	if o.Width <= 0 {
		o.Width = 100
	}
	r := &renderer{o: o}
	_, err := io.WriteString(w, r.recommendation(rec))
	return err
}

// loadScoreCap is the score at which the load bar is full.
const loadScoreCap = 3.0

func levelStyle(level string) Style {
	switch level {
	case wifi.LevelClear:
		return colGood
	case wifi.LevelLight:
		return colLime
	case wifi.LevelModerate:
		return colYellow
	}
	return colRed
}

// loadBar draws a bar of the given width: the fuller, the more loaded.
func loadBar(score float64, cells int) (filled, empty string) {
	pct := math.Min(math.Max(score/loadScoreCap, 0), 1)
	halves := int(math.Round(pct * float64(cells) * 2))
	if score > 0 && halves == 0 {
		halves = 1 // any load at all shows
	}
	full := halves / 2
	filled = strings.Repeat("█", full)
	used := full
	if halves%2 == 1 {
		filled += "▌"
		used++
	}
	return filled, strings.Repeat("░", cells-used)
}

// recColumn is one column of a recommendation table.
type recColumn struct {
	title string
	prio  int // higher survives longer on narrow terminals
	right bool
	width int
	cells [][]seg // one cell per row
}

func (r *renderer) recommendation(rec *wifi.Recommendation) string {
	title := r.paint("wscan", Style{Bold: true})
	if r.o.Version != "" && r.o.Version != "dev" {
		title += " " + r.dim("v"+r.o.Version)
	}
	var parts []string
	if rec.Interface != "" {
		parts = append(parts, rec.Interface)
	}
	parts = append(parts,
		"channel recommendations",
		fmt.Sprintf("%d scan%s", rec.Scans, plural(rec.Scans)),
		rec.Time.Format("15:04:05"))
	sep := "  ·  "
	plain := "wscan"
	if r.o.Version != "" && r.o.Version != "dev" {
		plain += " v" + r.o.Version
	}
	plain += sep + strings.Join(parts, sep)
	if 2+textWidth(plain) <= r.o.Width {
		if rec.Interface != "" {
			parts[0] = r.paint(rec.Interface, Style{Bold: true})
		}
		r.sb.WriteString("\n  " + title + r.dim(sep) + strings.Join(parts, r.dim(sep)) + "\n")
	} else {
		// Too narrow for one line: the title alone, the facts wrapped below.
		r.sb.WriteString("\n  " + title + "\n")
		for _, line := range wrapParts(parts, r.o.Width-2) {
			r.sb.WriteString("  " + r.dim(line) + "\n")
		}
	}
	for _, n := range rec.Notes {
		r.warn(n)
	}

	country := "unknown"
	if rec.Country != "" {
		country = rec.Country
	}
	r.sb.WriteString("\n  " + r.dim("Country  ") + country + r.dim(" ("+rec.CountrySource+")") + "\n")
	if len(rec.Ignored) > 0 {
		const label = "Ignored  "
		lines := wrapWords(strings.Join(rec.Ignored, ", "), r.o.Width-2-len(label))
		for i, l := range lines {
			prefix := strings.Repeat(" ", len(label))
			if i == 0 {
				prefix = r.dim(label)
			}
			r.sb.WriteString("  " + prefix + l + "\n")
		}
	}
	for _, h := range rec.Hints {
		r.warn(h)
	}

	for i := range rec.Bands {
		r.recBand(&rec.Bands[i])
	}

	notes := []string{
		"Load 0 is empty; one strong network on the same channel is about 0.7",
		"Channel is the primary channel to enter in the router, Span the spectrum it occupies",
		"DFS channels must yield to radar, so the router may leave them without warning",
		"If a channel is missing in the router, take the next one",
	}
	r.sb.WriteString("\n")
	for _, n := range notes {
		for _, line := range wrapWords(n, r.o.Width-4) {
			r.sb.WriteString("  " + r.dim(line) + "\n")
		}
	}
	r.sb.WriteString("\n")
	return r.sb.String()
}

// warn prints a wrapped warning line marked with "!".
func (r *renderer) warn(text string) {
	for i, l := range wrapWords(text, r.o.Width-4) {
		prefix := "  "
		if i == 0 {
			prefix = "! "
		}
		r.sb.WriteString("  " + r.paint(prefix+l, colWarn) + "\n")
	}
}

func (r *renderer) recBand(br *wifi.BandRecommendation) {
	bc := bandColors[br.Band]
	label := fmt.Sprintf(" %s ", br.Band)
	info := fmt.Sprintf(" %d MHz · %d network%s ", br.WidthMHz, br.Networks, plural(br.Networks))
	if br.Support == wifi.SupportNone || br.Support == wifi.SupportDisabled {
		info = " not scannable "
	}

	cols := r.recColumns(br)
	total := indent
	for _, c := range cols {
		total += c.width + len(gutter)
	}
	total = max(total-len(gutter), 30)
	fill := max(total-3-textWidth(label)-textWidth(info), 3)
	r.sb.WriteString("\n  " + r.paint("──", bc) + r.paint(label, bc.bold()) +
		r.paint("─", bc) + r.dim(info) + r.dim(strings.Repeat("─", fill)) + "\n")

	if len(br.Candidates) == 0 {
		if br.Note != "" {
			r.sb.WriteString("  " + r.dim(br.Note) + "\n")
		}
		return
	}

	// Titles.
	var line strings.Builder
	line.WriteString(strings.Repeat(" ", indent))
	for i, c := range cols {
		if i > 0 {
			line.WriteString(gutter)
		}
		line.WriteString(r.pad([]seg{{c.title, colDim.bold()}}, c.width, c.right, i == len(cols)-1))
	}
	r.sb.WriteString(strings.TrimRight(line.String(), " ") + "\n")

	rows := len(br.Candidates)
	if br.Current != nil {
		rows++
	}
	for row := 0; row < rows; row++ {
		line.Reset()
		line.WriteString(strings.Repeat(" ", indent))
		for i, c := range cols {
			if i > 0 {
				line.WriteString(gutter)
			}
			line.WriteString(r.pad(c.cells[row], c.width, c.right, i == len(cols)-1))
		}
		// An empty last cell (no DFS flag) must not leave trailing spaces.
		r.sb.WriteString(strings.TrimRight(line.String(), " ") + "\n")
	}
	if br.Note != "" {
		r.sb.WriteString("  " + r.dim(br.Note) + "\n")
	}
}

// recColumns builds the columns for a band and drops the least important
// ones until the table fits the terminal.
func (r *renderer) recColumns(br *wifi.BandRecommendation) []*recColumn {
	rows := append([]wifi.Candidate(nil), br.Candidates...)
	if br.Current != nil {
		rows = append(rows, *br.Current)
	}
	isNow := func(i int) bool { return br.Current != nil && i == len(rows)-1 }

	mk := func(title string, prio int, right bool, cell func(i int, c wifi.Candidate) []seg) *recColumn {
		col := &recColumn{title: title, prio: prio, right: right, width: textWidth(title)}
		for i, c := range rows {
			s := cell(i, c)
			col.cells = append(col.cells, s)
			col.width = max(col.width, segsWidth(s))
		}
		return col
	}

	cols := []*recColumn{
		mk("#", 100, false, func(i int, c wifi.Candidate) []seg {
			if isNow(i) {
				return []seg{{"now", colDim.italic()}}
			}
			return []seg{{fmt.Sprint(i + 1), colDim}}
		}),
		mk("CH", 100, true, func(i int, c wifi.Candidate) []seg {
			return []seg{{fmt.Sprint(c.Channel), bandColors[br.Band].bold()}}
		}),
		mk("FREQUENCY", 90, true, func(i int, c wifi.Candidate) []seg {
			return []seg{{fmt.Sprint(c.FrequencyMHz), Style{}}, {" MHz", colDim}}
		}),
		mk("WIDTH", 60, true, func(i int, c wifi.Candidate) []seg {
			return []seg{{fmt.Sprint(c.WidthMHz), Style{}}, {" MHz", colDim}}
		}),
		mk("SPAN", 70, true, func(i int, c wifi.Candidate) []seg {
			if c.WidthMHz <= 20 {
				return []seg{{fmt.Sprintf("%d–%d", c.FromMHz, c.ToMHz), colDim}, {" MHz", colDim}}
			}
			return []seg{{fmt.Sprintf("%d–%d", c.FromMHz, c.ToMHz), Style{}}, {" MHz", colDim}}
		}),
		mk("LOAD", 95, false, func(i int, c wifi.Candidate) []seg {
			filled, empty := loadBar(c.Score, 10)
			st := levelStyle(c.Level)
			return []seg{{filled, st}, {empty, colFaint}, {"  " + c.Level, st}}
		}),
		mk("SCORE", 50, true, func(i int, c wifi.Candidate) []seg {
			return []seg{{fmt.Sprintf("%.2f", c.Score), colDim}}
		}),
		mk("NETS", 40, true, func(i int, c wifi.Candidate) []seg {
			return []seg{{fmt.Sprint(c.Networks), Style{}}}
		}),
		mk("STRONGEST", 30, true, func(i int, c wifi.Candidate) []seg {
			if c.Networks == 0 {
				return []seg{{"-", colDim}}
			}
			return []seg{{fmt.Sprint(c.StrongestDBM), Style{}}, {" dBm", colDim}}
		}),
		mk("", 80, false, func(i int, c wifi.Candidate) []seg {
			if c.DFS {
				return []seg{{"DFS", colWarn}}
			}
			return nil
		}),
	}
	// The unnamed DFS column is useless in bands without DFS.
	if br.Band != wifi.Band5G {
		cols = cols[:len(cols)-1]
	}

	for {
		total := indent
		for _, c := range cols {
			total += c.width + len(gutter)
		}
		total -= len(gutter)
		if total <= r.o.Width-2 || len(cols) <= 3 {
			return cols
		}
		drop := 0
		for i, c := range cols {
			if c.prio < cols[drop].prio {
				drop = i
			}
		}
		cols = append(cols[:drop], cols[drop+1:]...)
	}
}
