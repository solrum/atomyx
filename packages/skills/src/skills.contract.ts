export interface CopyOptions {
  readonly overwrite?: boolean;
}

export interface CopyResult {
  readonly written: readonly string[];
  readonly skipped: readonly string[];
}

export interface InstalledVersionResult {
  readonly version: string | null;
  readonly current: string;
  readonly upToDate: boolean;
}

export interface SkillsApi {
  copyTo(targetDir: string, opts?: CopyOptions): Promise<CopyResult>;
  getInstalledVersion(targetDir: string): Promise<InstalledVersionResult>;
}
