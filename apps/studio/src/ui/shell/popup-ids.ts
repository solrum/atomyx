export const POPUP_IDS = {
  wizard: "wizard",
  settings: "settings",
  recentFiles: "recent-files",
  findEverywhere: "find-everywhere",
  findInPath: "find-in-path",
  runConfigs: "run-configs",
  recentLocations: "recent-locations",
  bookmarks: "bookmarks",
  keymap: "keymap",
  fileSwitcher: "file-switcher",
} as const;

export type PopupIdKey = keyof typeof POPUP_IDS;
