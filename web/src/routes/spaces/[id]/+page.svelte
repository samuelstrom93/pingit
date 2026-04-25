<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { api } from '$lib/api/client';
  import { Button } from '$lib/components/ui/button';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';

  type Space = { id: string; name: string; description: string | null; join_code: string | null };
  type Match = { id: string; kind: string; best_of: number; status: string; winner_side: string | null; started_at: number };
  type Tournament = { id: string; name: string; format: string; status: string };

  let space = $state<Space | null>(null);
  let matches = $state<Match[]>([]);
  let tournaments = $state<Tournament[]>([]);
  let loading = $state(true);

  $effect(() => {
    const spaceID = $page.params.id;
    (async () => {
      loading = true;
      try {
        space = await api<Space>(`/api/spaces/${spaceID}`);
        const m = await api<{ matches: Match[] }>(`/api/spaces/${spaceID}/matches?limit=10`);
        matches = m.matches ?? [];
        const t = await api<{ tournaments: Tournament[] }>(`/api/spaces/${spaceID}/tournaments`);
        tournaments = t.tournaments ?? [];
      } finally {
        loading = false;
      }
    })();
  });
</script>

<svelte:head><title>{space?.name ?? 'Space'} · Pingit</title></svelte:head>

{#if loading}
  <p class="text-sm text-muted-foreground">Loading…</p>
{:else if space}
  <div class="space-y-6">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h1 class="text-3xl font-bold">{space.name}</h1>
        {#if space.description}<p class="text-sm text-muted-foreground">{space.description}</p>{/if}
      </div>
      <div class="flex flex-wrap gap-2">
        <Button onclick={() => (window.location.href = `/spaces/${space!.id}/new-match`)}>+ New match</Button>
        <Button variant="outline" onclick={() => (window.location.href = `/spaces/${space!.id}/players`)}>Players</Button>
        <Button variant="outline" onclick={() => (window.location.href = `/spaces/${space!.id}/tournaments`)}>Tournaments</Button>
        <Button variant="outline" onclick={() => (window.location.href = `/spaces/${space!.id}/invitations`)}>Invitations</Button>
      </div>
    </div>

    <div class="grid gap-4 md:grid-cols-2">
      <Card>
        <CardHeader><CardTitle>Recent matches</CardTitle></CardHeader>
        <CardContent>
          {#if matches.length === 0}
            <p class="text-sm text-muted-foreground">No matches yet.</p>
          {:else}
            <ul class="space-y-2">
              {#each matches as m (m.id)}
                <li>
                  <a class="flex items-center justify-between rounded-md border p-3 hover:bg-accent" href={`/matches/${m.id}`}>
                    <div>
                      <div class="font-medium">{m.kind === 'doubles' ? 'Doubles' : 'Singles'} · Best of {m.best_of}</div>
                      <div class="text-xs text-muted-foreground">{new Date(m.started_at).toLocaleString()}</div>
                    </div>
                    <span class="text-xs uppercase tracking-wide text-muted-foreground">
                      {m.status === 'in_progress' ? 'Live' : m.winner_side ? `${m.winner_side} won` : 'Final'}
                    </span>
                  </a>
                </li>
              {/each}
            </ul>
          {/if}
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Tournaments</CardTitle></CardHeader>
        <CardContent>
          {#if tournaments.length === 0}
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
        </CardContent>
      </Card>
    </div>
  </div>
{/if}
