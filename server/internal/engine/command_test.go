package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"testing"
)

type fakeCommandStore struct {
	saved           *storage.WorldReceipt
	commitErr       error
	saveBeforeError bool
	loads, commits  int
}

func (f *fakeCommandStore) ReadWorldReceipt(context.Context, string, string, json.RawMessage) (storage.WorldReceipt, error) {
	if f.saved == nil {
		return storage.WorldReceipt{}, sql.ErrNoRows
	}
	r := *f.saved
	r.Replayed = true
	return r, nil
}
func (f *fakeCommandStore) LoadWorld(context.Context, string) (storage.WorldSnapshot, error) {
	f.loads++
	return storage.WorldSnapshot{State: json.RawMessage(`{"hp":30}`)}, nil
}
func (f *fakeCommandStore) CommitWorldCommand(_ context.Context, _ string, _ string, _ json.RawMessage, expected int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	f.commits++
	r := storage.WorldReceipt{Revision: expected + 1, Response: append(json.RawMessage(nil), response...)}
	if f.commitErr == nil || f.saveBeforeError {
		f.saved = &r
	}
	return r, f.commitErr
}

func TestExecutorRecoversCommittedResponseWithoutReexecution(t *testing.T) {
	store := &fakeCommandStore{commitErr: errors.New("connection lost"), saveBeforeError: true}
	calls := 0
	reduce := func(state json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		calls++
		return json.RawMessage(`{"hp":23}`), json.RawMessage(`{"damage":7}`), nil
	}
	r, err := Execute(context.Background(), store, "world", "command", json.RawMessage(`{"actor":"a"}`), reduce)
	if err != nil || r.Revision != 1 || !r.Replayed {
		t.Fatalf("%+v %v", r, err)
	}
	_, err = Execute(context.Background(), store, "world", "command", json.RawMessage(`{"actor":"a"}`), reduce)
	if err != nil || calls != 1 || store.loads != 1 || store.commits != 1 {
		t.Fatalf("reexecuted: calls%d loads%d writes%d %v", calls, store.loads, store.commits, err)
	}
}

func TestExecutorDoesNotReturnUncommittedResponse(t *testing.T) {
	failure := errors.New("rollback")
	store := &fakeCommandStore{commitErr: failure}
	r, err := Execute(context.Background(), store, "w", "c", json.RawMessage(`{}`), func(json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		return json.RawMessage(`{}`), json.RawMessage(`{"ok":true}`), nil
	})
	if !errors.Is(err, failure) || len(r.Response) != 0 {
		t.Fatalf("uncommitted response: %+v %v", r, err)
	}
}

func TestExecutorReducerFailureDoesNotWrite(t *testing.T) {
	failure := errors.New("invalid world")
	store := &fakeCommandStore{}
	_, err := Execute(context.Background(), store, "w", "c", json.RawMessage(`{}`), func(json.RawMessage) (json.RawMessage, json.RawMessage, error) { return nil, nil, failure })
	if !errors.Is(err, failure) || store.commits != 0 {
		t.Fatal(err)
	}
}
