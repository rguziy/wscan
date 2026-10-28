// Command wscan lists the Wi-Fi networks around you, grouped by band, with the
// details access points advertise: signal, channel, width, Wi-Fi generation,
// data rates, security, uptime and more.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rguziy/wscan/internal/render"
	"github.com/rguziy/wscan/internal/version"
	"github.com/rguziy/wscan/internal/wifi"
)

const usageText = `wscan - show nearby Wi-Fi networks, grouped by band

Usage:
  wscan [flags]

Flags:
  -i, --interface NAME   Wi-Fi interface to scan (default: first station interface)
  -b, --band LIST        Bands to show: all, or a comma separated list of 2.4, 5, 6
  -s, --sort KEY         Sort by signal (default), channel, ssid or bssid
  -d, --details          Add a line of extra facts below every network
  -w, --watch            Keep scanning and redraw the screen
      --interval DUR     Time between scans in --watch mode (default 5s)
      --cached           Only read the kernel's cached results; needs no root
      --timeout DUR      How long to wait for a fresh scan (default 15s)
      --json             Print JSON instead of tables
      --color MODE       Colour output: auto (default), always or never
      --list-interfaces  List the Wi-Fi interfaces and exit
      --demo             Show made-up networks (no Wi-Fi hardware needed)
  -V, --version          Print the version and exit
  -h, --help             Show this help

A fresh scan needs root or CAP_NET_ADMIN. Without it wscan falls back to the
cached results and says so. Colours follow the NO_COLOR convention.
`

type config struct {
	iface      string
	bands      string
	sortKey    string
	details    bool
	watch      bool
	interval   time.Duration
	cached     bool
	timeout    time.Duration
	jsonOut    bool
	color      string
	listIfaces bool
	demo       bool
	showVer    bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// Exit codes.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func run(args []string, stdout, stderr *os.File) int {
	var cfg config
	fs := flag.NewFlagSet("wscan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	str := func(p *string, short, long, def string) {
		fs.StringVar(p, short, def, "")
		fs.StringVar(p, long, def, "")
	}
	boolean := func(p *bool, short, long string) {
		if short != "" {
			fs.BoolVar(p, short, false, "")
		}
		fs.BoolVar(p, long, false, "")
	}
	str(&cfg.iface, "i", "interface", "")
	str(&cfg.bands, "b", "band", "all")
	str(&cfg.sortKey, "s", "sort", "signal")
	boolean(&cfg.details, "d", "details")
	boolean(&cfg.watch, "w", "watch")
	boolean(&cfg.cached, "", "cached")
	boolean(&cfg.jsonOut, "", "json")
	boolean(&cfg.listIfaces, "", "list-interfaces")
	boolean(&cfg.demo, "", "demo")
	boolean(&cfg.showVer, "V", "version")
	fs.DurationVar(&cfg.interval, "interval", 5*time.Second, "")
	fs.DurationVar(&cfg.timeout, "timeout", 15*time.Second, "")
	fs.StringVar(&cfg.color, "color", "auto", "")
	help := false
	fs.BoolVar(&help, "h", false, "")
	fs.BoolVar(&help, "help", false, "")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			help = true
		} else {
			fmt.Fprintf(stderr, "wscan: %v\nTry 'wscan --help'.\n", err)
			return exitUsage
		}
	}
	if help {
		fmt.Fprint(stdout, usageText)
		return exitOK
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "wscan: unexpected argument %q\nTry 'wscan --help'.\n", fs.Arg(0))
		return exitUsage
	}
	if cfg.showVer {
		fmt.Fprintf(stdout, "wscan %s\n", version.Version)
		return exitOK
	}

	bands, err := parseBands(cfg.bands)
	if err != nil {
		fmt.Fprintf(stderr, "wscan: %v\n", err)
		return exitUsage
	}
	sortKey, err := wifi.ParseSortKey(cfg.sortKey)
	if err != nil {
		fmt.Fprintf(stderr, "wscan: %v\n", err)
		return exitUsage
	}
	render.PrepareTerminal(stdout)
	profile, err := render.ParseColorMode(cfg.color, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "wscan: %v\n", err)
		return exitUsage
	}
	if cfg.interval < time.Second {
		fmt.Fprintln(stderr, "wscan: --interval must be at least 1s")
		return exitUsage
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.listIfaces {
		return listInterfaces(stdout, stderr, cfg.jsonOut)
	}

	a := &app{cfg: cfg, bands: bands, sortKey: sortKey, profile: profile, stdout: stdout}
	if cfg.watch && !cfg.jsonOut && !render.IsTerminal(stdout) {
		fmt.Fprintln(stderr, "wscan: --watch needs a terminal (or --json for a stream of JSON lines)")
		return exitUsage
	}

	var runErr error
	switch {
	case cfg.watch && cfg.jsonOut:
		runErr = a.streamJSON(ctx)
	case cfg.watch:
		runErr = a.watch(ctx)
	default:
		runErr = a.once(ctx)
	}
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		fmt.Fprintf(stderr, "wscan: %v\n", runErr)
		return exitError
	}
	return exitOK
}

