import { writable, get } from "svelte/store";
import type { Comment, CommentStatus } from "./types";
import * as api from "./comments-api";

export const comments = writable<Comment[]>([]);
export const commentsLoading = writable<boolean>(false);
export const commentsError = writable<string>("");

// requestedLine is bumped when something (a read-view marker, a jump link)
// wants the sidebar to surface a given source line. The payload carries a
// nonce so re-requesting the same line still triggers the sidebar effect.
export const requestedLine = writable<{ line: number; nonce: number } | null>(null);

let lineNonce = 0;
export function requestLine(line: number): void {
  requestedLine.set({ line, nonce: ++lineNonce });
}

let currentPath = "";

export async function loadForPath(path: string): Promise<void> {
  currentPath = path;
  commentsLoading.set(true);
  commentsError.set("");
  try {
    const list = await api.listComments(path);
    if (currentPath === path) comments.set(list);
  } catch (err) {
    commentsError.set(err instanceof Error ? err.message : "Failed to load comments");
    comments.set([]);
  } finally {
    commentsLoading.set(false);
  }
}

export async function addComment(input: api.CreateCommentInput): Promise<Comment> {
  const c = await api.createComment(input);
  if (c.path === currentPath) {
    comments.update((list) => [...list, c]);
  }
  return c;
}

export async function setStatus(id: number, status: CommentStatus): Promise<void> {
  const updated = await api.updateComment(id, { status });
  comments.update((list) => list.map((c) => (c.id === id ? updated : c)));
}

export async function updateBody(id: number, body: string): Promise<void> {
  const updated = await api.updateComment(id, { body });
  comments.update((list) => list.map((c) => (c.id === id ? updated : c)));
}

export function refreshAfterSave(): void {
  if (!currentPath) return;
  void loadForPath(currentPath);
}

export function snapshot(): Comment[] {
  return get(comments);
}
