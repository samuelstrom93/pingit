<script lang="ts">
  import { page } from '$app/stores';
  import { api } from '$lib/api/client';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';

  type Space = { id: string; name: string; is_invite_only: boolean; join_code: string | null };

  let space = $state<Space | null>(null);
  let inviteEmail = $state('');
  let error = $state('');
  let success = $state('');
  let spaceID = $derived($page.params.id);

  async function load() {
    space = await api<Space>(`/api/spaces/${spaceID}`);
  }

  $effect(() => {
    if (spaceID) load();
  });

  async function invite(e: Event) {
    e.preventDefault();
    error = '';
    success = '';
    if (!inviteEmail.trim()) return;
    try {
      await api(`/api/spaces/${spaceID}/invitations`, { method: 'POST', bodyJson: { email: inviteEmail } });
      success = `Invitation sent to ${inviteEmail}`;
      inviteEmail = '';
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed';
    }
  }

  async function toggleInviteOnly() {
    if (!space) return;
    try {
      space = await api<Space>(`/api/spaces/${spaceID}`, {
        method: 'PATCH',
        bodyJson: { is_invite_only: !space.is_invite_only }
      });
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed';
    }
  }

  async function rotateCode() {
    if (!space) return;
    try {
      space = await api<Space>(`/api/spaces/${spaceID}`, {
        method: 'PATCH',
        bodyJson: { rotate_join_code: true }
      });
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed';
    }
  }
</script>

<svelte:head><title>Invitations · Pingit</title></svelte:head>

<div class="space-y-6">
  <h1 class="text-3xl font-bold">Invitations</h1>

  {#if space}
    <Card>
      <CardHeader><CardTitle>Invite by email</CardTitle></CardHeader>
      <CardContent>
        <form class="flex gap-2" onsubmit={invite}>
          <div class="flex-1 space-y-1.5">
            <Label for="invite-email">Email</Label>
            <Input id="invite-email" type="email" bind:value={inviteEmail} placeholder="bob@example.com" />
          </div>
          <Button type="submit" class="self-end">Send invite</Button>
        </form>
        {#if error}<p class="mt-2 text-sm text-destructive">{error}</p>{/if}
        {#if success}<p class="mt-2 text-sm text-emerald-500">{success}</p>{/if}
      </CardContent>
    </Card>

    <Card>
      <CardHeader><CardTitle>Open join-code</CardTitle></CardHeader>
      <CardContent class="space-y-3">
        <p class="text-sm">
          Status: <strong>{space.is_invite_only ? 'Invite only' : 'Open with code'}</strong>
        </p>
        {#if space.join_code}
          <p class="text-sm">Code: <code class="rounded bg-muted px-2 py-1">{space.join_code}</code></p>
        {/if}
        <div class="flex gap-2">
          <Button variant="outline" onclick={toggleInviteOnly}>
            {space.is_invite_only ? 'Open the space' : 'Make invite-only'}
          </Button>
          <Button variant="outline" onclick={rotateCode}>Rotate code</Button>
        </div>
      </CardContent>
    </Card>
  {/if}
</div>
