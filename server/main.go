package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"arena-backend/game"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	origin := flag.String("origin", "http://localhost:5173", "allowed browser origin")
	maxPlayers := flag.Int("max-players", 0, "maximum joined players (0 disables the limit)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	zones := []game.ControlZone{
		{Position: game.Vec2{Z: -16}, Radius: 4.5},
		{Position: game.Vec2{}, Radius: 5},
		{Position: game.Vec2{Z: 16}, Radius: 4.5},
	}
	cfg := Config{
		Bounds:        game.Rect{Min: game.Vec2{X: -40, Z: -26}, Max: game.Vec2{X: 40, Z: 26}},
		Zones:         zones,
		Walls:         courtyardCover(zones),
		BlueSpawns:    []game.Vec2{{X: -33, Z: -12}, {X: -35}, {X: -33, Z: 12}},
		RedSpawns:     []game.Vec2{{X: 33, Z: -12}, {X: 35}, {X: 33, Z: 12}},
		MaxPlayers:    *maxPlayers,
		BotTarget:     10,
		AllowedOrigin: *origin,
	}
	a, err := startArena(ctx, cfg.Bounds, cfg.Walls, cfg.Zones...)
	if err != nil {
		log.Fatal(err)
	}
	socket, err := newWebSocketServer(a, cfg)
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /ws", socket)
	clientFiles := http.FileServer(http.Dir("../client"))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		clientFiles.ServeHTTP(w, r)
	})
	server := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	finished := make(chan error, 1)
	go func() { finished <- server.ListenAndServe() }()
	log.Printf("open http://%s in your browser", *addr)
	select {
	case err := <-finished:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			log.Print(err)
		}
		<-a.Done()
	}
}
