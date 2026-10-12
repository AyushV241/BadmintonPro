"use client";

import { useEffect, useState } from "react";
import { listEvents, type BadmintonEvent } from "@/lib/api";

/** Used when the browser can't or won't share a location. */
const FALLBACK_CITY = "bangalore";
const RADIUS_KM = 10;

type Nearby =
  | { state: "loading" }
  | { state: "ready"; events: BadmintonEvent[]; byDistance: boolean }
  | { state: "error"; message: string };

function position(): Promise<GeolocationPosition> {
  return new Promise((resolve, reject) => {
    if (!("geolocation" in navigator)) {
      reject(new Error("no geolocation"));
      return;
    }
    navigator.geolocation.getCurrentPosition(resolve, reject, { timeout: 8000, maximumAge: 10 * 60 * 1000 });
  });
}

/**
 * Upcoming events near the player: within 10 km when the browser shares the
 * location, otherwise the fallback city (design doc, section 4.1).
 */
export function useNearbyEvents(): Nearby {
  const [nearby, setNearby] = useState<Nearby>({ state: "loading" });

  useEffect(() => {
    let cancelled = false;
    (async () => {
      let events: BadmintonEvent[];
      let byDistance = true;
      try {
        const { coords } = await position();
        events = await listEvents({ lat: coords.latitude, lng: coords.longitude, radiusKm: RADIUS_KM });
        if (events.length === 0) throw new Error("nothing in range");
      } catch {
        byDistance = false;
        events = await listEvents({ city: FALLBACK_CITY });
      }
      if (!cancelled) setNearby({ state: "ready", events, byDistance });
    })().catch((err: unknown) => {
      if (!cancelled) setNearby({ state: "error", message: err instanceof Error ? err.message : "Couldn't load events." });
    });
    return () => {
      cancelled = true;
    };
  }, []);

  return nearby;
}
