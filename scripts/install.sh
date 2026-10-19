#!/bin/sh
# macOS / Linux installer:  curl -fsSL https://raw.githubusercontent.com/yeongseoo/tchat/main/scripts/install.sh | sh
set -e
REPO="yeongseoo/tchat"
case "$(uname -s)" in Darwin) os=darwin ;; Linux) os=linux ;; *) echo "지원하지 않는 OS입니다"; exit 1 ;; esac
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) echo "지원하지 않는 CPU입니다"; exit 1 ;; esac
url="https://github.com/$REPO/releases/latest/download/tchat_${os}_${arch}.tar.gz"
dir="${TCHAT_INSTALL_DIR:-$HOME/.local/bin}"
mkdir -p "$dir"
tmp="$(mktemp -d)"
echo "다운로드: $url"
curl -fsSL "$url" | tar -xz -C "$tmp"
mv "$tmp/tchat" "$dir/tchat"
chmod +x "$dir/tchat"
rm -rf "$tmp"
echo "설치 완료: $dir/tchat"
case ":$PATH:" in *":$dir:"*) ;; *)
  echo ""
  echo "$dir 가 PATH에 없습니다. 아래 줄을 ~/.zshrc (또는 ~/.bashrc)에 추가하세요:"
  echo "  export PATH=\"$dir:\$PATH\"" ;;
esac
echo "실행: tchat"
