// All requests go to this app's own origin; next.config.ts proxies /api/* to
// the Go backend. The session is an httpOnly cookie the browser attaches
// automatically, so no token is ever handled in JavaScript.

export type User = {
  id: string;
  name: string;
  email: string;
  emailVerified: boolean;
};

/** Thrown for any non-2xx response, carrying the API's message. */
export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, {
      ...init,
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", ...init.headers },
    });
  } catch {
    throw new ApiError("Cannot reach the server. Check your connection.", 0);
  }

  if (res.status === 204) {
    return undefined as T;
  }

  const body = await res.json().catch(() => null);

  if (!res.ok) {
    // The dev proxy answers 500 with no JSON when the Go server is down.
    const message =
      body && typeof body.error === "string"
        ? body.error
        : res.status >= 500
          ? "The server is unavailable. Is the backend running?"
          : `Request failed (${res.status})`;
    throw new ApiError(message, res.status);
  }

  return body as T;
}

export async function login(email: string, password: string): Promise<User> {
  const { user } = await request<{ user: User }>("/api/login", {
    method: "POST",
    body: JSON.stringify({ email, password }),
  });
  return user;
}

/** Creates a password account and signs it in. 409 means the email is taken. */
export async function signup(
  name: string,
  email: string,
  password: string,
): Promise<User> {
  const { user } = await request<{ user: User }>("/api/signup", {
    method: "POST",
    body: JSON.stringify({ name, email, password }),
  });
  return user;
}

export async function me(): Promise<User> {
  const { user } = await request<{ user: User }>("/api/me");
  return user;
}

export function logout(): Promise<void> {
  return request<void>("/api/logout", { method: "POST" });
}

/** Names of the external login providers the backend has enabled. */
export async function loginProviders(): Promise<string[]> {
  const { providers } = await request<{ providers: string[] }>(
    "/api/auth/providers",
  );
  return providers;
}

/**
 * Where to send the browser to sign in with a provider. This is a full-page
 * navigation, not a fetch: the provider's consent screen must load in the
 * top-level window.
 */
export function providerLoginUrl(provider: string): string {
  return `/api/auth/${encodeURIComponent(provider)}/start`;
}
