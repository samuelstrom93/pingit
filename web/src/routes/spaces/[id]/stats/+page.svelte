<script lang="ts">
  import { page } from '$app/stores';
  import { api } from '$lib/api/client';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';

  type Bucket = { matches: number; wins: number; losses: number; points_for: number; points_against: number; average_points?: number };
  type PlayerStats = {
    player_id: string;
    display_name: string;
    matches: number;
    wins: number;
    losses: number;
    win_rate: number;
    points_for: number;
    points_against: number;
    average_points: number;
    breakdown_by_kind: Record<string, Bucket>;
    breakdown_by_set: Record<string, Bucket>;
  };
  type Stats = { players: PlayerStats[]; breakdown_by_kind: Record<string, Bucket>; breakdown_by_set: Record<string, Bucket> };

  let stats = $state<Stats | null>(null);
  let error = $state('');
  let spaceID = $derived($page.params.id);

  $effect(() => {
    if (spaceID) {
      api<Stats>(`/api/spaces/${spaceID}/stats`)
        .then((r) => (stats = r))
        .catch((err) => (error = err instanceof Error ? err.message : 'Failed to load statistics'));
    }
  });

  function pct(value: number) {
    return `${Math.round(value * 100)}%`;
  }
</script>

<svelte:head><title>Statistics · Pingit</title></svelte:head>

<div class="space-y-6">
  <h1 class="text-3xl font-bold">Statistics</h1>
  {#if error}<p class="text-sm text-destructive">{error}</p>{/if}

  {#if stats}
    <div class="grid gap-4 lg:grid-cols-2">
      <Card>
        <CardHeader><CardTitle>Breakdown by match type</CardTitle></CardHeader>
        <CardContent>
          {#if Object.keys(stats.breakdown_by_kind).length === 0}
            <p class="text-sm text-muted-foreground">No completed matches yet.</p>
          {:else}
            <ul class="space-y-2 text-sm">
              {#each Object.entries(stats.breakdown_by_kind) as [kind, b]}
                <li class="rounded-md border p-3"><strong class="capitalize">{kind}</strong>: {b.matches} player-results · avg {b.average_points?.toFixed(1) ?? '0.0'} pts</li>
              {/each}
            </ul>
          {/if}
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Breakdown by set type</CardTitle></CardHeader>
        <CardContent>
          {#if Object.keys(stats.breakdown_by_set).length === 0}
            <p class="text-sm text-muted-foreground">No completed matches yet.</p>
          {:else}
            <ul class="space-y-2 text-sm">
              {#each Object.entries(stats.breakdown_by_set) as [setType, b]}
                <li class="rounded-md border p-3"><strong>{setType.replace('_', ' to ')}</strong>: {b.matches} player-results · avg {b.average_points?.toFixed(1) ?? '0.0'} pts</li>
              {/each}
            </ul>
          {/if}
        </CardContent>
      </Card>
    </div>

    <Card>
      <CardHeader><CardTitle>Player stats</CardTitle></CardHeader>
      <CardContent>
        {#if stats.players.length === 0}
          <p class="text-sm text-muted-foreground">No completed matches yet.</p>
        {:else}
          <div class="overflow-x-auto">
            <table class="w-full text-sm">
              <thead class="text-left text-muted-foreground">
                <tr><th class="py-2">Player</th><th>W-L</th><th>Win %</th><th>Avg pts</th><th>Points</th><th>Types</th></tr>
              </thead>
              <tbody>
                {#each stats.players as p (p.player_id)}
                  <tr class="border-t">
                    <td class="py-2 font-medium">{p.display_name}</td>
                    <td>{p.wins}-{p.losses}</td>
                    <td>{pct(p.win_rate)}</td>
                    <td>{p.average_points.toFixed(1)}</td>
                    <td>{p.points_for}-{p.points_against}</td>
                    <td class="text-xs text-muted-foreground">{Object.entries(p.breakdown_by_kind).map(([kind, b]) => `${kind}: ${b.wins}-${b.losses}`).join(' · ')}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </CardContent>
    </Card>
  {/if}
</div>
