export type AuthTokens = {
  accessToken: string;
  accessExpiresAt: string;
  refreshToken: string;
  refreshExpiresAt: string;
};

export type AuthUser = {
  id: string;
  username: string;
  email?: string;
  role?: string;
  bio?: string;
  avatar_url?: string;
  followers_count?: number;
  following_count?: number;
};

export type AuthSession = {
  user: AuthUser;
  tokens: AuthTokens;
};

const STORAGE_KEY = "moevideo.session.v1";

function isBrowser(): boolean {
  return typeof window !== "undefined";
}

export function loadSession(): AuthSession | null {
  if (!isBrowser()) {
    return null;
  }
  const raw = window.localStorage.getItem(STORAGE_KEY);
  if (!raw) {
    return null;
  }
  try {
    return JSON.parse(raw) as AuthSession;
  } catch {
    window.localStorage.removeItem(STORAGE_KEY);
    return null;
  }
}

export function saveSession(session: AuthSession | null): void {
  if (!isBrowser()) {
    return;
  }
  if (!session) {
    window.localStorage.removeItem(STORAGE_KEY);
    return;
  }
  window.localStorage.setItem(STORAGE_KEY, JSON.stringify(session));
}
