export type ApiOptions = RequestInit & {
  bodyJson?: unknown;
};

export async function api<T>(path: string, options: ApiOptions = {}): Promise<T> {
  const headers = new Headers(options.headers);
  headers.set('Content-Type', 'application/json');

  const response = await fetch(path, {
    ...options,
    headers,
    credentials: 'include',
    body: options.bodyJson ? JSON.stringify(options.bodyJson) : options.body
  });

  if (!response.ok) {
    const payload = await response.json().catch(() => ({ error: 'Request failed' }));
    throw new Error(payload.error ?? 'Request failed');
  }

  if (response.status === 204) {
    return undefined as T;
  }

  return response.json() as Promise<T>;
}

