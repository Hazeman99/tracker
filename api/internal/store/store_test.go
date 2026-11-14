package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"tracker/api/internal/db"
)

func newStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	return New(d, func() time.Time { return now }), &now
}

func TestTagsLifecycle(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)
	u, err := s.CreateUser(ctx, "a@example.com", "h")
	if err != nil {
		t.Fatal(err)
	}
	p1, err := s.CreateProject(ctx, u.ID, "One", "", []string{"Mobile App", "Freelance"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p1.Tags) != 2 || p1.Tags[0] != "Freelance" {
		t.Fatalf("tags %v", p1.Tags)
	}
	// Case-insensitive reuse keeps the first-typed spelling.
	p2, err := s.CreateProject(ctx, u.ID, "Two", "", []string{"mobile app", "Émigré"})
	if err != nil {
		t.Fatal(err)
	}
	if p2.Tags[0] != "Mobile App" || p2.Tags[1] != "Émigré" {
		t.Fatalf("tags %v", p2.Tags)
	}
	p3, err := s.CreateProject(ctx, u.ID, "Three", "", []string{"ÉMIGRÉ"})
	if err != nil {
		t.Fatal(err)
	}
	if p3.Tags[0] != "Émigré" {
		t.Fatalf("unicode fold failed: %v", p3.Tags)
	}
	tags, _ := s.ListTags(ctx, u.ID)
	if len(tags) != 3 {
		t.Fatalf("tags %+v", tags)
	}
	// Removing the last use deletes the tag.
	empty := []string{}
	if _, err := s.UpdateProject(ctx, u.ID, p1.ID, ProjectPatch{Tags: &empty}); err != nil {
		t.Fatal(err)
	}
	tags, _ = s.ListTags(ctx, u.ID)
	for _, tg := range tags {
		if tg.Name == "Freelance" {
			t.Fatal("orphan tag not deleted")
		}
	}
	// Deleting a project cascades and garbage-collects tags.
	if err := s.DeleteProject(ctx, u.ID, p2.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProject(ctx, u.ID, p3.ID); err != nil {
		t.Fatal(err)
	}
	tags, _ = s.ListTags(ctx, u.ID)
	if len(tags) != 0 {
		t.Fatalf("tags left after cascade: %+v", tags)
	}
}

func TestBoardListing(t *testing.T) {
	ctx := context.Background()
	s, now := newStore(t)
	u, _ := s.CreateUser(ctx, "a@example.com", "h")
	p, _ := s.CreateProject(ctx, u.ID, "One", "", nil)
	q, _ := s.CreateProject(ctx, u.ID, "Two", "", nil)
	for i := 0; i < 4; i++ {
		*now = now.Add(time.Minute)
		if _, err := s.CreateLogEntry(ctx, u.ID, p.ID, string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateTask(ctx, u.ID, p.ID, "t1"); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListProjects(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != p.ID || len(list[0].Log) != 2 || list[0].Log[0].Body != "d" || list[0].Log[1].Body != "c" {
		t.Fatalf("listing %+v", list)
	}
	full, _ := s.GetProject(ctx, u.ID, p.ID)
	if len(full.Log) != 4 || len(full.Tasks) != 1 {
		t.Fatalf("full %+v", full)
	}
	if err := s.ReorderProjects(ctx, u.ID, []string{q.ID, p.ID}); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListProjects(ctx, u.ID)
	if list[0].ID != q.ID {
		t.Fatal("reorder ignored")
	}
	if err := s.ReorderProjects(ctx, u.ID, []string{q.ID}); !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("partial order accepted: %v", err)
	}
	if err := s.ReorderProjects(ctx, u.ID, []string{q.ID, q.ID}); !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("duplicate order accepted: %v", err)
	}
}
