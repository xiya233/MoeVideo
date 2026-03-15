"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

import { createApiClient } from "@/lib/api/client";
import { ApiError } from "@/lib/api/types";
import type { RequestOptions } from "@/lib/api/types";
import type { LoginOrRegisterData } from "@/lib/dto";
import type { AuthSession, AuthUser } from "@/lib/auth/session";
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
  login: (payload: { account: string; password: string; captcha_id: string; captcha_code: string }) => Promise<void>;
  register: (payload: {
    username: string;
    email: string;
    password: string;
    password_confirm: string;
    captcha_id: string;
    captcha_code: string;
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

  const clearSession = useCallback(() => {
    setSession(null);
  }, []);

  const api = useMemo(
    () =>
      createApiClient({
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

  useEffect(() => {
    let cancelled = false;

    const bootstrap = async () => {
      try {
        const me = await api.request<AuthUser>("/users/me", { auth: true });
        if (!cancelled) {
          setSession({ user: me });
        }
      } catch (error) {
        if (!(error instanceof ApiError) || error.status !== 401) {
          // Ignore bootstrap failures; app can still work in anonymous mode.
        }
        if (!cancelled) {
          setSession(null);
        }
      } finally {
        if (!cancelled) {
          setReady(true);
        }
      }
    };

    void bootstrap();
    return () => {
      cancelled = true;
    };
  }, [api]);

  const syncCurrentUser = useCallback(async () => {
    if (!session) {
      return;
    }
    try {
      const me = await request<AuthUser>("/users/me", { auth: true });
      setSession({ user: me });
    } catch {
      // Ignore sync errors. Request layer handles auth failures.
    }
  }, [request, session]);

  const refreshUser = useCallback(async () => {
    await syncCurrentUser();
  }, [syncCurrentUser]);

  const login = useCallback(
    async (payload: { account: string; password: string; captcha_id: string; captcha_code: string }) => {
      const data = await request<LoginOrRegisterData>("/auth/login", {
        method: "POST",
        body: payload,
        auth: false,
      });
      const mapped = mapLoginRegisterData(data);
      setSession({
        user: mapped.user,
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
      captcha_id: string;
      captcha_code: string;
    }) => {
      const data = await request<LoginOrRegisterData>("/auth/register", {
        method: "POST",
        body: payload,
        auth: false,
      });
      const mapped = mapLoginRegisterData(data);
      setSession({
        user: mapped.user,
      });
      setAuthDialogOpen(false);
    },
    [request],
  );

  const logout = useCallback(async () => {
    try {
      await request<{ revoked: boolean }>("/auth/logout", {
        method: "POST",
        auth: true,
      });
    } catch {
      // even if logout API fails, clear local session
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
