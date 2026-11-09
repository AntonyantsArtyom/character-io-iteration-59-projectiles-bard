package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"arena-backend/game"
	"arena-backend/messages"

	"github.com/gorilla/websocket"
)

const (
	joinTimeout  = 5 * time.Second
	writeTimeout = 3 * time.Second
	pongTimeout  = 30 * time.Second
	pingInterval = 10 * time.Second
)

type Config struct {
	Bounds        game.Rect
	Walls         []game.Rect
	Zones         []game.ControlZone
	BlueSpawns    []game.Vec2
	RedSpawns     []game.Vec2
	MaxPlayers    int
	BotTarget     int
	AllowedOrigin string
}

type client struct {
	nick      string
	snapshots chan messages.Snapshot
	pongs     chan uint64
}

type Server struct {
	arena      *Arena
	upgrader   websocket.Upgrader
	welcome    messages.Welcome
	blueSpawns []game.Vec2
	redSpawns  []game.Vec2
	slots      chan struct{}
	nextID     atomic.Uint64

	mu      sync.Mutex
	clients map[string]*client
}

func newWebSocketServer(a *Arena, cfg Config) (*Server, error) {
	if cfg.MaxPlayers < 0 || len(cfg.BlueSpawns) == 0 || len(cfg.RedSpawns) == 0 {
		return nil, errors.New("nonnegative player limit and spawn points for both teams required")
	}
	world, err := game.NewWorld(cfg.Bounds, cfg.Walls, cfg.Zones...)
	if err != nil {
		return nil, err
	}
	for i, spawn := range append(append([]game.Vec2(nil), cfg.BlueSpawns...), cfg.RedSpawns...) {
		if err := world.Spawn(fmt.Sprint(i), spawn); err != nil {
			return nil, err
		}
	}
	walls := make([]messages.Rect, 0, len(cfg.Walls))
	for _, wall := range cfg.Walls {
		walls = append(walls, wireRect(wall))
	}
	zones := make([]messages.ControlZone, 0, len(cfg.Zones))
	for _, zone := range cfg.Zones {
		zones = append(zones, messages.ControlZone{X: zone.Position.X, Z: zone.Position.Z, Radius: zone.Radius})
	}
	projectilePrediction := make(map[string]messages.ProjectilePrediction)
	for _, hero := range [...]game.Hero{game.Archer, game.Mage, game.Bard} {
		speed, reach, windup := game.ProjectileSpec(hero)
		projectilePrediction[string(hero)] = messages.ProjectilePrediction{Speed: speed, Reach: reach, Windup: windup}
	}
	s := &Server{
		arena: a,
		upgrader: websocket.Upgrader{
			HandshakeTimeout: joinTimeout,
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				if origin == "" || origin == cfg.AllowedOrigin {
					return true
				}
				parsed, err := url.Parse(origin)
				return err == nil && strings.EqualFold(parsed.Host, r.Host)
			},
		},
		welcome: messages.Welcome{
			ProjectilePrediction: projectilePrediction, InspirationMoveMultiplier: game.InspirationMoveMultiplier,
			Type: "welcome", TickRate: game.TickRate, Zones: zones, WinScore: game.WinScore,
			WindupEnemyRadius: game.WindupEnemyRadius, PlayerRadius: game.PlayerRadius, Bounds: wireRect(cfg.Bounds), Walls: walls,
		},
		blueSpawns: append([]game.Vec2(nil), cfg.BlueSpawns...),
		redSpawns:  append([]game.Vec2(nil), cfg.RedSpawns...),
		clients:    make(map[string]*client),
	}
	if cfg.MaxPlayers > 0 {
		s.slots = make(chan struct{}, cfg.MaxPlayers)
	}
	if cfg.BotTarget > 0 {
		if err := a.EnableBots(cfg.BotTarget, s.blueSpawns, s.redSpawns); err != nil {
			return nil, err
		}
	}
	go s.broadcast()
	return s, nil
}

func wireRect(r game.Rect) messages.Rect {
	return messages.Rect{Min: messages.Point(r.Min), Max: messages.Point(r.Max)}
}
