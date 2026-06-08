/**
 * Score how well `name` matches `query` for the file-switcher
 * picker. Higher is better; a negative score means no match and
 * callers should drop the row.
 *
 * Tiering (case-insensitive):
 *
 *   - Exact equality:           1000
 *   - Prefix match:              500 minus extra-length penalty
 *   - Substring match:           250 minus position and length penalty
 *   - Subsequence (typed chars
 *     all appear in order):      100 minus gap penalty
 *   - No match:                   -1
 *
 * Subsequence matching tolerates skipped characters between the
 * first and last matched position; gaps before the first match
 * are not counted so leading prefixes outside the match window
 * do not bias the score.
 */
export function fuzzyScore(query: string, name: string): number {
  const q = query.toLowerCase();
  const n = name.toLowerCase();
  if (n === q) return 1000;
  if (n.startsWith(q)) return 500 - (n.length - q.length);
  const idx = n.indexOf(q);
  if (idx >= 0) return 250 - idx - (n.length - q.length);
  let qi = 0;
  let gaps = 0;
  for (let i = 0; i < n.length && qi < q.length; i++) {
    if (n[i] === q[qi]) qi++;
    else if (qi > 0) gaps++;
  }
  if (qi < q.length) return -1;
  return 100 - gaps;
}
