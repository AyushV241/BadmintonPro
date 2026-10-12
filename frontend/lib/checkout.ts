"use client";

import type { Checkout, CheckoutResult } from "@/lib/api";

const RAZORPAY_SCRIPT = "https://checkout.razorpay.com/v1/checkout.js";

type RazorpayResponse = { razorpay_payment_id: string; razorpay_order_id: string; razorpay_signature: string };
type RazorpayInstance = { open: () => void };
type RazorpayConstructor = new (options: Record<string, unknown>) => RazorpayInstance;

declare global {
  interface Window {
    Razorpay?: RazorpayConstructor;
  }
}

let loading: Promise<RazorpayConstructor> | null = null;

/** Loads Razorpay's checkout script once, on first use. */
function loadRazorpay(): Promise<RazorpayConstructor> {
  if (window.Razorpay) return Promise.resolve(window.Razorpay);
  loading ??= new Promise((resolve, reject) => {
    const script = document.createElement("script");
    script.src = RAZORPAY_SCRIPT;
    script.async = true;
    script.onload = () => (window.Razorpay ? resolve(window.Razorpay) : reject(new Error("Checkout didn't load.")));
    script.onerror = () => {
      loading = null;
      reject(new Error("Couldn't load the payment page. Check your connection."));
    };
    document.head.appendChild(script);
  });
  return loading;
}

/**
 * Opens the provider's checkout. Resolves with what to verify, or null if
 * the player closed it without paying.
 */
export async function openCheckout(checkout: Checkout): Promise<CheckoutResult | null> {
  if (checkout.provider === "fake") {
    // Local development only: the server refuses the fake provider in production.
    const amount = `₹${checkout.amountPaise / 100}`;
    if (!window.confirm(`Test payment (fake provider)\n\nPay ${amount} for ${checkout.description}?`)) return null;
    const paymentId = `pay_fake_${Math.random().toString(16).slice(2, 14)}`;
    return { orderId: checkout.orderId, paymentId, signature: `fake:${checkout.orderId}|${paymentId}` };
  }

  const Razorpay = await loadRazorpay();
  return new Promise((resolve) => {
    const rzp = new Razorpay({
      key: checkout.keyId,
      order_id: checkout.orderId,
      amount: checkout.amountPaise,
      currency: checkout.currency,
      name: checkout.name,
      description: checkout.description,
      prefill: checkout.prefill,
      theme: { color: "#151712" },
      handler: (r: RazorpayResponse) =>
        resolve({ orderId: r.razorpay_order_id, paymentId: r.razorpay_payment_id, signature: r.razorpay_signature }),
      modal: { ondismiss: () => resolve(null), confirm_close: true },
    });
    rzp.open();
  });
}
