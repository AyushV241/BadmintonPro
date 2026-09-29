package otp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const twilioVerifyURL = "https://verify.twilio.com/v2"

// Twilio adapts Twilio Verify, which generates, delivers, expires (after 10
// minutes) and rate-limits codes itself. Its REST API is two form POSTs, so
// this uses net/http rather than the Twilio SDK.
type Twilio struct {
	username   string // Account SID, or an API key SID
	password   string // Auth Token, or the API key secret
	serviceSID string // the Verify Service, VA…
	baseURL    string
	client     *http.Client
}

// NewTwilio authenticates with an Account SID and Auth Token (or an API key
// SID and secret) and sends codes through the given Verify Service.
func NewTwilio(username, password, serviceSID string) (*Twilio, error) {
	if username == "" || password == "" || serviceSID == "" {
		return nil, errors.New("twilio: account SID, auth token and Verify service SID are all required")
	}
	return &Twilio{
		username:   username,
		password:   password,
		serviceSID: serviceSID,
		baseURL:    twilioVerifyURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (t *Twilio) Name() string { return "twilio" }

func (t *Twilio) Start(ctx context.Context, phone string, ch Channel) error {
	status, body, err := t.post(ctx, "Verifications", url.Values{"To": {phone}, "Channel": {string(ch)}})
	if err != nil {
		return err
	}
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusBadRequest:
		// The number is the only caller input: invalid, a landline, or refused
		// by Twilio's geo permissions or fraud protection.
		return fmt.Errorf("%w: %s", ErrUnsupportedPhone, describe(status, body))
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("%w: %s", ErrTooManyAttempts, describe(status, body))
	default:
		// 401/403: bad credentials. 404: wrong Verify service SID.
		return fmt.Errorf("twilio: start verification: %s", describe(status, body))
	}
}

func (t *Twilio) Check(ctx context.Context, phone, code string) error {
	status, body, err := t.post(ctx, "VerificationCheck", url.Values{"To": {phone}, "Code": {code}})
	if err != nil {
		return err
	}
	switch status {
	case http.StatusOK:
		var check struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(body, &check); err != nil {
			return fmt.Errorf("twilio: decode verification check: %w", err)
		}
		// A wrong code comes back 200 with status "pending".
		if check.Status != "approved" {
			return ErrInvalidCode
		}
		return nil
	case http.StatusNotFound, http.StatusBadRequest:
		// Twilio deletes a verification once it is approved, expired or out of
		// attempts, so a check against it is a 404. A 400 is a malformed code,
		// for example the wrong length.
		return ErrInvalidCode
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w: %s", ErrTooManyAttempts, describe(status, body))
	default:
		return fmt.Errorf("twilio: check verification: %s", describe(status, body))
	}
}

func (t *Twilio) post(ctx context.Context, resource string, form url.Values) (int, []byte, error) {
	endpoint := fmt.Sprintf("%s/Services/%s/%s", t.baseURL, url.PathEscape(t.serviceSID), resource)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, err
	}
	req.SetBasicAuth(t.username, t.password)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("twilio: %s: %w", resource, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return 0, nil, fmt.Errorf("twilio: read %s response: %w", resource, err)
	}
	return resp.StatusCode, body, nil
}

// describe renders a Twilio error body for logs. It never includes the code.
func describe(status int, body []byte) string {
	var e struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil && e.Code != 0 {
		return fmt.Sprintf("HTTP %d, Twilio error %d: %s", status, e.Code, e.Message)
	}
	return fmt.Sprintf("HTTP %d", status)
}
