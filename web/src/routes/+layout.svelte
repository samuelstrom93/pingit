<script lang="ts">
  import '../app.css';
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { page } from '$app/stores';
  import { api, ApiError } from '$lib/api/client';
  import { session, sessionLoaded, type SessionUser } from '$lib/stores/session';
  import { Button } from '$lib/components/ui/button';

  let { children } = $props();

  const PUBLIC_PATHS = ['/login', '/auth/callback'];

  function isPublic(pathname: string): boolean {
    return PUBLIC_PATHS.some((p) => pathname === p || pathname.startsWith(`${p}/`));
  }

  onMount(async () => {
    try {
      const user = await api<SessionUser>('/api/me');
      session.set(user);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        session.set(null);
        if (!isPublic($page.url.pathname)) {
          goto('/login');
        }
      }
    } finally {
      sessionLoaded.set(true);
    }
  });

  async function logout() {
    try {
      await api('/api/auth/logout', { method: 'POST' });
    } catch {
      // ignore
    }
    session.set(null);
    goto('/login');
  }
</script>

<div class="min-h-screen bg-background text-foreground">
  {#if $session}
    <header class="border-b">
      <div class="container mx-auto flex h-14 items-center justify-between px-4">
        <a href="/" class="text-lg font-bold">🏓 Pingit</a>
        <nav class="flex items-center gap-2 text-sm">
          <a class="hover:underline" href="/spaces">Spaces</a>
          <a class="hover:underline" href="/settings">Settings</a>
          <Button size="sm" variant="outline" onclick={logout}>Sign out</Button>
        </nav>
      </div>
    </header>
  {/if}
  <main class="container mx-auto px-4 py-6">
    {@render children?.()}
  </main>
</div>
