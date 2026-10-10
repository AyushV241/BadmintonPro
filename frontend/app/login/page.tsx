"use client";

import Image from "next/image";
import { useRouter } from "next/navigation";
import { useEffect, useState, useSyncExternalStore } from "react";
import { PhoneCodeForm } from "@/components/auth/PhoneCodeForm";
import { Logo } from "@/components/brand/Logo";
import { FieldLabel } from "@/components/form/FieldLabel";
import { ArrowRightIcon, GoogleIcon, PhoneIcon } from "@/components/icons";
import { Button } from "@/components/ui";
import {
  ApiError,
  loginOptions,
  providerLoginUrl,
  startPhoneLogin,
  verifyPhoneLogin,
  type LoginOptions,
} from "@/lib/api";
import courtPhoto from "@/public/images/court-night.jpg";

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

// What the bottom sheet shows: the sign-in options, or a step of phone sign-in.
type Sheet = "options" | "phone" | "code";

const sheetCopy: Record<Sheet, { title: string; subtitle?: string }> = {
  options: { title: "Ready to rally?", subtitle: "One account for every court, match and rating." },
  phone: { title: "Sign in with your phone", subtitle: "We'll text you a 6-digit code to confirm it's you." },
  code: { title: "Enter the code" },
};

function OrDivider() {
  return (
    <div className="flex items-center gap-3 text-[11px] font-semibold tracking-[0.14em] text-muted">
      <span className="h-px flex-1 bg-line" />
      OR
      <span className="h-px flex-1 bg-line" />
    </div>
  );
}

export default function LoginPage() {
  const router = useRouter();
  const callbackError = useCallbackError();
  // null until the backend says which sign-in methods are enabled.
  const [options, setOptions] = useState<LoginOptions | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [sheet, setSheet] = useState<Sheet>("options");

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
  const loading = options === null && !loadError;
  const copy = sheetCopy[sheet];

  return (
    <main className="flex min-h-dvh justify-center bg-ink">
      {/* Phone-width column; on a desktop it sits centred on the ink background. */}
      <div className="relative flex min-h-dvh w-full max-w-[480px] flex-col overflow-hidden">
        {/* Photo fills the top 60%, running a little under the sheet's rounded top. */}
        <div className="absolute inset-x-0 top-0 h-[64%]">
          <Image
            src={courtPhoto}
            alt=""
            fill
            loading="eager"
            fetchPriority="high"
            placeholder="blur"
            sizes="(max-width: 480px) 100vw, 480px"
            className="object-cover object-[50%_20%]"
          />
          <div
            aria-hidden="true"
            className="absolute inset-0 bg-[linear-gradient(180deg,rgba(21,23,18,0.55)_0%,rgba(21,23,18,0.2)_30%,rgba(21,23,18,0.5)_65%,#151712_100%)]"
          />
        </div>

        <header className="relative px-5 pt-5">
          <Logo onPhoto />
        </header>

        <section className="relative mt-auto flex flex-col gap-3 px-6 pt-24 pb-7 text-white">
          <p className="flex items-center gap-2 text-xs font-semibold tracking-[0.2em] text-lime">
            <span aria-hidden="true" className="h-px w-6 bg-lime" />
            PLAY • RANK • RISE
          </p>
          <h1 className="text-[44px] leading-[0.98] font-bold tracking-[-0.05em]">
            Your game.
            <br />
            <span className="text-lime">Levelled up.</span>
          </h1>
          <p className="max-w-[300px] text-[15px] leading-normal text-white/75">
            Find your perfect match, own the court, and watch your rating climb.
          </p>
        </section>

        <section
          aria-labelledby="sign-in-title"
          className="relative flex min-h-[40dvh] flex-col gap-4 rounded-t-[28px] bg-surface px-[22px] pt-2.5 pb-[max(28px,env(safe-area-inset-bottom))] text-foreground"
        >
          <span aria-hidden="true" className="mx-auto h-[5px] w-10 rounded-full bg-line" />

          <div className="flex flex-col gap-1 pt-1">
            <h2 id="sign-in-title" className="text-[26px] leading-tight font-semibold tracking-[-0.04em]">
              {copy.title}
            </h2>
            {copy.subtitle && <p className="text-sm text-muted">{copy.subtitle}</p>}
          </div>

          {error && (
            <p role="alert" className="rounded-xl bg-danger/10 px-3 py-2 text-sm text-danger">
              {error}
            </p>
          )}

          {loading && (
            <div aria-hidden="true" className="flex flex-col gap-3">
              <span className="h-[54px] animate-pulse rounded-2xl bg-line" />
              <span className="h-[54px] animate-pulse rounded-2xl bg-line" />
            </div>
          )}

          {sheet === "options" && (
            <div className="flex flex-col gap-3">
              {phoneEnabled && (
                <Button size="lg" fullWidth startIcon={<PhoneIcon />} onClick={() => setSheet("phone")}>
                  Continue with phone
                </Button>
              )}

              {phoneEnabled && providers.length > 0 && <OrDivider />}

              {providers.map((name) => (
                <a
                  key={name}
                  href={providerLoginUrl(name)}
                  className="flex min-h-[54px] w-full items-center justify-center gap-2.5 rounded-2xl border border-line bg-surface text-base font-semibold tracking-[-0.01em] transition-colors hover:bg-foreground/5"
                >
                  {name === "google" && <GoogleIcon className="size-5" />}
                  Continue with {providerLabel(name)}
                </a>
              ))}

              {nothingEnabled && (
                <p className="text-sm text-muted">
                  No sign-in methods are enabled. Set OTP_PROVIDER or GOOGLE_CLIENT_ID on the
                  backend.
                </p>
              )}
            </div>
          )}

          {sheet !== "options" && (
            <div className="flex flex-col gap-2">
              {sheet === "phone" && <FieldLabel htmlFor="login-phone">Phone number</FieldLabel>}
              <PhoneCodeForm
                start={startPhoneLogin}
                verify={verifyPhoneLogin}
                // A first sign-in goes to the profile step.
                onVerified={(user) =>
                  router.push(user.profileComplete ? "/dashboard" : "/setup-profile")
                }
                onStepChange={setSheet}
                label=""
                inputId="login-phone"
                helperText=""
                // Matches the backend's PHONE_DEFAULT_REGION (IN): numbers
                // typed without a country code are read as Indian.
                numberPrefix="+91"
                sendLabel="Send OTP"
                sendIcon={<ArrowRightIcon />}
                verifyLabel="Verify & continue"
              />
              {sheet === "phone" && providers.length > 0 && (
                <Button
                  variant="ghost"
                  fullWidth
                  className="text-muted"
                  onClick={() => setSheet("options")}
                >
                  Back to all options
                </Button>
              )}
            </div>
          )}
        </section>
      </div>
    </main>
  );
}
