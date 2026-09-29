"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState, type FormEvent } from "react";
import { PhoneCodeForm } from "@/components/auth/PhoneCodeForm";
import { Button, Input, Typography } from "@/components/ui";
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
 * - Google sign-ins: name, username, Google's email read-only, and an
 *   optional phone verified by a code.
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
      <main className="flex min-h-screen items-center justify-center bg-slate-50 dark:bg-slate-950">
        <p className="text-sm text-slate-500 dark:text-slate-400">Loading…</p>
      </main>
    );
  }

  const phoneAccount = user.signInMethod === "phone";

  return (
    <main className="flex min-h-screen items-center justify-center bg-slate-50 px-4 py-12 dark:bg-slate-950">
      <div className="w-full max-w-sm">
        <div className="mb-8 text-center">
          <Typography variant="h2">Set up your profile</Typography>
          <Typography variant="bodySmall" color="muted" className="mt-1">
            This is how other players will find you.
          </Typography>
        </div>

        <div className="space-y-6 rounded-xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">
          <form id="profile" onSubmit={handleSubmit} className="space-y-4">
            {formError && (
              <p
                role="alert"
                className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-950/50 dark:text-red-400"
              >
                {formError}
              </p>
            )}

            <Input
              label="Name"
              value={name}
              onChange={setName}
              autoComplete="name"
              maxLength={100}
              required
            />

            <Input
              label="Username"
              value={username}
              onChange={(v) => {
                setUsername(v);
                setUsernameError(null);
              }}
              autoComplete="username"
              startAdornment="@"
              maxLength={20}
              error={usernameError ?? undefined}
              helperText="3–20 characters: letters, numbers, _ and ."
              required
            />

            {user.emailVerified ? (
              <Input
                label="Email"
                value={user.email}
                readOnly
                helperText="From your Google account"
              />
            ) : (
              <Input
                label="Email (optional)"
                type="email"
                value={email}
                onChange={setEmail}
                autoComplete="email"
                helperText="For contact only. You'll still sign in with your phone."
              />
            )}
          </form>

          {phoneAccount ? (
            <Input
              label="Phone"
              value={user.phone}
              readOnly
              helperText="Your sign-in number"
            />
          ) : user.phoneVerified && !changingPhone ? (
            <div className="flex items-center justify-between gap-3">
              <div>
                <Typography variant="label">Phone</Typography>
                <Typography variant="bodySmall">
                  {user.phone}{" "}
                  <span className="ml-1 rounded bg-emerald-50 px-1.5 py-0.5 text-xs text-emerald-700 dark:bg-emerald-950/50 dark:text-emerald-400">
                    verified
                  </span>
                </Typography>
              </div>
              <Button variant="ghost" size="sm" onClick={() => setChangingPhone(true)}>
                Change
              </Button>
            </div>
          ) : (
            <div className="space-y-2">
              <Typography variant="label">Phone (optional)</Typography>
              <PhoneCodeForm
                start={startContactPhone}
                verify={verifyContactPhone}
                onVerified={(updated) => {
                  setUser(updated);
                  setChangingPhone(false);
                }}
                helperText="We'll text a code to confirm it's yours"
                verifyLabel="Verify number"
              />
            </div>
          )}

          <Button type="submit" form="profile" fullWidth loading={saving}>
            Continue
          </Button>
        </div>
      </div>
    </main>
  );
}
