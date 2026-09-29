"use client";

import { useEffect, useState, type FormEvent } from "react";
import { Button, Input, OTPInput, Typography } from "@/components/ui";
import { ApiError, startPhoneLogin, verifyPhoneLogin } from "@/lib/api";

// Matches the backend's per-number resend cooldown.
const RESEND_SECONDS = 30;

function messageFor(err: unknown): string {
  return err instanceof ApiError ? err.message : "Something went wrong.";
}

/**
 * Sign in with a one-time code texted to a mobile number: type the number,
 * then the code. The account is created on the first login.
 */
export function PhoneLogin({ onSignedIn }: { onSignedIn: () => void }) {
  const [step, setStep] = useState<"phone" | "code">("phone");
  const [typed, setTyped] = useState("");
  // The number as the server normalised it (+919876543210). Verify must use
  // this, not what was typed.
  const [phone, setPhone] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [resendIn, setResendIn] = useState(0);

  useEffect(() => {
    if (resendIn <= 0) return;
    const timer = setTimeout(() => setResendIn((s) => s - 1), 1000);
    return () => clearTimeout(timer);
  }, [resendIn]);

  async function send(number: string) {
    setError(null);
    setBusy(true);
    try {
      const sentTo = await startPhoneLogin(number);
      setPhone(sentTo);
      setCode("");
      setStep("code");
      setResendIn(RESEND_SECONDS);
    } catch (err) {
      setError(messageFor(err));
      if (err instanceof ApiError && err.retryAfter) {
        setResendIn(err.retryAfter);
      }
    } finally {
      setBusy(false);
    }
  }

  async function verify(value: string) {
    if (busy || value.length !== 6) return;
    setError(null);
    setBusy(true);
    try {
      await verifyPhoneLogin(phone, value);
      onSignedIn(); // navigates away; stay busy until then
    } catch (err) {
      setError(messageFor(err));
      setCode("");
      setBusy(false);
    }
  }

  function handleSendSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    void send(typed);
  }

  function handleCodeSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    void verify(code);
  }

  function changeNumber() {
    setStep("phone");
    setCode("");
    setError(null);
  }

  if (step === "phone") {
    return (
      <form onSubmit={handleSendSubmit} className="space-y-3">
        <Input
          label="Mobile number"
          type="tel"
          inputMode="tel"
          autoComplete="tel"
          placeholder="98765 43210"
          value={typed}
          onChange={setTyped}
          error={error ?? undefined}
          helperText="We'll text you a 6-digit code"
          required
        />
        <Button type="submit" fullWidth loading={busy} disabled={!typed.trim()}>
          Send code
        </Button>
      </form>
    );
  }

  return (
    <form onSubmit={handleCodeSubmit} className="space-y-3">
      <Typography variant="bodySmall" color="muted">
        Enter the 6-digit code sent to{" "}
        <Typography as="span" variant="bodySmall" weight="semibold">
          {phone}
        </Typography>
      </Typography>

      <OTPInput
        value={code}
        onChange={setCode}
        onComplete={(value) => void verify(value)}
        error={error ?? undefined}
        disabled={busy}
        autoFocus
        ariaLabel="Verification code"
      />

      <Button type="submit" fullWidth loading={busy} disabled={code.length !== 6}>
        Verify and sign in
      </Button>

      <div className="flex items-center justify-between">
        <Button
          variant="ghost"
          size="sm"
          disabled={busy || resendIn > 0}
          onClick={() => void send(phone)}
        >
          {resendIn > 0 ? `Resend code in ${resendIn}s` : "Resend code"}
        </Button>
        <Button variant="ghost" size="sm" disabled={busy} onClick={changeNumber}>
          Use a different number
        </Button>
      </div>
    </form>
  );
}
