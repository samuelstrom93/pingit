<script lang="ts">
  import { api } from '$lib/api/client';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';

  type Space = { id: string; name: string; created_at: number; deleted_at: number | null };
  type Match = { id: string; space_id: string; kind: string; status: string; updated_at: number };
  type Tournament = { id: string; space_id: string; name: string; format: string; status: string; updated_at: number };
  type Overview = { counts: Record<string, number>; spaces: Space[]; matches: Match[]; tournaments: Tournament[] };

  let overview = $state<Overview | null>(null);
  let error = $state('');

  $effect(() => {
    api<Overview>('/api/admin/overview')
      .then((r) => (overview = r))
      .catch((err) => (error = err instanceof Error ? err.message : 'Failed to load admin overview'));
  });
</script>

<svelte:head><title>SuperAdmin · Pingit</title></svelte:head>

<div class="space-y-6">
  <h1 class="text-3xl font-bold">SuperAdmin overview</h1>
  {#if error}<p class="text-sm text-destructive">{error}</p>{/if}

  {#if overview}
    <div class="grid gap-3 sm:grid-cols-3 lg:grid-cols-5">
      {#each Object.entries(overview.counts) as [key, value]}
        <Card>
          <CardHeader class="pb-2"><CardTitle class="text-sm capitalize">{key.replaceAll('_', ' ')}</CardTitle></CardHeader>
          <CardContent><div class="text-2xl font-bold">{value}</div></CardContent>
        </Card>
      {/each}
    </div>

    <div class="grid gap-4 lg:grid-cols-3">
      <Card>
        <CardHeader><CardTitle>Spaces</CardTitle></CardHeader>
        <CardContent>
          <ul class="space-y-2">
            {#each overview.spaces as space (space.id)}
              <li><a class="block rounded-md border p-3 text-sm hover:bg-accent" href={`/spaces/${space.id}`}>{space.name}{space.deleted_at ? ' · deleted' : ''}</a></li>
            {/each}
          </ul>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Recent matches</CardTitle></CardHeader>
        <CardContent>
          <ul class="space-y-2">
            {#each overview.matches as match (match.id)}
              <li><a class="block rounded-md border p-3 text-sm hover:bg-accent" href={`/matches/${match.id}`}>{match.kind} · {match.status}</a></li>
            {/each}
          </ul>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Recent tournaments</CardTitle></CardHeader>
        <CardContent>
          <ul class="space-y-2">
            {#each overview.tournaments as tournament (tournament.id)}
              <li><a class="block rounded-md border p-3 text-sm hover:bg-accent" href={`/tournaments/${tournament.id}`}>{tournament.name} · {tournament.format} · {tournament.status}</a></li>
            {/each}
          </ul>
        </CardContent>
      </Card>
    </div>
  {/if}
</div>
