<script lang="ts">
  import { onDestroy } from 'svelte';
  import { page } from '$app/stores';
  import { api } from '$lib/api/client';
  import { Button } from '$lib/components/ui/button';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';

  type Player = { id: string; display_name: string };
  type Game = { id: string; game_number: number; home_score: number; visitor_score: number; status: string };
  type Participant = { match_id: string; player_id: string; side: 'home' | 'visitor'; slot: number };
  type Match = {
    id: string;
    space_id: string;
    kind: 'singles' | 'doubles';
    best_of: number;
    points_to_win: number;
    status: 'in_progress' | 'completed' | 'abandoned';
    winner_side: 'home' | 'visitor' | null;
  };
  type MatchPayload = {
    match: Match;
    games: Game[];
    participants: Participant[];
    players?: Player[];
  };

  let payload = $state<MatchPayload | null>(null);
  let error = $state('');
  let scoring = $state(false);
  let ws: WebSocket | null = null;
  let wakeLock: WakeLockSentinel | null = null;
  let matchID = $derived($page.params.id);

  const currentGame = $derived(
    payload?.games.find((g) => g.status === 'in_progress') ??
      payload?.games[payload.games.length - 1] ??
      null
  );
  const playerById = $derived(new Map((payload?.players ?? []).map((p) => [p.id, p])));
  const homeNames = $derived(
    (payload?.participants ?? [])
      .filter((p) => p.side === 'home')
      .sort((a, b) => a.slot - b.slot)
      .map((p) => playerById.get(p.player_id)?.display_name ?? 'Player')
      .join(' & ')
  );
  const visitorNames = $derived(
    (payload?.participants ?? [])
      .filter((p) => p.side === 'visitor')
      .sort((a, b) => a.slot - b.slot)
      .map((p) => playerById.get(p.player_id)?.display_name ?? 'Player')
      .join(' & ')
  );
  const homeWins = $derived(
    (payload?.games ?? []).filter((g) => g.status === 'completed' && g.home_score > g.visitor_score).length
  );
  const visitorWins = $derived(
    (payload?.games ?? []).filter((g) => g.status === 'completed' && g.visitor_score > g.home_score).length
  );

  async function load() {
    try {
      const data = await api<MatchPayload>(`/api/matches/${matchID}`);
      payload = data;
      error = '';
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed';
    }
  }

  $effect(() => {
    if (!matchID) return;
    load();
    connectWS();
    requestWakeLock();
    return () => {
      ws?.close();
      ws = null;
      releaseWakeLock();
    };
  });

  function connectWS() {
    if (typeof window === 'undefined') return;
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${proto}//${window.location.host}/api/ws/matches/${matchID}`;
    try {
      ws = new WebSocket(url);
      ws.addEventListener('message', () => load());
      ws.addEventListener('close', () => (ws = null));
    } catch {
      // ignore
    }
  }

  async function requestWakeLock() {
    if (typeof navigator === 'undefined') return;
    const navAny = navigator as Navigator & { wakeLock?: { request: (t: string) => Promise<WakeLockSentinel> } };
    if (!navAny.wakeLock) return;
    try {
      wakeLock = await navAny.wakeLock.request('screen');
    } catch {
      // ignore
    }
  }

  function releaseWakeLock() {
    wakeLock?.release().catch(() => undefined);
    wakeLock = null;
  }

  function buzz() {
    if (typeof navigator !== 'undefined' && 'vibrate' in navigator) {
      try {
        navigator.vibrate(30);
      } catch {
        // ignore
      }
    }
  }

  async function score(side: 'home' | 'visitor') {
    if (scoring || !payload || payload.match.status !== 'in_progress') return;
    scoring = true;
    buzz();
    try {
      await api(`/api/matches/${matchID}/score`, { method: 'POST', bodyJson: { side } });
      await load();
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed';
    } finally {
      scoring = false;
    }
  }

  async function undo() {
    if (!payload || payload.match.status !== 'in_progress') return;
    if (!confirm('Undo last point?')) return;
    try {
      await api(`/api/matches/${matchID}/undo`, { method: 'POST' });
      await load();
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed';
    }
  }

  type WakeLockSentinel = { release(): Promise<void> };
