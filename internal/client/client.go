// Package client is the terminal UI people run to chat.
// Nothing is written to disk: the conversation lives only in this
// program's memory and is gone when it exits.
package client

import (
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gorilla/websocket"

	"tchat/internal/protocol"
)

const (
	maxLines     = 1000 // scrollback kept in memory
	sidebarWidth = 22
)

// conn wraps the WebSocket so the UI can send from its own goroutine.
type conn struct {
	ws *websocket.Conn
	mu sync.Mutex
}

func (c *conn) send(m protocol.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	c.ws.WriteJSON(m)
}

type serverMsg protocol.Message
type disconnectedMsg struct{ err error }

type state int

const (
	stateNick state = iota
	stateLobby
	stateChat
)

type line struct {
	kind string // "msg", "system", "error", "info"
	from string
	text string
	at   time.Time
}

type model struct {
	conn   *conn
	server string
	state  state
	width  int
	height int

	input  textinput.Model
	status string // error or hint shown above the input

	nick string

	rooms    []protocol.RoomInfo
	cursor   int
	creating bool

	room  string
	users []string
	lines []line
	vp    viewport.Model
}

var (
	accent    = lipgloss.AdaptiveColor{Light: "#5A4FCF", Dark: "#9D8CFF"}
	dim       = lipgloss.AdaptiveColor{Light: "#777777", Dark: "#888888"}
	errColor  = lipgloss.AdaptiveColor{Light: "#C62828", Dark: "#FF6B6B"}
	nickColor = []lipgloss.AdaptiveColor{
		{Light: "#1565C0", Dark: "#64B5F6"},
		{Light: "#2E7D32", Dark: "#81C784"},
		{Light: "#AD1457", Dark: "#F06292"},
		{Light: "#E65100", Dark: "#FFB74D"},
		{Light: "#00838F", Dark: "#4DD0E1"},
		{Light: "#6A1B9A", Dark: "#BA68C8"},
		{Light: "#827717", Dark: "#DCE775"},
	}

	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(accent)
	dimStyle     = lipgloss.NewStyle().Foreground(dim)
	errStyle     = lipgloss.NewStyle().Foreground(errColor)
	selStyle     = lipgloss.NewStyle().Bold(true).Foreground(accent)
	systemStyle  = lipgloss.NewStyle().Foreground(dim).Italic(true)
	sidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(dim).
			PaddingLeft(1)
)

func colorFor(nick string) lipgloss.Style {
	h := fnv.New32a()
	h.Write([]byte(nick))
	return lipgloss.NewStyle().Bold(true).Foreground(nickColor[h.Sum32()%uint32(len(nickColor))])
}

// Run connects to the server and starts the UI.
func Run(url string) error {
	fmt.Printf("%s 에 연결하는 중...\n", url)
	dialer := websocket.Dialer{Proxy: http.ProxyFromEnvironment, HandshakeTimeout: 10 * time.Second}
	ws, _, err := dialer.Dial(url, nil)
	if err != nil {
		return fmt.Errorf("서버에 연결하지 못했습니다 (%s): %w", url, err)
	}
	defer ws.Close()
	c := &conn{ws: ws}

	ti := textinput.New()
	ti.Placeholder = "별명을 입력하세요"
	ti.CharLimit = 20
	ti.Focus()

	vp := viewport.New(0, 0)
	vp.KeyMap = viewport.KeyMap{
		PageUp:   key.NewBinding(key.WithKeys("pgup")),
		PageDown: key.NewBinding(key.WithKeys("pgdown")),
	}

	m := model{conn: c, server: url, input: ti, vp: vp}
	p := tea.NewProgram(m, tea.WithAltScreen())

	// Keep the connection alive and feed server messages into the UI.
	ws.SetPingHandler(func(data string) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		return ws.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(5*time.Second))
	})
	go func() {
		for {
			var msg protocol.Message
			if err := ws.ReadJSON(&msg); err != nil {
				p.Send(disconnectedMsg{err})
				return
			}
			p.Send(serverMsg(msg))
		}
	}()

	_, err = p.Run()
	return err
}

