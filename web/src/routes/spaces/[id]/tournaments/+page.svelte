<script lang="ts">
  import { page } from '$app/stores';
  import { api } from '$lib/api/client';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';

  type Tournament = { id: string; name: string; format: string; status: string };
  type Player = { id: string; display_name: string };

  let tournaments = $state<Tournament[]>([]);
  let players = $state<Player[]>([]);
  let selected = $state<Set<string>>(new Set());
  let name = $state('');
  let format = $state<'round_robin' | 'bracket' | 'groups_knockout'>('round_robin');
  let bestOf = $state(3);
  let pointsToWin = $state(11);
  let error = $state('');
  let loading = $state(true);
  let spaceID = $derived($page.params.id);

  async function load() {
    loading = true;
    try {
      const t = await api<{ tournaments: Tournament[] }>(`/api/spaces/${spaceID}/tournaments`);
      tournaments = t.tournaments ?? [];
      const p = await api<{ players: Player[] }>(`/api/spaces/${spaceID}/players`);
      players = p.players ?? [];
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    if (spaceID) load();
  });

  function toggle(id: string) {
    if (selected.has(id)) selected.delete(id);
    else selected.add(id);
    selected = new Set(selected);
  }

  async function create(e: Event) {
    e.preventDefault();
    error = '';
    if (!name.trim() || selected.size < 2) {
      error = 'Need a name and at least 2 players';
      return;
    }
    try {
      const t = await api<{ id: string }>(`/api/spaces/${spaceID}/tournaments`, {
        method: 'POST',
        bodyJson: {
          name,
          format,
          best_of: bestOf,
          points_to_win: pointsToWin,
          player_ids: Array.from(selected)
        }
      });
      await api(`/api/tournaments/${t.id}/start`, { method: 'POST' });
      window.location.href = `/tournaments/${t.id}`;
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed';
    }
  }
</script>

<svelte:head><title>Tournaments · Pingit</title></svelte:head>

<div class="space-y-6">
  <h1 class="text-3xl font-bold">Tournaments</h1>

  <Card>
    <CardHeader><CardTitle>Create tournament</CardTitle></CardHeader>
    <CardContent>
      <form class="space-y-4" onsubmit={create}>
        <div class="space-y-1.5">
          <Label for="t-name">Name</Label>
          <Input id="t-name" bind:value={name} placeholder="Summer Cup" required />
        </div>
        <div class="grid gap-3 sm:grid-cols-3">
          <div class="space-y-1.5">
            <Label for="t-format">Format</Label>
            <select id="t-format" bind:value={format} class="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm">
              <option value="round_robin">Round robin</option>
              <option value="bracket">Single-elim bracket</option>
              <option value="groups_knockout">Groups + knockout foundation</option>
            </select>
          </div>
          <div class="space-y-1.5">
            <Label for="t-bestof">Best of</Label>
            <select id="t-bestof" bind:value={bestOf} class="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm">
              <option value={1}>1</option>
              <option value={3}>3</option>
              <option value={5}>5</option>
              <option value={7}>7</option>
            </select>
          </div>
          <div class="space-y-1.5">
            <Label for="t-points">Points</Label>
            <select id="t-points" bind:value={pointsToWin} class="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm">
              <option value={11}>11</option>
              <option value={5}>5</option>
            </select>
          </div>
        </div>
        <div class="space-y-2">
          <Label>Players ({selected.size} selected)</Label>
          <div class="grid grid-cols-2 gap-2 sm:grid-cols-3">
            {#each players as p (p.id)}
              <label class="flex items-center gap-2 rounded-md border p-2 text-sm">
                <input type="checkbox" checked={selected.has(p.id)} onchange={() => toggle(p.id)} />
                {p.display_name}
              </label>
            {/each}
          </div>
        </div>
        {#if error}<p class="text-sm text-destructive">{error}</p>{/if}
        <Button type="submit">Create &amp; start</Button>
      </form>
    </CardContent>
  </Card>

  <div>
    <h2 class="mb-3 text-xl font-semibold">All tournaments</h2>
    {#if loading}
      <p class="text-sm text-muted-foreground">Loading…</p>
    {:else if tournaments.length === 0}
      <p class="text-sm text-muted-foreground">No tournaments yet.</p>
    {:else}
      <ul class="space-y-2">
        {#each tournaments as t (t.id)}
          <li>
            <a class="flex items-center justify-between rounded-md border p-3 hover:bg-accent" href={`/tournaments/${t.id}`}>
              <div>
                <div class="font-medium">{t.name}</div>
                <div class="text-xs text-muted-foreground">{t.format} · {t.status}</div>
              </div>
            </a>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</div>
