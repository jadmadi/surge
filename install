#!/usr/bin/env bash
# Installer script for surge - lightning-fast HTTP load tester.
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/jadmadi/surge/main/install.sh | bash
#
# Custom install location:
#   curl -fsSL https://raw.githubusercontent.com/jadmadi/surge/main/install.sh | BINDIR=/custom/bin bash
set -euo pipefail

REPO="jadmadi/surge"
BIN_NAME="surge"

# ANSI colors
if [[ -t 1 ]]; then
  GREEN="\033[32m"
  CYAN="\033[36m"
  YELLOW="\033[33m"
  RED="\033[31m"
  BOLD="\033[1m"
  RESET="\033[0m"
else
  GREEN=""
  CYAN=""
  YELLOW=""
  RED=""
  BOLD=""
  RESET=""
fi

log_info() {
  echo -e "${CYAN}→${RESET} $*"
}

log_success() {
  echo -e "${GREEN}✓${RESET} $*"
}

log_warn() {
  echo -e "${YELLOW}⚠${RESET} $*"
}

log_error() {
  echo -e "${RED}✗${RESET} $*" >&2
}

# 1. Detect OS
OS_RAW="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "${OS_RAW}" in
  linux*)   OS="linux" ;;
  darwin*)  OS="darwin" ;;
  mingw*|msys*|cygwin*) OS="windows" ;;
  *)
    log_error "Unsupported operating system: ${OS_RAW}"
    exit 1
    ;;
esac

# 2. Detect Architecture
ARCH_RAW="$(uname -m | tr '[:upper:]' '[:lower:]')"
case "${ARCH_RAW}" in
  x86_64|amd64)   ARCH="amd64" ;;
  aarch64|arm64)  ARCH="arm64" ;;
  *)
    log_error "Unsupported CPU architecture: ${ARCH_RAW}"
    exit 1
    ;;
esac

EXT="tar.gz"
if [[ "${OS}" == "windows" ]]; then
  EXT="zip"
fi

# 3. Determine Installation Directory
if [[ -n "${BINDIR:-}" ]]; then
  INSTALL_DIR="${BINDIR}"
elif [[ -w "/usr/local/bin" ]]; then
  INSTALL_DIR="/usr/local/bin"
elif [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
  INSTALL_DIR="/usr/local/bin"
else
  INSTALL_DIR="${HOME}/.local/bin"
fi

mkdir -p "${INSTALL_DIR}"

# 4. Resolve download URL
DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/${BIN_NAME}_${OS}_${ARCH}.${EXT}"

log_info "Installing ${BOLD}${BIN_NAME}${RESET} (${OS}/${ARCH}) to ${BOLD}${INSTALL_DIR}${RESET}..."

TMP_DIR="$(mktemp -d)"
cleanup() {
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

ARCHIVE_PATH="${TMP_DIR}/${BIN_NAME}.${EXT}"

# 5. Download archive
if command -v curl >/dev/null 2>&1; then
  HTTP_STATUS="$(curl -sSL -w "%{http_code}" -o "${ARCHIVE_PATH}" "${DOWNLOAD_URL}" || true)"
elif command -v wget >/dev/null 2>&1; then
  wget -q -O "${ARCHIVE_PATH}" "${DOWNLOAD_URL}"
  HTTP_STATUS="200"
else
  log_error "Neither curl nor wget is available. Please install one to continue."
  exit 1
fi

if [[ ! -s "${ARCHIVE_PATH}" ]] || [[ "${HTTP_STATUS}" != "200" && "${HTTP_STATUS}" != "000" ]]; then
  log_warn "Direct download from releases/latest failed (HTTP ${HTTP_STATUS})."
  log_info "Attempting fallback: build from source via Go..."
  if command -v go >/dev/null 2>&1; then
    GOBIN="${INSTALL_DIR}" go install "github.com/${REPO}@latest"
    log_success "Successfully compiled and installed ${BIN_NAME} via go install!"
  else
    log_error "Release asset could not be downloaded and Go compiler is not installed."
    log_error "Please check: https://github.com/${REPO}/releases"
    exit 1
  fi
else
  # 6. Extract archive
  if [[ "${EXT}" == "zip" ]]; then
    unzip -q -o "${ARCHIVE_PATH}" -d "${TMP_DIR}"
  else
    tar -xzf "${ARCHIVE_PATH}" -C "${TMP_DIR}"
  fi

  SRC_BIN="$(find "${TMP_DIR}" -type f -name "${BIN_NAME}*" | head -n 1)"
  if [[ -z "${SRC_BIN}" || ! -f "${SRC_BIN}" ]]; then
    log_error "Failed to locate extracted binary in archive."
    exit 1
  fi

  DEST_BIN="${INSTALL_DIR}/${BIN_NAME}"
  if [[ "${OS}" == "windows" ]]; then
    DEST_BIN="${INSTALL_DIR}/${BIN_NAME}.exe"
  fi

  # 7. Install binary
  if [[ -w "${INSTALL_DIR}" ]]; then
    mv "${SRC_BIN}" "${DEST_BIN}"
    chmod +x "${DEST_BIN}"
  else
    log_info "Elevated permissions required to write to ${INSTALL_DIR}."
    sudo mv "${SRC_BIN}" "${DEST_BIN}"
    sudo chmod +x "${DEST_BIN}"
  fi

  log_success "Successfully installed ${BOLD}${BIN_NAME}${RESET} to ${DEST_BIN}"
fi

# 8. PATH check
if [[ ":${PATH}:" != *":${INSTALL_DIR}:"* ]]; then
  log_warn "${INSTALL_DIR} is not in your PATH."
  echo -e "  Add it to your shell configuration (e.g. ~/.bashrc or ~/.zshrc):"
  echo -e "    ${BOLD}export PATH=\"${INSTALL_DIR}:\$PATH\"${RESET}"
fi

# 9. Quickstart Banner
echo
echo -e "${GREEN}╭──────────────────────────────────────────────────────────╮${RESET}"
echo -e "${GREEN}│${RESET}${BOLD}  surge is ready to run!                                 ${RESET}${GREEN}│${RESET}"
echo -e "${GREEN}╰──────────────────────────────────────────────────────────╯${RESET}"
echo
echo -e "  Quick start:"
echo -e "    ${CYAN}surge baseline https://example.com${RESET}"
echo -e "    ${CYAN}surge realistic https://example.com${RESET}"
echo -e "    ${CYAN}surge --help${RESET}"
echo
