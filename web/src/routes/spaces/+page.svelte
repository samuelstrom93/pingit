<script lang="ts">
  import { onMount } from 'svelte';
  import { ApiError, api } from '$lib/api/client';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';

  type Space = { id: string; name: string; description: string | null; join_code: string | null };

  let spaces = $state<Space[]>([]);
  let loading = $state(true);
  let createName = $state('');
  let createDesc = $state('');
  let joinCode = $state('');
  let joinMessage = $state('');
  let error = $state('');
  let success = $state('');

  async function load() {
    loading = true;
    try {
      const r = await api<{ spaces: Space[] }>('/api/spaces');
      spaces = r.spaces ?? [];
    } finally {
      loading = false;
    }
  }

  onMount(load);

  async function create(e: Event) {
    e.preventDefault();
    error = '';
    if (!createName.trim()) return;
    try {
      await api('/api/spaces', { method: 'POST', bodyJson: { name: createName, description: createDesc } });
      createName = '';
      createDesc = '';
      await load();
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed';
    }
  }

  async function join(e: Event) {
    e.preventDefault();
    error = '';
    success = '';
    if (!joinCode.trim()) return;
    const code = encodeURIComponent(joinCode.trim());
    try {
      await api(`/api/spaces/join/${code}`, { method: 'POST' });
      joinCode = '';
      joinMessage = '';
      await load();
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        try {
          await api(`/api/spaces/join/${code}/request`, { method: 'POST', bodyJson: { message: joinMessage } });
          success = 'Space is invite-only — join request sent to the admins.';
          joinCode = '';
          joinMessage = '';
          return;
        } catch (requestErr) {
          error = requestErr instanceof Error ? requestErr.message : 'Failed to request access';
          return;
        }
      }
      error = err instanceof Error ? err.message : 'Failed';
    }
  }
</script>

<svelte:head><title>Spaces · Pingit</title></svelte:head>

<div class="space-y-6">
  <h1 class="text-3xl font-bold">Spaces</h1>

  <div class="grid gap-4 md:grid-cols-2">
    <Card>
      <CardHeader><CardTitle>Create a space</CardTitle></CardHeader>
      <CardContent>
        <form class="space-y-3" onsubmit={create}>
          <div class="space-y-1.5">
            <Label for="name">Name</Label>
            <Input id="name" bind:value={createName} placeholder="Bläckfiskligan" required />
          </div>
          <div class="space-y-1.5">
            <Label for="desc">Description (optional)</Label>
            <Input id="desc" bind:value={createDesc} />
          </div>
          <Button type="submit">Create</Button>
        </form>
      </CardContent>
    </Card>

    <Card>
      <CardHeader><CardTitle>Join with code</CardTitle></CardHeader>
      <CardContent>
        <form class="space-y-3" onsubmit={join}>
          <div class="space-y-1.5">
            <Label for="code">Join code</Label>
            <Input id="code" bind:value={joinCode} placeholder="abc12345" />
          </div>
          <div class="space-y-1.5">
            <Label for="join-message">Message to admin (optional)</Label>
            <Input id="join-message" bind:value={joinMessage} placeholder="Let me in" />
          </div>
          <Button type="submit" variant="outline">Join or request access</Button>
        </form>
      </CardContent>
    </Card>
  </div>

  {#if error}<p class="text-sm text-destructive">{error}</p>{/if}
  {#if success}<p class="text-sm text-emerald-500">{success}</p>{/if}

  <div>
    <h2 class="mb-3 text-xl font-semibold">Your spaces</h2>
    {#if loading}
      <p class="text-sm text-muted-foreground">Loading…</p>
    {:else if spaces.length === 0}
      <p class="text-sm text-muted-foreground">No spaces yet.</p>
    {:else}
      <ul class="space-y-2">
        {#each spaces as s (s.id)}
          <li>
            <a class="block rounded-md border p-3 hover:bg-accent" href={`/spaces/${s.id}`}>
              <div class="font-medium">{s.name}</div>
              {#if s.description}<div class="text-xs text-muted-foreground">{s.description}</div>{/if}
              {#if s.join_code}<div class="mt-1 text-xs text-muted-foreground">Join code: <code>{s.join_code}</code></div>{/if}
            </a>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</div>
