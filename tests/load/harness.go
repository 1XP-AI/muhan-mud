// Package load contains a local-only load harness for the current Go world
// connector. It deliberately wraps the connector in test-owned HTTP handlers;
// it does not add or change a production endpoint.
package load

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/transport"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

const (
	MaxSessions      = 32
	DBMaxOpenConns   = 16
	GatewayMaxConns  = 200
	MaxTarget        = 1000
	defaultWorkers   = MaxSessions
	defaultLine      = "점수"
	defaultWorldID   = "load-world"
	maxFailureDetail = 12
)

// StagedTargets are the only target values accepted by Run. Keeping these
// explicit prevents an accidental unbounded local stress test.
var StagedTargets = [...]int{100, 250, 500, 1000}

type Mode string

const (
	ModeRESTLike   Mode = "rest-like"
	ModePersistent Mode = "persistent"
)

func (m Mode) valid() bool { return m == ModeRESTLike || m == ModePersistent }

// Config controls one deterministic local run. Target means total attempted
// requests in REST-like mode and total virtual sessions in persistent mode.
// Workers is capped at MaxSessions to avoid creating an unsafe local burst.
type Config struct {
	Mode    Mode
	Target  int
	Workers int
	Line    string
}

func (c Config) normalized() (Config, error) {
	if !c.Mode.valid() {
		return Config{}, fmt.Errorf("mode must be %q or %q", ModeRESTLike, ModePersistent)
	}
	if !isStagedTarget(c.Target) {
		return Config{}, fmt.Errorf("target must be one of %v", StagedTargets)
	}
	if c.Workers == 0 {
		c.Workers = defaultWorkers
	}
	if c.Workers < 1 || c.Workers > MaxSessions {
		return Config{}, fmt.Errorf("workers must be between 1 and %d", MaxSessions)
	}
	if c.Line == "" {
		c.Line = defaultLine
	}
	if strings.ContainsAny(c.Line, "\r\n\x00") {
		return Config{}, errors.New("line contains a prohibited delimiter")
	}
	return c, nil
}

func isStagedTarget(target int) bool {
	for _, candidate := range StagedTargets {
		if target == candidate {
			return true
		}
	}
	return false
}

type HardLimits struct {
	MaxSessions     int    `json:"max_sessions"`
	DBMaxOpenConns  int    `json:"db_max_open_conns"`
	CommandMu       string `json:"command_mu"`
	GatewayMaxConns int    `json:"gateway_max_connections"`
}

// Failure is intentionally structured so a failed run points to the actor,
// stage, and local response rather than only returning a count.
type Failure struct {
	Stage  string `json:"stage"`
	Actor  string `json:"actor,omitempty"`
	Status int    `json:"status,omitempty"`
	Error  string `json:"error"`
}

type Report struct {
	Mode              Mode          `json:"mode"`
	Target            int           `json:"target"`
	Workers           int           `json:"workers"`
	Attempted         int           `json:"attempted"`
	Accepted          int           `json:"accepted"`
	Completed         int           `json:"completed"`
	Rejected          int           `json:"rejected"`
	Duration          time.Duration `json:"duration"`
	RequestsPerSecond float64       `json:"requests_per_second"`
	P50               time.Duration `json:"p50"`
	P95               time.Duration `json:"p95"`
	Failures          []Failure     `json:"failures,omitempty"`
	CleanupPending    int           `json:"cleanup_pending"`
	Limits            HardLimits    `json:"limits"`
}

func (r Report) Err() error {
	if len(r.Failures) == 0 && r.CleanupPending == 0 {
		return nil
	}
	if len(r.Failures) == 0 {
		return fmt.Errorf("cleanup left %d pending session(s)", r.CleanupPending)
	}
	return fmt.Errorf("load run failed: %d failure(s); first=%s", len(r.Failures), r.Failures[0].Error)
}

