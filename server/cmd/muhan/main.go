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
	backupWorld := flag.String("backup-world", "", "explicitly export one world snapshot and exit")
	backupFile := flag.String("backup-file", "", "private destination file for -backup-world")
	backupOverwrite := flag.Bool("backup-overwrite", false, "allow replacing an existing backup destination")
	restoreWorld := flag.String("restore-world", "", "explicitly restore one world snapshot and exit")
	restoreFile := flag.String("restore-file", "", "private source file for -restore-world")
	restoreExpectedRevision := flag.Int64("restore-expected-revision", -1, "expected target revision for a non-force restore; -1 means unset")
	restoreAllowCreate := flag.Bool("restore-allow-create", false, "allow restore to create an absent world")
	restoreForce := flag.Bool("restore-force", false, "explicitly remove receipts and fence the target writer generation")
	playerSnapshotManifest := flag.String("import-player-snapshot-manifest", "", "explicitly import reviewed PlayerSnapshotV1 records from a private manifest and exit")
	playerSnapshotDryRun := flag.Bool("import-player-snapshot-manifest-dry-run", false, "validate a PlayerSnapshotV1 manifest and its private files without connecting to PostgreSQL")
	playerSnapshotInspectDir := flag.String("inspect-player-snapshot-dir", "", "explicitly inspect a private PlayerSnapshotV1 directory and record metadata")
	playerSnapshotInspectWorld := flag.String("inspect-player-snapshot-world", "", "world ID to bind to -inspect-player-snapshot-dir")
	playerSnapshotInspectFormat := flag.String("inspect-player-snapshot-format", "cdto-v1", "snapshot format for inspection: cdto-v1 or legacy-player-raw-v1")
	playerSnapshotInspectDryRun := flag.Bool("inspect-player-snapshot-dry-run", false, "inspect PlayerSnapshotV1 files without connecting to PostgreSQL")
	playerSnapshotRawDir := flag.String("convert-player-snapshot-raw-dir", "", "explicitly convert audited legacy raw player files to reviewed CDTO files")
	playerSnapshotRawWorld := flag.String("convert-player-snapshot-raw-world", "", "world ID to bind to -convert-player-snapshot-raw-dir")
	playerSnapshotRawOutput := flag.String("convert-player-snapshot-cdto-dir", "", "private destination directory for converted CDTO files and review metadata")
	playerSnapshotRawABI := flag.String("convert-player-snapshot-raw-abi", "", "exact LegacyPlayerSnapshotRawV1ABI contract required for raw conversion")
	playerSnapshotRawDryRun := flag.Bool("convert-player-snapshot-raw-dry-run", false, "validate raw player files without writing CDTO output")
	playerSnapshotManifestReview := flag.String("build-player-snapshot-manifest-review", "", "review JSON produced by raw player conversion")
	playerSnapshotManifestMapping := flag.String("build-player-snapshot-manifest-mapping", "", "private operator identity/item mapping JSON for reviewed player snapshots")
	playerSnapshotManifestOutput := flag.String("build-player-snapshot-manifest-output", "", "private destination manifest for reviewed player snapshot import")
	playerSnapshotManifestDryRun := flag.Bool("build-player-snapshot-manifest-dry-run", false, "validate review, mapping, and CDTO files without writing an import manifest or connecting to PostgreSQL")
	bankSnapshotInspectDir := flag.String("inspect-bank-snapshot-dir", "", "inspect all private BankSnapshotV1 files under a directory and emit metadata-only JSON")
	bankSnapshotInspectFile := flag.String("inspect-bank-snapshot-file", "", "inspect one private BankSnapshotV1 file and emit metadata-only JSON")
	bankSnapshotInspectDryRun := flag.Bool("inspect-bank-snapshot-dry-run", false, "run BankSnapshotV1 inspection without connecting to PostgreSQL (inspection is always DB-free)")
	worldID := flag.String("world", "", "explicitly take over an existing Go world (no automatic import)")
	templates := flag.String("templates", "", "directory containing legacy mNN/oNN template tables")
	gameHour := flag.Int("game-hour", -1, "explicit game hour 0..23 until the persistent game clock is implemented")
	helpDir := flag.String("help-dir", os.Getenv("MUD_HELP_DIR"), "directory containing UTF-8 help, spell and policy documents")
	npcTalkDir := flag.String("npc-talk-dir", os.Getenv("MUD_NPC_TALK_DIR"), "directory containing canonical <name>-<level> NPC talk files (optional)")
	voteIssueFile := flag.String("vote-issue-file", os.Getenv("MUD_VOTE_ISSUE_FILE"), "explicit legacy post/ISSUE file for the server-owned vote catalog (optional)")
	playerTickInterval := flag.Duration("player-tick", 20*time.Second, "player vital scheduler cadence; whole seconds")
	roomResourceTickInterval := flag.Duration("room-resource-tick", 20*time.Second, "canonical floor/door resource scheduler cadence; whole seconds")
	npcResourceTickInterval := flag.Duration("npc-resource-tick", 20*time.Second, "canonical permanent NPC scheduler cadence; whole seconds")
	npcCombatTickInterval := flag.Duration("npc-combat-tick", time.Second, "NPC combat scheduler cadence; C update_active cadence; whole seconds")
	npcMaintenanceTickInterval := flag.Duration("npc-maintenance-tick", time.Second, "bounded pre-combat NPC maintenance scheduler cadence; whole seconds")
	flag.Parse()
	backupRestore, err := validateBackupRestoreFlags(
		*backupWorld, *backupFile, *backupOverwrite,
		*restoreWorld, *restoreFile, *restoreExpectedRevision,
		*restoreAllowCreate, *restoreForce,
	)
	if err != nil {
		log.Fatal(err)
	}
	playerSnapshotOptions, err := validatePlayerSnapshotImportFlags(*playerSnapshotManifest, *playerSnapshotDryRun)
	if err != nil {
		log.Fatal(err)
	}
	playerSnapshotInspectOptions, err := validatePlayerSnapshotInspectionFlagsWithFormat(*playerSnapshotInspectDir, *playerSnapshotInspectWorld, *playerSnapshotInspectDryRun, *playerSnapshotInspectFormat)
	if err != nil {
		log.Fatal(err)
	}
	playerSnapshotRawOptions, err := validatePlayerSnapshotRawConversionFlags(*playerSnapshotRawDir, *playerSnapshotRawWorld, *playerSnapshotRawOutput, *playerSnapshotRawABI, *playerSnapshotRawDryRun)
	if err != nil {
		log.Fatal(err)
	}
	playerSnapshotManifestBuildOptions, err := validatePlayerSnapshotManifestBuildFlags(*playerSnapshotManifestReview, *playerSnapshotManifestMapping, *playerSnapshotManifestOutput, *playerSnapshotManifestDryRun)
	if err != nil {
		log.Fatal(err)
	}
	bankSnapshotInspectOptions, err := validateBankSnapshotInspectionFlags(*bankSnapshotInspectDir, *bankSnapshotInspectFile)
	if err != nil {
		log.Fatal(err)
	}
	bankSnapshotInspectionSelected := bankSnapshotInspectOptions.Directory != "" || bankSnapshotInspectOptions.File != ""
	if *bankSnapshotInspectDryRun && !bankSnapshotInspectionSelected {
		log.Fatal("-inspect-bank-snapshot-dry-run requires -inspect-bank-snapshot-dir or -inspect-bank-snapshot-file")
	}
	if playerSnapshotOptions.ManifestPath != "" && playerSnapshotInspectOptions.Directory != "" {
		log.Fatal("player snapshot import and inspection modes are mutually exclusive")
	}
	if (playerSnapshotOptions.ManifestPath != "" || playerSnapshotInspectOptions.Directory != "") && playerSnapshotRawOptions.SourceDir != "" {
		log.Fatal("player snapshot import/inspection and raw conversion modes are mutually exclusive")
	}
	if playerSnapshotManifestBuildOptions.ReviewPath != "" && (playerSnapshotOptions.ManifestPath != "" || playerSnapshotInspectOptions.Directory != "" || playerSnapshotRawOptions.SourceDir != "") {
		log.Fatal("player snapshot manifest build mode cannot be combined with import, inspection, or raw conversion modes")
	}
	if backupRestore.mode != backupRestoreNone &&
		(*migrate || *seedWorld != "" || *seedRooms != "" || *seedCanonical || *seedIfAbsent || *worldID != "" || *templates != "" || *gameHour >= 0 || *npcTalkDir != "" || *voteIssueFile != "" || playerSnapshotOptions.ManifestPath != "" || playerSnapshotInspectOptions.Directory != "" || playerSnapshotRawOptions.SourceDir != "" || playerSnapshotManifestBuildOptions.ReviewPath != "") {
		log.Fatal("backup/restore mode cannot be combined with migrate, seed, or world flags")
	}
	if (playerSnapshotOptions.ManifestPath != "" || playerSnapshotInspectOptions.Directory != "") &&
		(*migrate || *seedWorld != "" || *seedRooms != "" || *seedCanonical || *seedIfAbsent || *worldID != "" || *templates != "" || *gameHour >= 0 || *npcTalkDir != "" || *voteIssueFile != "" || playerSnapshotRawOptions.SourceDir != "" || playerSnapshotManifestBuildOptions.ReviewPath != "") {
		log.Fatal("player snapshot import/inspection mode cannot be combined with migrate, seed, or world flags")
	}
	if playerSnapshotRawOptions.SourceDir != "" &&
		(*migrate || *seedWorld != "" || *seedRooms != "" || *seedCanonical || *seedIfAbsent || *worldID != "" || *templates != "" || *gameHour >= 0 || *npcTalkDir != "" || *voteIssueFile != "" || playerSnapshotManifestBuildOptions.ReviewPath != "") {
		log.Fatal("raw player conversion mode cannot be combined with migrate, seed, or world flags")
	}
	if playerSnapshotManifestBuildOptions.ReviewPath != "" &&
		(*migrate || *seedWorld != "" || *seedRooms != "" || *seedCanonical || *seedIfAbsent || *worldID != "" || *templates != "" || *gameHour >= 0 || *npcTalkDir != "" || *voteIssueFile != "") {
		log.Fatal("player snapshot manifest build mode cannot be combined with migrate, seed, or world flags")
	}
	if bankSnapshotInspectionSelected &&
		(backupRestore.mode != backupRestoreNone || *migrate || *seedWorld != "" || *seedRooms != "" || *seedCanonical || *seedIfAbsent || *worldID != "" || *templates != "" || *gameHour >= 0 || *npcTalkDir != "" || *voteIssueFile != "" || playerSnapshotOptions.ManifestPath != "" || playerSnapshotInspectOptions.Directory != "" || playerSnapshotRawOptions.SourceDir != "" || playerSnapshotManifestBuildOptions.ReviewPath != "") {
		log.Fatal("bank snapshot inspection mode cannot be combined with import, conversion, seed, backup, or world flags")
	}
	if bankSnapshotInspectionSelected {
		inspectionJSON, inspectErr := InspectBankSnapshotReviewJSON(bankSnapshotInspectOptions.Directory, bankSnapshotInspectOptions.File)
		if inspectErr != nil {
			log.Fatalf("bank snapshot inspection rejected: %v", inspectErr)
		}
		if written, writeErr := os.Stdout.Write(inspectionJSON); writeErr != nil || written != len(inspectionJSON) {
			if writeErr != nil {
				log.Fatalf("bank snapshot inspection output failed: %v", writeErr)
			}
			log.Fatal("bank snapshot inspection output was incomplete")
		}
		return
	}
	if playerSnapshotManifestBuildOptions.ReviewPath != "" {
		builtBatch, manifestRaw, buildErr := buildPlayerSnapshotImportManifest(playerSnapshotManifestBuildOptions.ReviewPath, playerSnapshotManifestBuildOptions.MappingPath)
		if buildErr != nil {
			log.Fatalf("player snapshot manifest build rejected: %v", buildErr)
		}
		if playerSnapshotManifestBuildOptions.DryRun {
			log.Printf("player snapshot manifest build validated: world=%s records=%d; no import manifest or database write performed", builtBatch.WorldID, len(builtBatch.Requests))
			return
		}
		if writeErr := writePlayerSnapshotManifestBuild(playerSnapshotManifestBuildOptions.OutputPath, manifestRaw, playerSnapshotManifestBuildOptions.ReviewPath); writeErr != nil {
			log.Fatalf("player snapshot manifest build failed: %v", writeErr)
		}
		log.Printf("player snapshot import manifest built: world=%s records=%d output=%s; database import still requires a separate explicit command", builtBatch.WorldID, len(builtBatch.Requests), playerSnapshotManifestBuildOptions.OutputPath)
		return
	}
	if playerSnapshotRawOptions.SourceDir != "" {
		playerSnapshotRawBatch, convertErr := convertPlayerSnapshotRawDirectory(playerSnapshotRawOptions)
		if convertErr != nil {
			log.Fatalf("legacy raw player conversion rejected: %v", convertErr)
		}
		if playerSnapshotRawOptions.DryRun {
			log.Printf("legacy raw player conversion validated: world=%s files=%d; no CDTO output or database write performed", playerSnapshotRawBatch.WorldID, len(playerSnapshotRawBatch.Records))
			return
		}
		if err := writePlayerSnapshotRawConversion(playerSnapshotRawBatch, playerSnapshotRawOptions.OutputDir); err != nil {
			log.Fatalf("legacy raw player conversion failed: %v", err)
		}
		log.Printf("legacy raw player conversion completed: world=%s files=%d output=%s; identity and credential review still required", playerSnapshotRawBatch.WorldID, len(playerSnapshotRawBatch.Records), playerSnapshotRawOptions.OutputDir)
		return
	}
	if *voteIssueFile != "" && *worldID == "" {
		log.Fatal("-vote-issue-file requires -world")
	}
	var playerSnapshotBatch playerSnapshotImportBatch
	if playerSnapshotOptions.ManifestPath != "" {
		playerSnapshotBatch, err = readPlayerSnapshotImportManifest(playerSnapshotOptions.ManifestPath)
		if err != nil {
			log.Fatalf("player snapshot manifest rejected: %v", err)
		}
		if playerSnapshotOptions.DryRun {
			log.Printf("player snapshot manifest validated: world=%s records=%d; no database connection or write performed", playerSnapshotBatch.WorldID, len(playerSnapshotBatch.Requests))
			return
		}
	}
	var playerSnapshotInspectionBatch playerSnapshotInspectionBatch
	if playerSnapshotInspectOptions.Directory != "" {
		playerSnapshotInspectionBatch, err = inspectPlayerSnapshotDirectoryWithFormat(playerSnapshotInspectOptions.Directory, playerSnapshotInspectOptions.WorldID, playerSnapshotInspectOptions.Format)
		if err != nil {
			log.Fatalf("player snapshot inspection rejected: %v", err)
		}
		if playerSnapshotInspectOptions.DryRun {
			quarantined := 0
			for _, report := range playerSnapshotInspectionBatch.Reports {
				if report.Result == "quarantined" {
					quarantined++
				}
			}
			log.Printf("player snapshot inspection validated: world=%s files=%d quarantined=%d; no database connection or write performed", playerSnapshotInspectionBatch.WorldID, len(playerSnapshotInspectionBatch.Reports), quarantined)
			return
		}
	}
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
	if backupRestore.mode != backupRestoreNone {
		backupCtx, backupCancel := context.WithTimeout(ctx, 30*time.Second)
		if err := runBackupRestore(backupCtx, repo, backupRestore); err != nil {
			backupCancel()
			log.Fatal(err)
		}
		backupCancel()
		world := backupRestore.backupWorld
		if backupRestore.mode == backupRestoreImport {
			world = backupRestore.restoreWorld
		}
		log.Printf("world %s backup/restore completed without starting a listener", world)
		return
	}
	if playerSnapshotOptions.ManifestPath != "" {
		importCtx, importCancel := context.WithTimeout(ctx, 5*time.Minute)
		err := runPlayerSnapshotImport(importCtx, repo, playerSnapshotBatch)
		importCancel()
		if err != nil {
			log.Fatalf("player snapshot import failed: %v", err)
		}
		log.Printf("player snapshot manifest import completed: world=%s records=%d", playerSnapshotBatch.WorldID, len(playerSnapshotBatch.Requests))
		return
	}
	if playerSnapshotInspectOptions.Directory != "" {
		inspectCtx, inspectCancel := context.WithTimeout(ctx, 5*time.Minute)
		err := runPlayerSnapshotInspection(inspectCtx, repo, playerSnapshotInspectionBatch)
		inspectCancel()
		if err != nil {
			log.Fatalf("player snapshot inspection failed: %v", err)
		}
		return
	}
	if *seedWorld != "" || *seedRooms != "" || *seedCanonical {
		if *seedWorld == "" || *seedRooms == "" || *worldID != "" || *voteIssueFile != "" {
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
	var npcWorldScheduler *transport.NPCWorldScheduler
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
		if *npcResourceTickInterval <= 0 || *npcResourceTickInterval%time.Second != 0 {
			log.Fatal("npc-resource-tick must be a positive whole number of seconds")
		}
		if *npcCombatTickInterval <= 0 || *npcCombatTickInterval%time.Second != 0 {
			log.Fatal("npc-combat-tick must be a positive whole number of seconds")
		}
		if *npcMaintenanceTickInterval <= 0 || *npcMaintenanceTickInterval%time.Second != 0 {
			log.Fatal("npc-maintenance-tick must be a positive whole number of seconds")
		}
		info, err := os.Stat(*templates)
		if err != nil || !info.IsDir() {
			log.Fatal("template directory unavailable")
		}
		helpInfo, err := os.Stat(*helpDir)
		if err != nil || !helpInfo.IsDir() {
			log.Fatal("help document directory unavailable")
		}
		var talkCatalog *world.TalkCatalog
		if *npcTalkDir != "" {
			talkInfo, err := os.Stat(*npcTalkDir)
			if err != nil || !talkInfo.IsDir() {
				log.Fatal("NPC talk directory unavailable")
			}
			loaded, err := world.LoadTalkCatalog(os.DirFS(*npcTalkDir))
			if err != nil {
				log.Fatalf("NPC talk catalog admission failed: %v", err)
			}
			talkCatalog = &loaded
			log.Printf("loaded NPC talk catalog: %d files (%d ignored)", loaded.Len(), len(loaded.IgnoredPaths()))
		}
		var voteCatalog world.VoteCatalog
		if *voteIssueFile != "" {
			source, err := world.LoadVoteCatalogFile(*voteIssueFile)
			if err != nil {
				log.Fatalf("vote ISSUE catalog admission failed: %v", err)
			}
			voteCatalog = source.Catalog
			digest, err := voteCatalog.Digest()
			if err != nil {
				log.Fatalf("vote ISSUE catalog digest failed: %v", err)
			}
			log.Printf("loaded vote ISSUE catalog: %s (%d options; sha256=%x; digest=%s)", source.Path, voteCatalog.Issue.Number, source.SHA256, digest)
		}
		startupCtx, startupCancel := context.WithTimeout(ctx, 30*time.Second)
		writer, _, err := engine.StartWorld(startupCtx, repo, *worldID, "boot-"+rand.Text())
		startupCancel()
		if err != nil {
			log.Fatal("world takeover/recovery failed; no listener started")
		}
		connector, err = transport.NewWorldConnector(transport.WorldConnectorConfig{
			Store: writer, WorldID: *worldID, MaxSessions: 32,
			Clock:         func() (int32, int) { return int32(time.Now().Unix()), *gameHour },
			PasswordStore: repo,
			Catalog:       world.TemplateCatalog{FS: os.DirFS(*templates)},
			TalkCatalog:   talkCatalog,
			VoteCatalog:   voteCatalog,
			HelpFS:        os.DirFS(*helpDir),
			Roll:          func(low, high int) int { return low + mathrand.IntN(high-low+1) },
			Allocate:      func() (string, error) { return "item-" + rand.Text(), nil },
		})
		if err != nil {
			log.Fatal("world connector configuration failed")
		}
		npcWorldScheduler, err = transport.NewNPCWorldScheduler(
			connector,
			*npcMaintenanceTickInterval,
			*npcResourceTickInterval,
			*npcCombatTickInterval,
		)
		if err != nil {
			log.Fatal("NPC world scheduler configuration failed")
		}
		if err := npcWorldScheduler.Start(workerCtx); err != nil {
			log.Fatal("NPC world scheduler start failed")
		}
		log.Printf("NPC world scheduler started (wake=%s; maintenance=%s; resource=%s; combat=%s; ordered)",
			npcWorldScheduler.Interval(), npcWorldScheduler.MaintenanceInterval(),
			npcWorldScheduler.ResourceInterval(), npcWorldScheduler.CombatInterval())
		go func() {
			defer close(workerDone)
			var workers sync.WaitGroup
			workers.Add(4)
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
			go func() {
				defer workers.Done()
				if err := npcWorldScheduler.Wait(context.Background()); err != nil {
					log.Printf("NPC world scheduler stopped: %v", err)
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
		if npcWorldScheduler != nil {
			if err := npcWorldScheduler.Shutdown(shutdownCtx); err != nil {
				log.Printf("NPC world scheduler shutdown incomplete: %v", err)
			}
		}
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
