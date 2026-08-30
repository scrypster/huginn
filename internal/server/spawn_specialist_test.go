package server

import "testing"

func TestSpaceCompanyIDForSpawn_NilSpaceStore(t *testing.T) {
	srv := testServer(t)
	got, err := srv.SpaceCompanyIDForSpawn("some-space")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty company id with no space store, got %q", got)
	}
}

func TestSpaceCompanyIDForSpawn_ResolvesCompanyForSpace(t *testing.T) {
	srv, store := companyTestServer(t)
	company, err := store.CreateCompany("Acme", "", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	sp, err := store.CreateChannelForCompany("acme-channel", "Winston", []string{"Winston"}, "", "", company.ID)
	if err != nil {
		t.Fatalf("CreateChannelForCompany: %v", err)
	}

	got, err := srv.SpaceCompanyIDForSpawn(sp.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != company.ID {
		t.Errorf("SpaceCompanyIDForSpawn = %q, want %q", got, company.ID)
	}

	// Desk-level space (no company) still resolves cleanly to "".
	desk, err := store.CreateChannel("desk-channel", "Winston", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel desk: %v", err)
	}
	got, err = srv.SpaceCompanyIDForSpawn(desk.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty company id for desk-level space, got %q", got)
	}
}
