import { writable, get } from "svelte/store";

// Desktop build: there is exactly one local user and no auth layer. The
// stores keep their server-era shape so consumers ($me checks in
// ArchiveTree, CommentsSidebar, AddCommentModal, DocumentView) work
// unchanged — they are simply always "signed in".
export type Me = {
  id: number;
  display_name: string;
};

export const me = writable<Me | null>({ id: 1, display_name: "You" });
export const meLoaded = writable<boolean>(true);

export async function refreshMe(): Promise<Me | null> {
  return get(me);
}
