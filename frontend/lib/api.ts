const API_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export type User = {
  id: string;
  name: string;
  email: string;
};

type LoginResponse = {
  token: string;
  user: User;
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
    res = await fetch(`${API_URL}${path}`, {
      ...init,
      headers: { "Content-Type": "application/json", ...init.headers },
    });
  } catch {
    // fetch only rejects on network-level failures, which locally almost
    // always means the Go server is not running.
    throw new ApiError("Cannot reach the server. Is the backend running?", 0);
  }

  if (res.status === 204) {
    return undefined as T;
  }

  const body = await res.json().catch(() => null);

  if (!res.ok) {
    const message =
      body && typeof body.error === "string"
        ? body.error
        : `Request failed (${res.status})`;
    throw new ApiError(message, res.status);
  }

  return body as T;
}

export function login(email: string, password: string) {
  return request<LoginResponse>("/api/login", {
    method: "POST",
    body: JSON.stringify({ email, password }),
  });
}

export function me(token: string) {
  return request<User>("/api/me", {
    headers: { Authorization: `Bearer ${token}` },
  });
}

export function logout(token: string) {
  return request<void>("/api/logout", {
    method: "POST",
    headers: { Authorization: `Bearer ${token}` },
  });
}

const TOKEN_KEY = "badmintonpro.token";

/**
 * Session token persistence. localStorage is fine for local development but is
 * readable by any script on the page — move to an httpOnly cookie before this
 * goes anywhere real.
 */
export const tokenStore = {
  get(): string | null {
    if (typeof window === "undefined") return null;
    try {
      return window.localStorage.getItem(TOKEN_KEY);
    } catch {
      return null;
    }
  },
  set(token: string) {
    try {
      window.localStorage.setItem(TOKEN_KEY, token);
    } catch {
      /* private browsing, blocked storage */
    }
  },
  clear() {
    try {
      window.localStorage.removeItem(TOKEN_KEY);
    } catch {
      /* ignore */
    }
  },
};
