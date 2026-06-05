export type Prefs = {
  content_dir: string;
  theme: string;
  font: string;
  last_doc: string;
  push_remote: string;
  push_token_set: boolean;
};

export type PrefsPatch = {
  theme?: string;
  font?: string;
  last_doc?: string;
  push_remote?: string;
  push_token?: string;
};

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
