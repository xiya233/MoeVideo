export type ApiEnvelope<T> = {
  code: number;
  message: string;
  data: T;
};

export class ApiError extends Error {
  status: number;
  code?: number;

  constructor(message: string, status: number, code?: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

export type RequestOptions = {
  method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  body?: unknown;
  headers?: Record<string, string>;
  auth?: boolean;
  skipRefresh?: boolean;
};

export type RequestContext = {
  onAuthFailed: () => void;
};
