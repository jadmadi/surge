package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand/v2"
	"net/http"
	urlpkg "net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type result struct {
	status  int
	latency time.Duration
	err     error
}

// errCategory classifies an error into a human-readable bucket.
func errCategory(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "timeout") || strings.Contains(low, "deadline exceeded"):
		return "Timeout"
	case strings.Contains(low, "connection refused"):
		return "Connection refused"
	case strings.Contains(low, "connection reset"):
		return "Connection reset"
	case strings.Contains(low, "eof"):
		return "Unexpected EOF"
	case strings.Contains(low, "no such host") || strings.Contains(low, "dns"):
		return "DNS resolution"
	case strings.Contains(low, "tls") || strings.Contains(low, "certificate"):
		return "TLS/certificate"
	case strings.Contains(low, "broken pipe"):
		return "Broken pipe"
	case strings.Contains(low, "context canceled"):
		return "Canceled (test ended)"
	case strings.Contains(low, "too many open files") || strings.Contains(low, "socket"):
		return "Resource exhaustion"
	default:
		return "Other"
	}
}

// ANSI color helpers (disabled when --no-color or non-tty).
var c = struct {
	reset, dim, bold, green, red, yellow, cyan, gray string
}{
	reset: "\033[0m", dim: "\033[2m", bold: "\033[1m",
	green: "\033[32m", red: "\033[31m", yellow: "\033[33m",
	cyan: "\033[36m", gray: "\033[90m",
}
var useColor bool

func col(color, s string) string {
	if !useColor {
		return s
	}
	return color + s + c.reset
}

// headerList is a repeatable flag.Value for -H/--header.
type headerList struct {
	headers []string
}

func (h *headerList) String() string { return strings.Join(h.headers, ", ") }
func (h *headerList) Set(v string) error {
	h.headers = append(h.headers, v)
	return nil
}

// parseHeader splits "Key: Value" and supports "@file" to load from a file.
func parseHeader(raw string) (key, val string, err error) {
	if strings.HasPrefix(raw, "@") {
		data, e := os.ReadFile(strings.TrimPrefix(raw, "@"))
		if e != nil {
			return "", "", fmt.Errorf("read header file: %w", e)
		}
		raw = strings.TrimSpace(string(data))
	}
	idx := strings.Index(raw, ":")
	if idx < 0 {
		return "", "", fmt.Errorf("header must be 'Key: Value', got %q", raw)
	}
	return strings.TrimSpace(raw[:idx]), strings.TrimSpace(raw[idx+1:]), nil
}

// ---------- Presets ----------

type preset struct {
	desc        string
	concurrency int
	total       int
	duration    time.Duration
	ramp        time.Duration
	think       time.Duration
}

