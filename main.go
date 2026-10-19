package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"

	"tchat/internal/client"
	"tchat/internal/server"
)

// defaultServer is the address users connect to when they just type `tchat`.
// Set it at build time:  go build -ldflags "-X main.defaultServer=wss://chat.example.com/ws"
var defaultServer = "ws://localhost:8080/ws"

var version = "dev"

const usage = `tchat - 터미널 채팅

사용법:
  tchat                 기본 서버에 접속
  tchat <서버주소>        다른 서버에 접속 (예: tchat chat.example.com)
  tchat serve [-addr :8080]  서버 실행
  tchat version         버전 보기
`

func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "serve":
			fs := flag.NewFlagSet("serve", flag.ExitOnError)
			addr := fs.String("addr", defaultAddr(), "listen address")
			fs.Parse(args[1:])
			if err := server.Run(*addr); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "version", "-v", "--version":
			fmt.Println("tchat", version)
			return
		case "help", "-h", "--help":
			fmt.Print(usage)
			return
		}
	}

	target := defaultServer
	if env := os.Getenv("TCHAT_SERVER"); env != "" {
		target = env
	}
	if len(args) > 0 {
		target = args[0]
	}
	wsURL, err := normalizeURL(target)
	if err != nil {
		fmt.Fprintln(os.Stderr, "서버 주소가 올바르지 않습니다:", err)
		os.Exit(1)
	}
	if err := client.Run(wsURL); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func defaultAddr() string {
	// Most hosting platforms tell the app which port to use via $PORT.
	if p := os.Getenv("PORT"); p != "" {
		return ":" + p
	}
	return ":8080"
}

// normalizeURL accepts friendly forms like "chat.example.com" or
// "https://chat.example.com" and turns them into a WebSocket URL.
func normalizeURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "://") {
		if strings.HasPrefix(s, "localhost") || strings.HasPrefix(s, "127.") {
			s = "ws://" + s
		} else {
			s = "wss://" + s
		}
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("지원하지 않는 주소 형식: %s", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("호스트가 없습니다")
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/ws"
	}
	return u.String(), nil
}
