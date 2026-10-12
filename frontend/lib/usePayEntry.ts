"use client";

import { useState } from "react";
import { ApiError, startPayment, verifyPayment, type Entry } from "@/lib/api";
import { openCheckout } from "@/lib/checkout";

export type PayOutcome = { paid: Entry } | { closed: true } | { failed: string };

/**
 * Pays for a held entry: asks the server for a checkout, opens it, and has
 * the server verify the result.
 */
export function usePayEntry() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function pay(entryId: string): Promise<PayOutcome> {
    setError(null);
    setBusy(true);
    try {
      const start = await startPayment(entryId);
      if (start.entry) return { paid: start.entry }; // nothing to pay
      const result = await openCheckout(start.checkout);
      if (!result) return { closed: true };
      return { paid: await verifyPayment(result) };
    } catch (err) {
      const message =
        err instanceof ApiError || err instanceof Error
          ? err.message
          : "The payment didn't go through. Your spot is still held.";
      setError(message);
      return { failed: message };
    } finally {
      setBusy(false);
    }
  }

  return { pay, busy, error };
}