// Run executes a bounded local run. It uses only httptest loopback and an
// in-memory CommandStore populated from deterministic synthetic state.
func Run(ctx context.Context, config Config) (Report, error) {
	c, err := config.normalized()
	if err != nil {
		return Report{}, err
	}
	rt, err := newRuntime(c.Target, c.Workers, c.Mode == ModePersistent)
	if err != nil {
		return Report{}, err
	}
	defer rt.close()

	var report Report
	switch c.Mode {
	case ModeRESTLike:
		report = rt.runRESTLike(ctx, c)
	case ModePersistent:
		report = rt.runPersistent(ctx, c)
	}
	report.Limits = HardLimits{
		MaxSessions:     MaxSessions,
		DBMaxOpenConns:  DBMaxOpenConns,
		CommandMu:       "one connector-wide sync.Mutex serializes commands",
		GatewayMaxConns: GatewayMaxConns,
	}
	if closeErr := rt.close(); closeErr != nil {
		report.Failures = appendFailure(report.Failures, Failure{Stage: "runtime-close", Error: closeErr.Error()})
	}
	report.CleanupPending = rt.pendingCleanup()
	return report, report.Err()
}

type runtime struct {
	connector *transport.WorldConnector
	store     *memoryCommandStore
	server    *httptest.Server
	registry  *sessionRegistry
	closed    atomic.Bool
}

func newRuntime(target, workers int, persistent ...bool) (*runtime, error) {
	playerCount := workers
	if len(persistent) > 0 && persistent[0] {
		// Persistent runs hold the connector's hard limit and probe one overflow
		// actor. The remaining virtual clients are accounted for without creating
		// a 1000-player JSON snapshot or an unsafe local burst.
		playerCount = MaxSessions + 1
	}
	state, err := fixtureState(playerCount)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	store := &memoryCommandStore{state: raw, receipts: map[string]memoryReceipt{}}
	connector, err := transport.NewWorldConnector(transport.WorldConnectorConfig{
		Store:       store,
		WorldID:     defaultWorldID,
		Clock:       func() (int32, int) { return 1_700_000_000, 12 },
		MaxSessions: MaxSessions,
	})
	if err != nil {
		return nil, err
	}
	registry := newSessionRegistry(connector, workers)
	server := httptest.NewServer(newLoadHandler(connector, registry))
	return &runtime{connector: connector, store: store, server: server, registry: registry}, nil
}

func (r *runtime) close() error {
	if !r.closed.CompareAndSwap(false, true) {
		return nil
	}
	err := r.registry.closeAll(context.Background())
	r.server.Close()
	if pending := r.pendingCleanup(); pending != 0 {
		if err == nil {
			err = fmt.Errorf("cleanup left %d pending session(s)", pending)
		}
	}
	return err
}

func (r *runtime) pendingCleanup() int { return len(r.connector.PendingCleanup()) }

func (r *runtime) runRESTLike(ctx context.Context, c Config) Report {
	report := Report{Mode: c.Mode, Target: c.Target, Workers: c.Workers, Attempted: c.Target}
	client := r.server.Client()
	jobs := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	latencies := make([]time.Duration, 0, c.Target)
	start := time.Now()
	for worker := 0; worker < c.Workers; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				actor := actorID(worker)
				begin := time.Now()
				status, body, err := postJSON(ctx, client, r.server.URL+"/rest/command", commandRequest{Actor: actor, Line: c.Line, Sequence: index})
				latency := time.Since(begin)
				mu.Lock()
				latencies = append(latencies, latency)
				if err != nil {
					report.Failures = appendFailure(report.Failures, Failure{Stage: "rest-command", Actor: actor, Status: status, Error: responseError(body, err)})
				} else {
					report.Completed++
				}
				mu.Unlock()
			}
		}()
	}
	for index := 0; index < c.Target; index++ {
		select {
		case jobs <- index:
		case <-ctx.Done():
			mu.Lock()
			report.Failures = appendFailure(report.Failures, Failure{Stage: "rest-dispatch", Error: ctx.Err().Error()})
			mu.Unlock()
			index = c.Target
		}
	}
	close(jobs)
	wg.Wait()
	report.Accepted = report.Completed
	report.Rejected = report.Target - report.Accepted
	report.Duration = time.Since(start)
	report.P50, report.P95 = percentile(latencies, .50), percentile(latencies, .95)
	if report.Duration > 0 {
		report.RequestsPerSecond = float64(report.Completed) / report.Duration.Seconds()
	}
	if report.Completed != report.Target {
		report.Failures = appendFailure(report.Failures, Failure{Stage: "rest-capacity", Error: fmt.Sprintf("completed %d/%d request(s); use the first failure details", report.Completed, report.Target)})
	}
	return report
}

