export interface NavLocation {
  readonly path: string;
  readonly line: number;
  readonly column: number;
  readonly timestamp: number;
}

export interface NavHistorySnapshot {
  readonly entries: readonly NavLocation[];
  readonly cursor: number;
  readonly suppress: boolean;
}

export interface NavHistoryApi {
  getSnapshot(): NavHistorySnapshot;
  subscribe(listener: () => void): () => void;
  record(location: Omit<NavLocation, "timestamp">): void;
  back(): NavLocation | null;
  forward(): NavLocation | null;
  clear(): void;
  beginNavigation(): void;
  endNavigation(): void;
}
