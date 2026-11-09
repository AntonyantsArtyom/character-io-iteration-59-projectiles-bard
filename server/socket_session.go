package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"arena-backend/game"
	"arena-backend/messages"

	"github.com/gorilla/websocket"
)

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(1024)
	_ = conn.SetReadDeadline(time.Now().Add(joinTimeout))

	data, kind, err := readMessage(conn)
	if err != nil || kind != "join" {
		reject(conn, "expected_join")
		return
	}
	var join messages.Join
	if json.Unmarshal(data, &join) != nil {
		reject(conn, "invalid_join")
		return
	}
	join.Nick = strings.TrimSpace(join.Nick)
	if join.Nick == "" || utf8.RuneCountInString(join.Nick) > 24 {
		reject(conn, "invalid_nick")
		return
	}
	hero := game.Hero(join.Hero)
	if !hero.Valid() {
		reject(conn, "invalid_hero")
		return
	}
	if s.slots != nil {
		select {
		case s.slots <- struct{}{}:
			defer func() { <-s.slots }()
		default:
			reject(conn, "arena_full")
			return
		}
	}

	number := s.nextID.Add(1)
	id := fmt.Sprint(number)
	team, err := s.arena.Join(id, hero, s.blueSpawns, s.redSpawns)
	if err != nil {
		reject(conn, "arena_unavailable")
		return
	}
	defer s.remove(id)

	welcome := s.welcome
	welcome.PlayerID = id
	welcome.Team = string(team)
	welcome.MoveSpeed = hero.MoveSpeed()
	if writeJSON(conn, welcome) != nil {
		return
	}
	c := &client{nick: join.Nick, snapshots: make(chan messages.Snapshot, 1), pongs: make(chan uint64, 1)}
	s.mu.Lock()
	s.clients[id] = c
	s.mu.Unlock()

	_ = conn.SetReadDeadline(time.Now().Add(pongTimeout))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongTimeout))
	})
	done := make(chan struct{})
	go s.writeSnapshots(conn, c, done)
	defer close(done)
	for {
		data, kind, err := readMessage(conn)
		if err != nil {
			return
		}
		if kind == "ping" {
			var ping struct {
				ID uint64 `json:"id"`
			}
			if json.Unmarshal(data, &ping) == nil && ping.ID != 0 {
				select {
				case c.pongs <- ping.ID:
				default:
				}
			}
			continue
		}
		if kind == "action" {
			var action messages.Action
			if json.Unmarshal(data, &action) != nil || action.ID == 0 || action.Aim == nil {
				continue
			}
			err = s.arena.submit(func(sim *simulation) error {
				applyAction(sim.world, id, action)
				return nil
			})
			if errors.Is(err, ErrClosed) {
				return
			}
			continue
		}
		var input messages.Input
		if (kind != "input" && kind != "move_step") || json.Unmarshal(data, &input) != nil {
			return
		}
		movement := game.Input{
			Move: game.Vec2{X: input.MoveX, Z: input.MoveZ}, Aim: input.Aim,
		}
		if kind == "move_step" {
			err = s.arena.queueStep(id, input.Seq, movement, input.Actions)
		} else {
			err = s.arena.SetInput(id, input.Seq, movement)
		}
		if err != nil && !errors.Is(err, ErrStaleInput) && !errors.Is(err, ErrBusy) {
			return
		}
	}
}

func (s *Server) remove(id string) {
	s.mu.Lock()
	delete(s.clients, id)
	s.mu.Unlock()

	for errors.Is(s.arena.Remove(id), ErrBusy) {
		select {
		case <-s.arena.Done():
			return
		case <-time.After(time.Second / game.TickRate):
		}
	}
}

func readMessage(conn *websocket.Conn) ([]byte, string, error) {
	messageType, data, err := conn.ReadMessage()
	if err != nil {
		return nil, "", err
	}
	var header messages.Header
	if messageType != websocket.TextMessage || json.Unmarshal(data, &header) != nil {
		return nil, "", errors.New("expected a JSON text message")
	}
	return data, header.Type, nil
}

func writeJSON(conn *websocket.Conn, value any) error {
	if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return err
	}
	return conn.WriteJSON(value)
}

func reject(conn *websocket.Conn, code string) {
	_ = writeJSON(conn, messages.Error{Type: "error", Code: code})
}
