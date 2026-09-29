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
    /** Seconds to wait before retrying, from a 429's Retry-After header. */
    readonly retryAfter?: number,
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
    const retryAfter = Number(res.headers.get("Retry-After")) || undefined;
    throw new ApiError(message, res.status, retryAfter);
  }

  return body as T;
}

export async function me(): Promise<User> {
  const { user } = await request<{ user: User }>("/api/me");
  return user;
}

export function logout(): Promise<void> {
  return request<void>("/api/logout", { method: "POST" });
}

export type LoginOptions = {
  /** Redirect logins, each shown as a "Continue with …" button. */
  providers: string[];
  /** Whether sign-in with a code sent to a mobile number is enabled. */
  phone: boolean;
};

/** Which sign-in methods the backend has enabled. */
export async function loginOptions(): Promise<LoginOptions> {
  return request<LoginOptions>("/api/auth/providers");
}

/**
 * Sends a one-time code to a mobile number. Resolves to the number the code
 * went to, in international format (+919876543210), which verifyPhoneLogin
 * needs.
 */
export async function startPhoneLogin(phone: string): Promise<string> {
  const res = await request<{ phone: string }>("/api/auth/phone/start", {
    method: "POST",
    body: JSON.stringify({ phone }),
  });
  return res.phone;
}

/** Checks a code and signs in, creating the account on first login. */
export async function verifyPhoneLogin(
  phone: string,
  code: string,
): Promise<User> {
  const { user } = await request<{ user: User }>("/api/auth/phone/verify", {
    method: "POST",
    body: JSON.stringify({ phone, code }),
  });
  return user;
}

/**
 * Where to send the browser to sign in with a provider. This is a full-page
 * navigation, not a fetch: the provider's consent screen must load in the
 * top-level window.
 */
export function providerLoginUrl(provider: string): string {
  return `/api/auth/${encodeURIComponent(provider)}/start`;
}
