package load

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfigAcceptsOnlySafeStagedTargets(t *testing.T) {
	for _, target := range StagedTargets {
		for _, mode := range []Mode{ModeRESTLike, ModePersistent} {
			config, err := (Config{Mode: mode, Target: target}).normalized()
			if err != nil {
				t.Fatalf("target=%d mode=%s: %v", target, mode, err)
			}
			if config.Workers != MaxSessions {
				t.Fatalf("target=%d default workers=%d, want %d", target, config.Workers, MaxSessions)
			}
		}
	}
	for _, target := range []int{0, 1, 99, 101, 2000} {
		if _, err := (Config{Mode: ModeRESTLike, Target: target}).normalized(); err == nil {
			t.Fatalf("unsafe target %d was accepted", target)
		}
	}
}

func TestRequestIsolationAcrossLocalRuntimes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	left, err := newRuntime(100, 2)
	if err != nil {
		t.Fatal(err)
	}
	right, err := newRuntime(100, 2)
	if err != nil {
		left.close()
		t.Fatal(err)
	}
	defer left.close()
	defer right.close()

	var wg sync.WaitGroup
	for _, runtime := range []*runtime{left, right} {
		runtime := runtime
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, body, callErr := postJSON(ctx, runtime.server.Client(), runtime.server.URL+"/rest/command", commandRequest{Actor: actorID(0), Line: defaultLine})
			if callErr != nil || status != http.StatusOK {
				t.Errorf("isolated request status=%d body=%s err=%v", status, body, callErr)
			}
		}()
	}
	wg.Wait()
	for name, runtime := range map[string]*runtime{"left": left, "right": right} {
		if runtime.pendingCleanup() != 0 {
			t.Fatalf("%s runtime leaked cleanup=%d", name, runtime.pendingCleanup())
		}
		snapshot, err := runtime.store.LoadWorld(ctx, defaultWorldID)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Revision != 3 {
			t.Fatalf("%s revision=%d, want open+command+close=3", name, snapshot.Revision)
		}
	}
}

func TestPersistentCleanupClosesAllSessions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rt, err := newRuntime(100, MaxSessions)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.close()
	client := rt.server.Client()
	for index := 0; index < 4; index++ {
		status, body, err := postJSON(ctx, client, rt.server.URL+"/session/open", sessionOpenRequest{Actor: actorID(index)})
		if err != nil || status != http.StatusOK {
			t.Fatalf("open index=%d status=%d body=%s err=%v", index, status, body, err)
		}
	}
	if got := len(rt.registry.sessions); got != 4 {
		t.Fatalf("registry size=%d, want 4", got)
	}
	if err := rt.close(); err != nil {
		t.Fatal(err)
	}
	if got := len(rt.registry.sessions); got != 0 {
		t.Fatalf("registry leaked %d sessions", got)
	}
	if got := rt.pendingCleanup(); got != 0 {
		t.Fatalf("pending cleanup=%d", got)
	}
	if _, _, err := deleteHTTP(ctx, client, rt.server.URL+"/session/"+actorID(0)); err == nil {
		t.Fatal("closed session unexpectedly remained addressable")
	}
}

func TestPersistentCapacityReportsMaxSessions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	report, err := Run(ctx, Config{Mode: ModePersistent, Target: 100})
	if err != nil {
		t.Fatalf("persistent run failed: %+v", err)
	}
	if report.Accepted != MaxSessions || report.Rejected != 100-MaxSessions || report.Completed != MaxSessions {
		t.Fatalf("capacity report=%+v", report)
	}
	if report.Limits.DBMaxOpenConns != DBMaxOpenConns || report.Limits.GatewayMaxConns != GatewayMaxConns {
		t.Fatalf("hard limits missing: %+v", report.Limits)
	}
}

func TestCollisionNamespaceAndLoopbackPortsAreUniquePerRun(t *testing.T) {
	first, second := CollisionNamespace("same-seed"), CollisionNamespace("same-seed")
	if first == second {
		t.Fatalf("resource namespace collision: %q", first)
	}
	one := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	two := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer one.Close()
	defer two.Close()
	if one.URL == two.URL || !strings.HasPrefix(one.URL, "http://127.0.0.1:") || !strings.HasPrefix(two.URL, "http://127.0.0.1:") {
		t.Fatalf("httptest servers did not get isolated loopback URLs: %s %s", one.URL, two.URL)
	}
	if strings.Contains(first, " ") || strings.Contains(first, "/") {
		t.Fatalf("namespace is not safe for a container/project label: %q", first)
	}
}

func TestRunFailureIncludesActionableDetails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Run(ctx, Config{Mode: "unknown", Target: 100})
	if err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("invalid mode error=%v", err)
	}

	// Ensure the JSON shape remains useful to a shell/CI caller.
	report := Report{Mode: ModeRESTLike, Target: 100, Failures: []Failure{{Stage: "rest-command", Actor: actorID(2), Status: 503, Error: "world sessions full"}}}
	raw, marshalErr := json.Marshal(report)
	if marshalErr != nil || !strings.Contains(string(raw), "world sessions full") {
		t.Fatalf("report JSON=%s err=%v", raw, marshalErr)
	}
}

func BenchmarkRESTLikeCommand(b *testing.B) {
	ctx := context.Background()
	rt, err := newRuntime(100, MaxSessions)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.close()
	client := rt.server.Client()
	var workers atomicCounter
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		actor := actorID(int(workers.Add()))
		for pb.Next() {
			status, body, err := postJSON(ctx, client, rt.server.URL+"/rest/command", commandRequest{Actor: actor, Line: defaultLine})
			if err != nil {
				b.Fatalf("REST-like status=%d body=%s err=%v", status, body, err)
			}
		}
	})
}

func BenchmarkPersistentSessionCommand(b *testing.B) {
	ctx := context.Background()
	rt, err := newRuntime(100, MaxSessions)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.close()
	client := rt.server.Client()
	for index := 0; index < MaxSessions; index++ {
		status, body, openErr := postJSON(ctx, client, rt.server.URL+"/session/open", sessionOpenRequest{Actor: actorID(index)})
		if openErr != nil {
			b.Fatalf("open status=%d body=%s err=%v", status, body, openErr)
		}
	}
	var workers atomicCounter
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		actor := actorID(int(workers.Add()) % MaxSessions)
		for pb.Next() {
			status, body, err := postJSON(ctx, client, rt.server.URL+"/session/"+actor+"/command", commandRequest{Actor: actor, Line: defaultLine})
			if err != nil {
				b.Fatalf("persistent status=%d body=%s err=%v", status, body, err)
			}
		}
	})
}

type atomicCounter struct{ value int64 }

func (c *atomicCounter) Add() int64 { return atomic.AddInt64(&c.value, 1) }

func ExampleRun() {
	report, err := Run(context.Background(), Config{Mode: ModeRESTLike, Target: 100})
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s %d/%d\n", report.Mode, report.Completed, report.Target)
	// Output: rest-like 100/100
}
