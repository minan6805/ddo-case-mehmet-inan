export type User = {
  id: number;
  email: string;
  display_name: string;
  role: string;
};

export class APIError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

export async function request<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 8000);
  try {
    const response = await fetch(`/api${path}`, {
      ...options,
      headers: options.body
        ? { 'Content-Type': 'application/json', ...options.headers }
        : options.headers,
      credentials: 'same-origin',
      signal: controller.signal,
    });
    if (!response.ok) {
      const body = await response.json().catch(() => null);
      throw new APIError(
        response.status,
        body?.error?.message ?? 'İşlem tamamlanamadı. Lütfen tekrar deneyin.',
      );
    }
    if (response.status === 204) return undefined as T;
    return (await response.json()) as T;
  } catch (error) {
    if (error instanceof APIError) throw error;
    throw new Error(
      'Sunucuya ulaşılamadı. Bağlantıyı kontrol edip tekrar deneyin.',
    );
  } finally {
    clearTimeout(timer);
  }
}

export function errorMessage(
  error: unknown,
  fallback = 'İşlem tamamlanamadı.',
): string {
  return error instanceof Error ? error.message : fallback;
}
