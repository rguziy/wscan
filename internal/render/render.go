package render

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/rguziy/wscan/internal/wifi"
)

// Options controls the output.
type Options struct {
	Profile Profile
	// Width is the terminal width in cells; 0 selects 100.
	Width int
	// Details adds a second line with extra facts below every access point.
	Details bool
	Sort    wifi.SortKey
	// Bands restricts the output to these bands; empty means all.
	Bands   map[wifi.Band]bool
	Version string
}

// Render writes the scan result as band-grouped tables.
func Render(w io.Writer, res *wifi.Result, o Options) error {
	if o.Width <= 0 {
		o.Width = 100
	}
	if o.Sort == "" {
		o.Sort = wifi.SortSignal
	}
	_, err := io.WriteString(w, (&renderer{o: o}).render(res))
	return err
}

type renderer struct {
	o  Options
	sb strings.Builder
}

func (r *renderer) paint(text string, s Style) string { return r.o.Profile.Paint(text, s) }

func (r *renderer) dim(text string) string { return r.paint(text, colDim) }

func (r *renderer) render(res *wifi.Result) string {
	aps := wifi.Select(res.APs, r.o.Bands, r.o.Sort)
	groups := wifi.GroupByBand(aps)

	r.header(res, len(aps))
	if len(aps) == 0 {
		r.sb.WriteString("  " + r.dim("No networks found.") + "\n")
		return r.sb.String()
	}

	cols := r.layout(r.columns(), aps)
	for _, b := range wifi.Bands() {
		if len(groups[b]) > 0 {
			r.band(b, groups[b], cols)
		}
	}
	r.legend()
	return r.sb.String()
}

func (r *renderer) header(res *wifi.Result, shown int) {
	title := r.paint("wscan", Style{Bold: true})
	if r.o.Version != "" && r.o.Version != "dev" {
		title += " " + r.dim("v"+r.o.Version)
	}
	parts := []string{
		r.paint(res.Interface, Style{Bold: true}),
		fmt.Sprintf("%d network%s", shown, plural(shown)),
		res.Time.Format("15:04:05"),
	}
	if res.Fresh {
		parts = append(parts, r.paint("fresh scan", colGood))
	} else {
		age := newestAge(res.APs)
		text := "cached results"
		if age >= 0 {
			text += fmt.Sprintf(", newest %s old", age.Round(100*time.Millisecond))
		}
		parts = append(parts, r.paint(text, colWarn))
	}
	r.sb.WriteString("\n  " + title + r.dim("  ·  ") + strings.Join(parts, r.dim("  ·  ")) + "\n")
	for _, n := range res.Notes {
		r.sb.WriteString("  " + r.paint("! "+n, colWarn) + "\n")
	}
}

