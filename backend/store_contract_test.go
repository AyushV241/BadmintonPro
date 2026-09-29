package main

import (
	"context"
	"errors"
	"testing"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
)

// runStoreContract checks behaviour every Store must share. It runs against
// MemoryStore always and PostgresStore when a test database is available, so
// the rules are verified in the real SQL, not just the in-memory copy.
func runStoreContract(t *testing.T, newStore func(t *testing.T) Store) {
	ctx := context.Background()

	google := func(subject, email string, verified bool) oauth.Identity {
		return oauth.Identity{Provider: "google", Subject: subject, Email: email, EmailVerified: verified, Name: "Test User"}
	}
	phone := func(e164 string) oauth.Identity {
		return oauth.Identity{Provider: phoneIdentityProvider, Subject: e164}
	}
	resolve := func(t *testing.T, s Store, ident oauth.Identity) User {
		t.Helper()
		u, err := s.ResolveExternalLogin(ctx, ident)
		if err != nil {
			t.Fatalf("ResolveExternalLogin(%s/%s): %v", ident.Provider, ident.Subject, err)
		}
		return u
	}

	t.Run("sessions", func(t *testing.T) {
		s := newStore(t)
		u := resolve(t, s, phone("+919876543210"))
		token, err := s.CreateSession(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := s.UserForToken(ctx, token); err != nil || got.ID != u.ID {
			t.Fatalf("UserForToken = %+v, %v", got, err)
		}
		if err := s.Revoke(ctx, token); err != nil {
			t.Fatal(err)
		}
		if _, err := s.UserForToken(ctx, token); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("after revoke: err = %v, want ErrInvalidToken", err)
		}
	})

	t.Run("create: a new Google identity keeps its verified email", func(t *testing.T) {
		s := newStore(t)
		u := resolve(t, s, google("g-1", "New@Gmail.com", true))
		if u.Email != "new@gmail.com" || !u.EmailVerified || u.Name != "Test User" {
			t.Errorf("user = %+v, want name and verified normalised email", u)
		}
	})

	t.Run("login: a returning identity gets the same account", func(t *testing.T) {
		s := newStore(t)
		first := resolve(t, s, google("g-2", "back@gmail.com", true))
		if again := resolve(t, s, google("g-2", "back@gmail.com", true)); again.ID != first.ID {
			t.Fatalf("second login = %s, want %s", again.ID, first.ID)
		}
	})

	t.Run("accounts are never joined by email", func(t *testing.T) {
		s := newStore(t)
		// Two identities vouching for the same address are still two people
		// as far as sign-in is concerned. (On Postgres this also needs the
		// email column to be non-unique.)
		a := resolve(t, s, google("g-3", "same@gmail.com", true))
		b := resolve(t, s, oauth.Identity{Provider: "other", Subject: "o-3", Email: "same@gmail.com", EmailVerified: true})
		if a.ID == b.ID {
			t.Error("a shared email joined two identities into one account")
		}
	})

	t.Run("an email the provider doesn't vouch for is not stored", func(t *testing.T) {
		s := newStore(t)
		u := resolve(t, s, google("g-4", "maybe@example.com", false))
		if u.Email != "" || u.EmailVerified {
			t.Errorf("user = %+v, want no email recorded", u)
		}
	})

	t.Run("identities without an email are separate accounts", func(t *testing.T) {
		s := newStore(t)
		a := resolve(t, s, oauth.Identity{Provider: "facebook", Subject: "fb-1"})
		b := resolve(t, s, oauth.Identity{Provider: "facebook", Subject: "fb-2"})
		if a.ID == b.ID || a.Name != "Player" {
			t.Errorf("a = %+v, b = %+v", a, b)
		}
	})

	t.Run("a phone identity logs back into its own account", func(t *testing.T) {
		s := newStore(t)
		googleUser := resolve(t, s, google("g-5", "p@gmail.com", true))
		first := resolve(t, s, phone("+919876543210"))
		again := resolve(t, s, phone("+919876543210"))
		if first.ID != again.ID {
			t.Errorf("second login = %s, want the account from the first (%s)", again.ID, first.ID)
		}
		if first.ID == googleUser.ID || first.Email != "" {
			t.Errorf("phone login must create a separate email-less account, got %+v", first)
		}
	})

	t.Run("same subject at different providers are different identities", func(t *testing.T) {
		s := newStore(t)
		a := resolve(t, s, oauth.Identity{Provider: "google", Subject: "123"})
		b := resolve(t, s, oauth.Identity{Provider: "facebook", Subject: "123"})
		if a.ID == b.ID {
			t.Error("provider must be part of the identity key")
		}
	})

	t.Run("an identity must name a provider and subject", func(t *testing.T) {
		s := newStore(t)
		for _, ident := range []oauth.Identity{{Provider: "google"}, {Subject: "x"}} {
			if _, err := s.ResolveExternalLogin(ctx, ident); err == nil {
				t.Errorf("ResolveExternalLogin(%+v) succeeded, want an error", ident)
			}
		}
	})
}

func TestMemoryStoreContract(t *testing.T) {
	runStoreContract(t, func(t *testing.T) Store { return NewMemoryStore() })
}