func (r *runtime) runPersistent(ctx context.Context, c Config) Report {
	report := Report{Mode: c.Mode, Target: c.Target, Workers: c.Workers, Attempted: c.Target}
	client := r.server.Client()
	type opened struct{ actor, id string }
	var openedSessions []opened
	var openWG sync.WaitGroup
	var mu sync.Mutex
	start := time.Now()
	admissionTarget := c.Target
	if admissionTarget > MaxSessions {
		admissionTarget = MaxSessions
	}
	openJobs := make(chan int)
	for worker := 0; worker < c.Workers; worker++ {
		openWG.Add(1)
		go func() {
			defer openWG.Done()
			for index := range openJobs {
				actor := actorID(index)
				status, body, err := postJSON(ctx, client, r.server.URL+"/session/open", sessionOpenRequest{Actor: actor})
				mu.Lock()
				if err != nil {
					if status != http.StatusServiceUnavailable {
						report.Failures = appendFailure(report.Failures, Failure{Stage: "persistent-open", Actor: actor, Status: status, Error: responseError(body, err)})
					}
					report.Rejected++
					mu.Unlock()
					continue
				}
				report.Accepted++
				openedSessions = append(openedSessions, opened{actor: actor, id: actor})
				mu.Unlock()
			}
		}()
	}
	for index := 0; index < admissionTarget; index++ {
		select {
		case openJobs <- index:
		case <-ctx.Done():
			mu.Lock()
			report.Failures = appendFailure(report.Failures, Failure{Stage: "persistent-dispatch", Error: ctx.Err().Error()})
			mu.Unlock()
			index = admissionTarget
		}
	}
	close(openJobs)
	openWG.Wait()
	if c.Target > MaxSessions {
		// One overflow request exercises the real connector rejection path. The
		// remaining virtual clients are deterministic accounting, not a burst of
		// requests that could exceed the local safety guard.
		status, body, err := postJSON(ctx, client, r.server.URL+"/session/open", sessionOpenRequest{Actor: actorID(MaxSessions)})
		if err == nil || status != http.StatusServiceUnavailable {
			report.Failures = appendFailure(report.Failures, Failure{Stage: "persistent-overflow-probe", Actor: actorID(MaxSessions), Status: status, Error: responseError(body, err)})
		}
	}

	var commandWG sync.WaitGroup
	latencies := make([]time.Duration, 0, report.Accepted)
	for _, session := range openedSessions {
		session := session
		commandWG.Add(1)
		go func() {
			defer commandWG.Done()
			begin := time.Now()
			status, body, err := postJSON(ctx, client, r.server.URL+"/session/"+session.id+"/command", commandRequest{Actor: session.actor, Line: c.Line})
			latency := time.Since(begin)
			mu.Lock()
			latencies = append(latencies, latency)
			defer mu.Unlock()
			if err != nil {
				report.Failures = appendFailure(report.Failures, Failure{Stage: "persistent-command", Actor: session.actor, Status: status, Error: responseError(body, err)})
				return
			}
			report.Completed++
		}()
	}
	commandWG.Wait()
	for _, session := range openedSessions {
		status, body, err := deleteHTTP(ctx, client, r.server.URL+"/session/"+session.id)
		if err != nil {
			report.Failures = appendFailure(report.Failures, Failure{Stage: "persistent-close", Actor: session.actor, Status: status, Error: responseError(body, err)})
		}
	}
	report.Duration = time.Since(start)
	report.P50, report.P95 = percentile(latencies, .50), percentile(latencies, .95)
	if report.Duration > 0 {
		report.RequestsPerSecond = float64(report.Completed) / report.Duration.Seconds()
	}
	report.Rejected = report.Target - report.Accepted
	expectedAccepted := c.Target
	if expectedAccepted > MaxSessions {
		expectedAccepted = MaxSessions
	}
	if report.Accepted != expectedAccepted {
		report.Failures = appendFailure(report.Failures, Failure{Stage: "persistent-capacity", Error: fmt.Sprintf("accepted %d session(s), expected %d from MaxSessions=%d", report.Accepted, expectedAccepted, MaxSessions)})
	}
	if report.Completed != report.Accepted {
		report.Failures = appendFailure(report.Failures, Failure{Stage: "persistent-capacity", Error: fmt.Sprintf("completed %d/%d admitted command(s)", report.Completed, report.Accepted)})
	}
	return report
}

