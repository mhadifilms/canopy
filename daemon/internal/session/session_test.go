package session

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/canopy-dev/canopyd/internal/parser"
)

func TestRegistryRegisterAndGet(t *testing.T) {
	reg := NewRegistry()

	meta := &Meta{
		SessionID: "test-1",
		StartedAt: time.Now(),
		Status:    StatusActive,
	}
	sess := NewSession(meta)
	reg.Register(sess)

	got := reg.Get("test-1")
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Meta.SessionID != "test-1" {
		t.Errorf("session ID: got %q, want %q", got.Meta.SessionID, "test-1")
	}
}

func TestRegistryList(t *testing.T) {
	reg := NewRegistry()

	s1 := NewSession(&Meta{SessionID: "s1", Status: StatusActive})
	s2 := NewSession(&Meta{SessionID: "s2", Status: StatusEnded})
	s3 := NewSession(&Meta{SessionID: "s3", Status: StatusIdle})

	reg.Register(s1)
	reg.Register(s2)
	reg.Register(s3)

	active := reg.List(false)
	if len(active) != 2 {
		t.Errorf("List(includeEnded=false): got %d, want 2", len(active))
	}

	all := reg.List(true)
	if len(all) != 3 {
		t.Errorf("List(includeEnded=true): got %d, want 3", len(all))
	}
}

func TestRegistryCount(t *testing.T) {
	reg := NewRegistry()
	reg.Register(NewSession(&Meta{SessionID: "a", Status: StatusActive}))
	reg.Register(NewSession(&Meta{SessionID: "b", Status: StatusEnded}))
	reg.Register(NewSession(&Meta{SessionID: "c", Status: StatusIdle}))

	if got := reg.Count(); got != 2 {
		t.Errorf("Count: got %d, want 2", got)
	}
}

func TestRegistryRemove(t *testing.T) {
	reg := NewRegistry()
	reg.Register(NewSession(&Meta{SessionID: "rm-me", Status: StatusActive}))

	reg.Remove("rm-me")
	if reg.Get("rm-me") != nil {
		t.Error("Get after Remove should return nil")
	}
}

func TestSessionSubscribeBroadcast(t *testing.T) {
	sess := NewSession(&Meta{SessionID: "sub-test"})

	sub1 := sess.Subscribe("client-1")
	sub2 := sess.Subscribe("client-2")

	event := parser.Event{
		Type:      parser.EventSystemOutput,
		Timestamp: time.Now(),
		Content:   "hello",
	}
	sess.Broadcast(event)

	// Both subscribers should receive the event.
	select {
	case got := <-sub1.Events:
		if got.Content != "hello" {
			t.Errorf("sub1 content: got %q", got.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("sub1 timed out")
	}

	select {
	case got := <-sub2.Events:
		if got.Content != "hello" {
			t.Errorf("sub2 content: got %q", got.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("sub2 timed out")
	}
}

func TestSessionUnsubscribe(t *testing.T) {
	sess := NewSession(&Meta{SessionID: "unsub-test"})
	sess.Subscribe("c1")
	if sess.SubscriberCount() != 1 {
		t.Fatalf("expected 1 subscriber, got %d", sess.SubscriberCount())
	}
	sess.Unsubscribe("c1")
	if sess.SubscriberCount() != 0 {
		t.Fatalf("expected 0 subscribers, got %d", sess.SubscriberCount())
	}
}

func TestSessionRecordCommandTitle(t *testing.T) {
	sess := NewSession(&Meta{SessionID: "title-test"})

	sess.RecordCommand("npm install")
	if sess.Meta.Title != "npm install" {
		t.Errorf("title after 1 cmd: got %q", sess.Meta.Title)
	}
	if sess.Meta.TotalCommands != 1 {
		t.Errorf("total commands: got %d", sess.Meta.TotalCommands)
	}

	sess.RecordCommand("npm run build")
	if sess.Meta.Title != "npm install && npm run build" {
		t.Errorf("title after 2 cmds: got %q", sess.Meta.Title)
	}
}

func TestSessionSetAITitle(t *testing.T) {
	sess := NewSession(&Meta{SessionID: "ai-title"})
	sess.RecordCommand("claude")
	sess.SetAITitle("claude", "fix the auth bug in server.ts")
	if sess.Meta.Title != "claude: fix the auth bug in server.ts" {
		t.Errorf("AI title: got %q", sess.Meta.Title)
	}
}

func TestRegistryConcurrency(t *testing.T) {
	reg := NewRegistry()
	var wg sync.WaitGroup

	// Concurrent registration and listing.
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := fmt.Sprintf("sess-%d", n)
			reg.Register(NewSession(&Meta{SessionID: id, Status: StatusActive}))
			reg.List(true)
			reg.Get(id)
			reg.Count()
		}(i)
	}
	wg.Wait()

	if reg.Count() != 100 {
		t.Errorf("concurrent count: got %d, want 100", reg.Count())
	}
}

func TestBroadcastNonBlocking(t *testing.T) {
	sess := NewSession(&Meta{SessionID: "nonblock"})
	sub := sess.Subscribe("slow")

	// Fill the subscriber buffer completely.
	for i := 0; i < 256; i++ {
		sess.Broadcast(parser.Event{Content: "fill"})
	}

	// This should not block even though the subscriber is full.
	done := make(chan struct{})
	go func() {
		sess.Broadcast(parser.Event{Content: "overflow"})
		close(done)
	}()

	select {
	case <-done:
		// Good — broadcast didn't block.
	case <-time.After(time.Second):
		t.Fatal("broadcast blocked on full subscriber")
	}

	// Drain to avoid goroutine leak.
	_ = sub
}