func newestAge(aps []wifi.AP) time.Duration {
	best := time.Duration(-1)
	for _, ap := range aps {
		if ap.SeenAgoMS < 0 {
			continue
		}
		d := time.Duration(ap.SeenAgoMS) * time.Millisecond
		if best < 0 || d < best {
			best = d
		}
	}
	return best
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// ---------------------------------------------------------------- columns

type column struct {
	title string
	prio  int // higher survives longer on narrow terminals
	right bool
	width int
	cell  func(ap *wifi.AP) []seg
}

const (
	indent      = 2
	gutter      = "  "
	ssidMax     = 28
	ssidMin     = 14
	signalBarWd = 10
)

func (r *renderer) columns() []*column {
	return []*column{
		{title: "SSID", prio: 100, cell: func(ap *wifi.AP) []seg {
			if ap.Hidden {
				return []seg{{"<hidden>", colDim.italic()}}
			}
			return []seg{{wifi.Sanitize(ap.SSID), Style{Bold: true}}}
		}},
		{title: "BSSID", prio: 50, cell: func(ap *wifi.AP) []seg {
			return []seg{{ap.BSSID, colDim}}
		}},
		{title: "SIGNAL", prio: 90, cell: func(ap *wifi.AP) []seg {
			filled, empty := signalBar(ap.SignalDBM, signalBarWd)
			st := signalStyle(ap.SignalDBM)
			return []seg{
				{filled, st},
				{empty, colFaint},
				{fmt.Sprintf(" %4d", ap.SignalDBM), st},
				{" dBm", colDim},
			}
		}},
		{title: "CH", prio: 80, right: true, cell: func(ap *wifi.AP) []seg {
			return []seg{{fmt.Sprint(ap.Channel), bandColors[ap.Band]}}
		}},
		{title: "WIDTH", prio: 60, right: true, cell: func(ap *wifi.AP) []seg {
			return []seg{{fmt.Sprintf("%d MHz", ap.WidthMHz), Style{}}}
		}},
		{title: "WI-FI", prio: 70, cell: func(ap *wifi.AP) []seg {
			st := generationStyle(ap.Generation)
			if label := ap.WiFiLabel(); label != "" {
				return []seg{{label, st.bold()}, {" (" + ap.Standard + ")", colDim}}
			}
			return []seg{{ap.Standard, colDim}}
		}},
		{title: "MAX RATE", prio: 55, right: true, cell: func(ap *wifi.AP) []seg {
			return []seg{{formatRate(ap.MaxRateMbps), Style{}}, {" Mbps", colDim}}
		}},
		{title: "SECURITY", prio: 65, cell: func(ap *wifi.AP) []seg {
			p := strings.Replace(ap.Security.Protocol, "Enterprise", "Ent", 1)
			return []seg{{p, securityStyle(ap.Security.Protocol)}}
		}},
		{title: "UPTIME", prio: 40, right: true, cell: func(ap *wifi.AP) []seg {
			if ap.UptimeSeconds <= 0 {
				return []seg{{"-", colDim}}
			}
			return []seg{{formatUptime(ap.Uptime()), Style{}}}
		}},
		{title: "CC", prio: 30, cell: func(ap *wifi.AP) []seg {
			if ap.Country == "" {
				return []seg{{"-", colDim}}
			}
			return []seg{{ap.Country, Style{}}}
		}},
	}
}

// layout measures all columns over every access point, so that the columns
// line up across the bands, and drops columns that do not fit.
func (r *renderer) layout(cols []*column, aps []wifi.AP) []*column {
	for _, c := range cols {
		c.width = textWidth(c.title)
		for i := range aps {
			if w := segsWidth(c.cell(&aps[i])); w > c.width {
				c.width = w
			}
		}
	}
	cols[0].width = min(cols[0].width, ssidMax)

	total := func() int {
		n := indent + 2 // left margin and the connected marker
		for _, c := range cols {
			n += c.width
		}
		return n + len(gutter)*(len(cols)-1)
	}
	for total() > r.o.Width && len(cols) > 3 {
		if over := total() - r.o.Width; cols[0].width > ssidMin {
			cols[0].width -= min(over, cols[0].width-ssidMin)
			continue
		}
		drop := 1
		for i := range cols {
			if i != 0 && cols[i].prio < cols[drop].prio {
				drop = i
			}
		}
		cols = append(cols[:drop], cols[drop+1:]...)
	}
	return cols
}

// ---------------------------------------------------------------- bands

func (r *renderer) band(b wifi.Band, aps []wifi.AP, cols []*column) {
	bc := bandColors[b]
	total := indent
	for _, c := range cols {
		total += c.width + len(gutter)
	}
	total += 2 - len(gutter)

	label := fmt.Sprintf(" %s ", b)
	count := fmt.Sprintf(" %d network%s ", len(aps), plural(len(aps)))
	fill := max(total-5-textWidth(label)-textWidth(count), 3)
	r.sb.WriteString("\n  " + r.paint("──", bc) + r.paint(label, bc.bold()) +
		r.paint("─", bc) + r.dim(count) + r.dim(strings.Repeat("─", fill)) + "\n")

	r.sb.WriteString("  " + r.dim("Channels ") + r.channelUsage(aps) + "\n")

	// Column titles.
	r.sb.WriteString(strings.Repeat(" ", indent+2))
	for i, c := range cols {
		if i > 0 {
			r.sb.WriteString(gutter)
		}
		r.sb.WriteString(r.pad([]seg{{c.title, colDim.bold()}}, c.width, c.right, i == len(cols)-1))
	}
	r.sb.WriteString("\n")

	for i := range aps {
		ap := &aps[i]
		r.sb.WriteString(strings.Repeat(" ", indent))
		if ap.Connected {
			r.sb.WriteString(r.paint("●", colGood) + " ")
		} else {
			r.sb.WriteString("  ")
		}
		for j, c := range cols {
			if j > 0 {
				r.sb.WriteString(gutter)
			}
			r.sb.WriteString(r.pad(c.cell(ap), c.width, c.right, j == len(cols)-1))
		}
		r.sb.WriteString("\n")
		if r.o.Details {
			for k, line := range wrapParts(detailParts(ap), r.o.Width-indent-4-2) {
				prefix := "  "
				if k == 0 {
					prefix = "└ "
				}
				r.sb.WriteString(strings.Repeat(" ", indent+2) + r.dim(prefix+line) + "\n")
			}
		}
	}
}

// channelUsage summarises how many access points use each channel,
// colour-coded by congestion.
func (r *renderer) channelUsage(aps []wifi.AP) string {
	count := map[int]int{}
	for _, ap := range aps {
		count[ap.Channel]++
	}
	chans := make([]int, 0, len(count))
	for ch := range count {
		chans = append(chans, ch)
	}
	sort.Ints(chans)
	parts := make([]string, len(chans))
	for i, ch := range chans {
		parts[i] = fmt.Sprint(ch) + r.paint("×"+fmt.Sprint(count[ch]), congestionStyle(count[ch]))
	}
	return strings.Join(parts, "  ")
}

// pad fits a cell into width cells and aligns it. The last cell of a line is
// not padded on the right, so lines carry no trailing spaces.
func (r *renderer) pad(segs []seg, width int, right, last bool) string {
	segs = fitSegs(segs, width)
	gap := strings.Repeat(" ", max(width-segsWidth(segs), 0))
	if last && !right {
		gap = ""
	}
	var sb strings.Builder
	if right {
		sb.WriteString(gap)
	}
	for _, s := range segs {
		sb.WriteString(r.paint(s.text, s.style))
	}
	if !right {
		sb.WriteString(gap)
	}
	return sb.String()
}

func (r *renderer) legend() {
	const title = "Signal (dBm)  "
	bounds := [...]string{"≥ -50", "≥ -60", "≥ -70", "≥ -80", "< -80"}

	// Build the entries twice: painted for output, plain for measuring.
	var painted []string
	plainWidth := 2 + textWidth(title)
	for i, name := range signalNames {
		label := name + " " + bounds[i]
		painted = append(painted, r.paint("■", signalStyles[i])+" "+r.dim(label))
		plainWidth += 2 + textWidth("■ "+label)
	}
	plainWidth -= 2 // no separator after the last entry

	sep := "  "
	if plainWidth <= r.o.Width {
		r.sb.WriteString("\n  " + r.dim(title) + strings.Join(painted, sep) + "\n\n")
		return
	}
	// Too narrow for one line: title on its own line, entries wrapped below.
	r.sb.WriteString("\n  " + r.dim(strings.TrimSpace(title)) + "\n")
	lineWidth, line := 0, "  "
	for i, e := range painted {
		w := 2 + textWidth("■ "+signalNames[i]+" "+bounds[i])
		if lineWidth > 0 && 2+lineWidth+w > r.o.Width {
			r.sb.WriteString(line + "\n")
			lineWidth, line = 0, "  "
		}
		if lineWidth > 0 {
			line += sep
		}
		line += e
		lineWidth += w
	}
	r.sb.WriteString(line + "\n\n")
}

// ---------------------------------------------------------------- formatting

// signalBar draws a bar of the given width in cells, with half-cell
// resolution: -100 dBm is empty, -40 dBm full.
func signalBar(dbm, cells int) (filled, empty string) {
	pct := math.Min(math.Max(float64(dbm+100)/60, 0), 1)
	// Half-cell resolution: the left half block (U+258C) exists in every
	// terminal font, unlike the finer eighth blocks.
	halves := int(math.Round(pct * float64(cells) * 2))
	full := halves / 2
	filled = strings.Repeat("█", full)
	used := full
	if halves%2 == 1 {
		filled += "▌"
		used++
	}
	return filled, strings.Repeat("░", cells-used)
}

func formatRate(mbps float64) string {
	if mbps <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f", mbps)
}

// formatUptime renders a duration compactly: "13d 05h", "05h 12m", "46m 37s".
func formatUptime(d time.Duration) string {
	s := int64(d / time.Second)
	days, hours, mins, secs := s/86400, s%86400/3600, s%3600/60, s%60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %02dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%02dh %02dm", hours, mins)
	}
	return fmt.Sprintf("%02dm %02ds", mins, secs)
}

