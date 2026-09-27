"use client";

import { useRouter } from "next/navigation";
import {
  useEffect,
  useState,
  useSyncExternalStore,
  type FormEvent,
} from "react";
import { ApiError, login, loginProviders, providerLoginUrl } from "@/lib/api";

// Messages for the ?error= codes the backend's OAuth callback redirects with.
const callbackErrors: Record<string, string> = {
  cancelled: "Sign-in was cancelled.",
  expired: "That sign-in attempt expired. Please try again.",
  conflict:
    "An account with this email already exists. Sign in with your password instead.",
  failed: "Sign-in failed. Please try again.",
};

const providerLabels: Record<string, string> = {
  google: "Google",
};

function providerLabel(name: string) {
  return providerLabels[name] ?? name.charAt(0).toUpperCase() + name.slice(1);
}

const noSubscription = () => () => {};

// Reads the callback's ?error= code. useSyncExternalStore with a null server
// snapshot lets this statically prerendered page hydrate without a mismatch,
// since the prerendered HTML never has a query string.
function useCallbackError(): string | null {
  const code = useSyncExternalStore(
    noSubscription,
    () => new URLSearchParams(window.location.search).get("error"),
    () => null,
  );
  return code ? (callbackErrors[code] ?? callbackErrors.failed) : null;
}

const inputClass =
  "w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 outline-none placeholder:text-slate-400 focus:border-slate-900 focus:ring-1 focus:ring-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100 dark:focus:border-slate-400 dark:focus:ring-slate-400";

export default function LoginPage() {
  const router = useRouter();
  const callbackError = useCallbackError();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [providers, setProviders] = useState<string[]>([]);

  useEffect(() => {
    let active = true;
    loginProviders()
      .then((names) => active && setProviders(names))
      .catch(() => {
        // Password login still works without the provider list.
      });
    return () => {
      active = false;
    };
  }, []);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(null);
    setSubmitting(true);

    try {
      await login(email, password);
      router.push("/dashboard");
    } catch (err) {
      setFormError(
        err instanceof ApiError ? err.message : "Something went wrong.",
      );
      setSubmitting(false);
    }
  }

  const error = formError ?? callbackError;

  return (
    <main className="flex min-h-screen items-center justify-center bg-slate-50 px-4 dark:bg-slate-950">
      <div className="w-full max-w-sm">
        <div className="mb-8 text-center">
          <h1 className="text-2xl font-semibold tracking-tight text-slate-900 dark:text-slate-50">
            BadmintonPro
          </h1>
          <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
            Sign in to your club account
          </p>
        </div>

        <div className="space-y-4 rounded-xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">
          {error && (
            <p
              role="alert"
              className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-950/50 dark:text-red-400"
            >
              {error}
            </p>
          )}

          {providers.length > 0 && (
            <>
              <div className="space-y-2">
                {providers.map((name) => (
                  <a
                    key={name}
                    href={providerLoginUrl(name)}
                    className="flex w-full items-center justify-center rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 transition-colors hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-200 dark:hover:bg-slate-800"
                  >
                    Continue with {providerLabel(name)}
                  </a>
                ))}
              </div>

              <div className="flex items-center gap-3 text-xs text-slate-400 dark:text-slate-500">
                <span className="h-px flex-1 bg-slate-200 dark:bg-slate-800" />
                or
                <span className="h-px flex-1 bg-slate-200 dark:bg-slate-800" />
              </div>
            </>
          )}

          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-1.5">
              <label
                htmlFor="email"
                className="block text-sm font-medium text-slate-700 dark:text-slate-300"
              >
                Email
              </label>
              <input
                id="email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
                autoComplete="email"
                placeholder="player@badmintonpro.local"
                className={inputClass}
              />
            </div>

            <div className="space-y-1.5">
              <label
                htmlFor="password"
                className="block text-sm font-medium text-slate-700 dark:text-slate-300"
              >
                Password
              </label>
              <input
                id="password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
                autoComplete="current-password"
                className={inputClass}
              />
            </div>

            <button
              type="submit"
              disabled={submitting}
              className="w-full rounded-lg bg-slate-900 px-3 py-2 text-sm font-medium text-white transition-colors hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-60 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-slate-300"
            >
              {submitting ? "Signing in…" : "Sign in"}
            </button>
          </form>
        </div>

        <p className="mt-4 text-center text-xs text-slate-400 dark:text-slate-500">
          Demo account: player@badmintonpro.local / smash123
        </p>
      </div>
    </main>
  );
}
