import { writable } from 'svelte/store';

export type SessionUser = {
  id: string;
  email: string;
  display_name: string;
};

export const session = writable<SessionUser | null>(null);

