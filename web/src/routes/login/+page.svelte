<script lang="ts">
  import { api } from '$lib/api/client';

  let email = '';
  let sent = false;
  let error = '';

  async function submit() {
    error = '';
    try {
      await api('/api/auth/magic-link', { method: 'POST', bodyJson: { email } });
      sent = true;
    } catch (err) {
      error = err instanceof Error ? err.message : 'Request failed';
    }
  }
</script>

<main class="shell py-8">
  <section class="panel mx-auto max-w-lg p-6">
    <div class="pill">Magic link</div>
    <h1 class="mt-3 text-3xl font-black">Sign in without a password</h1>
    <p class="mt-3 text-sm text-slate-300">Enter your email and Pingit will send a one-time sign-in link.</p>

    {#if sent}
      <div class="mt-5 rounded-2xl border border-emerald-400/20 bg-emerald-400/10 p-4 text-sm text-emerald-100">
        Check your email for the sign-in link.
      </div>
    {:else}
      <label class="mt-5 block">
        <span class="mb-2 block text-sm text-slate-300">Email</span>
        <input class="field" bind:value={email} type="email" placeholder="alice@example.com" />
      </label>
      {#if error}
        <p class="mt-3 text-sm text-rose-200">{error}</p>
      {/if}
      <button class="btn mt-5 w-full" type="button" on:click={submit}>Send magic link</button>
    {/if}
  </section>
</main>

