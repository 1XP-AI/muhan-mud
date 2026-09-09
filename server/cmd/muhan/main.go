package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"flag"
	"log"
	mathrand "math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/transport"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	migrate := flag.Bool("migrate", false, "explicitly provision the Go draft schema and exit")
	seedWorld := flag.String("seed-world", "", "explicitly create a new world snapshot from -seed-rooms and exit")
	seedRooms := flag.String("seed-rooms", "", "directory containing the legacy rooms/rNN/rNNNNN resource tree for -seed-world")
	seedCanonical := flag.Bool("seed-canonical", false, "convert admitted NPC and item graphs while provisioning -seed-world")
	seedIfAbsent := flag.Bool("seed-if-absent", false, "skip canonical/legacy seed when the target world already exists")
	worldID := flag.String("world", "", "explicitly take over an existing Go world (no automatic import)")
	templates := flag.String("templates", "", "directory containing legacy mNN/oNN template tables")
	gameHour := flag.Int("game-hour", -1, "explicit game hour 0..23 until the persistent game clock is implemented")
	helpDir := flag.String("help-dir", os.Getenv("MUD_HELP_DIR"), "directory containing UTF-8 help, spell and policy documents")
	playerTickInterval := flag.Duration("player-tick", 20*time.Second, "player vital scheduler cadence; whole seconds")
	roomResourceTickInterval := flag.Duration("room-resource-tick", 20*time.Second, "canonical floor/door resource scheduler cadence; whole seconds")
	flag.Parse()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatal("invalid database configuration")
	}
	defer db.Close()
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = db.PingContext(checkCtx)
	cancel()
	if err != nil {
		log.Fatal("database unavailable")
	}
	repo := storage.NewPostgres(db)
	if *migrate {
		migrationCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if repo.Migrate(migrationCtx) != nil {
			log.Fatal("migration failed")
		}
		return
	}
	if *seedWorld != "" || *seedRooms != "" || *seedCanonical {
		if *seedWorld == "" || *seedRooms == "" || *worldID != "" {
			log.Fatal("world seeding requires -seed-world and -seed-rooms, without -world")
		}
		info, err := os.Stat(*seedRooms)
		if err != nil || !info.IsDir() {
			log.Fatal("seed room directory unavailable")
		}
		catalog, err := world.LoadLegacyRoomCatalog(os.DirFS(*seedRooms), world.LegacyRoomCompatibilityPolicy)
		if err != nil {
			log.Fatal("legacy room catalog admission failed")
		}
		seedCtx, seedCancel := context.WithTimeout(ctx, 30*time.Second)
		if *seedIfAbsent {
			if _, loadErr := repo.LoadWorld(seedCtx, *seedWorld); loadErr == nil {
				seedCancel()
				log.Printf("world %s already exists; skipped seed", *seedWorld)
				return
			} else if !errors.Is(loadErr, sql.ErrNoRows) {
				seedCancel()
				log.Fatal("world seed existence check failed")
			}
		}
		if *seedCanonical {
			err = engine.SeedCanonicalWorldFromCatalog(seedCtx, repo, *seedWorld, catalog)
		} else {
			err = engine.SeedWorldFromCatalog(seedCtx, repo, *seedWorld, catalog)
		}
		seedCancel()
		if err != nil {
			log.Fatal("world seed failed")
		}
		mode := "legacy resources"
		if *seedCanonical {
			mode = "canonical NPC/item graphs"
		}
		log.Printf("seeded world %s from %s (%s; %d canonical rooms; %d ignored artifacts)", *seedWorld, *seedRooms, mode, catalog.Len(), len(catalog.Ignored()))
		return
	}
	origin := os.Getenv("ALLOWED_ORIGINS")
	if origin == "" {
		log.Fatal("ALLOWED_ORIGINS is required")
	}
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8081"
	}
	mux := http.NewServeMux()
	var connector *transport.WorldConnector
	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()
	workerDone := make(chan struct{})
	if *worldID != "" {
		if *gameHour < 0 || *gameHour > 23 || *templates == "" {
			log.Fatal("world mode requires templates and explicit game-hour 0..23")
		}
		if *helpDir == "" {
			*helpDir = "/home/muhan/help"
		}
		if *playerTickInterval <= 0 || *playerTickInterval%time.Second != 0 {
			log.Fatal("player-tick must be a positive whole number of seconds")
		}
		if *roomResourceTickInterval <= 0 || *roomResourceTickInterval%time.Second != 0 {
			log.Fatal("room-resource-tick must be a positive whole number of seconds")
		}
		info, err := os.Stat(*templates)
		if err != nil || !info.IsDir() {
			log.Fatal("template directory unavailable")
		}
		helpInfo, err := os.Stat(*helpDir)
		if err != nil || !helpInfo.IsDir() {
			log.Fatal("help document directory unavailable")
		}
		startupCtx, startupCancel := context.WithTimeout(ctx, 30*time.Second)
		writer, _, err := engine.StartWorld(startupCtx, repo, *worldID, "boot-"+rand.Text())
		startupCancel()
		if err != nil {
			log.Fatal("world takeover/recovery failed; no listener started")
		}
		connector, err = transport.NewWorldConnector(transport.WorldConnectorConfig{
			Store: writer, WorldID: *worldID, MaxSessions: 32,
			Clock:    func() (int32, int) { return int32(time.Now().Unix()), *gameHour },
			Catalog:  world.TemplateCatalog{FS: os.DirFS(*templates)},
			HelpFS:   os.DirFS(*helpDir),
			Roll:     func(low, high int) int { return low + mathrand.IntN(high-low+1) },
			Allocate: func() (string, error) { return "item-" + rand.Text(), nil },
		})
		if err != nil {
			log.Fatal("world connector configuration failed")
		}
		go func() {
			defer close(workerDone)
			var workers sync.WaitGroup
			workers.Add(3)
			go func() {
				defer workers.Done()
				if err := connector.RunCleanup(workerCtx); err != nil && workerCtx.Err() == nil {
					log.Printf("cleanup worker stopped: %v", err)
				}
			}()
			go func() {
				defer workers.Done()
				if err := connector.RunPlayerVitalScheduler(workerCtx, *playerTickInterval); err != nil {
					log.Printf("player vital scheduler stopped: %v", err)
				}
			}()
			go func() {
				defer workers.Done()
				if err := connector.RunRoomResourceScheduler(workerCtx, *roomResourceTickInterval); err != nil {
					log.Printf("room resource scheduler stopped: %v", err)
				}
			}()
			workers.Wait()
		}()
		mux.Handle("/ws", transport.NewGameHandler(ctx, session.NewWorldAccounts(repo, writer, *worldID), strings.Split(origin, ","), connector))
	} else {
		close(workerDone)
		mux.Handle("/ws", transport.NewHandler(ctx, repo, strings.Split(origin, ",")))
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		if ctx.Err() != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok\n"))
	})
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
		// Stop all new durable background writes before fencing and draining
		// connected sessions. Shutdown itself performs the final cleanup retry.
		workerCancel()
		<-workerDone
		if connector != nil {
			if err := connector.Shutdown(shutdownCtx); err != nil {
				log.Printf("world shutdown incomplete: %d pending sessions; cold-start recovery required", len(connector.PendingCleanup()))
			}
		}
	}()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Print("HTTP listener failed")
	} else {
		log.Printf("Go terminal server listening on %s (world mode=%t; game port incomplete)", listener.Addr(), connector != nil)
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Print("HTTP server failed")
		}
	}
	stop()
	<-shutdownDone
}
