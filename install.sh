#!/bin/sh
# Installs the latest Muster release on macOS or Linux; run it again to upgrade.
#   curl -fsSL https://raw.githubusercontent.com/YukiagoTpf/muster/main/install.sh | sh
# MUSTER_INSTALL_DIR picks the directory (default ~/.local/bin).
set -eu

dir="${MUSTER_INSTALL_DIR:-$HOME/.local/bin}"
case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) echo "muster: no release for $(uname -s); on Windows use install.ps1" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "muster: no release for $(uname -m)" >&2; exit 1 ;;
esac
asset="muster_${os}_${arch}"
url="https://github.com/YukiagoTpf/muster/releases/latest/download"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
curl -fsSL -o "$tmp/$asset" "$url/$asset"
curl -fsSL -o "$tmp/checksums.txt" "$url/checksums.txt"
cd "$tmp"
if command -v sha256sum >/dev/null 2>&1; then sum="sha256sum"; else sum="shasum -a 256"; fi
grep " $asset\$" checksums.txt | $sum -c - >/dev/null || { echo "muster: checksum mismatch for $asset" >&2; exit 1; }

mkdir -p "$dir"
chmod 755 "$asset"
mv -f "$asset" "$dir/muster"
echo "installed muster $("$dir/muster" version) to $dir/muster"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "$dir is not on your PATH; add it in your shell profile" ;;
esac