func actorID(index int) string { return "load-actor-" + fmt.Sprintf("%04d", index) }

func appendFailure(failures []Failure, failure Failure) []Failure {
	if len(failures) >= maxFailureDetail {
		return failures
	}
	return append(failures, failure)
}

func responseError(body []byte, err error) string {
	if err != nil {
		if len(body) == 0 {
			return err.Error()
		}
		return err.Error() + ": " + strings.TrimSpace(string(body))
	}
	return "unexpected successful response"
}

type commandRequest struct {
	Actor    string `json:"actor"`
	Line     string `json:"line"`
	Sequence int    `json:"sequence,omitempty"`
}

type sessionOpenRequest struct {
	Actor string `json:"actor"`
}

type commandResponse struct {
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}

type sessionRegistry struct {
	mu        sync.Mutex
	connector *transport.WorldConnector
	sessions  map[string]transport.GameConnection
}

func newSessionRegistry(connector *transport.WorldConnector, _ int) *sessionRegistry {
	return &sessionRegistry{connector: connector, sessions: map[string]transport.GameConnection{}}
}

func newLoadHandler(connector *transport.WorldConnector, registry *sessionRegistry) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/command", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "POST required")
			return
		}
		var req commandRequest
		if !decodeJSON(w, r, &req) || req.Actor == "" || req.Line == "" {
			return
		}
		connection, _, err := connector.Open(r.Context(), storage.Character{ID: req.Actor})
		if err != nil {
			if connection != nil {
				connection.Close(context.Background())
			}
			writeJSONError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		defer connection.Close(context.Background())
		text, err := connection.Submit(r.Context(), req.Line)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, commandResponse{Text: text})
	})
	mux.HandleFunc("/session/open", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "POST required")
			return
		}
		var req sessionOpenRequest
		if !decodeJSON(w, r, &req) || req.Actor == "" {
			return
		}
		connection, scene, err := connector.Open(r.Context(), storage.Character{ID: req.Actor})
		if err != nil {
			if connection != nil {
				connection.Close(context.Background())
			}
			writeJSONError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		if !registry.add(req.Actor, connection) {
			connection.Close(context.Background())
			writeJSONError(w, http.StatusConflict, "session identity already registered")
			return
		}
		writeJSON(w, http.StatusOK, commandResponse{Text: scene})
	})
	mux.HandleFunc("/session/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/session/")
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) != 2 && len(parts) != 1 {
			writeJSONError(w, http.StatusNotFound, "invalid session path")
			return
		}
		id := parts[0]
		if id == "open" {
			writeJSONError(w, http.StatusNotFound, "invalid session path")
			return
		}
		if len(parts) == 1 {
			if r.Method != http.MethodDelete {
				writeJSONError(w, http.StatusMethodNotAllowed, "DELETE required")
				return
			}
			connection, ok := registry.remove(id)
			if !ok {
				writeJSONError(w, http.StatusNotFound, "session not found")
				return
			}
			connection.Close(r.Context())
			writeJSON(w, http.StatusOK, commandResponse{})
			return
		}
		if parts[1] != "command" || r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "POST command required")
			return
		}
		var req commandRequest
		if !decodeJSON(w, r, &req) || req.Line == "" {
			return
		}
		connection, ok := registry.get(id)
		if !ok {
			writeJSONError(w, http.StatusNotFound, "session not found")
			return
		}
		text, err := connection.Submit(r.Context(), req.Line)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, commandResponse{Text: text})
	})
	return mux
}

func (s *sessionRegistry) add(id string, connection transport.GameConnection) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sessions[id]; exists {
		return false
	}
	s.sessions[id] = connection
	return true
}

func (s *sessionRegistry) get(id string) (transport.GameConnection, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	connection, ok := s.sessions[id]
	return connection, ok
}

func (s *sessionRegistry) remove(id string) (transport.GameConnection, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	connection, ok := s.sessions[id]
	if ok {
		delete(s.sessions, id)
	}
	return connection, ok
}