</script>

<svelte:head><title>Match · Pingit</title></svelte:head>

{#if error}<p class="text-sm text-destructive">{error}</p>{/if}

{#if payload}
  {@const m = payload.match}
  {@const g = currentGame}
  <div class="space-y-4">
    <div class="flex flex-wrap items-baseline justify-between gap-2">
      <div>
        <h1 class="text-2xl font-bold">{homeNames || 'Home'} vs {visitorNames || 'Visitor'}</h1>
        <p class="text-sm text-muted-foreground">
          {m.kind === 'doubles' ? 'Doubles' : 'Singles'} · Best of {m.best_of} · First to {m.points_to_win}
          {#if m.status === 'completed'}· <span class="font-medium">Winner: {m.winner_side}</span>{/if}
        </p>
      </div>
      <div class="text-right">
        <div class="text-3xl font-extrabold tabular-nums">{homeWins} – {visitorWins}</div>
        <div class="text-xs text-muted-foreground">games won</div>
      </div>
    </div>

    {#if m.status === 'in_progress' && g}
      <div class="grid grid-rows-2 gap-3 sm:gap-4">
        <button
          type="button"
          aria-label="Home scored a point"
          class="flex min-h-[40vh] flex-col items-center justify-center rounded-2xl border-4 border-primary/30 bg-primary/10 text-foreground transition-all hover:bg-primary/20 active:scale-[0.98] disabled:opacity-50"
          disabled={scoring}
          onclick={() => score('home')}
        >
          <span class="text-sm uppercase tracking-widest text-muted-foreground">{homeNames || 'Home'}</span>
          <span class="text-[16vh] font-extrabold leading-none tabular-nums">{g.home_score}</span>
          <span class="text-xs uppercase tracking-widest text-muted-foreground">tap to +1</span>
        </button>
        <button
          type="button"
          aria-label="Visitor scored a point"
          class="flex min-h-[40vh] flex-col items-center justify-center rounded-2xl border-4 border-secondary/30 bg-secondary/30 text-foreground transition-all hover:bg-secondary/50 active:scale-[0.98] disabled:opacity-50"
          disabled={scoring}
          onclick={() => score('visitor')}
        >
          <span class="text-sm uppercase tracking-widest text-muted-foreground">{visitorNames || 'Visitor'}</span>
          <span class="text-[16vh] font-extrabold leading-none tabular-nums">{g.visitor_score}</span>
          <span class="text-xs uppercase tracking-widest text-muted-foreground">tap to +1</span>
        </button>
      </div>

      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="text-sm text-muted-foreground">Game {g.game_number} of (max) {m.best_of}</div>
        <Button variant="outline" onclick={undo}>Undo last point</Button>
      </div>
    {:else}
      <Card>
        <CardHeader><CardTitle>Final score</CardTitle></CardHeader>
        <CardContent>
          <ul class="space-y-1 text-sm">
            {#each payload.games as game (game.id)}
              <li class="flex justify-between border-b py-1 last:border-0">
                <span>Game {game.game_number}</span>
                <span class="font-mono">{game.home_score} – {game.visitor_score}</span>
              </li>
            {/each}
          </ul>
        </CardContent>
      </Card>
    {/if}

    {#if payload.games.some((g) => g.status === 'completed')}
      <div class="flex flex-wrap gap-2 text-xs">
        {#each payload.games.filter((g) => g.status === 'completed') as game (game.id)}
          <span class="rounded-full border px-3 py-1 font-mono">{game.home_score}-{game.visitor_score}</span>
        {/each}
      </div>
    {/if}
  </div>
{:else}
  <p class="text-sm text-muted-foreground">Loading…</p>
{/if}
