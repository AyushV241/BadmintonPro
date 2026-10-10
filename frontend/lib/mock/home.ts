// SAMPLE DATA for the Home screen until the backend has events, matches and
// ratings. The types are what the real API should return, so swapping this
// file for API calls shouldn't change the page.

export type Player = {
  initials: string;
  /** Avatar background. */
  color: string;
};

export type EventSummary = {
  id: string;
  title: string;
  format: "Singles" | "Doubles";
  /** ISO 8601 with offset. */
  startsAt: string;
  venue: string;
  distanceKm: number;
  spotsLeft: number;
  feeInr: number;
  /** A few of the players who have joined, for the avatar stack. */
  players: Player[];
};

export type WeeklyMomentum = {
  matches: number;
  wins: number;
  rating: number;
  /** Rating change this week. */
  ratingChange: number;
  /** Matches played Monday → Sunday. */
  perDay: [number, number, number, number, number, number, number];
};

export const avatarColors = {
  peach: "#f0b07a",
  blue: "#9cc2f5",
  pink: "#f0a3b8",
  sand: "#e5d38f",
  lime: "#e2fb6c",
} as const;

const { peach, blue, pink, sand } = avatarColors;

export const sampleEvents: EventSummary[] = [
  {
    id: "evt_saturday_smash",
    title: "Saturday Smash Session",
    format: "Singles",
    startsAt: "2026-10-10T19:30:00+05:30",
    venue: "Koramangala Sports Arena",
    distanceKm: 1.2,
    spotsLeft: 6,
    feeInr: 350,
    players: [
      { initials: "RM", color: blue },
      { initials: "SK", color: pink },
      { initials: "DP", color: peach },
    ],
  },
  {
    id: "evt_sunday_ladder",
    title: "Sunday Doubles Ladder",
    format: "Doubles",
    startsAt: "2026-10-11T06:00:00+05:30",
    venue: "HSR Shuttle Club",
    distanceKm: 3.4,
    spotsLeft: 2,
    feeInr: 300,
    players: [
      { initials: "TN", color: peach },
      { initials: "IJ", color: blue },
    ],
  },
  {
    id: "evt_midweek_rally",
    title: "Midweek Rally Night",
    format: "Singles",
    startsAt: "2026-10-14T20:00:00+05:30",
    venue: "Indiranagar Badminton Hub",
    distanceKm: 4.8,
    spotsLeft: 9,
    feeInr: 250,
    players: [
      { initials: "MK", color: sand },
      { initials: "KB", color: pink },
      { initials: "AS", color: blue },
    ],
  },
];

export const sampleMomentum: WeeklyMomentum = {
  matches: 4,
  wins: 3,
  rating: 4.2,
  ratingChange: 0.12,
  perDay: [1, 2, 0, 1, 0, 3, 0],
};
