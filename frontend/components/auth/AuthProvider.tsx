"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";

import { createApiClient } from "@/lib/api/client";
import type { RequestOptions } from "@/lib/api/types";
import type { LoginOrRegisterData } from "@/lib/dto";
import { loadSession, saveSession, type AuthSession, type AuthUser } from "@/lib/auth/session";
import { mapLoginRegisterData } from "@/lib/dto/mappers";

type AuthDialogMode = "login" | "register";

type AuthContextValue = {
  ready: boolean;
  session: AuthSession | null;
  user: AuthUser | null;
  isAuthDialogOpen: boolean;
  authDialogMode: AuthDialogMode;
  openAuthDialog: (mode?: AuthDialogMode) => void;
  closeAuthDialog: () => void;
  login: (payload: { account: string; password: string }) => Promise<void>;
  register: (payload: {
    username: string;
    email: string;
    password: string;
    password_confirm: string;
  }) => Promise<void>;
  logout: () => Promise<void>;
  refreshUser: () => Promise<void>;
  request: <T>(path: string, options?: RequestOptions) => Promise<T>;
  uploadBinary: (url: string, file: File, headers?: Record<string, string>) => Promise<void>;
};

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [ready, setReady] = useState(false);
  const [session, setSession] = useState<AuthSession | null>(null);
  const [isAuthDialogOpen, setAuthDialogOpen] = useState(false);
  const [authDialogMode, setAuthDialogMode] = useState<AuthDialogMode>("login");

  const sessionRef = useRef<AuthSession | null>(null);
  sessionRef.current = session;

  useEffect(() => {
    const existing = loadSession();
    if (existing) {
      setSession(existing);
    }
    setReady(true);
  }, []);

  useEffect(() => {
    if (!ready) {
      return;
    }
    saveSession(session);
  }, [ready, session]);

  const clearSession = useCallback(() => {
    setSession(null);
    saveSession(null);
  }, []);

  const api = useMemo(
    () =>
      createApiClient({
        getAccessToken: () => sessionRef.current?.tokens.accessToken ?? null,
        getRefreshToken: () => sessionRef.current?.tokens.refreshToken ?? null,
        onTokensRefreshed: (tokens) => {
          setSession((prev) => (prev ? { ...prev, tokens } : prev));
        },
        onAuthFailed: () => {
          clearSession();
        },
      }),
    [clearSession],
  );

  const request = useCallback(
    async <T,>(path: string, options?: RequestOptions): Promise<T> => {
      return api.request<T>(path, options);
    },
    [api],
  );

  const syncCurrentUser = useCallback(async () => {
    if (!sessionRef.current) {
      return;
    }
    try {
      const me = await request<AuthUser>("/users/me", { auth: true });
      setSession((prev) => (prev ? { ...prev, user: { ...prev.user, ...me } } : prev));
    } catch {
      // Ignore sync errors. Request layer handles auth failures.
    }
  }, [request]);

  const refreshUser = useCallback(async () => {
    await syncCurrentUser();
  }, [syncCurrentUser]);

  useEffect(() => {
    if (!ready || !session?.tokens.accessToken) {
      return;
    }
    void syncCurrentUser();
  }, [ready, session?.tokens.accessToken, syncCurrentUser]);

  const login = useCallback(
    async (payload: { account: string; password: string }) => {
      const data = await request<LoginOrRegisterData>("/auth/login", {
        method: "POST",
        body: payload,
        auth: false,
      });
      const mapped = mapLoginRegisterData(data);
      setSession({
        user: mapped.user,
        tokens: {
          accessToken: mapped.tokens.access_token,
          accessExpiresAt: mapped.tokens.access_expires_at,
          refreshToken: mapped.tokens.refresh_token,
          refreshExpiresAt: mapped.tokens.refresh_expires_at,
        },
      });
      setAuthDialogOpen(false);
    },
    [request],
  );

  const register = useCallback(
    async (payload: {
      username: string;
      email: string;
      password: string;
      password_confirm: string;
    }) => {
      const data = await request<LoginOrRegisterData>("/auth/register", {
        method: "POST",
        body: payload,
        auth: false,
      });
      const mapped = mapLoginRegisterData(data);
      setSession({
        user: mapped.user,
        tokens: {
          accessToken: mapped.tokens.access_token,
          accessExpiresAt: mapped.tokens.access_expires_at,
          refreshToken: mapped.tokens.refresh_token,
          refreshExpiresAt: mapped.tokens.refresh_expires_at,
        },
      });
      setAuthDialogOpen(false);
    },
    [request],
  );

  const logout = useCallback(async () => {
    const current = sessionRef.current;
    if (current) {
      try {
        await request<{ revoked: boolean }>("/auth/logout", {
          method: "POST",
          auth: true,
          body: { refresh_token: current.tokens.refreshToken },
        });
      } catch {
        // even if logout API fails, clear local session
      }
    }
    clearSession();
  }, [clearSession, request]);

  const openAuthDialog = useCallback((mode: AuthDialogMode = "login") => {
    setAuthDialogMode(mode);
    setAuthDialogOpen(true);
  }, []);

  const closeAuthDialog = useCallback(() => {
    setAuthDialogOpen(false);
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({
      ready,
      session,
      user: session?.user ?? null,
      isAuthDialogOpen,
      authDialogMode,
      openAuthDialog,
      closeAuthDialog,
      login,
      register,
      logout,
      refreshUser,
      request,
      uploadBinary: api.uploadBinary,
    }),
    [
      ready,
      session,
      isAuthDialogOpen,
      authDialogMode,
      openAuthDialog,
      closeAuthDialog,
      login,
      register,
      logout,
      refreshUser,
      request,
      api.uploadBinary,
    ],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error("useAuth must be used within AuthProvider");
  }
  return ctx;
}
