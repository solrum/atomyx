/**
 * Default filename for an unattended recording. Stamps the device
 * id and the session start time so concurrent sessions on the
 * same machine do not overwrite each other.
 *
 * The signature accepts the minimal shape both `MirrorSession`
 * (domain) and `MirrorSessionStatus` (state) satisfy — keeping
 * this helper in the domain layer without forcing a state-layer
 * import here.
 */
export interface RecordingPathSession {
  readonly target: { readonly id: string };
  readonly startedAt: number;
}

export function defaultRecordingPath(session: RecordingPathSession): string {
  const stamp = new Date(session.startedAt).toISOString().replace(/[:.]/g, "-");
  return `mirror-${session.target.id}-${stamp}.mp4`;
}
