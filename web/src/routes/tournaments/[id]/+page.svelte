<script lang="ts">
  import { page } from '$app/stores';
  import { api } from '$lib/api/client';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';

  type Tournament = {
    id: string;
    name: string;
    format: 'round_robin' | 'bracket';
    status: 'setup' | 'in_progress' | 'completed';
    winner_player_id: string | null;
  };
  type Match = {
    id: string;
    kind: string;
    status: string;
    winner_side: string | null;
    tournament_bracket_round: number | null;
    started_at: number;
  };
  type Player = { id: string; display_name: string };
  type Standing = {
    player_id: string;
    wins: number;
    losses: number;
    sets_won: number;
    sets_lost: number;
    points_won: number;
    points_lost: number;
    points_ratio: number;
    rank: number;
  };

  let tournament = $state<Tournament | null>(null);
  let players = $state<Player[]>([]);
  let matches = $state<Match[]>([]);
  let standings = $state<Standing[]>([]);
  let loading = $state(true);
  let tournamentID = $derived($page.params.id);

  $effect(() => {
    if (!tournamentID) return;
    (async () => {
      loading = true;
      try {
        const detail = await api<{
          tournament: Tournament;
          players: Player[];
          matches: Match[];
          standings?: Standing[];
        }>(`/api/tournaments/${tournamentID}`);
        tournament = detail.tournament;
        players = detail.players ?? [];
        matches = detail.matches ?? [];
        if (detail.standings) standings = detail.standings;
        if (tournament?.format === 'round_robin' && standings.length === 0) {
          try {
            const s = await api<{ standings: Standing[] }>(`/api/tournaments/${tournamentID}/standings`);
            standings = s.standings ?? [];
          } catch {
            // ignore
          }
        }
      } finally {
        loading = false;
      }
    })();
  });

  function playerName(id: string | null): string {
    if (!id) return '—';
    return players.find((p) => p.id === id)?.display_name ?? id.slice(0, 6);
  }
</script>

<svelte:head><title>{tournament?.name ?? 'Tournament'} · Pingit</title></svelte:head>

{#if loading}
  <p class="text-sm text-muted-foreground">Loading…</p>
{:else if tournament}
  <div class="space-y-6">
    <div>
      <h1 class="text-3xl font-bold">{tournament.name}</h1>
      <p class="text-sm text-muted-foreground">
        {tournament.format === 'round_robin' ? 'Round robin' : 'Single-elim bracket'} ·
        {tournament.status}
        {#if tournament.winner_player_id}· Winner: <strong>{playerName(tournament.winner_player_id)}</strong>{/if}
      </p>
    </div>

    {#if tournament.format === 'round_robin' && standings.length}
      <Card>
        <CardHeader><CardTitle>Standings</CardTitle></CardHeader>
        <CardContent>
          <table class="w-full text-sm">
            <thead>
              <tr class="border-b text-left text-xs uppercase text-muted-foreground">
                <th class="py-2">#</th>
                <th class="py-2">Player</th>
                <th class="py-2">W-L</th>
                <th class="py-2">Sets</th>
                <th class="py-2">Pts ratio</th>
              </tr>
            </thead>
            <tbody>
              {#each standings as s (s.player_id)}
                <tr class="border-b last:border-0">
                  <td class="py-2">{s.rank}</td>
                  <td class="py-2">{playerName(s.player_id)}</td>
                  <td class="py-2 font-mono">{s.wins}-{s.losses}</td>
                  <td class="py-2 font-mono">{s.sets_won}-{s.sets_lost}</td>
                  <td class="py-2 font-mono">{(s.points_ratio || 0).toFixed(2)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </CardContent>
      </Card>
    {/if}

    <Card>
      <CardHeader><CardTitle>Matches</CardTitle></CardHeader>
      <CardContent>
        {#if matches.length === 0}
          <p class="text-sm text-muted-foreground">No matches yet.</p>
        {:else}
          <ul class="space-y-2">
            {#each matches as m (m.id)}
              <li>
                <a class="flex items-center justify-between rounded-md border p-3 hover:bg-accent" href={`/matches/${m.id}`}>
                  <div>
                    <div class="font-medium">{m.kind === 'doubles' ? 'Doubles' : 'Singles'}</div>
                    {#if m.tournament_bracket_round}<div class="text-xs text-muted-foreground">Round {m.tournament_bracket_round}</div>{/if}
                  </div>
                  <span class="text-xs uppercase text-muted-foreground">
                    {m.status === 'in_progress' ? 'Live' : m.winner_side ? `${m.winner_side} won` : 'Final'}
                  </span>
                </a>
              </li>
            {/each}
          </ul>
        {/if}
      </CardContent>
    </Card>
  </div>
{/if}
