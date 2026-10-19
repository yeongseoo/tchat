// Package server is the chat relay. It keeps everything in memory only:
// messages are forwarded to the people in the room and then forgotten,
// and a room disappears as soon as its last member leaves.
package server

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gorilla/websocket"

	"tchat/internal/protocol"
)

const (
	maxNickLen    = 20
	maxRoomLen    = 30
	maxTextLen    = 500
	maxRooms      = 200
	maxClients    = 2000
	readLimit     = 4096
	sendBuffer    = 64
	pingInterval  = 30 * time.Second
	readTimeout   = 70 * time.Second
	writeTimeout  = 10 * time.Second
	burstMessages = 5   // messages a client may send at once
	refillPerSec  = 1.0 // sustained messages per second
)

type Server struct {
	mu      sync.Mutex
	rooms   map[string]*room
	clients map[*client]struct{}
	nicks   map[string]*client // lower-cased nick -> client
}

type room struct {
	name    string
	members map[*client]struct{}
}

type client struct {
	conn     *websocket.Conn
	send     chan protocol.Message
	done     chan struct{}
	stop     sync.Once
	nick     string
	room     *room
	tokens   float64
	lastFill time.Time
}

func New() *Server {
	return &Server{
		rooms:   map[string]*room{},
		clients: map[*client]struct{}{},
		nicks:   map[string]*client{},
	}
}

var upgrader = websocket.Upgrader{
	// Clients are terminal programs, not browsers, so there is no Origin to check.
	CheckOrigin: func(*http.Request) bool { return true },
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.serveWS)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		rooms, clients := len(s.rooms), len(s.clients)
		s.mu.Unlock()
		fmt.Fprintf(w, "tchat server is running. rooms=%d online=%d\n", rooms, clients)
	})
	return mux
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	full := len(s.clients) >= maxClients
	s.mu.Unlock()
	if full {
		http.Error(w, "server is full", http.StatusServiceUnavailable)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &client{
		conn:     conn,
		send:     make(chan protocol.Message, sendBuffer),
		done:     make(chan struct{}),
		tokens:   burstMessages,
		lastFill: time.Now(),
	}
	s.mu.Lock()
	s.clients[c] = struct{}{}
	s.mu.Unlock()

	go c.writePump()
	s.readPump(c)
}

func (c *client) close() {
	c.stop.Do(func() { close(c.done) })
}

// push queues a message without blocking. A client that cannot keep up is
// disconnected rather than allowed to stall everyone else.
func (c *client) push(m protocol.Message) {
	select {
	case c.send <- m:
	case <-c.done:
	default:
		c.close()
	}
}

func (c *client) writePump() {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case m := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := c.conn.WriteJSON(m); err != nil {
				c.close()
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.close()
				return
			}
		case <-c.done:
			c.conn.SetWriteDeadline(time.Now().Add(time.Second))
			c.conn.WriteMessage(websocket.CloseMessage, nil)
			return
		}
	}
}

func (s *Server) readPump(c *client) {
	defer func() {
		c.close()
		s.disconnect(c)
	}()
	c.conn.SetReadLimit(readLimit)
	c.conn.SetReadDeadline(time.Now().Add(readTimeout))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(readTimeout))
		return nil
	})
	for {
		var m protocol.Message
		if err := c.conn.ReadJSON(&m); err != nil {
			return
		}
		c.conn.SetReadDeadline(time.Now().Add(readTimeout))
		s.handle(c, m)
	}
}

func (s *Server) handle(c *client, m protocol.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if c.nick == "" && m.Type != protocol.Hello {
		c.push(errorMsg("먼저 별명을 정해주세요."))
		return
	}

	switch m.Type {
	case protocol.Hello:
		if c.nick != "" {
			return
		}
		nick, err := s.checkNick(m.Nick)
		if err != nil {
			c.push(errorMsg(err.Error()))
			return
		}
		c.nick = nick
		s.nicks[strings.ToLower(nick)] = c
		c.push(protocol.Message{Type: protocol.Welcome, Nick: nick})
		c.push(s.roomList())

	case protocol.List:
		c.push(s.roomList())

	case protocol.Create:
		name, err := cleanName(m.Room, maxRoomLen, true)
		if err != nil {
			c.push(errorMsg("방 이름: " + err.Error()))
			return
		}
		if _, ok := s.rooms[strings.ToLower(name)]; ok {
			c.push(errorMsg("이미 있는 방 이름입니다."))
			return
		}
		if len(s.rooms) >= maxRooms {
			c.push(errorMsg("방이 너무 많습니다. 잠시 후 다시 시도해주세요."))
			return
		}
		r := &room{name: name, members: map[*client]struct{}{}}
		s.rooms[strings.ToLower(name)] = r
		s.enter(c, r)

	case protocol.Join:
		r, ok := s.rooms[strings.ToLower(strings.TrimSpace(m.Room))]
		if !ok {
			c.push(errorMsg("방이 없어졌습니다. 목록을 새로고침해주세요."))
			c.push(s.roomList())
			return
		}
		if c.room == r {
			return
		}
		s.enter(c, r)

	case protocol.Leave:
		if c.room != nil {
			s.exit(c)
			c.push(protocol.Message{Type: protocol.Left})
			c.push(s.roomList())
		}

	case protocol.Say:
		if c.room == nil {
			c.push(errorMsg("방에 들어가야 말할 수 있습니다."))
			return
		}
		text := sanitize(m.Text)
		if text == "" {
			return
		}
		if utf8.RuneCountInString(text) > maxTextLen {
			c.push(errorMsg(fmt.Sprintf("메시지는 %d자까지 보낼 수 있습니다.", maxTextLen)))
			return
		}
		if !c.allow() {
			c.push(errorMsg("너무 빨리 보내고 있습니다. 잠시만 기다려주세요."))
			return
		}
		s.broadcast(c.room, protocol.Message{
			Type: protocol.Msg, From: c.nick, Text: text, Time: time.Now().Unix(),
		})

	case protocol.Nick:
		nick, err := s.checkNick(m.Nick)
		if err != nil {
			c.push(errorMsg(err.Error()))
			return
		}
		old := c.nick
		delete(s.nicks, strings.ToLower(old))
		c.nick = nick
		s.nicks[strings.ToLower(nick)] = c
		c.push(protocol.Message{Type: protocol.NickOK, Nick: nick})
		if c.room != nil {
			s.broadcast(c.room, systemMsg(fmt.Sprintf("%s님이 별명을 %s(으)로 바꿨습니다.", old, nick)))
			s.broadcast(c.room, protocol.Message{Type: protocol.Users, Users: c.room.userList()})
		}
	}
}

