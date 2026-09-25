# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- `--fail-threshold` flag to exit with status code 1 when error rate exceeds threshold.
- Live animated progress bar, spinner, and status line for interactive terminal runs.
- Automated diagnostic verdict badge with latency and error rate categorization.
- Helper functions and unit tests for number, percentage, middle truncation, and status code formatting.

### Changed
- Refined terminal reporting layout and styling.

## [0.1.0] - 2026-09-12

### Added
- Concurrent HTTP load testing engine with configurable concurrency, duration, requests, and rate limiting.
- Presets: `baseline`, `realistic`, `capacity`, and `spike`.
- Detailed latency histogram and percentiles (min, p25, p50, p75, p90, p95, p99, max).
- Machine-readable JSON output mode (`-o json`).
- One-liner installer script (`install.sh`).
