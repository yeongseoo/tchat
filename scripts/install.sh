#!/bin/sh
# macOS / Linux installer:  curl -fsSL https://raw.githubusercontent.com/yeongseoo/tchat/main/scripts/install.sh | sh
set -e
REPO="yeongseoo/tchat"
case "$(uname -s)" in Darwin) os=darwin ;; Linux) os=linux ;; *) echo "지원하지 않는 OS입니다"; exit 1 ;; esac
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) echo "지원하지 않는 CPU입니다"; exit 1 ;; esac
url="https://github.com/$REPO/releases/latest/download/tchat_${os}_${arch}.tar.gz"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
echo "다운로드 중..."
curl -fsSL "$url" | tar -xz -C "$tmp"
chmod +x "$tmp/tchat"

# Install somewhere already on PATH so `tchat` works right away.
# /usr/local/bin is on the default PATH of macOS and most Linux systems.
dest=""
if [ -n "$TCHAT_INSTALL_DIR" ]; then
  dest="$TCHAT_INSTALL_DIR"; mkdir -p "$dest"; mv "$tmp/tchat" "$dest/tchat"
elif [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
  dest=/usr/local/bin; mv "$tmp/tchat" "$dest/tchat"
elif command -v sudo >/dev/null 2>&1 && [ -r /dev/tty ]; then
  echo "/usr/local/bin 에 설치하려면 컴퓨터 비밀번호가 필요합니다 (입력해도 화면에 안 보이는 게 정상입니다)."
  if sudo mkdir -p /usr/local/bin </dev/tty && sudo mv "$tmp/tchat" /usr/local/bin/tchat </dev/tty; then
    dest=/usr/local/bin
  fi
fi

if [ -z "$dest" ]; then
  # No admin rights: install for this user and add it to the shell's PATH.
  dest="$HOME/.local/bin"
  mkdir -p "$dest"
  mv "$tmp/tchat" "$dest/tchat"
  line="export PATH=\"$dest:\$PATH\""
  for rc in "$HOME/.zshrc" "$HOME/.bashrc"; do
    if [ "$rc" = "$HOME/.zshrc" ] || [ -f "$rc" ]; then
      grep -qsF "$line" "$rc" || echo "$line" >> "$rc"
    fi
  done
fi

# Downloaded with curl, so macOS Gatekeeper does not quarantine it, but be safe.
[ "$os" = darwin ] && xattr -d com.apple.quarantine "$dest/tchat" 2>/dev/null || true

echo ""
echo "설치 완료!"
case ":$PATH:" in
  *":$dest:"*) echo "이제 'tchat' 을 입력하면 바로 시작됩니다." ;;
  *) echo "새 터미널 창을 열고 'tchat' 을 입력하세요. (이 창에서 바로 하려면: $dest/tchat)" ;;
esac
