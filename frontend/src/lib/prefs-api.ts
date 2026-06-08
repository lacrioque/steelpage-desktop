export type Prefs = {
  content_dir: string;
  theme: string;
  font: string;
  last_doc: string;
  push_remote: string;
  push_token_set: boolean;
  server_url: string;
  server_token_set: boolean;
};

export type PrefsPatch = {
  theme?: string;
  font?: string;
  last_doc?: string;
  push_remote?: string;
  push_token?: string;
  server_url?: string;
  server_token?: string;
};

export type RemoteUser = { id: number; display_name: string };

export type Connection = {
  mode: "local" | "server";
  server_url?: string;
  user?: RemoteUser;
};

export type TestResult = { ok: boolean; user?: RemoteUser; error?: string };

export async function getPrefs(): Promise<Prefs> {
  const res = await fetch("/api/prefs");
  if (!res.ok) throw new Error(`Failed to load preferences (${res.status})`);
  return res.json();
}

export async function patchPrefs(patch: PrefsPatch): Promise<Prefs> {
  const res = await fetch("/api/prefs", {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(patch),
  });
  if (!res.ok) {
    let msg = `Failed to save preferences (${res.status})`;
    try {
      const body = await res.json();
      if (body?.error) msg = body.error;
    } catch {
      // ignore
    }
    throw new Error(msg);
  }
  return res.json();
}

export async function getConnection(): Promise<Connection> {
  const res = await fetch("/api/connection");
  if (!res.ok) throw new Error(`Failed to load connection (${res.status})`);
  return res.json();
}

export async function testConnection(url: string, token: string): Promise<TestResult> {
  const res = await fetch("/api/connection/test", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ url, token }),
  });
  if (!res.ok) return { ok: false, error: `Test failed (${res.status})` };
  return res.json();
}
