"use client";

import { useEffect, useState, type FormEvent } from "react";
import { Button, Input, OTPInput, Typography } from "@/components/ui";
import { ApiError } from "@/lib/api";

// Matches the backend's per-number resend cooldown.
const RESEND_SECONDS = 30;

function messageFor(err: unknown): string {
  return err instanceof ApiError ? err.message : "Something went wrong.";
}

export type PhoneCodeFormProps<T> = {
  /** Sends a code; resolves to the number in international format. */
  start: (phone: string) => Promise<string>;
  /** Checks the code for that number. */
  verify: (phone: string, code: string) => Promise<T>;
  /** Called with verify's result once the code is correct. */
  onVerified: (result: T) => void;
  /** Field label. Default "Mobile number". */
  label?: string;
  /** Under the number field. */
  helperText?: string;
  /** Default "Send code". */
  sendLabel?: string;
  /** Default "Verify". */
  verifyLabel?: string;
};

/**
 * Two steps: type a mobile number, then the 6-digit code texted to it. Used
 * both to sign in and to verify a contact phone on the profile; the caller
 * decides what happens by passing start and verify.
 */
export function PhoneCodeForm<T>({
  start,
  verify,
  onVerified,
  label = "Mobile number",
  helperText = "We'll text you a 6-digit code",
  sendLabel = "Send code",
  verifyLabel = "Verify",
}: PhoneCodeFormProps<T>) {
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
      const sentTo = await start(number);
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

  async function check(value: string) {
    if (busy || value.length !== 6) return;
    setError(null);
    setBusy(true);
    try {
      const result = await verify(phone, value);
      setBusy(false);
      onVerified(result);
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
    void check(code);
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
          label={label}
          type="tel"
          inputMode="tel"
          autoComplete="tel"
          placeholder="98765 43210"
          value={typed}
          onChange={setTyped}
          error={error ?? undefined}
          helperText={helperText}
        />
        <Button type="submit" fullWidth loading={busy} disabled={!typed.trim()}>
          {sendLabel}
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
        onComplete={(value) => void check(value)}
        error={error ?? undefined}
        disabled={busy}
        autoFocus
        ariaLabel="Verification code"
      />

      <Button type="submit" fullWidth loading={busy} disabled={code.length !== 6}>
        {verifyLabel}
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
