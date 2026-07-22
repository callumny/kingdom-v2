package memory

import (
	"context"
	"database/sql"
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
		if err := store.SaveExchange(ctx, exchange.session, exchange.user, exchange.reply, Usage{}); err != nil {
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

func TestSessionSummaryReportsPreviewTokenUsageAndContext(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "memory.db"))
	defer store.Close()
	ctx := context.Background()
	if err := store.SaveExchange(ctx, "session-a", "Design the session screen", "Use a compact two-column layout.", Usage{PromptTokens: 120, CompletionTokens: 30}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveExchange(ctx, "session-a", "Add resume support", "Restore the selected transcript.", Usage{PromptTokens: 180, CompletionTokens: 40}); err != nil {
		t.Fatal(err)
	}

	sessions, err := store.ListSessions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions=%+v", sessions)
	}
	session := sessions[0]
	if session.Preview != "Design the session screen" || session.TotalTokens != 370 || session.TokenUsageEstimated || session.ContextTokens <= 0 || session.ContextWindow != DefaultContextWindow {
		t.Fatalf("session=%+v", session)
	}
}

func TestCompactSessionKeepsRawTranscriptButReducesFutureContext(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "memory.db"))
	defer store.Close()
	ctx := context.Background()
	for _, prompt := range []string{"first topic", "second topic", "recent topic"} {
		if err := store.SaveExchange(ctx, "session-a", prompt, "answer to "+prompt, Usage{PromptTokens: 10, CompletionTokens: 5}); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := store.SessionExchanges(ctx, "session-a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CompactSession(ctx, "session-a", "Earlier discussion covered the first two topics.", raw[1].ID, Usage{PromptTokens: 20, CompletionTokens: 5}); err != nil {
		t.Fatal(err)
	}
	contextView, err := store.SessionContext(ctx, "session-a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if contextView.Summary == "" || len(contextView.Exchanges) != 1 || contextView.Exchanges[0].User != "recent topic" {
		t.Fatalf("context=%+v", contextView)
	}
	raw, err = store.SessionExchanges(ctx, "session-a", 10)
	if err != nil || len(raw) != 3 {
		t.Fatalf("raw transcript=%+v err=%v", raw, err)
	}
	sessions, err := store.ListSessions(ctx, 1)
	if err != nil || len(sessions) != 1 || sessions[0].TotalTokens != 70 {
		t.Fatalf("session usage=%+v err=%v", sessions, err)
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
		if err := store.SaveExchange(ctx, input.session, input.user, input.reply, Usage{}); err == nil {
			t.Fatalf("accepted %+v", input)
		}
	}
	user := strings.Repeat("u", MaxUserBytes+100)
	reply := strings.Repeat("界", MaxReplyBytes)
	if err := store.SaveExchange(ctx, "bounded", user, reply, Usage{}); err != nil {
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
	if err := store.SaveExchange(ctx, "delete-me", "q", "a", Usage{}); err != nil {
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
	if err := store.SaveExchange(ctx, "session", "q", "a", Usage{}); err == nil {
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
			if err := store.SaveExchange(context.Background(), "shared", "question", "answer", Usage{}); err != nil {
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

func TestVersionOneDatabaseMigratesWithoutLosingConversations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, started_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE exchanges (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, user_text TEXT NOT NULL, reply_text TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE INDEX exchanges_session_id_id ON exchanges(session_id, id)`,
		`INSERT INTO sessions(id, started_at, updated_at) VALUES('old', '2026-07-19T12:00:00.000000000Z', '2026-07-19T12:00:00.000000000Z')`,
		`INSERT INTO exchanges(session_id, user_text, reply_text, created_at) VALUES('old', 'hello', 'welcome', '2026-07-19T12:00:00.000000000Z')`,
		`PRAGMA user_version = 1`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	store := openStore(t, path)
	defer store.Close()
	sessions, err := store.ListSessions(context.Background(), 10)
	if err != nil || len(sessions) != 1 || sessions[0].Preview != "hello" || !sessions[0].TokenUsageEstimated {
		t.Fatalf("sessions=%+v err=%v", sessions, err)
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
