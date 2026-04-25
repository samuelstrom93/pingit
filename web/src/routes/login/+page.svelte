<script lang="ts">
  import { api } from '$lib/api/client';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';

  let email = $state('');
  let sent = $state(false);
  let error = $state('');
  let busy = $state(false);

  async function submit(e: Event) {
    e.preventDefault();
    if (!email || busy) return;
    busy = true;
    error = '';
    try {
      await api('/api/auth/magic-link', { method: 'POST', bodyJson: { email } });
      sent = true;
    } catch (err) {
      error = err instanceof Error ? err.message : 'Request failed';
    } finally {
      busy = false;
    }
  }
</script>

<svelte:head><title>Sign in · Pingit</title></svelte:head>

<div class="mx-auto max-w-md py-12">
  <Card>
    <CardHeader>
      <CardTitle>🏓 Sign in to Pingit</CardTitle>
      <CardDescription>We'll email you a one-time link. No password needed.</CardDescription>
    </CardHeader>
    <CardContent>
      {#if sent}
        <div class="rounded-md border bg-muted/50 p-4 text-sm">
          Check your email for the sign-in link. (Dev: see console / <code>./dev-emails.log</code>.)
        </div>
      {:else}
        <form class="space-y-4" onsubmit={submit}>
          <div class="space-y-2">
            <Label for="email">Email</Label>
            <Input id="email" type="email" placeholder="alice@example.com" bind:value={email} required />
          </div>
          {#if error}<p class="text-sm text-destructive">{error}</p>{/if}
          <Button type="submit" class="w-full" disabled={busy}>
            {busy ? 'Sending…' : 'Send magic link'}
          </Button>
        </form>
      {/if}
    </CardContent>
  </Card>
</div>
