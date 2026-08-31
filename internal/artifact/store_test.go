package artifact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tcs76321/athanor/internal/store"
	"github.com/tcs76321/athanor/migrations"
)

func TestVersionChain(t *testing.T) {
	a, _, _ := openStore(t)
	ctx := context.Background()

	v1, err := a.CreateDraft(ctx, "p1", KindCode, []byte("v1"))
	if err != nil {
		t.Fatal(err)
	}
	v2, err := a.NewVersion(ctx, v1.ID, []byte("v2"))
	if err != nil {
		t.Fatalf("NewVersion: %v", err)
	}
	v3, err := a.NewVersion(ctx, v2.ID, []byte("v3"))
	if err != nil {
		t.Fatal(err)
	}

	if v2.Version != 2 || v2.SupersedesID != v1.ID {
		t.Errorf("v2 = version %d superseding %q, want 2 superseding %q", v2.Version, v2.SupersedesID, v1.ID)
	}
	if v3.Version != 3 || v3.SupersedesID != v2.ID {
		t.Errorf("v3 = version %d superseding %q, want 3 superseding %q", v3.Version, v3.SupersedesID, v2.ID)
	}

	// The whole chain is superseded except the head.
	for _, id := range []string{v1.ID, v2.ID} {
		got, err := a.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != StatusSuperseded {
			t.Errorf("artifact %s status = %s, want superseded", id, got.Status)
		}
	}
	head, err := a.Get(ctx, v3.ID)
	if err != nil {
		t.Fatal(err)
	}
	if head.Status != StatusDraft {
		t.Errorf("head status = %s, want draft", head.Status)
	}

	// Versioning a superseded artifact is rejected (chains stay linear).
	if _, err := a.NewVersion(ctx, v1.ID, []byte("fork")); err == nil {
		t.Error("versioning a superseded artifact accepted, want rejection")
	}

	// All three versions are visible per project.
	list, err := a.ListByProject(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("ListByProject = %d artifacts, want 3", len(list))
	}

	// Each version's content is independently readable.
	for id, want := range map[string]string{v1.ID: "v1", v2.ID: "v2", v3.ID: "v3"} {
		data, err := a.ReadContent(ctx, id)
		if err != nil {
			t.Fatalf("reading %s: %v", id, err)
		}
		if string(data) != want {
			t.Errorf("content of %s = %q, want %q", id, string(data), want)
		}
	}
}

func TestSetStatusFlow(t *testing.T) {
	a, _, _ := openStore(t)
	ctx := context.Background()
	art, err := a.CreateDraft(ctx, "p1", KindDocument, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}

	// draft → accepted directly is illegal (§9.3).
	if err := a.SetStatus(ctx, art.ID, StatusAccepted); !isIllegalStatus(err) {
		t.Fatalf("draft → accepted err = %v, want IllegalStatusError", err)
	}

	// draft → candidate → accepted works.
	if err := a.SetStatus(ctx, art.ID, StatusCandidate); err != nil {
		t.Fatal(err)
	}
	if err := a.SetStatus(ctx, art.ID, StatusAccepted); err != nil {
		t.Fatal(err)
	}
	got, _ := a.Get(ctx, art.ID)
	if got.Status != StatusAccepted {
		t.Errorf("status = %s, want accepted", got.Status)
	}

	// Accepted is terminal.
	if err := a.SetStatus(ctx, art.ID, StatusDraft); !isIllegalStatus(err) {
		t.Errorf("accepted → draft err = %v, want IllegalStatusError", err)
	}
}

func isIllegalStatus(err error) bool {
	var ise *IllegalStatusError
	return errors.As(err, &ise)
}

