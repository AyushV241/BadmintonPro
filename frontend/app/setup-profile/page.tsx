"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState, type FormEvent } from "react";
import { PhoneCodeForm } from "@/components/auth/PhoneCodeForm";
import { Logo } from "@/components/brand/Logo";
import { FieldLabel } from "@/components/form/FieldLabel";
import { ArrowRightIcon, MailIcon, ShieldCheckIcon, UserIcon } from "@/components/icons";
import { Button, Input } from "@/components/ui";
import { initials } from "@/lib/initials";
import {
  ApiError,
  me,
  startContactPhone,
  updateProfile,
  verifyContactPhone,
  type ProfileUpdate,
  type User,
} from "@/lib/api";

/**
 * "Set up your profile", shown after the first sign-in:
 * - phone sign-ins: name, username, and an optional (unverified) email;
 * - Google sign-ins: name, username, and an optional phone verified by a code.
 *   Their email comes from Google and isn't asked for.
 */
export default function SetupProfilePage() {
  const router = useRouter();
  const [user, setUser] = useState<User | null>(null);
  const [name, setName] = useState("");
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [usernameError, setUsernameError] = useState<string | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [changingPhone, setChangingPhone] = useState(false);

  useEffect(() => {
    me()
      .then((u) => {
        if (u.profileComplete) {
          router.replace("/dashboard");
          return;
        }
        setUser(u);
        setName(u.name);
        setUsername(u.username);
        setEmail(u.emailVerified ? "" : u.email);
      })
      .catch(() => router.replace("/login"));
  }, [router]);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!user) return;
    setUsernameError(null);
    setFormError(null);
    setSaving(true);

    const profile: ProfileUpdate = { name, username };
    // Only an account without a verified email may type one.
    if (!user.emailVerified) profile.email = email;

    try {
      await updateProfile(profile);
      router.replace("/dashboard");
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setUsernameError(err.message);
      } else {
        setFormError(err instanceof ApiError ? err.message : "Something went wrong.");
      }
      setSaving(false);
    }
  }

  if (!user) {
    return (
      <main className="flex min-h-dvh items-center justify-center bg-background">
        <p className="text-sm text-muted">Loading…</p>
      </main>
    );
  }

  const phoneAccount = user.signInMethod === "phone";
  const avatarText = initials(name, username);

  return (
    <main className="relative min-h-dvh overflow-hidden bg-background text-foreground">
      {/* Decorative lime circles, as in the design. */}
      <span aria-hidden="true" className="absolute -top-28 -right-28 size-72 rounded-full bg-lime/25" />
      <span aria-hidden="true" className="absolute top-[420px] -left-40 size-64 rounded-full bg-lime/10" />

      <div className="relative mx-auto flex min-h-dvh w-full max-w-[480px] flex-col px-6 pt-5 pb-[max(32px,env(safe-area-inset-bottom))]">
        <header>
          <Logo />
        </header>

        <span className="mt-10 flex size-16 items-center justify-center rounded-[18px] bg-lime text-ink shadow-[0_10px_30px_rgba(143,174,18,0.35)]">
          <ShieldCheckIcon className="size-7" />
        </span>

        <p className="mt-6 text-[11px] font-semibold tracking-[0.14em] text-muted uppercase">
          Profile setup
        </p>
        <h1 className="mt-2 text-[40px] leading-none font-bold tracking-[-0.05em]">
          Let&rsquo;s get you
          <br />
          <span className="text-lime-text">court-ready.</span>
        </h1>
        <p className="mt-3 text-[15px] leading-normal text-muted">
          A few basics before we start matching you with the right players.
        </p>

        <div className="mt-8 flex items-center gap-4">
          <span
            aria-hidden="true"
            className="flex size-20 shrink-0 items-center justify-center rounded-full border-4 border-surface bg-[#f0b07a] text-2xl font-semibold tracking-[-0.03em] text-ink shadow-sm"
          >
            {avatarText || <UserIcon className="size-8" />}
          </span>
          <div className="flex flex-col gap-0.5">
            <span className="text-base font-semibold tracking-[-0.02em]">Your player card</span>
            <span className="text-sm text-muted">This is how other players will see you.</span>
          </div>
        </div>

        <form id="profile" onSubmit={handleSubmit} className="mt-8 flex flex-col gap-5">
          {formError && (
            <p role="alert" className="rounded-xl bg-danger/10 px-3 py-2 text-sm text-danger">
              {formError}
            </p>
          )}

          <div className="flex flex-col gap-2">
            <FieldLabel htmlFor="profile-name">Full name</FieldLabel>
            <Input
              id="profile-name"
              value={name}
              onChange={setName}
              autoComplete="name"
              placeholder="Arjun Kapoor"
              maxLength={100}
              startAdornment={<UserIcon className="size-5" />}
              required
            />
          </div>

          <div className="flex flex-col gap-2">
            <FieldLabel htmlFor="profile-username">Username</FieldLabel>
            <Input
              id="profile-username"
              value={username}
              onChange={(v) => {
                setUsername(v);
                setUsernameError(null);
              }}
              autoComplete="username"
              placeholder="arjunk"
              startAdornment="@"
              maxLength={20}
              error={usernameError ?? undefined}
              helperText="How other players find you. 3–20 letters, numbers, _ or ."
              required
            />
          </div>

          {phoneAccount && (
            <div className="flex flex-col gap-2">
              <FieldLabel htmlFor="profile-email" optional>
                Email
              </FieldLabel>
              <Input
                id="profile-email"
                type="email"
                value={email}
                onChange={setEmail}
                autoComplete="email"
                placeholder="you@example.com"
                startAdornment={<MailIcon className="size-5" />}
                helperText="For contact only. You'll still sign in with your phone."
              />
            </div>
          )}
        </form>

        {/* Google accounts: an optional contact phone, confirmed by a code. It
            sits outside the profile form because it's a form of its own. */}
        {!phoneAccount && (
          <div className="mt-5 flex flex-col gap-2">
            <FieldLabel htmlFor="profile-phone" optional>
              Phone number
            </FieldLabel>
            {user.phoneVerified && !changingPhone ? (
              <div className="flex min-h-14 items-center gap-3 rounded-2xl border border-line bg-surface px-4">
                <span className="flex-1 text-[15px]">{user.phone}</span>
                <span className="rounded-full bg-lime px-2.5 py-1 text-xs font-semibold text-ink">
                  Verified
                </span>
                <Button variant="ghost" size="sm" onClick={() => setChangingPhone(true)}>
                  Change
                </Button>
              </div>
            ) : (
              <PhoneCodeForm
                start={startContactPhone}
                verify={verifyContactPhone}
                onVerified={(updated) => {
                  setUser(updated);
                  setChangingPhone(false);
                }}
                label=""
                inputId="profile-phone"
                numberPrefix="+91"
                helperText="Useful for match updates and court coordination."
                sendLabel="Verify"
                inlineSend
                verifyLabel="Verify number"
              />
            )}
          </div>
        )}

        <Button
          type="submit"
          form="profile"
          variant="accent"
          size="lg"
          fullWidth
          loading={saving}
          endIcon={<ArrowRightIcon />}
          className="mt-8"
        >
          Create my player card
        </Button>
      </div>
    </main>
  );
}