var presets = map[string]preset{
	"baseline": {
		desc:        "Single user, 100 requests — pure latency floor, no concurrency noise",
		concurrency: 1,
		total:       100,
	},
	"realistic": {
		desc:        "50 users with ramp-up and think time — simulates real browsing behavior",
		concurrency: 50,
		duration:    60 * time.Second,
		ramp:        10 * time.Second,
		think:       3 * time.Second,
	},
	"capacity": {
		desc:        "200 users ramped over 20s — find where the site starts degrading",
		concurrency: 200,
		duration:    30 * time.Second,
		ramp:        20 * time.Second,
	},
	"spike": {
		desc:        "500 users instantly for 10s — burst survival, not comfort",
		concurrency: 500,
		duration:    10 * time.Second,
	},
}

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	var (
		url         string
		concurrency int
		total       int
		duration    time.Duration
		method      string
		body        string
		contentType string
		timeout     time.Duration
		rps         int
		insecure    bool
		noColor     bool
		showVersion bool
		ramp        time.Duration
		think       time.Duration
		outputFmt   string
		profile     string
		headers     headerList
	)

	// Short flags (power users).
	flag.StringVar(&url, "url", "", "target URL")
	flag.IntVar(&concurrency, "c", 10, "concurrent users")
	flag.IntVar(&total, "n", 0, "total requests (0 = run for --duration)")
	flag.DurationVar(&duration, "d", 10*time.Second, "test duration when -n is 0")
	flag.StringVar(&method, "m", "GET", "HTTP method")
	flag.StringVar(&body, "body", "", "request body")
	flag.StringVar(&contentType, "ct", "application/json", "Content-Type header when -body is set")
	flag.DurationVar(&timeout, "timeout", 30*time.Second, "per-request timeout")
	flag.IntVar(&rps, "rps", 0, "max requests per second (0 = no limit)")
	flag.BoolVar(&insecure, "insecure", false, "skip TLS certificate verification")
	flag.BoolVar(&noColor, "no-color", false, "disable colored output")
	flag.BoolVar(&showVersion, "v", false, "print version and exit")
	flag.DurationVar(&ramp, "ramp", 0, "gradually launch concurrency over this duration")
	flag.DurationVar(&think, "think", 0, "random sleep (0 to this) between requests per worker")
	flag.StringVar(&outputFmt, "o", "text", "output format: text or json")
	flag.Var(&headers, "H", "custom header 'Key: Value' (repeatable, @file supported)")
	flag.StringVar(&profile, "profile", "", "test preset: baseline, realistic, capacity, spike")

	// Long aliases (semantic / self-documenting).
	flag.StringVar(&url, "target", "", "target URL (alias for -url)")
	flag.IntVar(&concurrency, "concurrency", 10, "concurrent users (alias for -c)")
	flag.IntVar(&total, "total", 0, "total requests (alias for -n)")
	flag.DurationVar(&duration, "duration", 10*time.Second, "test duration (alias for -d)")
	flag.StringVar(&method, "method", "GET", "HTTP method (alias for -m)")
	flag.BoolVar(&showVersion, "version", false, "print version and exit (alias for -v)")
	flag.DurationVar(&ramp, "ramp-up", 0, "gradually launch concurrency (alias for -ramp)")
	flag.DurationVar(&think, "think-time", 0, "random sleep between requests (alias for -think)")
	flag.StringVar(&outputFmt, "output", "text", "output format (alias for -o)")
	flag.Var(&headers, "header", "custom header (alias for -H)")

	flag.Usage = func() {
		// Colors for help: check stderr TTY + --no-color in args.
		helpColor := isTTY(os.Stderr)
		for _, a := range os.Args {
			if a == "--no-color" || a == "-no-color" {
				helpColor = false
			}
		}
		hc := func(color, s string) string {
			if !helpColor {
				return s
			}
			return color + s + c.reset
		}
		// Shorthand for help colors.
		dim := func(s string) string { return hc(c.dim, s) }
		bold := func(s string) string { return hc(c.bold, s) }
		cyan := func(s string) string { return hc(c.cyan, s) }
		green := func(s string) string { return hc(c.green, s) }
		yellow := func(s string) string { return hc(c.yellow, s) }
		gray := func(s string) string { return hc(c.gray, s) }
		red := func(s string) string { return hc(c.red, s) }

		w := os.Stderr
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  %s\n", cyan("╭──────────────────────────────────────────────────────────╮"))
		fmt.Fprintf(w, "  %s\n", cyan("│")+bold("  surge                                                   ")+cyan("│"))
		fmt.Fprintf(w, "  %s\n", cyan("│")+dim("  Concurrent HTTP load tester with latency percentiles    ")+cyan("│"))
		fmt.Fprintf(w, "  %s\n", cyan("╰──────────────────────────────────────────────────────────╯"))
		fmt.Fprintln(w)

		// USAGE
		fmt.Fprintf(w, "  %s\n", bold("USAGE"))
		fmt.Fprintf(w, "  %s\n", dim(strings.Repeat("─", 56)))
		fmt.Fprintf(w, "  %s %s\n", gray("→"), "surge "+bold("<profile>")+" "+bold("<URL>")+" "+dim("[flags]"))
		fmt.Fprintf(w, "  %s %s\n", gray("→"), "surge "+bold("<URL>")+" "+dim("--profile")+" "+bold("<name>")+" "+dim("[flags]"))
		fmt.Fprintln(w)

		// PRESETS
		fmt.Fprintf(w, "  %s\n", bold("PRESETS"))
		fmt.Fprintf(w, "  %s\n", dim(strings.Repeat("─", 56)))
		presetRows := []struct {
			name, params, desc string
		}{
			{"baseline", "1 user · 100 requests", "Pure latency floor, no concurrency noise"},
			{"realistic", "50 users · ramp 10s · think 3s · 1m", "Simulates real browsing behavior"},
			{"capacity", "200 users · ramp 20s · 30s", "Find where the site starts degrading"},
			{"spike", "500 users · instant · 10s", "Burst survival, not comfort"},
		}
		for _, p := range presetRows {
			fmt.Fprintf(w, "  %s %-12s %s %s\n",
				green("●"),
				bold(p.name),
				dim(padRight(p.params, 38)),
				gray(p.desc))
		}
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  %s %s\n", dim("ℹ"), dim("Override any preset flag: surge realistic https://example.com -c 100"))
		fmt.Fprintln(w)

		// EXAMPLES
		fmt.Fprintf(w, "  %s\n", bold("EXAMPLES"))
		fmt.Fprintf(w, "  %s\n", dim(strings.Repeat("─", 56)))
		examples := []struct {
			comment, cmd string
		}{
			{"Is the site fast for one user?", "surge baseline https://example.com"},
			{"What do 50 real users experience?", "surge realistic https://example.com"},
			{"Find the breaking point", "surge capacity https://example.com"},
			{"Survive a traffic burst", "surge spike https://example.com"},
			{"API endpoint with auth", `surge https://api.example.com -c 100 -d 30s -H "Authorization: Bearer ..."`},
			{"Machine-readable output for CI", "surge realistic https://example.com -o json | jq .summary"},
		}
		for _, e := range examples {
			fmt.Fprintf(w, "  %s %s\n", gray("#"), dim(e.comment))
			fmt.Fprintf(w, "  %s %s\n", gray("→"), cyan(e.cmd))
			fmt.Fprintln(w)
		}

		// FLAGS
		fmt.Fprintf(w, "  %s\n", bold("FLAGS"))
		fmt.Fprintf(w, "  %s\n", dim(strings.Repeat("─", 56)))
		flagRows := []struct {
			short, long, typ, def, desc string
		}{
			{"-c", "--concurrency", "int", "10", "Concurrent users"},
			{"-n", "--total", "int", "0", "Total requests (0 = run for duration)"},
			{"-d", "--duration", "dur", "10s", "Test duration when -n is 0"},
			{"-H", "--header", "str", "", "Custom header 'Key: Value' (repeatable, @file)"},
			{"-m", "--method", "str", "GET", "HTTP method"},
			{"", "--ramp-up", "dur", "0", "Gradually launch concurrency over this duration"},
			{"", "--think-time", "dur", "0", "Random sleep (0 to this) between requests per worker"},
			{"-o", "--output", "str", "text", "Output format: text or json"},
			{"", "--profile", "str", "", "Test preset: baseline, realistic, capacity, spike"},
			{"-rps", "", "int", "0", "Max requests per second (0 = no limit)"},
			{"", "--timeout", "dur", "30s", "Per-request timeout"},
			{"-body", "", "str", "", "Request body"},
			{"-ct", "", "str", "application/json", "Content-Type header when -body is set"},
			{"-url", "--target", "str", "", "Target URL (or pass as positional arg)"},
			{"-v", "--version", "", "", "Print version and exit"},
			{"", "--insecure", "", "", "Skip TLS certificate verification"},
			{"", "--no-color", "", "", "Disable colored output"},
		}
		for _, f := range flagRows {
			flagStr := ""
			if f.short != "" && f.long != "" {
				flagStr = fmt.Sprintf("%s, %s", bold(f.short), bold(f.long))
			} else if f.short != "" {
				flagStr = bold(f.short)
			} else {
				flagStr = bold(f.long)
			}
			if f.typ != "" {
				flagStr += " " + dim("<"+f.typ+">")
			}
			desc := f.desc
			if f.def != "" {
				desc += " " + dim("(default: "+f.def+")")
			}
			fmt.Fprintf(w, "  %s  %s\n", padRight(flagStr, 30), gray(desc))
		}
		fmt.Fprintln(w)

		// LEGAL
		fmt.Fprintf(w, "  %s\n", bold("LEGAL"))
		fmt.Fprintf(w, "  %s\n", dim(strings.Repeat("─", 56)))
		fmt.Fprintf(w, "  %s\n", yellow("⚠  This tool is for testing YOUR OWN websites and APIs only."))
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  %s\n", dim("  Do NOT use it against sites you do not own or have explicit"))
		fmt.Fprintf(w, "  %s\n", dim("  permission to test. Unauthorized load testing may violate"))
		fmt.Fprintf(w, "  %s\n", dim("  computer fraud, abuse, and trespass laws in your jurisdiction"))
		fmt.Fprintf(w, "  %s\n", dim("  (e.g. CFAA in the US, Computer Misuse Act in the UK)."))
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  %s\n", red("  Illegal use will be reported to the relevant authorities."))
		fmt.Fprintf(w, "  %s\n", dim("  You alone are responsible for ensuring you have authorization"))
		fmt.Fprintf(w, "  %s\n", dim("  to test the target."))
		fmt.Fprintln(w)
	}

	// Extract positional args before flag parsing, so flags can appear after the URL.
	// Go's flag package stops at the first non-flag arg; we work around this by
	// pulling positionals out of os.Args first.
	var positionalURL string
	var positionalProfile string
	filtered := make([]string, 0, len(os.Args))
	// Known bool flags that don't consume a following value.
	boolFlags := map[string]bool{
		"insecure": true, "no-color": true, "v": true, "version": true,
	}
	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		if strings.HasPrefix(arg, "-") && arg != "-" {
			filtered = append(filtered, arg)
			// Check if this is a bool flag (no value consumed) or needs a value.
			name := strings.TrimPrefix(strings.TrimPrefix(arg, "--"), "-")
			// Handle -flag=value form (no extra arg consumed).
			if strings.Contains(name, "=") {
				continue
			}
			// If it's a bool flag, it doesn't consume the next arg.
			if boolFlags[name] {
				continue
			}
			// Non-bool flag: the next arg is its value (unless it's -flag=value).
			if i+1 < len(os.Args) {
				i++
				filtered = append(filtered, os.Args[i])
			}
			continue
		}
		// Non-flag arg: check if it's a known profile name, otherwise treat as URL.
		if _, isPreset := presets[arg]; isPreset {
			positionalProfile = arg
		} else if positionalURL == "" {
			positionalURL = arg
		}
	}
	os.Args = append(os.Args[:1], filtered...)
	flag.Parse()

	if showVersion {
		fmt.Printf("surge %s (commit: %s, built: %s)\n", version, commit, date)
		return
	}

	// Resolve profile: positional profile takes priority, then --profile flag.
	if profile == "" && positionalProfile != "" {
		profile = positionalProfile
	}

	// Track which flags were explicitly set (to avoid overriding them with preset values).
	explicitlySet := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { explicitlySet[f.Name] = true })

	// Apply preset values only for flags not explicitly set by the user.
	if profile != "" {
		p, ok := presets[profile]
		if !ok {
			fmt.Fprintf(os.Stderr, "error: unknown profile %q (available: baseline, realistic, capacity, spike)\n", profile)
			os.Exit(2)
		}
		if !explicitlySet["c"] && !explicitlySet["concurrency"] {
			concurrency = p.concurrency
		}
		if !explicitlySet["n"] && !explicitlySet["total"] {
			total = p.total
		}
		if !explicitlySet["d"] && !explicitlySet["duration"] {
			duration = p.duration
		}
		if !explicitlySet["ramp"] && !explicitlySet["ramp-up"] {
			ramp = p.ramp
		}
		if !explicitlySet["think"] && !explicitlySet["think-time"] {
			think = p.think
		}
	}

	// Positional URL (extracted before flag parsing) or -url/--target flag.
	if url == "" {
		url = positionalURL
	}
	if url == "" {
		fmt.Fprintln(os.Stderr, "error: URL is required (positional or -url/--target)")
		flag.Usage()
		os.Exit(2)
	}

	parsedURL, err := urlpkg.Parse(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid URL %q: %v\n", url, err)
		os.Exit(2)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		fmt.Fprintf(os.Stderr, "error: URL scheme must be http or https (e.g. https://example.com)\n")
		os.Exit(2)
	}
	if parsedURL.Host == "" {
		fmt.Fprintf(os.Stderr, "error: URL host is missing (e.g. https://example.com)\n")
		os.Exit(2)
	}

	useColor = !noColor && isTTY(os.Stdout) && outputFmt == "text"

	// Parse custom headers once.
	parsedHeaders := make([][2]string, 0, len(headers.headers))
	for _, h := range headers.headers {
		k, v, err := parseHeader(h)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: bad header %q: %v\n", h, err)
			os.Exit(2)
		}
		parsedHeaders = append(parsedHeaders, [2]string{k, v})
	}

	baseReq, err := http.NewRequest(method, url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid request parameters: %v\n", err)
		os.Exit(2)
	}
	var bodyBytes []byte
	if body != "" {
		bodyBytes = []byte(body)
		baseReq.Header.Set("Content-Type", contentType)
		baseReq.ContentLength = int64(len(bodyBytes))
	}
	for _, h := range parsedHeaders {
		baseReq.Header.Add(h[0], h[1])
	}

	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConns:        concurrency,
			MaxIdleConnsPerHost: concurrency,
			MaxConnsPerHost:     concurrency,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: insecure},
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if total > 0 {
		ctx = withLimit(ctx, int64(total))
	} else {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, duration)
		defer cancel()
	}

	var limiter <-chan time.Time
	if rps > 0 {
		interval := time.Second / time.Duration(rps)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		limiter = ticker.C
	}

	if outputFmt == "text" {
		fmt.Fprintln(os.Stderr, col(c.gray, "⚠  Only test sites you own or have permission to test. Unauthorized use may be illegal."))
		printBanner(method, url, concurrency, total, duration, rps, ramp, think, profile)
	}

	var (
		results   []result
		resultsMu sync.Mutex
		wg        sync.WaitGroup
		sent      atomic.Int64
	)

	start := time.Now()
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		// Ramp-up: stagger goroutine starts evenly across the ramp duration.
		if ramp > 0 && i > 0 {
			delay := time.Duration(int64(ramp) * int64(i) / int64(concurrency))
			timer := time.NewTimer(delay)
			go func() {
				select {
				case <-timer.C:
					worker(ctx, client, baseReq, bodyBytes, limiter, think, &results, &resultsMu, &wg, &sent)
				case <-ctx.Done():
					timer.Stop()
					wg.Done()
				}
			}()
		} else {
			go worker(ctx, client, baseReq, bodyBytes, limiter, think, &results, &resultsMu, &wg, &sent)
		}
	}

	wg.Wait()
	elapsed := time.Since(start)

	if len(results) == 0 {
		if outputFmt == "text" {
			fmt.Println(col(c.red, "no requests completed"))
		} else {
			fmt.Println(`{"error":"no requests completed"}`)
		}
		return
	}

	stats := computeStats(results, elapsed, sent.Load(), concurrency)

	switch outputFmt {
	case "json":
		printJSON(stats)
	default:
		report(stats)
	}
}

