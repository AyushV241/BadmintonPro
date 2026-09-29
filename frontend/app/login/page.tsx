"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState, useSyncExternalStore } from "react";
import { PhoneCodeForm } from "@/components/auth/PhoneCodeForm";
import {
  ApiError,
  loginOptions,
  providerLoginUrl,
  startPhoneLogin,
  verifyPhoneLogin,
  type LoginOptions,
} from "@/lib/api";

// Messages for the ?error= codes the backend's OAuth callback redirects with.
const callbackErrors: Record<string, string> = {
  cancelled: "Sign-in was cancelled.",
  expired: "That sign-in attempt expired. Please try again.",
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

function OrDivider() {
  return (
    <div className="flex items-center gap-3 text-xs text-slate-400 dark:text-slate-500">
      <span className="h-px flex-1 bg-slate-200 dark:bg-slate-800" />
      or
      <span className="h-px flex-1 bg-slate-200 dark:bg-slate-800" />
    </div>
  );
}

export default function LoginPage() {
  const router = useRouter();
  const callbackError = useCallbackError();
  // null until the backend says which sign-in methods are enabled.
  const [options, setOptions] = useState<LoginOptions | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    loginOptions()
      .then((o) => active && setOptions(o))
      .catch((err) => {
        if (active) {
          setLoadError(err instanceof ApiError ? err.message : "Something went wrong.");
        }
      });
    return () => {
      active = false;
    };
  }, []);

  const error = loadError ?? callbackError;
  const providers = options?.providers ?? [];
  const phoneEnabled = options?.phone ?? false;
  const nothingEnabled = options !== null && !phoneEnabled && providers.length === 0;

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

          {phoneEnabled && (
            <PhoneCodeForm
              start={startPhoneLogin}
              verify={verifyPhoneLogin}
              // A first sign-in goes to the profile step.
              onVerified={(user) =>
                router.push(user.profileComplete ? "/dashboard" : "/setup-profile")
              }
              verifyLabel="Verify and sign in"
            />
          )}

          {phoneEnabled && providers.length > 0 && <OrDivider />}

          {providers.length > 0 && (
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
          )}

          {nothingEnabled && (
            <p className="text-sm text-slate-500 dark:text-slate-400">
              No sign-in methods are enabled. Set OTP_PROVIDER or GOOGLE_CLIENT_ID on
              the backend.
            </p>
          )}
        </div>
      </div>
    </main>
  );
}
