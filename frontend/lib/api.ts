export type ApiEnvelope<T> = {
  code: number;
  message: string;
  data: T;
};

const API_BASE = process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080/api/v1";

export async function apiGet<T>(path: string): Promise<T | null> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: "GET",
    cache: "no-store",
  });

  if (!res.ok) {
    return null;
  }

  const body = (await res.json()) as ApiEnvelope<T>;
  return body.data;
}
