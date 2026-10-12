// Ratings arrive with the matcher (design doc, build step 6). Until then every
// player is at the starting rating, which is also what the server checks
// rating bands against.
export const STARTING_RATING = 1200;

/** How far apart the matcher tries to keep a pair (design doc, section 6). */
export const PAIRING_GAP = 200;