func (m model) Init() tea.Cmd { return textinput.Blink }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case disconnectedMsg:
		return m, tea.Sequence(tea.ExitAltScreen, tea.Println("서버와 연결이 끊어졌습니다."), tea.Quit)

	case serverMsg:
		m.handleServer(protocol.Message(msg))
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		switch m.state {
		case stateNick:
			return m.updateNick(msg)
		case stateLobby:
			return m.updateLobby(msg)
		case stateChat:
			return m.updateChat(msg)
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *model) handleServer(msg protocol.Message) {
	switch msg.Type {
	case protocol.Welcome:
		m.nick = msg.Nick
		m.toLobby()
	case protocol.Rooms:
		m.rooms = msg.Rooms
		if m.cursor >= len(m.rooms) {
			m.cursor = max(0, len(m.rooms)-1)
		}
	case protocol.Joined:
		m.state = stateChat
		m.room = msg.Room
		m.users = msg.Users
		m.lines = nil
		m.status = ""
		m.creating = false
		m.input.Reset()
		m.input.Placeholder = "메시지를 입력하세요  (/help 도움말)"
		m.input.CharLimit = 500
		m.input.Focus()
		m.addLine(line{kind: "info", text: fmt.Sprintf("'%s' 방에 들어왔습니다. 대화는 저장되지 않습니다.", msg.Room), at: time.Now()})
		m.layout()
	case protocol.Left:
		m.toLobby()
	case protocol.Users:
		m.users = msg.Users
	case protocol.Msg:
		m.addLine(line{kind: "msg", from: msg.From, text: msg.Text, at: time.Unix(msg.Time, 0)})
	case protocol.System:
		m.addLine(line{kind: "system", text: msg.Text, at: time.Unix(msg.Time, 0)})
	case protocol.NickOK:
		m.nick = msg.Nick
	case protocol.Error:
		if m.state == stateChat {
			m.addLine(line{kind: "error", text: msg.Text, at: time.Now()})
		} else {
			m.status = msg.Text
		}
	}
}

func (m *model) toLobby() {
	m.state = stateLobby
	m.room = ""
	m.users = nil
	m.lines = nil
	m.creating = false
	m.status = ""
	m.input.Reset()
	m.input.Blur()
}

func (m model) updateNick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "enter" {
		if nick := strings.TrimSpace(m.input.Value()); nick != "" {
			m.status = ""
			m.conn.send(protocol.Message{Type: protocol.Hello, Nick: nick})
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) updateLobby(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.creating {
		switch msg.String() {
		case "esc":
			m.creating = false
			m.input.Reset()
			m.input.Blur()
			return m, nil
		case "enter":
			if name := strings.TrimSpace(m.input.Value()); name != "" {
				m.status = ""
				m.conn.send(protocol.Message{Type: protocol.Create, Room: name})
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.rooms)-1 {
			m.cursor++
		}
	case "r":
		m.conn.send(protocol.Message{Type: protocol.List})
	case "n":
		m.creating = true
		m.status = ""
		m.input.Reset()
		m.input.Placeholder = "새 방 이름"
		m.input.CharLimit = 30
		return m, m.input.Focus()
	case "enter":
		if len(m.rooms) > 0 {
			m.status = ""
			m.conn.send(protocol.Message{Type: protocol.Join, Room: m.rooms[m.cursor].Name})
		}
	}
	return m, nil
}

func (m model) updateChat(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "pgup", "pgdown":
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	case "esc":
		m.conn.send(protocol.Message{Type: protocol.Leave})
		return m, nil
	case "enter":
		text := strings.TrimSpace(m.input.Value())
		m.input.Reset()
		if text == "" {
			return m, nil
		}
		if strings.HasPrefix(text, "/") {
			return m.command(text)
		}
		m.conn.send(protocol.Message{Type: protocol.Say, Text: text})
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) command(text string) (tea.Model, tea.Cmd) {
	cmd, arg, _ := strings.Cut(text, " ")
	arg = strings.TrimSpace(arg)
	switch cmd {
	case "/nick":
		if arg == "" {
			m.addLine(line{kind: "error", text: "사용법: /nick 새별명", at: time.Now()})
		} else {
			m.conn.send(protocol.Message{Type: protocol.Nick, Nick: arg})
		}
	case "/who":
		m.addLine(line{kind: "info", text: fmt.Sprintf("접속자 %d명: %s", len(m.users), strings.Join(m.users, ", ")), at: time.Now()})
	case "/leave":
		m.conn.send(protocol.Message{Type: protocol.Leave})
	case "/quit", "/exit":
		return m, tea.Quit
	case "/help":
		for _, h := range []string{
			"/nick 새별명   별명 바꾸기",
			"/who          접속자 보기",
			"/leave 또는 Esc  방 나가기",
			"/quit 또는 Ctrl+C  프로그램 종료",
			"PgUp/PgDn     이전 대화 스크롤",
		} {
			m.addLine(line{kind: "info", text: h, at: time.Now()})
		}
	default:
		m.addLine(line{kind: "error", text: "모르는 명령어입니다. /help 를 입력해보세요.", at: time.Now()})
	}
	return m, nil
}

func (m *model) addLine(l line) {
	m.lines = append(m.lines, l)
	if len(m.lines) > maxLines {
		m.lines = m.lines[len(m.lines)-maxLines:]
	}
	m.refreshViewport()
}

func (m *model) chatWidth() int {
	if m.width >= 60 {
		return m.width - sidebarWidth - 1
	}
	return m.width
}

func (m *model) layout() {
	m.input.Width = max(10, m.width-4)
	if m.state == stateChat {
		m.vp.Width = m.chatWidth()
		m.vp.Height = max(1, m.height-4) // header, blank, status, input
		m.refreshViewport()
	}
}

func (m *model) refreshViewport() {
	atBottom := m.vp.AtBottom() || m.vp.TotalLineCount() == 0
	w := max(10, m.vp.Width)
	rendered := make([]string, len(m.lines))
	for i, l := range m.lines {
		rendered[i] = lipgloss.NewStyle().Width(w).Render(m.renderLine(l))
	}
	m.vp.SetContent(strings.Join(rendered, "\n"))
	if atBottom {
		m.vp.GotoBottom()
	}
}

func (m *model) renderLine(l line) string {
	ts := dimStyle.Render(l.at.Format("15:04"))
	switch l.kind {
	case "msg":
		name := colorFor(l.from).Render(l.from)
		if l.from == m.nick {
			name = colorFor(l.from).Underline(true).Render(l.from)
		}
		return fmt.Sprintf("%s %s: %s", ts, name, l.text)
	case "error":
		return fmt.Sprintf("%s %s", ts, errStyle.Render("! "+l.text))
	default:
		return fmt.Sprintf("%s %s", ts, systemStyle.Render("* "+l.text))
	}
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	switch m.state {
	case stateNick:
		return m.viewNick()
	case stateLobby:
		return m.viewLobby()
	default:
		return m.viewChat()
	}
}

func (m model) statusLine() string {
	if m.status == "" {
		return ""
	}
	return errStyle.Render(m.status)
}

func (m model) viewNick() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("tchat") + dimStyle.Render("  ·  "+m.server) + "\n\n")
	b.WriteString("사용할 별명을 입력하고 Enter를 누르세요.\n")
	b.WriteString(dimStyle.Render("(최대 20자, 글자·숫자·_·- 사용 가능)") + "\n\n")
	b.WriteString(m.input.View() + "\n\n")
	b.WriteString(m.statusLine())
	return b.String()
}

