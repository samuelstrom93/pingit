<script lang="ts">
  import { goto } from '$app/navigation';
  import { api } from '$lib/api/client';
  import { session } from '$lib/stores/session';
  import { Button } from '$lib/components/ui/button';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';

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

<svelte:head><title>Settings · Pingit</title></svelte:head>

<div class="mx-auto max-w-xl space-y-6">
  <h1 class="text-3xl font-bold">Settings</h1>

  <Card>
    <CardHeader><CardTitle>Profile</CardTitle></CardHeader>
    <CardContent class="space-y-2 text-sm">
      <p><span class="text-muted-foreground">Display name:</span> <strong>{$session?.display_name ?? '—'}</strong></p>
      <p><span class="text-muted-foreground">Email:</span> {$session?.email ?? '—'}</p>
      {#if $session?.is_super_admin}
        <p class="text-xs uppercase tracking-wide text-emerald-500">Super admin</p>
        <p><a class="text-primary underline" href="/admin">Open SuperAdmin overview</a></p>
      {/if}
    </CardContent>
  </Card>

  <Card>
    <CardHeader><CardTitle>Session</CardTitle></CardHeader>
    <CardContent>
      <Button variant="outline" onclick={logout}>Sign out</Button>
    </CardContent>
  </Card>
</div>
