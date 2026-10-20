package server

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"tchat/internal/protocol"
)

type tc struct {
	t  *testing.T
	ws *websocket.Conn
}

func dial(t *testing.T, srv *httptest.Server) *tc {
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	return &tc{t, ws}
}

func (c *tc) send(m protocol.Message) {
	if err := c.ws.WriteJSON(m); err != nil {
		c.t.Fatal(err)
	}
}

// expect reads until a message of the given type arrives.
func (c *tc) expect(typ string) protocol.Message {
	c.t.Helper()
	c.ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var m protocol.Message
		if err := c.ws.ReadJSON(&m); err != nil {
			c.t.Fatalf("waiting for %s: %v", typ, err)
		}
		if m.Type == typ {
			return m
		}
	}
}

func login(t *testing.T, srv *httptest.Server, nick string) *tc {
	c := dial(t, srv)
	c.send(protocol.Message{Type: protocol.Hello, Nick: nick})
	if got := c.expect(protocol.Welcome).Nick; got != nick {
		t.Fatalf("nick = %q", got)
	}
	c.expect(protocol.Rooms)
	return c
}

func TestChatFlow(t *testing.T) {
	srv := httptest.NewServer(New().Handler(nil))
	defer srv.Close()

	a := login(t, srv, "철수")
	b := login(t, srv, "영희")

	// Duplicate nick is rejected (case-insensitive).
	d := dial(t, srv)
	d.send(protocol.Message{Type: protocol.Hello, Nick: "철수"})
	d.expect(protocol.Error)

	a.send(protocol.Message{Type: protocol.Create, Room: "잡담방"})
	if j := a.expect(protocol.Joined); j.Room != "잡담방" || len(j.Users) != 1 {
		t.Fatalf("joined = %+v", j)
	}
	if r := b.expect(protocol.Rooms); len(r.Rooms) != 1 || r.Rooms[0].Count != 1 {
		t.Fatalf("lobby rooms = %+v", r.Rooms)
	}

	b.send(protocol.Message{Type: protocol.Join, Room: "잡담방"})
	if j := b.expect(protocol.Joined); len(j.Users) != 2 {
		t.Fatalf("users = %v", j.Users)
	}
	if s := a.expect(protocol.System); !strings.Contains(s.Text, "영희") {
		t.Fatalf("system = %q", s.Text)
	}

	// Escape sequences are stripped.
	b.send(protocol.Message{Type: protocol.Say, Text: "안녕\x1b[2J하세요"})
	if m := a.expect(protocol.Msg); m.From != "영희" || m.Text != "안녕[2J하세요" {
		t.Fatalf("msg = %+v", m)
	}

	b.send(protocol.Message{Type: protocol.Nick, Nick: "영희2"})
	b.expect(protocol.NickOK)
	if u := a.expect(protocol.Users); u.Users[1] != "철수" && u.Users[0] != "영희2" {
		t.Fatalf("users = %v", u.Users)
	}

	// Disconnect: remaining member is told, then last leave deletes the room.
	b.ws.Close()
	if s := a.expect(protocol.System); !strings.Contains(s.Text, "나갔습니다") {
		t.Fatalf("system = %q", s.Text)
	}
	a.send(protocol.Message{Type: protocol.Leave})
	a.expect(protocol.Left)
	if r := a.expect(protocol.Rooms); len(r.Rooms) != 0 {
		t.Fatalf("room should be gone, got %+v", r.Rooms)
	}
}

func TestRateLimit(t *testing.T) {
	srv := httptest.NewServer(New().Handler(nil))
	defer srv.Close()
	a := login(t, srv, "spam")
	a.send(protocol.Message{Type: protocol.Create, Room: "r"})
	a.expect(protocol.Joined)
	for i := 0; i < burstMessages+1; i++ {
		a.send(protocol.Message{Type: protocol.Say, Text: "x"})
	}
	a.expect(protocol.Error)
}