// enter moves c into r, leaving its current room first. Caller holds s.mu.
func (s *Server) enter(c *client, r *room) {
	if c.room != nil {
		s.exit(c)
	}
	r.members[c] = struct{}{}
	c.room = r
	users := r.userList()
	c.push(protocol.Message{Type: protocol.Joined, Room: r.name, Users: users})
	for m := range r.members {
		if m != c {
			m.push(systemMsg(c.nick + "님이 들어왔습니다."))
			m.push(protocol.Message{Type: protocol.Users, Users: users})
		}
	}
	s.pushLobby()
}

// exit removes c from its room, deleting the room if it is now empty.
// Caller holds s.mu.
func (s *Server) exit(c *client) {
	r := c.room
	delete(r.members, c)
	c.room = nil
	if len(r.members) == 0 {
		delete(s.rooms, strings.ToLower(r.name))
	} else {
		s.broadcast(r, systemMsg(c.nick+"님이 나갔습니다."))
		s.broadcast(r, protocol.Message{Type: protocol.Users, Users: r.userList()})
	}
	s.pushLobby()
}

func (s *Server) disconnect(c *client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.room != nil {
		s.exit(c)
	}
	if c.nick != "" && s.nicks[strings.ToLower(c.nick)] == c {
		delete(s.nicks, strings.ToLower(c.nick))
	}
	delete(s.clients, c)
}

func (s *Server) broadcast(r *room, m protocol.Message) {
	for c := range r.members {
		c.push(m)
	}
}

// pushLobby sends the fresh room list to everyone who is not in a room.
func (s *Server) pushLobby() {
	list := s.roomList()
	for _, c := range s.nicks {
		if c.room == nil {
			c.push(list)
		}
	}
}

func (s *Server) roomList() protocol.Message {
	rooms := make([]protocol.RoomInfo, 0, len(s.rooms))
	for _, r := range s.rooms {
		rooms = append(rooms, protocol.RoomInfo{Name: r.name, Count: len(r.members)})
	}
	sort.Slice(rooms, func(i, j int) bool {
		if rooms[i].Count != rooms[j].Count {
			return rooms[i].Count > rooms[j].Count
		}
		return rooms[i].Name < rooms[j].Name
	})
	return protocol.Message{Type: protocol.Rooms, Rooms: rooms}
}

func (r *room) userList() []string {
	users := make([]string, 0, len(r.members))
	for c := range r.members {
		users = append(users, c.nick)
	}
	sort.Strings(users)
	return users
}

func (s *Server) checkNick(raw string) (string, error) {
	nick, err := cleanName(raw, maxNickLen, false)
	if err != nil {
		return "", fmt.Errorf("별명: %v", err)
	}
	if _, taken := s.nicks[strings.ToLower(nick)]; taken {
		return "", fmt.Errorf("'%s'은(는) 이미 누가 쓰고 있는 별명입니다.", nick)
	}
	return nick, nil
}

// allow is a small token bucket so one person cannot flood a room.
func (c *client) allow() bool {
	now := time.Now()
	c.tokens += now.Sub(c.lastFill).Seconds() * refillPerSec
	if c.tokens > burstMessages {
		c.tokens = burstMessages
	}
	c.lastFill = now
	if c.tokens < 1 {
		return false
	}
	c.tokens--
	return true
}

func cleanName(raw string, max int, allowSpace bool) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("비어 있습니다")
	}
	if utf8.RuneCountInString(name) > max {
		return "", fmt.Errorf("%d자까지 가능합니다", max)
	}
	for _, r := range name {
		ok := unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || (allowSpace && r == ' ')
		if !ok {
			return "", fmt.Errorf("글자, 숫자, _, - 만 쓸 수 있습니다")
		}
	}
	return name, nil
}

// sanitize drops control characters so nobody can send terminal escape
// sequences that would mess with (or take over) other people's screens.
func sanitize(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) || r == '​' || (r >= '‪' && r <= '‮') || (r >= '⁦' && r <= '⁩') {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func errorMsg(text string) protocol.Message {
	return protocol.Message{Type: protocol.Error, Text: text}
}

func systemMsg(text string) protocol.Message {
	return protocol.Message{Type: protocol.System, Text: text, Time: time.Now().Unix()}
}

func Run(addr string) error {
	log.Printf("tchat server listening on %s (WebSocket path /ws)", addr)
	srv := &http.Server{Addr: addr, Handler: New().Handler(), ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}
