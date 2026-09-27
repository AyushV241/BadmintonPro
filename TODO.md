# TODO

## Password flows (deferred)

Today a password account that later uses "Continue with Google" with the same
email loses its password. Every signup is unverified, so the linking rules
treat Google's sign-in as a takeover: the password is deleted and its sessions
revoked. The user still gets in with Google, but nothing tells them their
password is gone, and they have no way to set a new one.

### Target design

Two mechanisms cover three user-facing flows:

| Flow | Trigger | Proof of identity | Endpoint | Page |
| --- | --- | --- | --- | --- |
| First setup, signed in | Google-only user adds a password | Recent sign-in | `POST /api/password` | `/account/password` |
| Re-enable after takeover | The Google callback just removed the password | The Google sign-in that just happened | same | same page, with a notice |
| Reset, signed out | "Forgot password?" | Link sent to the inbox | `POST /api/password/forgot`, then `POST /api/password/reset` | `/forgot-password`, `/reset-password?token=…` |

**Signed in (first setup and re-enable)**

- After a takeover, the Google callback redirects to
  `/account/password?reason=removed` instead of `/dashboard`.
- The notice stays calm and doesn't alarm. For example: "For security, the
  password on this account was removed because the email hadn't been
  verified. Set a new one to keep signing in with email." It should not
  suggest the account was hijacked, since almost every takeover is the honest
  owner.
- `/api/me` returns `hasPassword`, and the dashboard shows "Add a password"
  until one is set. Skipping the prompt is fine; Google keeps working.
- Setting a password requires a session created in the last ~10 minutes, so a
  stolen session cookie can't add a password and keep access for good.
- Changing an existing password requires the current password.
- Setting or changing a password signs out every other session.

**Signed out (reset)**

- Always reply "If an account exists, we've emailed a link", so the endpoint
  never reveals which emails have accounts.
- Reset tokens are random, stored hashed, single-use and expire in 30–60
  minutes. Rate-limit requests per email and per IP.
- Completing a reset marks the email verified, because the link proves inbox
  ownership, and it revokes all existing sessions.

### Related: email verification at signup

This uses the same email-sending setup and removes most of the need for the
re-enable flow:

- A verified password account gets *attach* instead of *takeover* when it
  uses Google, so it keeps its password.
- Signup can reply "check your inbox" instead of `409 email already
  registered`, which removes the account-enumeration tradeoff documented on
  `handleSignup`.

### Open decisions

- Email provider for reset and verification mail (Resend, Postmark, AWS SES,
  …). In local development, log the link to the backend console instead.

### Suggested order

1. Signed-in set password, the takeover redirect, and `hasPassword`. No email
   needed.
2. Forgot/reset, logging the link to the console.
3. Email verification at signup.
4. Connect a real email provider.

These steps touch `backend/api.go`, the store (plus a migration for reset
tokens), and the login/signup pages. Check which other sessions are editing
those files before starting.
