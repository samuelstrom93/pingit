import { writable } from 'svelte/store';

export type SessionUser = {
  id: string;
  email: string;
  display_name: string;
  avatar_url: string | null;
  is_super_admin: boolean;
  created_at: number;
};

export const session = writable<SessionUser | null>(null);
export const sessionLoaded = writable(false);