// wrapParts joins parts with " · ", starting a new line before a part that
// would not fit in width cells.
func wrapParts(parts []string, width int) []string {
	const sep = " · "
	var lines []string
	cur := ""
	for _, p := range parts {
		switch {
		case cur == "":
			cur = p
		case textWidth(cur)+len(sep)+textWidth(p) <= width:
			cur += sep + p
		default:
			lines = append(lines, cur)
			cur = p
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// detailParts collects the facts that do not fit in the table.
func detailParts(ap *wifi.AP) []string {
	var p []string
	if ap.BeaconIntervalMS > 0 {
		p = append(p, fmt.Sprintf("beacon %.1f ms", ap.BeaconIntervalMS))
	}
	if ap.MaxRateMbps > 0 {
		p = append(p, fmt.Sprintf("rate %s–%s Mbps", formatRate(ap.MinRateMbps), formatRate(ap.MaxRateMbps)))
	}
	if ap.Streams > 0 {
		p = append(p, fmt.Sprintf("%d stream%s", ap.Streams, plural(ap.Streams)))
	}
	if ap.Load != nil {
		p = append(p, fmt.Sprintf("load %d station%s, %d%% busy", ap.Load.Stations, plural(ap.Load.Stations), ap.Load.Utilization))
	}
	if ap.BSSColor > 0 {
		p = append(p, fmt.Sprintf("BSS color %d", ap.BSSColor))
	}
	if len(ap.Roaming) > 0 {
		p = append(p, "802.11"+strings.Join(ap.Roaming, "/"))
	}
	if len(ap.Security.Ciphers) > 0 {
		p = append(p, strings.Join(ap.Security.Ciphers, "/"))
	}
	if len(ap.Security.AKMs) > 0 {
		p = append(p, strings.Join(ap.Security.AKMs, "+"))
	}
	if ap.Security.PMF != "" {
		p = append(p, "PMF "+ap.Security.PMF)
	}
	if ap.Security.WPS {
		p = append(p, "WPS")
	}
	if ap.SeenAgoMS >= 0 {
		p = append(p, fmt.Sprintf("seen %.1fs ago", float64(ap.SeenAgoMS)/1000))
	}
	return p
}
