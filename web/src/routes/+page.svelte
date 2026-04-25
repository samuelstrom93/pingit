<script lang="ts">
  const recentMatches = [
    { title: 'Alice vs Bob', meta: 'Best of 3', state: 'Live' },
    { title: 'Pappa vs Ingrid', meta: 'Completed 3-1', state: 'Final' }
  ];

  const tournaments = [
    { name: 'Cup Clash', state: 'Bracket in progress' },
    { name: 'Spring League', state: 'Round robin complete' }
  ];
</script>

<svelte:head>
  <title>Pingit</title>
</svelte:head>

<main class="shell space-y-4 py-6">
  <section class="panel p-5">
    <div class="pill">Pingit</div>
    <h1 class="mt-3 text-4xl font-black">Keep the rally moving.</h1>
    <p class="mt-3 max-w-xl text-sm text-slate-300">
      Mobile-first scorekeeping, spaces for your club, and tournaments that stay in sync across devices.
    </p>
    <div class="mt-5 flex gap-3">
      <a class="btn" href="/spaces">Open spaces</a>
      <a class="btn btn-secondary" href="/login">Send magic link</a>
    </div>
  </section>

  <section class="grid gap-4 md:grid-cols-2">
    <div class="panel p-5">
      <div class="flex items-center justify-between">
        <h2 class="text-xl font-bold">Recent matches</h2>
        <a class="text-sm text-sky-200" href="/spaces/demo/new-match">New match</a>
      </div>
      <div class="mt-4 space-y-3">
        {#each recentMatches as match}
          <article class="rounded-2xl border border-white/10 bg-white/5 p-4">
            <div class="flex items-center justify-between">
              <strong>{match.title}</strong>
              <span class="pill">{match.state}</span>
            </div>
            <p class="mt-2 text-sm text-slate-300">{match.meta}</p>
          </article>
        {/each}
      </div>
    </div>

    <div class="panel p-5">
      <h2 class="text-xl font-bold">Active tournaments</h2>
      <div class="mt-4 space-y-3">
        {#each tournaments as tournament}
          <article class="rounded-2xl border border-white/10 bg-white/5 p-4">
            <strong>{tournament.name}</strong>
            <p class="mt-2 text-sm text-slate-300">{tournament.state}</p>
          </article>
        {/each}
      </div>
    </div>
  </section>
</main>

