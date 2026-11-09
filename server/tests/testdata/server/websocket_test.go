package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"arena-backend/game"
	"arena-backend/messages"

	"github.com/gorilla/websocket"
)

func TestSharedArenaOverWebSocket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := Config{
		Bounds:     game.Rect{Min: game.Vec2{X: -10, Z: -10}, Max: game.Vec2{X: 10, Z: 10}},
		BlueSpawns: []game.Vec2{{X: -5}},
		RedSpawns:  []game.Vec2{{X: 5}},
		MaxPlayers: 2,
	}
	a, err := startArena(ctx, cfg.Bounds, cfg.Walls)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := newWebSocketServer(a, cfg)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	address := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	first, firstID := joinPlayer(t, address, "knight")
	second, secondID := joinPlayer(t, address, "archer")
	if firstID == secondID {
		t.Fatal("players received the same ID")
	}
	send(t, first, map[string]any{"type": "ping", "id": 42})
	for {
		var reply struct {
			Type string
			ID   uint64
		}
		receive(t, first, &reply)
		if reply.Type == "pong" {
			if reply.ID != 42 {
				t.Fatalf("pong echoed ID %d, want 42", reply.ID)
			}
			break
		}
	}
	third := connect(t, address)
	send(t, third, map[string]any{"type": "join", "nick": "Third", "hero": "mage"})
	var rejection messages.Error
	receive(t, third, &rejection)
	if rejection.Code != "arena_full" {
		t.Fatalf("unexpected rejection: %+v", rejection)
	}

	waitForSnapshot(t, second, func(s messages.Snapshot) bool {
		return len(s.Players) == 2
	})
	send(t, first, map[string]any{"type": "input", "seq": 1, "moveX": 1, "moveZ": 0, "aim": 1.25})
	waitForSnapshot(t, first, func(s messages.Snapshot) bool {
		return s.LastInputSeq == 1
	})
	waitForSnapshot(t, second, func(s messages.Snapshot) bool {
		if s.LastInputSeq != 0 {
			t.Fatalf("received another player's input acknowledgment: %+v", s)
		}
		for _, p := range s.Players {
			if p.ID == firstID && p.X > -5 && p.Aim == 1.25 && p.Hero == "knight" {
				return true
			}
		}
		return false
	})
	first.Close()
	waitForSnapshot(t, second, func(s messages.Snapshot) bool {
		return len(s.Players) == 1 && s.Players[0].ID == secondID
	})
}

func connect(t *testing.T, address string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(address, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func joinPlayer(t *testing.T, address, hero string) (*websocket.Conn, string) {
	t.Helper()
	conn := connect(t, address)
	send(t, conn, map[string]any{"type": "join", "nick": hero, "hero": hero})
	var welcome messages.Welcome
	receive(t, conn, &welcome)
	if welcome.Type != "welcome" || welcome.PlayerID == "" || welcome.TickRate != game.TickRate || welcome.WinScore != 300 {
		t.Fatalf("unexpected welcome: %+v", welcome)
	}
	return conn, welcome.PlayerID
}

func send(t *testing.T, conn *websocket.Conn, message any) {
	t.Helper()
	if err := conn.WriteJSON(message); err != nil {
		t.Fatal(err)
	}
}

func receive(t *testing.T, conn *websocket.Conn, message any) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := conn.ReadJSON(message); err != nil {
		t.Fatal(err)
	}
}

func waitForSnapshot(t *testing.T, conn *websocket.Conn, matches func(messages.Snapshot) bool) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var snapshot messages.Snapshot
		if err := conn.ReadJSON(&snapshot); err != nil {
			t.Fatal(err)
		}
		if snapshot.Type != "snapshot" {
			t.Fatalf("unexpected message: %+v", snapshot)
		}
		if matches(snapshot) {
			return
		}
	}
}

func TestCombatOverWebSocket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := Config{
		Bounds:     game.Rect{Min: game.Vec2{X: -10, Z: -10}, Max: game.Vec2{X: 10, Z: 10}},
		BlueSpawns: []game.Vec2{{}},
		RedSpawns:  []game.Vec2{{X: 1}},
		MaxPlayers: 2,
	}
	a, err := startArena(ctx, cfg.Bounds, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := newWebSocketServer(a, cfg)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	address := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	knight, knightID := joinPlayer(t, address, "knight")
	archer, archerID := joinPlayer(t, address, "archer")
	waitForSnapshot(t, knight, func(s messages.Snapshot) bool { return len(s.Players) == 2 })

	send(t, knight, map[string]any{"type": "action", "id": 1, "button": "primary", "aim": 0})
	waitForSnapshot(t, archer, func(s messages.Snapshot) bool {
		var hit, attack bool
		for _, p := range s.Players {
			if p.ID == archerID {
				hit = p.HP == 45
			}
			if p.ID == knightID {
				attack = p.Attack != nil && p.Attack.ID == 1
			}
		}
		return hit && attack
	})
	send(t, archer, map[string]any{"type": "action", "id": 1, "button": "secondary", "aim": 3.141592653589793})
	waitForSnapshot(t, knight, func(s messages.Snapshot) bool {
		for _, p := range s.Players {
			if p.ID == knightID && p.HP == 90 {
				return len(s.Projectiles) == 0
			}
		}
		return false
	})

}

func TestRogueCoordinatesAreOnlySentToOwner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := Config{
		Bounds:     game.Rect{Min: game.Vec2{X: -10, Z: -10}, Max: game.Vec2{X: 10, Z: 10}},
		BlueSpawns: []game.Vec2{{X: -5}}, RedSpawns: []game.Vec2{{X: 5}},
	}
	a, err := startArena(ctx, cfg.Bounds, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := newWebSocketServer(a, cfg)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	address := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	rogue, rogueID := joinPlayer(t, address, "rogue")
	viewer, _ := joinPlayer(t, address, "berserker")
	waitForSnapshot(t, viewer, func(s messages.Snapshot) bool { return len(s.Players) == 2 })
	send(t, rogue, map[string]any{"type": "action", "id": 1, "button": "secondary", "aim": 0})
	waitForSnapshot(t, rogue, func(s messages.Snapshot) bool {
		for _, p := range s.Players {
			if p.ID == rogueID && p.InvisibleUntil > s.ServerTick {
				return true
			}
		}
		return false
	})
	waitForSnapshot(t, viewer, func(s messages.Snapshot) bool {
		return len(s.Players) == 1 && s.Players[0].ID != rogueID && s.TeamCounts["blue"] == 1
	})
	send(t, rogue, map[string]any{"type": "action", "id": 2, "button": "primary", "aim": 0})
	waitForSnapshot(t, viewer, func(s messages.Snapshot) bool { return len(s.Players) == 2 })
}