type app struct {
	cfg     config
	bands   map[wifi.Band]bool
	sortKey wifi.SortKey
	profile render.Profile
	stdout  *os.File
}

func (a *app) scan(ctx context.Context) (*wifi.Result, error) {
	if a.cfg.demo {
		return wifi.Demo(), nil
	}
	return wifi.Scan(ctx, wifi.ScanOptions{
		Interface: a.cfg.iface,
		Fresh:     !a.cfg.cached,
		Timeout:   a.cfg.timeout,
	})
}

func (a *app) width() int {
	if w := render.TerminalWidth(a.stdout); w > 0 {
		return w
	}
	if w, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && w > 0 {
		return w
	}
	if render.IsTerminal(a.stdout) {
		return 100
	}
	return 120 // output redirected to a file or pipe
}

func (a *app) renderOptions() render.Options {
	return render.Options{
		Profile: a.profile,
		Width:   a.width(),
		Details: a.cfg.details,
		Sort:    a.sortKey,
		Bands:   a.bands,
		Version: version.Version,
	}
}

func (a *app) once(ctx context.Context) error {
	res, err := a.scan(ctx)
	if err != nil {
		return err
	}
	if a.cfg.jsonOut {
		return writeJSON(a.stdout, a.report(res), true)
	}
	return render.Render(a.stdout, res, a.renderOptions())
}

// watch redraws the tables on the alternate screen until interrupted.
func (a *app) watch(ctx context.Context) error {
	fmt.Fprint(a.stdout, "\x1b[?1049h\x1b[?25l") // alternate screen, hide cursor
	defer fmt.Fprint(a.stdout, "\x1b[?25h\x1b[?1049l")

	for {
		var frame bytes.Buffer
		res, err := a.scan(ctx)
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case err != nil:
			fmt.Fprintf(&frame, "\n  %s\n", a.profile.Paint("scan failed: "+err.Error(), render.ErrorStyle))
		default:
			if err := render.Render(&frame, res, a.renderOptions()); err != nil {
				return err
			}
		}
		fmt.Fprintf(&frame, "  %s\n", a.profile.Paint(
			fmt.Sprintf("Scanning every %s - press Ctrl+C to quit", a.cfg.interval), render.DimStyle))

		// Home the cursor and overwrite in place: clear each line's tail
		// instead of the whole screen, which avoids flicker.
		out := "\x1b[H" + strings.ReplaceAll(frame.String(), "\n", "\x1b[K\n") + "\x1b[J"
		if _, err := io.WriteString(a.stdout, out); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(a.cfg.interval):
		}
	}
}

// streamJSON prints one compact JSON object per scan.
func (a *app) streamJSON(ctx context.Context) error {
	for {
		res, err := a.scan(ctx)
		if err != nil {
			return err
		}
		if err := writeJSON(a.stdout, a.report(res), false); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(a.cfg.interval):
		}
	}
}

type jsonReport struct {
	Interface string    `json:"interface"`
	Time      time.Time `json:"time"`
	Fresh     bool      `json:"fresh"`
	Notes     []string  `json:"notes,omitempty"`
	Networks  []wifi.AP `json:"networks"`
}

func (a *app) report(res *wifi.Result) jsonReport {
	return jsonReport{
		Interface: res.Interface,
		Time:      res.Time,
		Fresh:     res.Fresh,
		Notes:     res.Notes,
		Networks:  wifi.Select(res.APs, a.bands, a.sortKey),
	}
}

func writeJSON(w io.Writer, v any, indent bool) error {
	enc := json.NewEncoder(w)
	if indent {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(v)
}

func listInterfaces(stdout, stderr *os.File, asJSON bool) int {
	ifaces, err := wifi.Interfaces()
	if err != nil {
		fmt.Fprintf(stderr, "wscan: %v\n", err)
		return exitError
	}
	if asJSON {
		if err := writeJSON(stdout, ifaces, true); err != nil {
			fmt.Fprintf(stderr, "wscan: %v\n", err)
			return exitError
		}
		return exitOK
	}
	for _, i := range ifaces {
		fmt.Fprintf(stdout, "%-12s %-10s %s\n", i.Name, i.Type, i.MAC)
	}
	return exitOK
}

// parseBands turns "2.4,5" (or "all") into a band set. An empty set means
// every band.
func parseBands(s string) (map[wifi.Band]bool, error) {
	set := make(map[wifi.Band]bool)
	for _, tok := range strings.Split(s, ",") {
		tok = strings.ToLower(strings.TrimSpace(tok))
		tok = strings.TrimSuffix(strings.TrimSuffix(tok, "ghz"), "g")
		switch tok {
		case "", "all":
			return map[wifi.Band]bool{}, nil
		case "2.4", "2":
			set[wifi.Band2G] = true
		case "5":
			set[wifi.Band5G] = true
		case "6":
			set[wifi.Band6G] = true
		default:
			return nil, fmt.Errorf("unknown band %q (use all, 2.4, 5 or 6)", tok)
		}
	}
	return set, nil
}
