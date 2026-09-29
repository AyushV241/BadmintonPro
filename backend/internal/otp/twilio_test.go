package otp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const (
	testAccountSID = "ACtest"
	testAuthToken  = "token"
	testServiceSID = "VAtest"
)

// fakeVerify imitates the two Twilio Verify endpoints the adapter uses,
// including the responses documented for wrong, expired and exhausted codes.
type fakeVerify struct {
	mu      sync.Mutex
	next    int
	pending map[string]*fakeVerification
	// forceStatus, when set, makes every request answer with this status and
	// Twilio error code instead.
	forceStatus, forceCode int
}

type fakeVerification struct {
	code     string
	attempts int
}

func (f *fakeVerify) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, pass, ok := r.BasicAuth()
	if !ok || user != testAccountSID || pass != testAuthToken {
		twilioError(w, http.StatusUnauthorized, 20003, "Authenticate")
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
		twilioError(w, http.StatusBadRequest, 0, "want a form body, got "+ct)
		return
	}
	if f.forceStatus != 0 {
		twilioError(w, f.forceStatus, f.forceCode, "forced")
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	to := r.FormValue("To")
	switch r.URL.Path {
	case "/Services/" + testServiceSID + "/Verifications":
		if !strings.HasPrefix(to, "+") {
			twilioError(w, http.StatusBadRequest, 60200, "Invalid parameter `To`")
			return
		}
		f.next++
		f.pending[to] = &fakeVerification{code: fmt.Sprintf("%06d", 100000+f.next)}
		writeJSON(w, http.StatusCreated, map[string]string{"status": "pending"})

	case "/Services/" + testServiceSID + "/VerificationCheck":
		v, ok := f.pending[to]
		if !ok {
			twilioError(w, http.StatusNotFound, 20404, "The requested resource was not found")
			return
		}
		if r.FormValue("Code") == v.code {
			delete(f.pending, to)
			writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
			return
		}
		v.attempts++
		if v.attempts >= 5 {
			delete(f.pending, to) // Twilio deletes it; later checks are 404
			writeJSON(w, http.StatusOK, map[string]string{"status": "max_attempts_reached"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "pending"})

	default:
		twilioError(w, http.StatusNotFound, 20404, "The requested resource was not found")
	}
}

func (f *fakeVerify) codeFor(t *testing.T, phone string) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.pending[phone]
	if !ok {
		t.Fatalf("no verification pending for %s", phone)
	}
	return v.code
}

func twilioError(w http.ResponseWriter, status, code int, message string) {
	writeJSON(w, status, map[string]any{"status": status, "code": code, "message": message})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func newTwilioAgainst(t *testing.T, f *fakeVerify) *Twilio {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	tw, err := NewTwilio(testAccountSID, testAuthToken, testServiceSID)
	if err != nil {
		t.Fatal(err)
	}
	tw.baseURL = srv.URL
	return tw
}

func TestTwilioContract(t *testing.T) {
	runContract(t, func(t *testing.T) harness {
		f := &fakeVerify{pending: make(map[string]*fakeVerification)}
		return harness{provider: newTwilioAgainst(t, f), codeFor: f.codeFor}
	})
}

func TestNewTwilioRequiresConfig(t *testing.T) {
	if _, err := NewTwilio(testAccountSID, "", testServiceSID); err == nil {
		t.Error("expected an error when the auth token is missing")
	}
}

func TestTwilioMapsErrors(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		status, code int
		check        bool
		want         error // nil means "not one of the sentinel errors"
	}{
		"start: invalid number":         {status: 400, code: 60200, want: ErrUnsupportedPhone},
		"start: blocked by fraud guard": {status: 400, code: 60410, want: ErrUnsupportedPhone},
		"start: too many sends":         {status: 429, code: 60203, want: ErrTooManyAttempts},
		"start: bad credentials":        {status: 401, code: 20003},
		"start: Twilio down":            {status: 503},
		"check: too many checks":        {status: 429, code: 60202, check: true, want: ErrTooManyAttempts},
		"check: expired or not found":   {status: 404, code: 20404, check: true, want: ErrInvalidCode},
		"check: malformed code":         {status: 400, code: 60200, check: true, want: ErrInvalidCode},
		"check: bad credentials":        {status: 401, code: 20003, check: true},
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeVerify{pending: make(map[string]*fakeVerification), forceStatus: tc.status, forceCode: tc.code}
			tw := newTwilioAgainst(t, f)
			var err error
			if tc.check {
				err = tw.Check(ctx, "+919876543210", "123456")
			} else {
				err = tw.Start(ctx, "+919876543210", SMS)
			}
			if err == nil {
				t.Fatal("expected an error")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
			if tc.want == nil {
				for _, sentinel := range []error{ErrInvalidCode, ErrTooManyAttempts, ErrUnsupportedPhone} {
					if errors.Is(err, sentinel) {
						t.Errorf("a provider failure must not look like %v: %v", sentinel, err)
					}
				}
			}
		})
	}
}

func TestTwilioErrorsNeverContainTheCode(t *testing.T) {
	f := &fakeVerify{pending: make(map[string]*fakeVerification), forceStatus: 500}
	tw := newTwilioAgainst(t, f)
	err := tw.Check(context.Background(), "+919876543210", "424242")
	if err == nil || strings.Contains(err.Error(), "424242") {
		t.Errorf("err = %v; it must exist and must not contain the code", err)
	}
}
