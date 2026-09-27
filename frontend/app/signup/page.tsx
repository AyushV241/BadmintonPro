"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState, type FormEvent } from "react";
import { ApiError, loginProviders, providerLoginUrl, signup } from "@/lib/api";

// Must match minPasswordLength and maxPasswordBytes in backend/api.go. The
// server enforces both; these only give earlier feedback.
const minPasswordLength = 8;
const maxPasswordLength = 72;

const providerLabels: Record<string, string> = {
  google: "Google",
};

function providerLabel(name: string) {
  return providerLabels[name] ?? name.charAt(0).toUpperCase() + name.slice(1);
}

const inputClass =
  "w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 outline-none placeholder:text-slate-400 focus:border-slate-900 focus:ring-1 focus:ring-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100 dark:focus:border-slate-400 dark:focus:ring-slate-400";

const labelClass =
  "block text-sm font-medium text-slate-700 dark:text-slate-300";

export default function SignupPage() {
  const router = useRouter();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  // A taken email gets a "sign in instead" link rather than a dead end.
  const [emailTaken, setEmailTaken] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [providers, setProviders] = useState<string[]>([]);

  useEffect(() => {
    let active = true;
    loginProviders()
      .then((names) => active && setProviders(names))
      .catch(() => {
        // Password signup still works without the provider list.
      });
    return () => {
      active = false;
    };
  }, []);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    setEmailTaken(false);
    setSubmitting(true);

    try {
      await signup(name, email, password);
      router.push("/dashboard");
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setEmailTaken(true);
        setError("An account with this email already exists.");
      } else {
        setError(
          err instanceof ApiError ? err.message : "Something went wrong.",
        );
      }
      setSubmitting(false);
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-slate-50 px-4 dark:bg-slate-950">
      <div className="w-full max-w-sm">
        <div className="mb-8 text-center">
          <h1 className="text-2xl font-semibold tracking-tight text-slate-900 dark:text-slate-50">
            BadmintonPro
          </h1>
          <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
            Create your club account
          </p>
        </div>

        <div className="space-y-4 rounded-xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">
          {error && (
            <p
              role="alert"
              className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-950/50 dark:text-red-400"
            >
              {error}
              {emailTaken && (
                <>
                  {" "}
                  <Link href="/login" className="font-medium underline">
                    Sign in instead
                  </Link>
                </>
              )}
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
              <label htmlFor="name" className={labelClass}>
                Name
              </label>
              <input
                id="name"
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
                maxLength={100}
                autoComplete="name"
                className={inputClass}
              />
            </div>

            <div className="space-y-1.5">
              <label htmlFor="email" className={labelClass}>
                Email
              </label>
              <input
                id="email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
                autoComplete="email"
                className={inputClass}
              />
            </div>

            <div className="space-y-1.5">
              <label htmlFor="password" className={labelClass}>
                Password
              </label>
              <input
                id="password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
                minLength={minPasswordLength}
                maxLength={maxPasswordLength}
                autoComplete="new-password"
                aria-describedby="password-hint"
                className={inputClass}
              />
              <p
                id="password-hint"
                className="text-xs text-slate-500 dark:text-slate-400"
              >
                At least {minPasswordLength} characters.
              </p>
            </div>

            <button
              type="submit"
              disabled={submitting}
              className="w-full rounded-lg bg-slate-900 px-3 py-2 text-sm font-medium text-white transition-colors hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-60 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-slate-300"
            >
              {submitting ? "Creating account…" : "Create account"}
            </button>
          </form>
        </div>

        <p className="mt-4 text-center text-sm text-slate-500 dark:text-slate-400">
          Already have an account?{" "}
          <Link
            href="/login"
            className="font-medium text-slate-900 underline dark:text-slate-100"
          >
            Sign in
          </Link>
        </p>
      </div>
    </main>
  );
}