func (s *sessionRegistry) closeAll(ctx context.Context) error {
	s.mu.Lock()
	connections := make([]transport.GameConnection, 0, len(s.sessions))
	for id, connection := range s.sessions {
		delete(s.sessions, id)
		connections = append(connections, connection)
	}
	s.mu.Unlock()
	for _, connection := range connections {
		connection.Close(ctx)
	}
	if pending := len(s.connector.PendingCleanup()); pending != 0 {
		return fmt.Errorf("cleanup left %d pending session(s)", pending)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, commandResponse{Error: message})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 4096))
	if err := decoder.Decode(value); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

func postJSON(ctx context.Context, client *http.Client, url string, value any) (int, []byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(payload)))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 32*1024))
	if readErr != nil {
		return response.StatusCode, body, readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, body, errors.New("HTTP request failed")
	}
	return response.StatusCode, body, nil
}

func deleteHTTP(ctx context.Context, client *http.Client, url string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return 0, nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 32*1024))
	if readErr != nil {
		return response.StatusCode, body, readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, body, errors.New("HTTP request failed")
	}
	return response.StatusCode, body, nil
}

func percentile(values []time.Duration, fraction float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), values...)
	for i := 1; i < len(sorted); i++ {
		value := sorted[i]
		j := i - 1
		for j >= 0 && sorted[j] > value {
			sorted[j+1] = sorted[j]
			j--
		}
		sorted[j+1] = value
	}
	index := int(float64(len(sorted)-1) * fraction)
	return sorted[index]
}

func fixtureState(target int) (world.State, error) {
	players := make(map[string]world.PlayerState, target)
	for index := 0; index < target; index++ {
		id := actorID(index)
		players[id] = world.PlayerState{
			Body: world.LegacyMonster{
				Name:      id,
				RoomID:    1,
				Class:     4,
				Level:     1,
				HPMax:     100,
				HPCurrent: 100,
				MPMax:     100,
				MPCurrent: 100,
			},
			Items: &world.ItemCollection{Items: map[string]world.Item{}},
		}
	}
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "load-room"}},
				Items:    &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
		Players: players,
	}
	if err := state.Validate(); err != nil {
		return world.State{}, err
	}
	return state, nil
}

type memoryReceipt struct {
	requestHash [sha256.Size]byte
	receipt     storage.WorldReceipt
}

type memoryCommandStore struct {
	mu       sync.Mutex
	revision int64
	state    json.RawMessage
	receipts map[string]memoryReceipt
}

func (s *memoryCommandStore) ReadWorldReceipt(_ context.Context, _ string, commandID string, request json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.receipts[commandID]
	if !ok {
		return storage.WorldReceipt{}, sql.ErrNoRows
	}
	hash := sha256.Sum256(request)
	if entry.requestHash != hash {
		return storage.WorldReceipt{}, storage.ErrCommandConflict
	}
	receipt := entry.receipt
	receipt.Replayed = true
	return receipt, nil
}

func (s *memoryCommandStore) LoadWorld(_ context.Context, _ string) (storage.WorldSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return storage.WorldSnapshot{Revision: s.revision, State: append(json.RawMessage(nil), s.state...)}, nil
}

func (s *memoryCommandStore) CommitWorldCommand(_ context.Context, _ string, commandID string, request json.RawMessage, expected int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hash := sha256.Sum256(request)
	if entry, ok := s.receipts[commandID]; ok {
		if entry.requestHash != hash {
			return storage.WorldReceipt{}, storage.ErrCommandConflict
		}
		receipt := entry.receipt
		receipt.Replayed = true
		return receipt, nil
	}
	if expected != s.revision {
		return storage.WorldReceipt{}, storage.ErrWorldConflict
	}
	s.revision++
	s.state = append(json.RawMessage(nil), state...)
	receipt := storage.WorldReceipt{Revision: s.revision, Response: append(json.RawMessage(nil), response...)}
	s.receipts[commandID] = memoryReceipt{requestHash: hash, receipt: receipt}
	return receipt, nil
}

var _ engine.CommandStore = (*memoryCommandStore)(nil)

// CollisionNamespace returns a deterministic label suitable for logs and a
// non-deterministic suffix for optional external resource names. The harness
// itself does not create containers; this helper makes future containerized
// adapters refuse to share a project namespace accidentally.
func CollisionNamespace(seed string) string {
	digest := sha256.Sum256([]byte(seed))
	return "muhan-load-" + hex.EncodeToString(digest[:6]) + "-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + strconv.FormatUint(collisionSequence.Add(1), 10)
}

var collisionSequence atomic.Uint64