func worker(
	ctx context.Context,
	client *http.Client,
	baseReq *http.Request,
	bodyBytes []byte,
	limiter <-chan time.Time,
	think time.Duration,
	results *[]result,
	resultsMu *sync.Mutex,
	wg *sync.WaitGroup,
	sent *atomic.Int64,
) {
	defer wg.Done()
	for {
		if limiter != nil {
			select {
			case <-limiter:
			case <-ctx.Done():
				return
			}
		}
		if ctx.Err() != nil {
			return
		}

		req := baseReq.Clone(ctx)
		if len(bodyBytes) > 0 {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		reqStart := time.Now()
		resp, err := client.Do(req)
		lat := time.Since(reqStart)
		sent.Add(1)

		if err != nil {
			resultsMu.Lock()
			*results = append(*results, result{latency: lat, err: err})
			resultsMu.Unlock()
		} else {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			resultsMu.Lock()
			*results = append(*results, result{status: resp.StatusCode, latency: lat})
			resultsMu.Unlock()
		}

		// Think time: random sleep between 0 and think duration.
		if think > 0 && ctx.Err() == nil {
			sleep := time.Duration(rand.Int64N(int64(think)))
			if sleep > 0 {
				select {
				case <-time.After(sleep):
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

// ---------- Reporting ----------

func printBanner(method, url string, concurrency, total int, duration time.Duration, rps int, ramp, think time.Duration, profile string) {
	width := 56
	line := strings.Repeat("─", width)
	fmt.Println()
	fmt.Printf("%s %s %s\n", col(c.cyan, "┌"+line+"┐"), "", "")
	title := "SURGE"
	if profile != "" {
		title = fmt.Sprintf("SURGE · %s", col(c.bold, strings.ToUpper(profile)))
	}
	fmt.Printf("%s %-"+fmt.Sprint(width-2)+"s %s\n",
		col(c.cyan, "│"), col(c.bold, title), col(c.cyan, "│"))
	fmt.Printf("%s %s\n", col(c.cyan, "└"+line+"┘"), "")

	rows := [][2]string{
		{"Target", fmt.Sprintf("%s %s", col(c.bold, method), col(c.cyan, url))},
		{"Concurrency", fmt.Sprintf("%d users", concurrency)},
		{"Total requests", ternary(total > 0, fmt.Sprintf("%d", total), "unlimited")},
		{"Duration", ternary(total > 0, "until total reached", duration.String())},
		{"Rate limit", ternary(rps > 0, fmt.Sprintf("%d req/s", rps), "unlimited")},
		{"Ramp-up", ternary(ramp > 0, ramp.String(), "instant")},
		{"Think time", ternary(think > 0, fmt.Sprintf("0-%s (random)", think), "none")},
	}
	for _, r := range rows {
		fmt.Printf("  %s %-18s %s\n", col(c.gray, "•"), col(c.dim, r[0]), r[1])
	}
	fmt.Println()
}

// stats holds all computed metrics, shared between text and JSON output.
type stats struct {
	elapsed      time.Duration
	sent         int64
	totalReq     int
	ok           int
	errs         int
	errRate      float64
	throughput   float64
	concurrency  int
	latencies    []float64 // sorted, in ms
	p25, p50, p75, p90, p95, p99, maxLat, avgLat float64
	statusCounts map[int]int
	errCats      map[string]int
	errSamples   map[string]string
}

func computeStats(results []result, elapsed time.Duration, sent int64, concurrency int) stats {
	s := stats{
		elapsed:      elapsed,
		sent:         sent,
		totalReq:     len(results),
		concurrency:  concurrency,
		statusCounts: map[int]int{},
		errCats:      map[string]int{},
		errSamples:   map[string]string{},
		latencies:    make([]float64, 0, len(results)),
	}
	for _, r := range results {
		if r.err != nil {
			s.errs++
			cat := errCategory(r.err)
			s.errCats[cat]++
			if _, has := s.errSamples[cat]; !has {
				s.errSamples[cat] = r.err.Error()
			}
			continue
		}
		s.ok++
		s.statusCounts[r.status]++
		s.latencies = append(s.latencies, float64(r.latency.Milliseconds()))
	}
	sort.Float64s(s.latencies)
	if s.totalReq > 0 {
		s.errRate = float64(s.errs) / float64(s.totalReq) * 100
	}
	if elapsed > 0 {
		s.throughput = float64(s.totalReq) / elapsed.Seconds()
	}
	if len(s.latencies) > 0 {
		s.p25 = percentile(s.latencies, 25)
		s.p50 = percentile(s.latencies, 50)
		s.p75 = percentile(s.latencies, 75)
		s.p90 = percentile(s.latencies, 90)
		s.p95 = percentile(s.latencies, 95)
		s.p99 = percentile(s.latencies, 99)
		s.maxLat = s.latencies[len(s.latencies)-1]
		s.avgLat = avg(s.latencies)
	}
	return s
}

func report(s stats) {
	// ---- Summary block ----
	fmt.Println(col(c.bold, "  SUMMARY"))
	fmt.Printf("  %s\n", strings.Repeat("─", 52))
	printStat("Elapsed", s.elapsed.Round(time.Millisecond).String())
	printStat("Requests sent", fmt.Sprintf("%d", s.sent))
	printStat("Completed", fmt.Sprintf("%d", s.totalReq))
	printStat("Successful", fmt.Sprintf("%s  %s",
		fmt.Sprintf("%d", s.ok),
		col(c.dim, fmt.Sprintf("(%.2f%%)", pct(s.ok, s.totalReq)))))
	errStr := fmt.Sprintf("%d", s.errs)
	if s.errs > 0 {
		errStr = col(c.red, errStr)
	}
	printStat("Errors", fmt.Sprintf("%s  %s",
		errStr,
		col(c.dim, fmt.Sprintf("(%.2f%%)", s.errRate))))
	printStat("Throughput", fmt.Sprintf("%.1f req/s", s.throughput))
	fmt.Println()

	// ---- Latency block ----
	if len(s.latencies) > 0 {
		fmt.Println(col(c.bold, "  LATENCY"))
		fmt.Printf("  %s\n", strings.Repeat("─", 52))
		pcts := []struct {
			label string
			p     float64
		}{
			{"min", 0}, {"p25", 25}, {"p50", 50}, {"p75", 75},
			{"p90", 90}, {"p95", 95}, {"p99", 99}, {"max", 100},
		}
		for _, pc := range pcts {
			var v float64
			if pc.p == 0 {
				v = s.latencies[0]
			} else if pc.p == 100 {
				v = s.maxLat
			} else {
				v = percentile(s.latencies, pc.p)
			}
			printLatencyRow(pc.label, v, s.maxLat)
		}
		printStat("avg", fmt.Sprintf("%.2f ms", s.avgLat))
		fmt.Println()

		// ---- Histogram ----
		printHistogram(s.latencies)
	}

	// ---- Status codes ----
	if len(s.statusCounts) > 0 {
		fmt.Println(col(c.bold, "  STATUS CODES"))
		fmt.Printf("  %s\n", strings.Repeat("─", 52))
		codes := make([]int, 0, len(s.statusCounts))
		for code := range s.statusCounts {
			codes = append(codes, code)
		}
		sort.Ints(codes)
		for _, code := range codes {
			count := s.statusCounts[code]
			color := statusColor(code)
			fmt.Printf("  %s  %-8s %s  %s\n",
				col(color, fmt.Sprintf("%d", code)),
				"",
				padRight(fmt.Sprintf("%d", count), 8),
				col(c.dim, fmt.Sprintf("%.2f%%", pct(count, s.totalReq))))
		}
		fmt.Println()
	}

	// ---- Error breakdown ----
	if s.errs > 0 {
		fmt.Println(col(c.bold, "  ERRORS"))
		fmt.Printf("  %s\n", strings.Repeat("─", 52))
		cats := make([]string, 0, len(s.errCats))
		for cat := range s.errCats {
			cats = append(cats, cat)
		}
		sort.Slice(cats, func(i, j int) bool { return s.errCats[cats[i]] > s.errCats[cats[j]] })
		for _, cat := range cats {
			count := s.errCats[cat]
			fmt.Printf("  %s %-20s %s  %s\n",
				col(c.red, "✗"),
				col(c.bold, cat),
				padLeft(fmt.Sprintf("%d", count), 6),
				col(c.dim, fmt.Sprintf("(%.2f%% of errors)", pct(count, s.errs))))
			if sample, has := s.errSamples[cat]; has {
				fmt.Printf("    %s %s\n", col(c.gray, "└"), col(c.dim, truncate(sample, 80)))
			}
		}
		fmt.Println()
	}

	// ---- Analysis & recommendations ----
	printAnalysis(s)
}

// ---------- JSON output ----------

type jsonOutput struct {
	Summary     jsonSummary     `json:"summary"`
	Latency     jsonLatency     `json:"latency"`
	Distribution []jsonBin      `json:"distribution"`
	StatusCodes map[string]int  `json:"status_codes"`
	Errors      jsonErrors      `json:"errors"`
}

type jsonSummary struct {
	Elapsed    string  `json:"elapsed"`
	Sent       int64   `json:"sent"`
	Completed  int     `json:"completed"`
	Successful int     `json:"successful"`
	Errors     int     `json:"errors"`
	ErrorRate  float64 `json:"error_rate_pct"`
	Throughput float64 `json:"throughput_req_s"`
	Concurrency int    `json:"concurrency"`
}

type jsonLatency struct {
	Min  float64 `json:"min_ms"`
	P25  float64 `json:"p25_ms"`
	P50  float64 `json:"p50_ms"`
	P75  float64 `json:"p75_ms"`
	P90  float64 `json:"p90_ms"`
	P95  float64 `json:"p95_ms"`
	P99  float64 `json:"p99_ms"`
	Max  float64 `json:"max_ms"`
	Avg  float64 `json:"avg_ms"`
}

type jsonBin struct {
	Range string  `json:"range"`
	Count int     `json:"count"`
	Pct   float64 `json:"pct"`
}

type jsonErrors struct {
	Total     int               `json:"total"`
	Rate      float64           `json:"rate_pct"`
	Categories map[string]int   `json:"categories"`
	Samples    map[string]string `json:"samples"`
}

func printJSON(s stats) {
	bins := []struct {
		lo, hi float64
		label  string
	}{
		{0, 50, "<50ms"}, {50, 100, "50-100ms"}, {100, 250, "100-250ms"},
		{250, 500, "250-500ms"}, {500, 1000, "500ms-1s"}, {1000, math.Inf(1), ">1s"},
	}
	dist := make([]jsonBin, 0, len(bins))
	for _, b := range bins {
		count := 0
		for _, l := range s.latencies {
			if l >= b.lo && l < b.hi {
				count++
			}
		}
		dist = append(dist, jsonBin{
			Range: b.label,
			Count: count,
			Pct:   pct(count, len(s.latencies)),
		})
	}

	statusStr := make(map[string]int, len(s.statusCounts))
	for code, count := range s.statusCounts {
		statusStr[fmt.Sprintf("%d", code)] = count
	}

	out := jsonOutput{
		Summary: jsonSummary{
			Elapsed:    s.elapsed.Round(time.Millisecond).String(),
			Sent:       s.sent,
			Completed:  s.totalReq,
			Successful: s.ok,
			Errors:     s.errs,
			ErrorRate:  s.errRate,
			Throughput: s.throughput,
			Concurrency: s.concurrency,
		},
		Latency: jsonLatency{
			Min: s.latencies[0], P25: s.p25, P50: s.p50, P75: s.p75,
			P90: s.p90, P95: s.p95, P99: s.p99, Max: s.maxLat, Avg: s.avgLat,
		},
		Distribution: dist,
		StatusCodes:  statusStr,
		Errors: jsonErrors{
			Total:      s.errs,
			Rate:       s.errRate,
			Categories: s.errCats,
			Samples:    s.errSamples,
		},
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(out)
}

func printAnalysis(a stats) {
	fmt.Println(col(c.bold, "  ANALYSIS"))
	fmt.Printf("  %s\n", strings.Repeat("─", 52))

	// Overall health verdict.
	verdict := "Excellent"
	vColor := c.green
	switch {
	case a.errRate > 5 || a.p99 > 1000:
		verdict = "Poor"
		vColor = c.red
	case a.errRate > 1 || a.p99 > 500:
		verdict = "Fair"
		vColor = c.yellow
	case a.errRate > 0.1 || a.p99 > 200:
		verdict = "Good"
		vColor = c.cyan
	}
	fmt.Printf("  %-18s %s\n", col(c.dim, "Verdict"), col(vColor, verdict))
	fmt.Println()

	// Latency interpretation.
	fmt.Printf("  %s\n", col(c.bold, "Latency"))
	addInsight("p50", a.p50, "ms",
		"Half your requests complete under this — most users experience this speed.",
		[]insightRule{
			{le: 100, msg: "Feels instant. Typical of a CDN-fronted or well-cached static site.", good: true},
			{le: 300, msg: "Acceptable for most web pages, but noticeable on slow connections.", good: true},
			{le: 1000, msg: "Users will perceive a delay. Investigate backend or cold-start latency.", good: false},
			{msg: "Over 1s p50 — most users will perceive the site as slow.", good: false},
		})
	addInsight("p90", a.p90, "ms",
		"10% of users experience this or worse — the start of the tail.",
		[]insightRule{
			{le: 200, msg: "Tail is well-controlled.", good: true},
			{le: 500, msg: "Some users see noticeable slowdowns under load.", good: false},
			{msg: "Tail latency is high — check for slow DB queries, cache misses, or origin overload.", good: false},
		})
	addInsight("p99", a.p99, "ms",
		"1% of users hit this — the long tail. SREs watch this closely.",
		[]insightRule{
			{le: 300, msg: "Excellent tail control.", good: true},
			{le: 1000, msg: "Acceptable but borderline — 1 in 100 requests is slow.", good: false},
			{msg: "p99 over 1s means 1% of users see a multi-second load. Investigate origin capacity.", good: false},
		})
	tailRatio := 0.0
	if a.p50 > 0 {
		tailRatio = a.p99 / a.p50
	}
	tailMsg := ""
	switch {
	case tailRatio < 3:
		tailMsg = "Tail is proportional — latency is consistent across requests."
	case tailRatio < 10:
		tailMsg = "Moderate tail — some requests are much slower, likely cache misses or GC pauses."
	default:
		tailMsg = "High tail variance — a small number of requests are dramatically slower. Look for cold starts, retries, or origin spikes."
	}
	fmt.Printf("  %s %s\n", col(c.gray, "└"), col(c.dim, tailMsg))
	fmt.Println()

	// Error interpretation.
	fmt.Printf("  %s\n", col(c.bold, "Errors"))
	switch {
	case a.errRate == 0:
		fmt.Printf("  %s %s\n", col(c.green, "✓"), "No errors — every request succeeded.")
	case a.errRate < 0.5:
		fmt.Printf("  %s %s\n", col(c.green, "✓"),
			fmt.Sprintf("Error rate %.2f%% is negligible — likely transient network blips under high concurrency.", a.errRate))
	case a.errRate < 1:
		fmt.Printf("  %s %s\n", col(c.yellow, "⚠"),
			fmt.Sprintf("Error rate %.2f%% is low but worth watching. Check if it scales with concurrency.", a.errRate))
	case a.errRate < 5:
		fmt.Printf("  %s %s\n", col(c.yellow, "⚠"),
			fmt.Sprintf("Error rate %.2f%% is meaningful — some users are failing. Investigate before raising load.", a.errRate))
	default:
		fmt.Printf("  %s %s\n", col(c.red, "✗"),
			fmt.Sprintf("Error rate %.2f%% is high — the site is failing under this load. Reduce concurrency or fix the origin.", a.errRate))
	}
	// Error category hints.
	if len(a.errCats) > 0 {
		hints := map[string]string{
			"Timeout":             "Origin is too slow to respond within the deadline. Increase -timeout or optimize the backend.",
			"Connection refused":  "Origin is rejecting connections — it may be down or at its connection limit.",
			"Connection reset":    "Connection dropped mid-request. Often a load balancer or origin restarting under pressure.",
			"Unexpected EOF":      "Connection closed before a full response. Origin or proxy is dropping requests.",
			"DNS resolution":      "DNS lookups are failing. Check your DNS provider and TTLs.",
			"TLS/certificate":     "TLS handshake failed. Check cert validity, SNI, and intermediate certs.",
			"Broken pipe":         "Client-side write failed after origin closed. Usually correlates with origin overload.",
			"Resource exhaustion": "You're hitting OS limits (file descriptors/sockets). Raise ulimit -n or reduce concurrency.",
			"Canceled (test ended)": "Request was in-flight when the test duration expired — not a real error.",
			"Other":               "Unclassified error. Re-run with verbose logging to inspect.",
		}
		for cat, count := range a.errCats {
			if hint, ok := hints[cat]; ok {
				fmt.Printf("  %s %s: %s\n",
					col(c.gray, "└"),
					col(c.dim, fmt.Sprintf("%s (%d)", cat, count)),
					hint)
			}
		}
	}
	fmt.Println()

	// Status code interpretation.
	if len(a.statusCounts) > 0 {
		fmt.Printf("  %s\n", col(c.bold, "Status codes"))
		for code, count := range a.statusCounts {
			msg := ""
			switch {
			case code >= 200 && code < 300:
				msg = "Success — requests are being served correctly."
			case code == 301 || code == 302:
				msg = "Redirects — each adds a round-trip. Consider testing the final URL directly."
			case code == 304:
				msg = "Not modified — caching/CDN is working."
			case code == 429:
				msg = "Rate limited — the origin or CDN is throttling you. Lower -rps or -c."
			case code >= 400 && code < 500:
				msg = "Client error — the request is being rejected. Check URL, headers, and auth."
			case code == 500:
				msg = "Internal server error — the origin is crashing or erroring under load."
			case code == 502:
				msg = "Bad gateway — a proxy/upstream can't reach the origin. Origin may be down or overloaded."
			case code == 503:
				msg = "Service unavailable — the origin is explicitly refusing load. It's at capacity."
			case code == 504:
				msg = "Gateway timeout — a proxy waited too long for the origin. Origin is too slow."
			case code >= 500:
				msg = "Server error — the origin is failing under this load."
			}
			if msg != "" {
				fmt.Printf("  %s %s: %s\n",
					col(c.gray, "└"),
					col(c.dim, fmt.Sprintf("%d (%d)", code, count)),
					msg)
			}
		}
		fmt.Println()
	}

	// Recommendations.
	fmt.Printf("  %s\n", col(c.bold, "Recommendations"))
	var recs []string
	// Tail vs p50.
	if tailRatio > 5 && a.p99 > 200 {
		recs = append(recs, "Tail latency is high relative to p50 — investigate cache misses, cold starts, or DB query timeouts at the origin.")
	}
	// p99 absolute.
	if a.p99 > 1000 {
		recs = append(recs, "p99 exceeds 1s — 1% of users see a multi-second load. Profile the slowest origin requests.")
	}
	// Error rate.
	if a.errRate > 0 && a.errRate < 0.5 {
		recs = append(recs, fmt.Sprintf("Error rate is %.2f%% — small enough to be transient, but re-run with higher -c to see if it grows.", a.errRate))
	}
	if a.errRate >= 0.5 && a.errRate < 5 {
		recs = append(recs, fmt.Sprintf("Error rate is %.2f%% — check the error breakdown above and address the dominant category before increasing load.", a.errRate))
	}
	if a.errRate >= 5 {
		recs = append(recs, fmt.Sprintf("Error rate is %.2f%% — the site cannot sustain this load. Reduce -c or fix the origin first.", a.errRate))
	}
	// Capacity hint.
	if a.errRate < 1 && a.p99 < 500 {
		recs = append(recs, fmt.Sprintf("Site handled %d concurrent users at %.0f req/s with no issues — try doubling -c to find the ceiling.", a.concurrency, a.throughput))
	}
	// 5xx.
	for code := range a.statusCounts {
		if code >= 500 {
			recs = append(recs, fmt.Sprintf("Got HTTP %d responses — the origin is erroring under load. Check origin logs and resource limits (CPU/memory/DB connections).", code))
			break
		}
	}
	// 429.
	if _, has := a.statusCounts[429]; has {
		recs = append(recs, "Received 429 rate-limiting — the CDN or origin is throttling. Use -rps to stay under the limit.")
	}
	// Resource exhaustion.
	if a.errCats["Resource exhaustion"] > 0 {
		recs = append(recs, "Hit OS resource limits (sockets/file descriptors). Run `ulimit -n` and increase it, or reduce -c.")
	}
	// Timeout dominant.
	if a.errCats["Timeout"] > 0 && a.errCats["Timeout"] == a.errs {
		recs = append(recs, "All errors are timeouts — the origin can't keep up. Try a longer -timeout or lower -c to see if it's a capacity wall.")
	}
	// Default positive note.
	if len(recs) == 0 {
		recs = append(recs, "No issues detected at this load level. To find the breaking point, increase -c (e.g. 100, 200, 500) and re-run.")
	}
	for _, r := range recs {
		fmt.Printf("  %s %s\n", col(c.cyan, "→"), r)
	}
	fmt.Println()
}

type insightRule struct {
	le   float64
	msg  string
	good bool
}

func addInsight(label string, value float64, unit string, context string, rules []insightRule) {
	matched := false
	for _, r := range rules {
		if r.le == 0 || value <= r.le {
			icon := col(c.green, "✓")
			if !r.good {
				icon = col(c.yellow, "⚠")
			}
			fmt.Printf("  %s %s: %s\n", icon, col(c.bold, fmt.Sprintf("%s = %.2f %s", label, value, unit)), r.msg)
			matched = true
			break
		}
	}
	if !matched {
		fmt.Printf("  %s %s: %s\n", col(c.red, "✗"), col(c.bold, fmt.Sprintf("%s = %.2f %s", label, value, unit)), rules[len(rules)-1].msg)
	}
	fmt.Printf("    %s %s\n", col(c.gray, "└"), col(c.dim, context))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func printStat(label, value string) {
	fmt.Printf("  %-18s %s\n", col(c.dim, label), value)
}

func printLatencyRow(label string, value, maxLat float64) {
	barWidth := 24
	frac := 0.0
	if maxLat > 0 {
		frac = value / maxLat
	}
	barLen := int(frac * float64(barWidth))
	if barLen < 1 {
		barLen = 1
	}
	bar := strings.Repeat("█", barLen) + strings.Repeat("░", barWidth-barLen)
	color := latencyColor(value)
	fmt.Printf("  %s %-6s %s  %s\n",
		col(c.dim, "•"),
		col(c.bold, label),
		col(color, bar),
		col(color, fmt.Sprintf("%7.2f ms", value)))
}

func printHistogram(latencies []float64) {
	fmt.Println(col(c.bold, "  DISTRIBUTION"))
	fmt.Printf("  %s\n", strings.Repeat("─", 52))
	bins := []struct {
		lo, hi float64
		label  string
	}{
		{0, 50, "<50ms"},
		{50, 100, "50-100ms"},
		{100, 250, "100-250ms"},
		{250, 500, "250-500ms"},
		{500, 1000, "500ms-1s"},
		{1000, math.Inf(1), ">1s"},
	}
	counts := make([]int, len(bins))
	for _, l := range latencies {
		for i, b := range bins {
			if l >= b.lo && l < b.hi {
				counts[i]++
				break
			}
		}
	}
	maxCount := 0
	for _, n := range counts {
		if n > maxCount {
			maxCount = n
		}
	}
	barWidth := 30
	for i, b := range bins {
		count := counts[i]
		frac := 0.0
		if maxCount > 0 {
			frac = float64(count) / float64(maxCount)
		}
		barLen := int(frac * float64(barWidth))
		bar := strings.Repeat("█", barLen) + strings.Repeat("░", barWidth-barLen)
		pctStr := col(c.dim, fmt.Sprintf("%.1f%%", pct(count, len(latencies))))
		fmt.Printf("  %s %-12s %s  %s  %s\n",
			col(c.dim, "•"),
			b.label,
			col(c.cyan, bar),
			padLeft(fmt.Sprintf("%d", count), 7),
			pctStr)
	}
	fmt.Println()
}

// ---------- Helpers ----------

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func avg(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func pct(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole) * 100
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func padLeft(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return strings.Repeat(" ", n-len(s)) + s
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func statusColor(code int) string {
	switch {
	case code >= 200 && code < 300:
		return c.green
	case code >= 300 && code < 400:
		return c.cyan
	case code >= 400 && code < 500:
		return c.yellow
	case code >= 500:
		return c.red
	default:
		return c.reset
	}
}

func latencyColor(ms float64) string {
	switch {
	case ms < 100:
		return c.green
	case ms < 500:
		return c.yellow
	default:
		return c.red
	}
}

func isTTY(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

type limitCtx struct {
	context.Context
	remaining *atomic.Int64
}

func (l *limitCtx) Err() error {
	if l.remaining.Add(-1) < 0 {
		return context.Canceled
	}
	return l.Context.Err()
}

func withLimit(parent context.Context, n int64) context.Context {
	rem := &atomic.Int64{}
	rem.Store(n)
	return &limitCtx{Context: parent, remaining: rem}
}


func init() { log.SetFlags(0) }
