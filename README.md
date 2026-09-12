# surge

[![CI](https://github.com/jadmadi/surge/actions/workflows/ci.yml/badge.svg)](https://github.com/jadmadi/surge/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/jadmadi/surge?color=brightgreen)](https://github.com/jadmadi/surge/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/jadmadi/surge)](go.mod)

A lightning-fast, zero-dependency concurrent HTTP load testing tool written in Go. Simulates virtual users hitting a target URL or API, reporting latency percentiles, throughput, status code distributions, error breakdowns, and automated health verdicts.

**Single binary. Zero runtime dependencies. No Docker required. Just download and run.**

---

## Why `surge`?

| Feature | `surge` | `hey` | `wrk` | `ab` |
| :--- | :---: | :---: | :---: | :---: |
| **Zero Dependencies** | :white_check_mark: Single static binary (~2 MB) | :white_check_mark: | :x: C / OpenSSL / Lua | :x: Apache utils |
| **Built-in Presets** | :white_check_mark: `baseline`, `realistic`, `capacity`, `spike` | :x: | :x: | :x: |
| **Ramp-Up Curves** | :white_check_mark: `--ramp-up 10s` | :x: | :x: (Lua only) | :x: |
| **Think-Time Simulation** | :white_check_mark: `--think-time 3s` | :x: | :x: (Lua only) | :x: |
| **Automated Diagnostic Verdict** | :white_check_mark: Excellent/Good/Fair/Poor + tips | :x: | :x: | :x: |
| **Latency Histogram & Percentiles** | :white_check_mark: min, p25, p50, p75, p90, p95, p99, max | :white_check_mark: Partial | :white_check_mark: Partial | :x: |
| **JSON Output for CI/CD** | :white_check_mark: `-o json` | :white_check_mark: | :x: (JSON scripts needed) | :x: |

---

## Installation

### 1. Go Install (Recommended for Go users)

```bash
go install github.com/jadmadi/surge@latest
```

### 2. Precompiled Binaries (Linux, macOS, Windows)

Download the latest release for your architecture from the [GitHub Releases](https://github.com/jadmadi/surge/releases) page:

```bash
# Linux (amd64)
curl -sSL -o surge https://github.com/jadmadi/surge/releases/latest/download/surge_linux_amd64.tar.gz | tar -xz
chmod +x surge
sudo mv surge /usr/local/bin/

# macOS (Apple Silicon / arm64)
curl -sSL -o surge https://github.com/jadmadi/surge/releases/latest/download/surge_darwin_arm64.tar.gz | tar -xz
chmod +x surge
sudo mv surge /usr/local/bin/
```

### 3. Build from Source

```bash
git clone https://github.com/jadmadi/surge.git
cd surge

# Build local binary
./build.sh
```

*(Optional: install `upx` if you want UPX binary compression to ~2 MB).*

---

## Quick Start

```bash
# Pure baseline latency floor (1 user, 100 requests)
surge baseline https://example.com

# Simulate 50 realistic users browsing with ramp-up and think time
surge realistic https://example.com

# Find the breaking point (200 users ramped over 20s)
surge capacity https://example.com

# Burst survival check (500 users hitting simultaneously for 10s)
surge spike https://example.com
```

---

## Presets

Presets bundle proven parameter combinations for standard performance engineering workflows. You can override any individual preset flag by passing it explicitly.

| Preset | Concurrency | Total | Duration | Ramp-up | Think time | Purpose |
| :--- | :---: | :---: | :---: | :---: | :---: | :--- |
| `baseline` | 1 | 100 | — | — | — | Measure pure latency floor without concurrency noise |
| `realistic` | 50 | — | 60s | 10s | 0–3s random | Real user browsing behavior with pauses and ramp-up |
| `capacity` | 200 | — | 30s | 20s | — | Gradually push system to locate performance degradation |
| `spike` | 500 | — | 10s | instant | — | Burst survival check under abrupt load |

```bash
# Override preset flags:
surge realistic https://example.com -c 100 -d 2m
```

---

## Common Recipes

### API Endpoint with Authentication

```bash
surge https://api.example.com/v1/orders \
  -c 50 -d 30s \
  -H "Authorization: Bearer <token>" \
  -H "X-Client-ID: my-app"
```

### Reading Headers from a File (e.g. Long JWTs)

```bash
surge https://api.example.com/v1/profile -H @/path/to/auth-header.txt
```

### POST Requests with JSON Body

```bash
surge https://api.example.com/items \
  -m POST \
  -body '{"sku":"ITEM-123","quantity":2}' \
  -ct application/json \
  -c 25 -n 500
```

### Rate Limiting (Cap Throughput)

```bash
# Cap aggregate traffic across all workers at 250 requests/sec
surge https://example.com -c 50 -rps 250 -d 1m
```

### CI/CD Quality Gates with JSON Output

```bash
# Assert p99 latency < 250ms and error rate == 0 in CI pipelines
surge baseline https://staging.example.com -o json | jq -e '
  .latency.p99_ms < 250 and .summary.errors == 0
' > /dev/null || (echo "Performance regression detected!" && exit 1)
```

---

## Flags & Options

```
Usage:
  surge <profile> <URL> [flags]
  surge <URL> --profile <name> [flags]
```

| Short | Long | Type | Default | Description |
| :--- | :--- | :---: | :---: | :--- |
| `-c` | `--concurrency` | `int` | `10` | Concurrent virtual users |
| `-n` | `--total` | `int` | `0` | Total requests (`0` = run for duration) |
| `-d` | `--duration` | `dur` | `10s` | Test duration when `-n` is `0` |
| `-H` | `--header` | `str` | `""` | Custom header `'Key: Value'` (repeatable, `@file` supported) |
| `-m` | `--method` | `str` | `"GET"` | HTTP method (GET, POST, PUT, DELETE, etc.) |
| | `--ramp-up` | `dur` | `0` | Gradually launch concurrency over this duration |
| | `--think-time` | `dur` | `0` | Random sleep (0 to this duration) between requests |
| `-o` | `--output` | `str` | `"text"` | Output format: `text` or `json` |
| | `--profile` | `str` | `""` | Test preset: `baseline`, `realistic`, `capacity`, `spike` |
| `-rps` | | `int` | `0` | Max aggregate requests per second (`0` = unconstrained) |
| | `--timeout` | `dur` | `30s` | Per-request timeout |
| `-body` | | `str` | `""` | Request body string |
| `-ct` | | `str` | `"application/json"` | Content-Type header when `-body` is set |
| `-url` | `--target` | `str` | `""` | Target URL (can also be passed positionally) |
| `-v` | `--version` | | | Print version, commit hash, build date, and exit |
| | `--insecure` | | | Skip TLS certificate verification |
| | `--no-color` | | | Disable ANSI colored terminal output |

---

## Interpreting Reports

### Verdict Thresholds

| Verdict | Error Rate | p99 Latency | Meaning |
| :--- | :---: | :---: | :--- |
| **Excellent** | `< 0.1%` | `< 200 ms` | Fast, healthy, fully stable under tested load |
| **Good** | `< 1.0%` | `< 500 ms` | Stable with minor latency variance |
| **Fair** | `< 5.0%` | `< 1000 ms` | Noticeable degradation; investigate server bottlenecks |
| **Poor** | `> 5.0%` | `> 1000 ms` | Severe degradation or service failures |

### Key Metrics to Monitor
- **p50 (Median)**: The typical experience for 50% of your requests.
- **p99 (Tail Latency)**: What the slowest 1% of users experience. Critical for detecting cold starts, GC stalls, and database locks.
- **p99/p50 Ratio**: A ratio greater than `10x` usually signals cache misses or downstream queuing issues.
- **Error Categories**: Classifies failures (DNS, TLS, connection refused, resets, timeouts) with sampled error messages.

---

## Contributing

We welcome contributions! Please review our [Contributing Guide](CONTRIBUTING.md) and [Code of Conduct](CODE_OF_CONDUCT.md) before submitting pull requests.

```bash
# Run tests with race detection
go test -v -race ./...

# Build release packages
./build.sh --release
```

---

## Legal & Ethical Disclaimer

`surge` is created for testing **your own** applications and infrastructure, or targets for which you have explicit written authorization. 

Unauthorized load testing may violate the Computer Fraud and Abuse Act (CFAA), the Computer Misuse Act, and international anti-trespass laws. You alone are responsible for your testing targets.

---

## License

[MIT](LICENSE) © 2025 Jad Madi
