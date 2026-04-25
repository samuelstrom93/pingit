<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api/client';
  import { Button } from '$lib/components/ui/button';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';

  type Space = { id: string; name: string; description: string | null };
  type Match = {
    id: string;
    space_id: string;
    kind: string;
    best_of: number;
    status: string;
    winner_side: string | null;
    started_at: number;
  };

  let spaces = $state<Space[]>([]);
  let recentMatches = $state<Match[]>([]);
  let loading = $state(true);

  onMount(async () => {
    try {
      const sp = await api<{ spaces: Space[] }>('/api/spaces');
      spaces = sp.spaces ?? [];
      if (spaces.length > 0) {
        const m = await api<{ matches: Match[] }>(`/api/spaces/${spaces[0].id}/matches?limit=5`);
        recentMatches = m.matches ?? [];
      }
    } finally {
      loading = false;
    }
  });

  function formatStatus(m: Match): string {
    if (m.status === 'in_progress') return 'Live';
    if (m.status === 'completed') return m.winner_side ? `${m.winner_side} won` : 'Final';
    return m.status;
  }
</script>

<svelte:head><title>Pingit</title></svelte:head>

<div class="space-y-6">
  <div>
    <h1 class="text-3xl font-bold">Welcome back 🏓</h1>
    <p class="text-sm text-muted-foreground">Keep the rally moving.</p>
  </div>

  {#if loading}
    <p class="text-sm text-muted-foreground">Loading…</p>
  {:else if spaces.length === 0}
    <Card>
      <CardContent class="space-y-3 pt-6">
        <p>You aren't part of any space yet.</p>
        <Button onclick={() => (window.location.href = '/spaces')}>Create or join one</Button>
      </CardContent>
    </Card>
  {:else}
    <div class="flex flex-wrap gap-2">
      <Button onclick={() => (window.location.href = `/spaces/${spaces[0].id}/new-match`)}>
        + New match
      </Button>
      <Button variant="outline" onclick={() => (window.location.href = '/spaces')}>
        Manage spaces
      </Button>
    </div>

    <div class="grid gap-4 md:grid-cols-2">
      <Card>
        <CardHeader><CardTitle>Recent matches</CardTitle></CardHeader>
        <CardContent>
          {#if recentMatches.length === 0}
            <p class="text-sm text-muted-foreground">No matches yet.</p>
          {:else}
            <ul class="space-y-2">
              {#each recentMatches as m (m.id)}
                <li>
                  <a class="flex items-center justify-between rounded-md border p-3 hover:bg-accent" href={`/matches/${m.id}`}>
                    <div>
                      <div class="font-medium">{m.kind === 'doubles' ? 'Doubles' : 'Singles'} · Best of {m.best_of}</div>
                      <div class="text-xs text-muted-foreground">{new Date(m.started_at).toLocaleString()}</div>
                    </div>
                    <span class="text-xs uppercase tracking-wide text-muted-foreground">{formatStatus(m)}</span>
                  </a>
                </li>
              {/each}
            </ul>
          {/if}
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Your spaces</CardTitle></CardHeader>
        <CardContent>
          <ul class="space-y-2">
            {#each spaces as s (s.id)}
              <li>
                <a class="flex items-center justify-between rounded-md border p-3 hover:bg-accent" href={`/spaces/${s.id}`}>
                  <span class="font-medium">{s.name}</span>
                  {#if s.description}<span class="text-xs text-muted-foreground">{s.description}</span>{/if}
                </a>
              </li>
            {/each}
          </ul>
        </CardContent>
      </Card>
    </div>
  {/if}
</div>
