<script lang="ts">
  import { page } from '$app/stores';
  import { api } from '$lib/api/client';
  import { Button } from '$lib/components/ui/button';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';

  type User = { email: string; display_name: string };
  type JoinRequest = { id: string; status: string; message: string | null; created_at: number; user: User };
  type Invitation = { id: string; email: string; status: string; created_at: number; inviter_email: string };
  type Match = { id: string; kind: string; status: string; updated_at: number; winner_side: string | null };
  type Tournament = { id: string; name: string; format: string; status: string; updated_at: number };
  type Dashboard = {
    pending_join_requests: JoinRequest[];
    pending_invitations: Invitation[];
    recent_matches: Match[];
    recent_tournaments: Tournament[];
    counts: Record<string, number>;
  };

  let dashboard = $state<Dashboard | null>(null);
  let error = $state('');
  let busyID = $state('');
  let spaceID = $derived($page.params.id);

  async function load() {
    error = '';
    try {
      dashboard = await api<Dashboard>(`/api/spaces/${spaceID}/admin/dashboard`);
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to load admin dashboard';
    }
  }

  $effect(() => {
    if (spaceID) load();
  });

  async function review(requestID: string, status: 'accepted' | 'rejected') {
    busyID = requestID;
    error = '';
    try {
      await api(`/api/spaces/${spaceID}/join-requests/${requestID}`, { method: 'PATCH', bodyJson: { status } });
      await load();
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to review request';
    } finally {
      busyID = '';
    }
  }
</script>

<svelte:head><title>Admin · Pingit</title></svelte:head>

<div class="space-y-6">
  <div class="flex flex-wrap items-center justify-between gap-3">
    <h1 class="text-3xl font-bold">Admin dashboard</h1>
    <div class="flex gap-2">
      <Button variant="outline" onclick={() => (window.location.href = `/spaces/${spaceID}/invitations`)}>Invitations</Button>
      <Button variant="outline" onclick={load}>Refresh</Button>
    </div>
  </div>

  {#if error}<p class="text-sm text-destructive">{error}</p>{/if}

  {#if dashboard}
    <div class="grid gap-3 sm:grid-cols-3 lg:grid-cols-6">
      {#each Object.entries(dashboard.counts) as [key, value]}
        <Card>
          <CardHeader class="pb-2"><CardTitle class="text-sm capitalize">{key.replaceAll('_', ' ')}</CardTitle></CardHeader>
          <CardContent><div class="text-2xl font-bold">{value}</div></CardContent>
        </Card>
      {/each}
    </div>

    <div class="grid gap-4 lg:grid-cols-2">
      <Card>
        <CardHeader><CardTitle>Pending join requests</CardTitle></CardHeader>
        <CardContent>
          {#if dashboard.pending_join_requests.length === 0}
            <p class="text-sm text-muted-foreground">No pending requests.</p>
          {:else}
            <ul class="space-y-3">
              {#each dashboard.pending_join_requests as request (request.id)}
                <li class="rounded-md border p-3">
                  <div class="flex items-start justify-between gap-3">
                    <div>
                      <div class="font-medium">{request.user.display_name}</div>
                      <div class="text-xs text-muted-foreground">{request.user.email} · {new Date(request.created_at).toLocaleString()}</div>
                      {#if request.message}<p class="mt-2 text-sm">{request.message}</p>{/if}
                    </div>
                    <div class="flex gap-2">
                      <Button disabled={busyID === request.id} onclick={() => review(request.id, 'accepted')}>Accept</Button>
                      <Button variant="outline" disabled={busyID === request.id} onclick={() => review(request.id, 'rejected')}>Reject</Button>
                    </div>
                  </div>
                </li>
              {/each}
            </ul>
          {/if}
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Pending invitations</CardTitle></CardHeader>
        <CardContent>
          {#if dashboard.pending_invitations.length === 0}
            <p class="text-sm text-muted-foreground">No pending invitations.</p>
          {:else}
            <ul class="space-y-2">
              {#each dashboard.pending_invitations.filter((i) => i.status === 'pending') as invite (invite.id)}
                <li class="rounded-md border p-3 text-sm">
                  <div class="font-medium">{invite.email}</div>
                  <div class="text-xs text-muted-foreground">Invited by {invite.inviter_email} · {new Date(invite.created_at).toLocaleString()}</div>
                </li>
              {/each}
            </ul>
          {/if}
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Recent matches</CardTitle></CardHeader>
        <CardContent>
          {#if dashboard.recent_matches.length === 0}
            <p class="text-sm text-muted-foreground">No matches yet.</p>
          {:else}
            <ul class="space-y-2">
              {#each dashboard.recent_matches as match (match.id)}
                <li><a class="block rounded-md border p-3 text-sm hover:bg-accent" href={`/matches/${match.id}`}>{match.kind} · {match.status} · {new Date(match.updated_at).toLocaleString()}</a></li>
              {/each}
            </ul>
          {/if}
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Recent tournaments</CardTitle></CardHeader>
        <CardContent>
          {#if dashboard.recent_tournaments.length === 0}
            <p class="text-sm text-muted-foreground">No tournaments yet.</p>
          {:else}
            <ul class="space-y-2">
              {#each dashboard.recent_tournaments as tournament (tournament.id)}
                <li><a class="block rounded-md border p-3 text-sm hover:bg-accent" href={`/tournaments/${tournament.id}`}>{tournament.name} · {tournament.format} · {tournament.status}</a></li>
              {/each}
            </ul>
          {/if}
        </CardContent>
      </Card>
    </div>
  {/if}
</div>
