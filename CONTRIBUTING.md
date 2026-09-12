# Contributing to surge

Thank you for your interest in contributing to `surge`! We welcome bug reports, feature suggestions, documentation improvements, and pull requests.

## Code of Conduct

All contributors are expected to uphold our [Code of Conduct](CODE_OF_CONDUCT.md). Please treat others with respect and kindness.

---

## Development Setup

### Prerequisites

- **Go**: 1.21 or higher
- **Git**
- *(Optional)* **UPX**: for binary compression (`sudo apt install upx` or `brew install upx`)

### Clone and Run

```bash
git clone https://github.com/jadmadi/surge.git
cd surge

# Run without building a binary
go run . baseline https://example.com

# Build the local binary
./build.sh
```

---

## Testing

Always run the full test suite with Go's race detector enabled before submitting changes:

```bash
# Run tests with race detection
go test -v -race ./...

# Run static analysis
go vet ./...
```

If you add new features or flags, please add corresponding tests in `main_test.go`.

---

## Development Principles

When contributing to `surge`, keep these core principles in mind:

1. **Zero Runtime Dependencies**: The tool is designed to compile into a completely standalone static binary (`CGO_ENABLED=0`). Avoid adding heavy third-party dependencies unless strictly necessary.
2. **Speed & Efficiency**: Hot paths (worker loops, latency tracking, stats computation) must remain allocation-light and thread-safe.
3. **Clean CLI UX**: Output must be readable, with clear visual hierarchy, sensible defaults, and both human-friendly text and machine-readable JSON modes.
4. **Safety First**: Respectful load testing guidelines and legal notices must be maintained.

---

## Pull Request Process

1. **Fork the repo** and create your branch from `main`:
   ```bash
   git checkout -b feature/my-cool-feature
   ```
2. **Format your code**:
   ```bash
   gofmt -s -w .
   ```
3. **Verify tests pass**:
   ```bash
   go test -v -race ./...
   go vet ./...
   ```
4. **Commit your changes**:
   Use clear, conventional commit messages (e.g., `feat: add ...`, `fix: resolve ...`, `docs: update ...`).
5. **Open a Pull Request**:
   Describe the rationale behind the change, what problem it solves, and how you tested it.

---

## Reporting Issues

- **Bug Reports**: Please include the command you ran (obfuscating sensitive URLs/tokens), expected behavior, actual output, OS, and `surge --version`.
- **Feature Requests**: Describe the use case and why existing flags/presets don't meet your needs.
