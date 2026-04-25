<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { api } from '$lib/api/client';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';

  type Player = { id: string; display_name: string; user_id: string | null };

  let players = $state<Player[]>([]);
  let loading = $state(true);
  let newName = $state('');
  let error = $state('');
  let spaceID = $derived($page.params.id);

  async function load() {
    loading = true;
    try {
      const r = await api<{ players: Player[] }>(`/api/spaces/${spaceID}/players`);
      players = r.players ?? [];
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    if (spaceID) load();
  });

  async function create(e: Event) {
    e.preventDefault();
    error = '';
    if (!newName.trim()) return;
    try {
      await api(`/api/spaces/${spaceID}/players`, { method: 'POST', bodyJson: { display_name: newName } });
      newName = '';
      await load();
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed';
    }
  }

  async function remove(playerID: string) {
    if (!confirm('Remove this player?')) return;
    try {
      await api(`/api/players/${playerID}`, { method: 'DELETE' });
      await load();
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed';
    }
  }
</script>

<svelte:head><title>Players · Pingit</title></svelte:head>

<div class="space-y-6">
  <h1 class="text-3xl font-bold">Players</h1>

  <Card>
    <CardHeader><CardTitle>Add anonymous player</CardTitle></CardHeader>
    <CardContent>
      <form class="flex gap-2" onsubmit={create}>
        <div class="flex-1 space-y-1.5">
          <Label for="name">Display name</Label>
          <Input id="name" bind:value={newName} placeholder="Pappa" />
        </div>
        <Button type="submit" class="self-end">Add</Button>
      </form>
      {#if error}<p class="mt-2 text-sm text-destructive">{error}</p>{/if}
    </CardContent>
  </Card>

  <Card>
    <CardHeader><CardTitle>All players</CardTitle></CardHeader>
    <CardContent>
      {#if loading}
        <p class="text-sm text-muted-foreground">Loading…</p>
      {:else if players.length === 0}
        <p class="text-sm text-muted-foreground">No players yet.</p>
      {:else}
        <ul class="space-y-2">
          {#each players as p (p.id)}
            <li class="flex items-center justify-between rounded-md border p-3">
              <div>
                <div class="font-medium">{p.display_name}</div>
                <div class="text-xs text-muted-foreground">{p.user_id ? 'User-linked' : 'Anonymous'}</div>
              </div>
              <Button size="sm" variant="outline" onclick={() => remove(p.id)}>Remove</Button>
            </li>
          {/each}
        </ul>
      {/if}
    </CardContent>
  </Card>
</div>
