package spaces_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
)

func TestForYou_MentionPersistsOnListAndClearsOnMarkRead(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, err := store.CreateChannel("Lab", "Winston", []string{"Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if ch.ForYou {
		t.Fatal("fresh channel must not have for_you")
	}

	if err := store.MarkForYou(ch.ID, spaces.LocalViewer); err != nil {
		t.Fatalf("MarkForYou: %v", err)
	}
	got, err := store.GetSpace(ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ForYou {
		t.Fatal("GetSpace for_you=false after mention persist")
	}
	res, err := store.ListSpaces(spaces.ListOpts{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, sp := range res.Spaces {
		if sp.ID == ch.ID {
			found = true
			if !sp.ForYou {
				t.Fatal("ListSpaces must return for_you after mention")
			}
			if sp.UnseenCount != 0 {
				t.Fatalf("spectator unseen_count=%d want 0 (count, not rail)", sp.UnseenCount)
			}
		}
	}
	if !found {
		t.Fatal("channel missing from list")
	}

	if err := store.MarkRead(ch.ID); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetSpace(ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ForYou {
		t.Fatal("MarkRead must clear for_you")
	}
}

func TestForYou_SpectatorUnseenDoesNotSetRail(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, err := store.CreateChannel("Spectate", "Winston", []string{"Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Agent chatter (no @me) can leave an unseen session count. That is a
	// count, not a company-rail badge.
	if err := store.MarkForYou("", spaces.LocalViewer); err == nil {
		t.Fatal("empty space must not mark for_you")
	}
	got, err := store.GetSpace(ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ForYou {
		t.Fatal("spectator channel must not have for_you without mention")
	}
	on, err := store.HasForYou(ch.ID, spaces.LocalViewer)
	if err != nil {
		t.Fatal(err)
	}
	if on {
		t.Fatal("HasForYou true without MarkForYou")
	}
}

func TestForYou_IdempotentAndUnknownSpace(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, err := store.CreateChannel("Ping", "Winston", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkForYou(ch.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkForYou(ch.ID, spaces.LocalViewer); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkForYou("ghost-space-id", spaces.LocalViewer); err == nil {
		t.Fatal("unknown space must fail closed")
	}
	if err := store.ClearForYou(ch.ID, spaces.LocalViewer); err != nil {
		t.Fatal(err)
	}
	if err := store.ClearForYou(ch.ID, spaces.LocalViewer); err != nil {
		t.Fatal(err)
	}
	on, err := store.HasForYou(ch.ID, spaces.LocalViewer)
	if err != nil || on {
		t.Fatalf("cleared for_you on=%v err=%v", on, err)
	}
}
