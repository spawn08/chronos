package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/storage"
)

type failCreateSessionStorage struct {
	*testStorage
}

func (f *failCreateSessionStorage) GetSession(context.Context, string) (*storage.Session, error) {
	return nil, errors.New("not found")
}

func (f *failCreateSessionStorage) CreateSession(context.Context, *storage.Session) error {
	return errors.New("create session failed")
}

func TestChatWithSession_CreateSessionError(t *testing.T) {
	store := &failCreateSessionStorage{testStorage: newTestStorage()}
	prov := &testProvider{response: &model.ChatResponse{Content: "x", StopReason: model.StopReasonEnd}}
	a, err := New("a1", "Test").WithModel(prov).WithStorage(store).Build()
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.ChatWithSession(context.Background(), "new-sess", "hi")
	if err == nil {
		t.Fatal("expected error when CreateSession fails")
	}
}

type failListEventsStorage struct {
	*testStorage
}

func (f *failListEventsStorage) ListEvents(context.Context, string, int64) ([]*storage.Event, error) {
	return nil, errors.New("list events failed")
}

func TestChatWithSession_ListEventsError(t *testing.T) {
	base := newTestStorage()
	base.sessions["s1"] = &storage.Session{ID: "s1", AgentID: "a1", Status: "active"}
	store := &failListEventsStorage{testStorage: base}
	prov := &testProvider{response: &model.ChatResponse{Content: "x", StopReason: model.StopReasonEnd}}
	a, err := New("a1", "Test").WithModel(prov).WithStorage(store).Build()
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.ChatWithSession(context.Background(), "s1", "hi")
	if err == nil {
		t.Fatal("expected error when ListEvents fails")
	}
}
