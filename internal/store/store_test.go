package store

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
)

func TestOpenMigrateIdempotent(t *testing.T) {
	ctx := context.Background()
	p := filepath.Join(t.TempDir(), "state.sqlite")
	s, err := Open(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := s.Version(ctx)
	if v != LatestVersion() || v < 1 {
		t.Fatalf("version = %d, latest = %d", v, LatestVersion())
	}
	if m, _ := s.JournalMode(ctx); m != "wal" {
		t.Fatalf("journal_mode = %s", m)
	}
	s.Close()
	s, err = Open(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n)
	if n != LatestVersion() {
		t.Fatalf("migrations applied %d times", n)
	}
}

func TestConcurrentOpen(t *testing.T) {
	ctx := context.Background()
	p := filepath.Join(t.TempDir(), "state.sqlite")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := Open(ctx, p)
			if err != nil {
				errs <- err
				return
			}
			s.Close()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestFTS5(t *testing.T) {
	if err := CheckFTS5(context.Background()); err != nil {
		t.Fatal(err)
	}
}
