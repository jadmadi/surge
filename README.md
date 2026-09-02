# load-test

A concurrent HTTP load testing tool written in Go. Simulates virtual users hitting a URL and reports latency percentiles, throughput, status code distribution, error breakdown, and actionable recommendations.

Single binary. No dependencies. No runtime. No Docker. Just download and run.

## Features

- **Concurrent virtual users** — spin up hundreds of workers hammering a target URL
- **Latency percentiles** — min, p25, p50, p75, p90, p95, p99, max, plus a visual histogram
- **Test presets** — `baseline`, `realistic`, `capacity`, `spike` for common load testing scenarios
- **Ramp-up** — gradually launch concurrency instead of a thundering herd
- **Think time** — random sleep between requests to simulate real user browsing behavior
- **Custom headers** — repeatable `-H` flag with `@file` support for long JWTs
- **Rate limiting** — cap requests per second across all workers
- **Error classification** — categorizes errors (timeout, connection refused, DNS, TLS, etc.) with sample messages
- **Status code analysis** — interprets 4xx/5xx codes and suggests fixes
- **Analysis & recommendations** — automatic verdict (Excellent/Good/Fair/Poor) with actionable insights
- **JSON output** — machine-readable format for CI/CD pipelines and dashboards
- **Colored terminal output** — visual report with ANSI colors (auto-disabled when piped)
- **Single binary** — UPX-compressed, ~2 MB, zero runtime dependencies

## Install

### Download the binary

```bash
# Linux (amd64)
curl -L -o load-test https://github.com/jadMadi/load-test/releases/latest/download/load-test-linux-amd64
chmod +x load-test
```

### Build from source

```bash
git clone https://github.com/jadMadi/load-test.git
cd load-test
go build -o load-test .

# Or use the UPX-compressed build script
./build.sh
```

**Requirements:** Go 1.21+ (only for building). The compiled binary has no runtime dependencies.

## Quick start

```bash
# Is the site fast for one user?
./load-test baseline https://example.com

# What do 50 real users experience?
./load-test realistic https://example.com

# Find the breaking point
./load-test capacity https://example.com

# Survive a traffic burst
./load-test spike https://example.com
```

## Test presets

Presets bundle sensible flag combinations for common load testing scenarios. Override any preset flag by passing it explicitly.

| Preset | Concurrency | Total | Duration | Ramp-up | Think time | Use case |
|---|---|---|---|---|---|---|
| `baseline` | 1 | 100 | — | — | — | Pure latency floor, no concurrency noise |
| `realistic` | 50 | — | 1m | 10s | 0–3s random | Simulates real browsing behavior |
| `capacity` | 200 | — | 30s | 20s | — | Find where the site starts degrading |
| `spike` | 500 | — | 10s | — | — | Burst survival, not comfort |

```bash
# Use a preset but override concurrency
./load-test realistic https://example.com -c 100

# Use a preset but extend the duration
./load-test spike https://example.com -d 30s
```

## Usage

```
load-test <profile> <URL> [flags]
load-test <URL> --profile <name> [flags]
```

### Flags

| Flag | Alias | Type | Default | Description |
|---|---|---|---|---|
| `-c` | `--concurrency` | int | 10 | Concurrent users |
| `-n` | `--total` | int | 0 | Total requests (0 = run for duration) |
| `-d` | `--duration` | dur | 10s | Test duration when -n is 0 |
| `-H` | `--header` | str | | Custom header `Key: Value` (repeatable, `@file` supported) |
| `-m` | `--method` | str | GET | HTTP method |
| | `--ramp-up` | dur | 0 | Gradually launch concurrency over this duration |
| | `--think-time` | dur | 0 | Random sleep (0 to this) between requests per worker |
| `-o` | `--output` | str | text | Output format: `text` or `json` |
| | `--profile` | str | | Test preset: `baseline`, `realistic`, `capacity`, `spike` |
| `-rps` | | int | 0 | Max requests per second (0 = no limit) |
| | `--timeout` | dur | 30s | Per-request timeout |
| `-body` | | str | | Request body |
| `-ct` | | str | application/json | Content-Type header when -body is set |
| `-url` | `--target` | str | | Target URL (or pass as positional arg) |
| | `--insecure` | | | Skip TLS certificate verification |
| | `--no-color` | | | Disable colored output |

### Examples

#### Basic load test

