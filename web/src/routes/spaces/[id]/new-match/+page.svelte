<script lang="ts">
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { api } from '$lib/api/client';
  import { Button } from '$lib/components/ui/button';
  import { Label } from '$lib/components/ui/label';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';

  type Player = { id: string; display_name: string };

  let players = $state<Player[]>([]);
  let kind = $state<'singles' | 'doubles'>('singles');
  let bestOf = $state(3);
  let pointsToWin = $state(11);
  let homeIds = $state<string[]>(['']);
  let visitorIds = $state<string[]>(['']);
  let error = $state('');
  let busy = $state(false);
  let spaceID = $derived($page.params.id);

  $effect(() => {
    homeIds = kind === 'singles' ? [''] : ['', ''];
    visitorIds = kind === 'singles' ? [''] : ['', ''];
  });

  $effect(() => {
    if (spaceID) {
      api<{ players: Player[] }>(`/api/spaces/${spaceID}/players`).then((r) => {
        players = r.players ?? [];
      });
    }
  });

  async function submit(e: Event) {
    e.preventDefault();
    error = '';
    if (homeIds.some((id) => !id) || visitorIds.some((id) => !id)) {
      error = 'Pick a player for every slot';
      return;
    }
    busy = true;
    try {
      type CreatedMatch = { match?: { id: string }; id?: string };
      const r = await api<CreatedMatch>(`/api/spaces/${spaceID}/matches`, {
        method: 'POST',
        bodyJson: {
          kind,
          home_players: homeIds,
          visitor_players: visitorIds,
          best_of: bestOf,
          points_to_win: pointsToWin
        }
      });
      const id = r.match?.id ?? r.id;
      if (id) goto(`/matches/${id}`);
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed';
    } finally {
      busy = false;
    }
  }
</script>

<svelte:head><title>New match · Pingit</title></svelte:head>

<Card class="mx-auto max-w-2xl">
  <CardHeader><CardTitle>Start a match</CardTitle></CardHeader>
  <CardContent>
    <form class="space-y-4" onsubmit={submit}>
      <div class="grid gap-3 sm:grid-cols-3">
        <div class="space-y-1.5">
          <Label for="kind">Kind</Label>
          <select id="kind" bind:value={kind} class="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm">
            <option value="singles">Singles</option>
            <option value="doubles">Doubles</option>
          </select>
        </div>
        <div class="space-y-1.5">
          <Label for="bestof">Best of</Label>
          <select id="bestof" bind:value={bestOf} class="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm">
            <option value={1}>1</option>
            <option value={3}>3</option>
            <option value={5}>5</option>
            <option value={7}>7</option>
          </select>
        </div>
        <div class="space-y-1.5">
          <Label for="points">Points to win</Label>
          <select id="points" bind:value={pointsToWin} class="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm">
            <option value={11}>11</option>
            <option value={5}>5</option>
          </select>
        </div>
      </div>

      <div class="grid gap-4 sm:grid-cols-2">
        <div class="space-y-2">
          <Label>Home side</Label>
          {#each homeIds as _, i}
            <select bind:value={homeIds[i]} class="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm">
              <option value="">— pick a player —</option>
              {#each players as p (p.id)}<option value={p.id}>{p.display_name}</option>{/each}
            </select>
          {/each}
        </div>
        <div class="space-y-2">
          <Label>Visitor side</Label>
          {#each visitorIds as _, i}
            <select bind:value={visitorIds[i]} class="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm">
              <option value="">— pick a player —</option>
              {#each players as p (p.id)}<option value={p.id}>{p.display_name}</option>{/each}
            </select>
          {/each}
        </div>
      </div>

      {#if error}<p class="text-sm text-destructive">{error}</p>{/if}
      <Button type="submit" disabled={busy}>{busy ? 'Starting…' : 'Start match'}</Button>
    </form>
  </CardContent>
</Card>
