// Package protocol defines the JSON messages exchanged over the WebSocket.
package protocol

// Client -> server message types.
const (
	Hello  = "hello"  // Nick: requested nickname
	List   = "list"   // ask for the room list
	Create = "create" // Room: create and join a new room
	Join   = "join"   // Room: join an existing room
	Leave  = "leave"  // leave the current room
	Say    = "say"    // Text: chat message
	Nick   = "nick"   // Nick: change nickname
)

// Server -> client message types.
const (
	Welcome = "welcome" // Nick: accepted nickname
	Rooms   = "rooms"   // Rooms: current room list
	Joined  = "joined"  // Room, Users: you joined a room
	Left    = "left"    // you left your room
	Users   = "users"   // Users: member list of your room changed
	Msg     = "msg"     // From, Text, Time: chat message
	System  = "system"  // Text, Time: join/leave/nick notices
	NickOK  = "nick_ok" // Nick: your new nickname
	Error   = "error"   // Text: something went wrong
)

type Message struct {
	Type  string     `json:"type"`
	Nick  string     `json:"nick,omitempty"`
	Room  string     `json:"room,omitempty"`
	Text  string     `json:"text,omitempty"`
	From  string     `json:"from,omitempty"`
	Time  int64      `json:"time,omitempty"`
	Rooms []RoomInfo `json:"rooms,omitempty"`
	Users []string   `json:"users,omitempty"`
}

type RoomInfo struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}