```bash
./load-test https://example.com -c 50 -d 15s
```

#### API endpoint with authentication

```bash
./load-test https://api.example.com/users -c 100 -d 30s \
  -H "Authorization: Bearer eyJhbGci..." \
  -H "X-API-Key: secret-key"
```

#### Load header from a file (useful for long JWTs)

```bash
echo "Authorization: Bearer eyJhbGciOiJIUzI1NiIs..." > /tmp/auth.txt
./load-test https://api.example.com -c 50 -d 30s -H @/tmp/auth.txt
```

#### Realistic user simulation with ramp-up and think time

```bash
./load-test https://example.com -c 200 --ramp-up 10s --think-time 2s -d 1m
```

#### Rate-limited test (cap at 200 req/s)

```bash
./load-test https://example.com -c 100 -rps 200 -d 30s
```

#### POST with a JSON body

```bash
./load-test https://api.example.com/users -m POST \
  -body '{"name":"test","email":"test@example.com"}' \
  -c 20 -n 100
```

#### JSON output for CI/CD pipelines

```bash
./load-test realistic https://example.com -o json | jq .summary
```

```bash
# Fail CI if p99 exceeds 500ms
./load-test https://example.com -c 50 -n 1000 -o json | \
  jq -e '.latency.p99_ms < 500' > /dev/null || exit 1
```

## Output

### Text output (default)

The text report includes:

1. **Banner** — test parameters (target, concurrency, duration, ramp-up, think time)
2. **Summary** — elapsed, requests sent, completed, successful, errors, throughput
3. **Latency** — percentile table with visual bars (min, p25, p50, p75, p90, p95, p99, max, avg)
4. **Distribution** — histogram of latency buckets (<50ms, 50-100ms, 100-250ms, etc.)
5. **Status codes** — HTTP response code counts with percentages
6. **Errors** — categorized error breakdown with sample messages
7. **Analysis** — overall verdict, latency interpretation, error analysis, and recommendations

### JSON output (`-o json`)

```json
{
  "summary": {
    "elapsed": "15.001s",
    "sent": 29530,
    "completed": 29530,
    "successful": 29480,
    "errors": 50,
    "error_rate_pct": 0.17,
    "throughput_req_s": 1968.5,
    "concurrency": 50
  },
  "latency": {
    "min_ms": 12,
    "p50_ms": 22,
    "p90_ms": 31,
    "p99_ms": 87,
    "max_ms": 294,
    "avg_ms": 24.64
  },
  "distribution": [...],
  "status_codes": { "200": 29480 },
  "errors": {
    "total": 50,
    "rate_pct": 0.17,
    "categories": { "Timeout": 50 },
    "samples": { "Timeout": "Get \"https://...\": context deadline exceeded" }
  }
}
```

## How to interpret results

### Verdict

| Verdict | Error rate | p99 | Meaning |
|---|---|---|---|
| Excellent | < 0.1% | < 200ms | Site is fast and stable under this load |
| Good | < 1% | < 500ms | Minor issues, room for optimization |
| Fair | < 5% | < 1000ms | Noticeable degradation, investigate before scaling |
| Poor | > 5% | > 1000ms | Site is failing under this load |

### Key metrics to watch

- **p50 (median)** — what most users experience
- **p99 (tail)** — what 1% of users experience; SREs watch this closely
- **p99/p50 ratio** — tail variance; a ratio > 10 indicates cold starts, cache misses, or GC pauses
- **Error rate** — anything above 1% is worth investigating
- **Throughput** — requests per second; compare across runs to track capacity changes

## Legal

This tool is for testing **your own** websites and APIs only. Do **not** use it against sites you do not own or have explicit permission to test.

Unauthorized load testing may violate computer fraud, abuse, and trespass laws in your jurisdiction, including but not limited to:
- **United States**: Computer Fraud and Abuse Act (CFAA)
- **United Kingdom**: Computer Misuse Act 1990
- **European Union**: Directive 2013/40/EU on attacks against information systems

Illegal use will be reported to the relevant authorities. You alone are responsible for ensuring you have authorization to test the target.

## Developer

**Jad Madi**

- Email: [jadmadi@duck.com](mailto:jadmadi@duck.com)
- X (Twitter): [@jadmadi](https://x.com/jadmadi)

## License

[MIT](LICENSE) — Jad Madi