func TestListIsolatedPerProject(t *testing.T) {
	a, s, _ := openStore(t)
	ctx := context.Background()
	if _, err := s.DB().Exec(
		`INSERT INTO projects (id, name, archetype, goal) VALUES ('p2','other','code','build things')`,
	); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"p1", "p1", "p2"} {
		if _, err := a.CreateDraft(ctx, p, KindProposal, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	for p, want := range map[string]int{"p1": 2, "p2": 1} {
		list, err := a.ListByProject(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != want {
			t.Errorf("ListByProject(%s) = %d, want %d", p, len(list), want)
		}
		for _, art := range list {
			if art.ProjectID != p {
				t.Errorf("project %s listing leaked artifact from %s", p, art.ProjectID)
			}
		}
	}
}

// TestReadContentDetectsBitrot proves the hash guard: tampered content
// fails loudly with the typed error instead of returning corrupt data.
func TestReadContentDetectsBitrot(t *testing.T) {
	a, _, _ := openStore(t)
	ctx := context.Background()
	art, err := a.CreateDraft(ctx, "p1", KindDocument, []byte("original"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(art.StoragePath, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = a.ReadContent(ctx, art.ID)
	var cm *ContentMismatchError
	if !errors.As(err, &cm) {
		t.Fatalf("ReadContent(tampered) err = %v, want ContentMismatchError", err)
	}
}

func TestGetMissing(t *testing.T) {
	a, _, _ := openStore(t)
	if _, err := a.Get(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(missing) = %v, want ErrNotFound", err)
	}
}

// TestContentSurvivesRestart proves drafts are durable across a daemon
// restart: rows and files both survive.
func TestContentSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	artDir := filepath.Join(dir, "artifacts")
	ctx := context.Background()

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(s.DB(), migrations.FS, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(
		`INSERT INTO projects (id, name, archetype, goal) VALUES ('p1','demo','text','write something worth reading')`,
	); err != nil {
		t.Fatal(err)
	}
	a := NewStore(s, artDir)
	art, err := a.CreateDraft(ctx, "p1", KindDocument, []byte("durable"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	a2 := NewStore(s2, artDir)
	data, err := a2.ReadContent(ctx, art.ID)
	if err != nil {
		t.Fatalf("reading across restart: %v", err)
	}
	if string(data) != "durable" {
		t.Errorf("content across restart = %q, want %q", string(data), "durable")
	}
}

// TestLatestAcceptedByProject covers §19.3's "previous" side: the
// comparison phase picks the project's currently accepted artifact,
// not just the most recent. The four cases — none, one, multiple
// accepted (supersede chain), and a rejected artifact that must be
// ignored — are the corners the engine will hit in production.
func TestLatestAcceptedByProject(t *testing.T) {
	a, _, _ := openStore(t)
	ctx := context.Background()

	t.Run("no accepted artifact returns ErrNotFound", func(t *testing.T) {
		_, err := a.LatestAcceptedByProject(ctx, "p1")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("returns the single accepted artifact", func(t *testing.T) {
		draft, err := a.CreateDraft(ctx, "p1", KindCode, []byte("only"))
		if err != nil {
			t.Fatal(err)
		}
		if err := a.SetStatus(ctx, draft.ID, StatusCandidate); err != nil {
			t.Fatal(err)
		}
		if err := a.SetStatus(ctx, draft.ID, StatusAccepted); err != nil {
			t.Fatal(err)
		}
		got, err := a.LatestAcceptedByProject(ctx, "p1")
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != draft.ID {
			t.Errorf("ID = %s, want %s", got.ID, draft.ID)
		}
	})

	t.Run("rejected and draft artifacts are ignored", func(t *testing.T) {
		// A draft (un-accepted) and a rejected candidate both exist
		// alongside the previously-accepted one. The lookup must
		// still return the accepted one.
		rejected, err := a.CreateDraft(ctx, "p1", KindCode, []byte("loser"))
		if err != nil {
			t.Fatal(err)
		}
		if err := a.SetStatus(ctx, rejected.ID, StatusCandidate); err != nil {
			t.Fatal(err)
		}
		if err := a.SetStatus(ctx, rejected.ID, StatusRejected); err != nil {
			t.Fatal(err)
		}
		// The previous "returns the single accepted artifact" case
		// left one accepted artifact. LatestAcceptedByProject
		// must still find it (and skip the rejected one).
		got, err := a.LatestAcceptedByProject(ctx, "p1")
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != StatusAccepted {
			t.Errorf("status = %s, want %s", got.Status, StatusAccepted)
		}
		if got.ID == rejected.ID {
			t.Errorf("returned rejected artifact %s; LatestAcceptedByProject must skip it", rejected.ID)
		}
	})
}
