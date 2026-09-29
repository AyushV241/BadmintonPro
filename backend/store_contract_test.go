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
		if a.ID == b.ID || a.Name != "" {
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

	t.Run("a new account records how it signs in and needs a profile", func(t *testing.T) {
		s := newStore(t)
		p := resolve(t, s, phone("+919876543210"))
		if p.SignInMethod != phoneIdentityProvider || p.Phone != "+919876543210" || !p.PhoneVerified || p.ProfileComplete {
			t.Errorf("phone account = %+v", p)
		}
		g := resolve(t, s, google("g-6", "g@gmail.com", true))
		if g.SignInMethod != "google" || g.Phone != "" || g.ProfileComplete {
			t.Errorf("google account = %+v", g)
		}
		// The same account comes back with the same fields.
		if again := resolve(t, s, phone("+919876543210")); again != p {
			t.Errorf("reloaded = %+v, want %+v", again, p)
		}
	})

	t.Run("profile: saving name and username completes it", func(t *testing.T) {
		s := newStore(t)
		u := resolve(t, s, phone("+919876543210"))
		email := "asha@example.com"
		got, err := s.UpdateProfile(ctx, u.ID, ProfileUpdate{Name: "Asha", Username: "asha", Email: &email})
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "Asha" || got.Username != "asha" || got.Email != email || got.EmailVerified || !got.ProfileComplete {
			t.Errorf("after update = %+v", got)
		}
		token, _ := s.CreateSession(ctx, u.ID)
		if reloaded, _ := s.UserForToken(ctx, token); reloaded != got {
			t.Errorf("reloaded = %+v, want %+v", reloaded, got)
		}
	})

	t.Run("profile: usernames are unique regardless of case", func(t *testing.T) {
		s := newStore(t)
		a := resolve(t, s, phone("+919876543210"))
		b := resolve(t, s, google("g-7", "b@gmail.com", true))
		if _, err := s.UpdateProfile(ctx, a.ID, ProfileUpdate{Name: "A", Username: "shuttle"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpdateProfile(ctx, b.ID, ProfileUpdate{Name: "B", Username: "Shuttle"}); !errors.Is(err, ErrUsernameTaken) {
			t.Errorf("taken username: err = %v, want ErrUsernameTaken", err)
		}
		// Saving your own username again, or changing it, is fine.
		if _, err := s.UpdateProfile(ctx, a.ID, ProfileUpdate{Name: "A2", Username: "shuttle"}); err != nil {
			t.Errorf("re-saving own username: %v", err)
		}
		if _, err := s.UpdateProfile(ctx, a.ID, ProfileUpdate{Name: "A", Username: "smash"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpdateProfile(ctx, b.ID, ProfileUpdate{Name: "B", Username: "shuttle"}); err != nil {
			t.Errorf("a freed username should be available: %v", err)
		}
	})

	t.Run("profile: email left out is unchanged, empty clears it", func(t *testing.T) {
		s := newStore(t)
		u := resolve(t, s, phone("+919876543210"))
		email := "x@example.com"
		if _, err := s.UpdateProfile(ctx, u.ID, ProfileUpdate{Name: "X", Username: "xx1", Email: &email}); err != nil {
			t.Fatal(err)
		}
		got, err := s.UpdateProfile(ctx, u.ID, ProfileUpdate{Name: "X", Username: "xx1"})
		if err != nil || got.Email != email {
			t.Errorf("email without an update = %+v, %v; want it kept", got, err)
		}
		empty := ""
		if got, err := s.UpdateProfile(ctx, u.ID, ProfileUpdate{Name: "X", Username: "xx1", Email: &empty}); err != nil || got.Email != "" {
			t.Errorf("cleared email = %+v, %v", got, err)
		}
	})

	t.Run("profile: unknown account", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.UpdateProfile(ctx, "usr_nobody", ProfileUpdate{Name: "N", Username: "nobody"}); !errors.Is(err, ErrNoSuchUser) {
			t.Errorf("err = %v, want ErrNoSuchUser", err)
		}
		if _, err := s.SetVerifiedPhone(ctx, "usr_nobody", "+919876543210"); !errors.Is(err, ErrNoSuchUser) {
			t.Errorf("err = %v, want ErrNoSuchUser", err)
		}
	})

	t.Run("a verified contact phone is not a way to sign in", func(t *testing.T) {
		s := newStore(t)
		g := resolve(t, s, google("g-8", "c@gmail.com", true))
		got, err := s.SetVerifiedPhone(ctx, g.ID, "+919876543210")
		if err != nil || got.Phone != "+919876543210" || !got.PhoneVerified {
			t.Fatalf("SetVerifiedPhone = %+v, %v", got, err)
		}
		// Signing in with that number is still a separate account.
		if p := resolve(t, s, phone("+919876543210")); p.ID == g.ID {
			t.Error("a contact phone became a sign-in for the Google account")
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
