# BadmintonPro frontend

Next.js 16 app (App Router, React 19, Tailwind v4) on port 3000. It is the only
thing the browser talks to: `/api/*` requests are proxied to the Go backend
(see [next.config.ts](next.config.ts)), so the session cookie stays on one
origin and no CORS is needed.

See the [root README](../README.md) for the whole project.

## Prerequisites

- Node.js 20+
- The backend running on port 8080. Start it first; see
  [backend/README.md](../backend/README.md). Without it the pages load, but
  sign-in and every `/api` call fail.

## Starting the dev server

Run these from `frontend/`.

**1. Install dependencies** (first time, and after `package.json` changes):

```bash
npm install
```

**2. Configure (optional).** By default `/api/*` is proxied to
`http://localhost:8080`. To point it elsewhere:

```bash
cp .env.example .env.local    # then edit BACKEND_URL
```

`BACKEND_URL` is read by the dev server only; the browser never sees it.
Restart `npm run dev` after changing it.

**3. Run:**

```bash
npm run dev
```

Open http://localhost:3000 and sign in with the demo account
`player@badmintonpro.local / smash123`. Edits hot reload in the browser.

From the repo root, `mise run web` does the same, and `mise run dev` starts it
together with the backend.

## Other commands

```bash
npm run lint     # ESLint
npm run build    # production build
npm run start    # serve the production build (after build)
```

## Troubleshooting

- **Sign-in fails, or the terminal shows `ECONNREFUSED` for `/api/...`.** The
  backend isn't running, or isn't on `BACKEND_URL`.
- **No "Continue with Google" button.** The backend has no Google credentials;
  see [Google sign-in](../README.md#google-sign-in-optional).
- **Google redirects to an error page.** Use `http://localhost:3000`, not
  `127.0.0.1`. The redirect URI registered with Google must match exactly.
