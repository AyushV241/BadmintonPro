package main

import (
	"context"
	"errors"
	"testing"

	"github.com/AyushV241/BadmintonPro/backend/internal/oauth"
)

// runStoreContract checks behaviour every Store must share. It runs against
// MemoryStore always and PostgresStore when a test database is available, so
// the linking rules are verified in the real SQL, not just the in-memory copy.
func runStoreContract(t *testing.T, newStore func(t *testing.T) Store) {
	ctx := context.Background()

	google := func(subject, email string, verified bool) oauth.Identity {
		return oauth.Identity{Provider: "google", Subject: subject, Email: email, EmailVerified: verified, Name: "Test User"}
	}

	t.Run("password login", func(t *testing.T) {
		s := newStore(t)
		created, err := s.CreateUser(ctx, NewUser{Name: "Pat", Email: " Pat@Example.com ", Password: "pw-123456"})
		if err != nil {
			t.Fatal(err)
		}
		if created.Email != "pat@example.com" {
			t.Errorf("email stored as %q, want normalised", created.Email)
		}
		u, err := s.VerifyPassword(ctx, "PAT@example.com", "pw-123456")
		if err != nil || u.ID != created.ID {
			t.Fatalf("VerifyPassword = %+v, %v", u, err)
		}
		if _, err := s.VerifyPassword(ctx, "pat@example.com", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("wrong password: err = %v", err)
		}
		if _, err := s.VerifyPassword(ctx, "nobody@example.com", "pw-123456"); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("unknown email: err = %v", err)
		}
	})

	t.Run("duplicate email is refused", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.CreateUser(ctx, NewUser{Name: "A", Email: "dup@example.com", Password: "pw-123456"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateUser(ctx, NewUser{Name: "B", Email: "DUP@example.com", Password: "pw-654321"}); !errors.Is(err, ErrEmailTaken) {
			t.Errorf("err = %v, want ErrEmailTaken", err)
		}
	})

	t.Run("sessions", func(t *testing.T) {
		s := newStore(t)
		u, _ := s.CreateUser(ctx, NewUser{Name: "S", Email: "s@example.com", Password: "pw-123456"})
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

	t.Run("create: new identity makes a verified account without a password", func(t *testing.T) {
		s := newStore(t)
		u, err := s.ResolveExternalLogin(ctx, google("g-1", "New@Gmail.com", true))
		if err != nil {
			t.Fatal(err)
		}
		if u.Email != "new@gmail.com" || !u.EmailVerified {
			t.Errorf("user = %+v, want verified normalised email", u)
		}
		// No password exists, and password login must fail like any other.
		if _, err := s.VerifyPassword(ctx, "new@gmail.com", ""); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("password login on Google-only account: err = %v", err)
		}
	})

	t.Run("login: returning identity gets the same account", func(t *testing.T) {
		s := newStore(t)
		first, _ := s.ResolveExternalLogin(ctx, google("g-2", "back@gmail.com", true))
		again, err := s.ResolveExternalLogin(ctx, google("g-2", "back@gmail.com", true))
		if err != nil || again.ID != first.ID {
			t.Fatalf("second login = %+v, %v; want user %s", again, err, first.ID)
		}
	})

	t.Run("password signup cannot claim a Google account's email", func(t *testing.T) {
		s := newStore(t)
		s.ResolveExternalLogin(ctx, google("g-3", "owned@gmail.com", true))
		if _, err := s.CreateUser(ctx, NewUser{Name: "X", Email: "owned@gmail.com", Password: "attacker-pw"}); !errors.Is(err, ErrEmailTaken) {
			t.Errorf("err = %v, want ErrEmailTaken", err)
		}
	})

	t.Run("attach: verified password account keeps its password", func(t *testing.T) {
		s := newStore(t)
		owner, _ := s.CreateUser(ctx, NewUser{Name: "V", Email: "v@gmail.com", Password: "pw-123456", EmailVerified: true})
		token, _ := s.CreateSession(ctx, owner.ID)

		u, err := s.ResolveExternalLogin(ctx, google("g-4", "v@gmail.com", true))
		if err != nil || u.ID != owner.ID {
			t.Fatalf("resolve = %+v, %v; want existing user %s", u, err, owner.ID)
		}
		if _, err := s.VerifyPassword(ctx, "v@gmail.com", "pw-123456"); err != nil {
			t.Errorf("password should still work after attach: %v", err)
		}
		if _, err := s.UserForToken(ctx, token); err != nil {
			t.Errorf("existing session should survive attach: %v", err)
		}
	})

	t.Run("takeover: unverified password account loses password and sessions", func(t *testing.T) {
		s := newStore(t)
		// Someone registered this address without proving they own it.
		squatter, _ := s.CreateUser(ctx, NewUser{Name: "Squatter", Email: "victim@gmail.com", Password: "squatter-pw"})
		squatterSession, _ := s.CreateSession(ctx, squatter.ID)

		u, err := s.ResolveExternalLogin(ctx, google("g-5", "victim@gmail.com", true))
		if err != nil {
			t.Fatal(err)
		}
		if u.ID != squatter.ID || !u.EmailVerified {
			t.Errorf("user = %+v, want same account now verified", u)
		}
		if _, err := s.VerifyPassword(ctx, "victim@gmail.com", "squatter-pw"); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("unproven password must be removed; err = %v", err)
		}
		if _, err := s.UserForToken(ctx, squatterSession); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("existing sessions must be revoked; err = %v", err)
		}
		// The Google login itself keeps working.
		if again, err := s.ResolveExternalLogin(ctx, google("g-5", "victim@gmail.com", true)); err != nil || again.ID != u.ID {
			t.Errorf("follow-up login = %+v, %v", again, err)
		}
	})

	t.Run("refuse: provider will not vouch for a clashing email", func(t *testing.T) {
		s := newStore(t)
		s.CreateUser(ctx, NewUser{Name: "E", Email: "clash@example.com", Password: "pw-123456", EmailVerified: true})
		if _, err := s.ResolveExternalLogin(ctx, google("g-6", "clash@example.com", false)); !errors.Is(err, ErrAccountConflict) {
			t.Errorf("err = %v, want ErrAccountConflict", err)
		}
		// Nothing was linked, so the refusal repeats rather than logging in.
		if _, err := s.ResolveExternalLogin(ctx, google("g-6", "clash@example.com", false)); !errors.Is(err, ErrAccountConflict) {
			t.Errorf("second attempt: err = %v, want ErrAccountConflict", err)
		}
	})

	t.Run("unvouched email is not stored and does not claim the address", func(t *testing.T) {
		s := newStore(t)
		u, err := s.ResolveExternalLogin(ctx, google("g-7", "maybe@example.com", false))
		if err != nil {
			t.Fatal(err)
		}
		if u.Email != "" || u.EmailVerified {
			t.Errorf("user = %+v, want no email recorded", u)
		}
		if _, err := s.CreateUser(ctx, NewUser{Name: "Real", Email: "maybe@example.com", Password: "pw-123456"}); err != nil {
			t.Errorf("address should still be free: %v", err)
		}
	})

	t.Run("identity without an email (e.g. Facebook)", func(t *testing.T) {
		s := newStore(t)
		a, err := s.ResolveExternalLogin(ctx, oauth.Identity{Provider: "facebook", Subject: "fb-1"})
		if err != nil {
			t.Fatal(err)
		}
		b, err := s.ResolveExternalLogin(ctx, oauth.Identity{Provider: "facebook", Subject: "fb-2"})
		if err != nil {
			t.Fatalf("second email-less account: %v", err)
		}
		if a.ID == b.ID || a.Name != "Player" {
			t.Errorf("a = %+v, b = %+v", a, b)
		}
	})

	t.Run("same subject at different providers are different identities", func(t *testing.T) {
		s := newStore(t)
		a, _ := s.ResolveExternalLogin(ctx, oauth.Identity{Provider: "google", Subject: "123"})
		b, _ := s.ResolveExternalLogin(ctx, oauth.Identity{Provider: "facebook", Subject: "123"})
		if a.ID == b.ID {
			t.Error("provider must be part of the identity key")
		}
	})
}

func TestMemoryStoreContract(t *testing.T) {
	runStoreContract(t, func(t *testing.T) Store { return NewMemoryStore() })
}
