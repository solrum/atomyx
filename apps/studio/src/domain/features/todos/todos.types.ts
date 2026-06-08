export interface TodoHit {
  readonly path: string;
  readonly line: number;
  readonly kind: string;
  readonly snippet: string;
}
