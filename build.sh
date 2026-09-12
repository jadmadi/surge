#!/usr/bin/env bash
# Production build & release script for surge.
#
# Usage:
#   ./build.sh                  Build optimized binary for current OS/arch
#   ./build.sh [output-path]    Build to custom output path
#   ./build.sh --release        Cross-compile all release platforms to dist/
#   ./build.sh --no-upx         Build without UPX compression
#   ./build.sh --clean          Remove build artifacts and dist/
#   ./build.sh -h, --help       Show this help
set -euo pipefail

BIN_NAME="surge"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo 'dev')}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo 'none')}"
DATE="${DATE:-$(date -u +'%Y-%m-%dT%H:%M:%SZ')}"

LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}"

USE_UPX=true
BUILD_RELEASE=false
OUTPUT_PATH="./${BIN_NAME}"

# Parse command line flags
while [[ $# -gt 0 ]]; do
  case "$1" in
    --release|--all)
      BUILD_RELEASE=true
      shift
      ;;
    --no-upx)
      USE_UPX=false
      shift
      ;;
    --clean)
      echo "→ cleaning build artifacts"
      rm -rf dist "${BIN_NAME}" "${BIN_NAME}.exe"
      echo "✓ clean complete"
      exit 0
      ;;
    -h|--help)
      cat <<EOF
Usage: ./build.sh [OPTIONS] [OUTPUT_PATH]

Options:
  --release, --all    Cross-compile all platform targets into dist/
  --no-upx            Skip UPX binary compression
  --clean             Remove dist/ and local binary outputs
  -h, --help          Show this help message

Examples:
  ./build.sh                     # Build local binary (${BIN_NAME})
  ./build.sh /usr/local/bin/${BIN_NAME}
  ./build.sh --release           # Build tarballs/zip with checksums in dist/
EOF
      exit 0
      ;;
    *)
      OUTPUT_PATH="$1"
      shift
      ;;
  esac
done

compress_with_upx() {
  local target="$1"
  if [[ "${USE_UPX}" == "true" ]]; then
    if command -v upx >/dev/null 2>&1; then
      echo "  ↳ compressing with UPX: $(basename "${target}")"
      upx --best --lzma "${target}" >/dev/null 2>&1 || {
        # Fallback to standard upx compression if lzma is not supported for platform
        upx --best "${target}" >/dev/null 2>&1 || true
      }
    else
      echo "  ↳ [notice] upx not installed; skipping compression"
    fi
  fi
}

build_single() {
  local out="$1"
  local os="${2:-$(go env GOOS)}"
  local arch="${3:-$(go env GOARCH)}"

  echo "→ building ${out} (${os}/${arch}, version: ${VERSION})"
  CGO_ENABLED=0 GOOS="${os}" GOARCH="${arch}" go build -trimpath -ldflags="${LDFLAGS}" -o "${out}" .
  compress_with_upx "${out}"
  echo "✓ built ${out} ($(du -h "${out}" | cut -f1))"
}

build_release() {
  local dist_dir="dist"
  rm -rf "${dist_dir}"
  mkdir -p "${dist_dir}"

  echo "=================================================="
  echo " Building ${BIN_NAME} ${VERSION} for release"
  echo " Git Commit: ${COMMIT}"
  echo " Build Date: ${DATE}"
  echo "=================================================="

  local targets=(
    "linux amd64"
    "linux arm64"
    "darwin amd64"
    "darwin arm64"
    "windows amd64"
  )

  for target in "${targets[@]}"; do
    read -r os arch <<<"${target}"
    local ext=""
    if [[ "${os}" == "windows" ]]; then
      ext=".exe"
    fi

    local bin_target="${dist_dir}/${BIN_NAME}-${os}-${arch}${ext}"
    echo "→ building ${os}/${arch}..."
    CGO_ENABLED=0 GOOS="${os}" GOARCH="${arch}" go build -trimpath -ldflags="${LDFLAGS}" -o "${bin_target}" .

    # Compress Linux & Windows binaries with UPX (Darwin binaries often break signature checks if UPX-ed)
    if [[ "${os}" != "darwin" ]]; then
      compress_with_upx "${bin_target}"
    fi

    # Create archive
    local archive_name="${BIN_NAME}_${os}_${arch}"
    local versioned_name="${BIN_NAME}_${VERSION#v}_${os}_${arch}"
    if [[ "${os}" == "windows" ]]; then
      (cd "${dist_dir}" && zip -q "${archive_name}.zip" "${BIN_NAME}-${os}-${arch}${ext}")
      if [[ "${archive_name}" != "${versioned_name}" ]]; then
        cp "${dist_dir}/${archive_name}.zip" "${dist_dir}/${versioned_name}.zip"
      fi
      rm -f "${dist_dir}/${BIN_NAME}-${os}-${arch}${ext}"
      echo "  ↳ created ${dist_dir}/${archive_name}.zip ($(du -h "${dist_dir}/${archive_name}.zip" | cut -f1))"
    else
      (cd "${dist_dir}" && tar -czf "${archive_name}.tar.gz" "${BIN_NAME}-${os}-${arch}")
      if [[ "${archive_name}" != "${versioned_name}" ]]; then
        cp "${dist_dir}/${archive_name}.tar.gz" "${dist_dir}/${versioned_name}.tar.gz"
      fi
      rm -f "${dist_dir}/${BIN_NAME}-${os}-${arch}"
      echo "  ↳ created ${dist_dir}/${archive_name}.tar.gz ($(du -h "${dist_dir}/${archive_name}.tar.gz" | cut -f1))"
    fi
  done

  echo "→ generating SHA256SUMS..."
  (cd "${dist_dir}" && sha256sum * > SHA256SUMS)
  echo "✓ release build complete in ${dist_dir}/:"
  ls -lh "${dist_dir}"
}

if [[ "${BUILD_RELEASE}" == "true" ]]; then
  build_release
else
  build_single "${OUTPUT_PATH}"
fi