func (m model) viewLobby() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("tchat") + dimStyle.Render("  ·  별명: ") + colorFor(m.nick).Render(m.nick) + "\n\n")
	b.WriteString(titleStyle.Render("방 목록") + "\n")

	// Leave room for header, help, input and status lines.
	visible := max(3, m.height-10)
	if len(m.rooms) == 0 {
		b.WriteString(dimStyle.Render("  아직 방이 없습니다. n 을 눌러 새 방을 만들어보세요.") + "\n")
	} else {
		start := 0
		if m.cursor >= visible {
			start = m.cursor - visible + 1
		}
		for i := start; i < len(m.rooms) && i < start+visible; i++ {
			r := m.rooms[i]
			count := dimStyle.Render(fmt.Sprintf("(%d명)", r.Count))
			if i == m.cursor {
				b.WriteString(selStyle.Render("> "+r.Name) + " " + count + "\n")
			} else {
				b.WriteString("  " + r.Name + " " + count + "\n")
			}
		}
	}
	b.WriteString("\n")
	if m.creating {
		b.WriteString("새 방 이름을 입력하고 Enter (취소: Esc)\n" + m.input.View() + "\n")
	} else {
		b.WriteString(dimStyle.Render("↑↓ 선택 · Enter 입장 · n 새 방 · r 새로고침 · q 종료") + "\n")
	}
	b.WriteString(m.statusLine())
	return b.String()
}

func (m model) viewChat() string {
	header := titleStyle.Render("# "+m.room) + dimStyle.Render(fmt.Sprintf("  ·  %d명  ·  나: ", len(m.users))) +
		colorFor(m.nick).Render(m.nick) + dimStyle.Render("  ·  Esc 나가기  /help")

	body := m.vp.View()
	if m.width >= 60 {
		var sb strings.Builder
		sb.WriteString(titleStyle.Render(fmt.Sprintf("접속자 (%d)", len(m.users))) + "\n")
		for i, u := range m.users {
			if i >= m.vp.Height-2 {
				sb.WriteString(dimStyle.Render(fmt.Sprintf("외 %d명", len(m.users)-i)))
				break
			}
			name := colorFor(u).Render(truncate(u, sidebarWidth-3))
			if u == m.nick {
				name += dimStyle.Render(" (나)")
			}
			sb.WriteString(name + "\n")
		}
		side := sidebarStyle.Width(sidebarWidth).Height(m.vp.Height).Render(sb.String())
		body = lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(m.chatWidth()).Render(body), side)
	}
	return header + "\n" + body + "\n" + strings.Repeat("─", max(0, m.width)) + "\n" + m.input.View()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
