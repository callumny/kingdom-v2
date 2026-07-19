package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOpenCreatesPrivateDatabaseAndPersistsSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "memory.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%v err=%v", info.Mode(), err)
	}
	if info, err = os.Stat(filepath.Dir(path)); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("directory mode=%v err=%v", info.Mode(), err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
}

func TestSaveRecallAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store := openStore(t, path)
	clock := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}
	ctx := context.Background()
	for _, exchange := range []struct{ session, user, reply string }{
		{"session-a", "first question", "first answer"},
		{"session-a", "second question", "second answer"},
		{"session-b", "third question", "third answer"},
	} {
		if err := store.SaveExchange(ctx, exchange.session, exchange.user, exchange.reply); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, path)
	defer store.Close()

	recent, err := store.RecentExchanges(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 || recent[0].User != "second question" || recent[1].User != "third question" {
		t.Fatalf("recent=%+v", recent)
	}
	sessions, err := store.ListSessions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0].ID != "session-b" || sessions[1].ID != "session-a" || sessions[1].ExchangeCount != 2 {
		t.Fatalf("sessions=%+v", sessions)
	}
	exchanges, err := store.SessionExchanges(ctx, "session-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(exchanges) != 1 || exchanges[0].User != "second question" {
		t.Fatalf("session exchanges=%+v", exchanges)
	}
}

func TestSaveValidatesAndBoundsContent(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "memory.db"))
	defer store.Close()
	ctx := context.Background()
	for _, input := range []struct{ session, user, reply string }{
		{"", "user", "reply"},
		{"session", "", "reply"},
		{"session", "user", ""},
	} {
		if err := store.SaveExchange(ctx, input.session, input.user, input.reply); err == nil {
			t.Fatalf("accepted %+v", input)
		}
	}
	user := strings.Repeat("u", MaxUserBytes+100)
	reply := strings.Repeat("界", MaxReplyBytes)
	if err := store.SaveExchange(ctx, "bounded", user, reply); err != nil {
		t.Fatal(err)
	}
	got, err := store.SessionExchanges(ctx, "bounded", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].User) > MaxUserBytes || len(got[0].Reply) > MaxReplyBytes || !strings.HasPrefix(got[0].Reply, "界") {
		t.Fatalf("exchange was not bounded safely: %+v", got)
	}
}

func TestDeleteSessionCascadesExchanges(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "memory.db"))
	defer store.Close()
	ctx := context.Background()
	if err := store.SaveExchange(ctx, "delete-me", "q", "a"); err != nil {
		t.Fatal(err)
	}
	if deleted, err := store.DeleteSession(ctx, "delete-me"); err != nil || !deleted {
		t.Fatalf("deleted=%v err=%v", deleted, err)
	}
	if deleted, err := store.DeleteSession(ctx, "delete-me"); err != nil || deleted {
		t.Fatalf("second delete=%v err=%v", deleted, err)
	}
	got, err := store.SessionExchanges(ctx, "delete-me", 10)
	if err != nil || len(got) != 0 {
		t.Fatalf("exchanges=%+v err=%v", got, err)
	}
}

func TestCancelledOperationsReturnContextError(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "memory.db"))
	defer store.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.SaveExchange(ctx, "session", "q", "a"); err == nil {
		t.Fatal("cancelled save succeeded")
	}
	if _, err := store.RecentExchanges(ctx, 1); err == nil {
		t.Fatal("cancelled recall succeeded")
	}
}

func TestConcurrentSavesAreRaceSafe(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "memory.db"))
	defer store.Close()
	var wait sync.WaitGroup
	for index := 0; index < 20; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := store.SaveExchange(context.Background(), "shared", "question", "answer"); err != nil {
				t.Errorf("save: %v", err)
			}
		}()
	}
	wait.Wait()
	sessions, err := store.ListSessions(context.Background(), 1)
	if err != nil || len(sessions) != 1 || sessions[0].ExchangeCount != 20 {
		t.Fatalf("sessions=%+v err=%v", sessions, err)
	}
}

func TestRenderRecallIsBoundedAndChronological(t *testing.T) {
	exchanges := []Exchange{
		{SessionID: "one", User: "first", Reply: strings.Repeat("a", MaxRecallBytes)},
		{SessionID: "two", User: "second", Reply: "must truncate 界"},
	}
	prompt, truncated := RenderRecall(exchanges)
	if !truncated || len(prompt) > MaxRecallBytes || !strings.Contains(prompt, "Memory from session one") || strings.Contains(prompt, "�") {
		t.Fatalf("len=%d truncated=%v prompt=%q", len(prompt), truncated, prompt)
	}
}

func TestSessionIDsAreUniqueAndOpaque(t *testing.T) {
	first, err := NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.HasPrefix(first, "session-") || len(first) != len("session-")+32 {
		t.Fatalf("ids=%q %q", first, second)
	}
}

func TestUnsupportedSchemaVersionIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store := openStore(t, path)
	if _, err := store.db.Exec(`PRAGMA user_version = 99`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "version 99") {
		t.Fatalf("unsupported version error=%v", err)
	}
}

func openStore(t *testing.T, path string) *Store {
	t.Helper()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
