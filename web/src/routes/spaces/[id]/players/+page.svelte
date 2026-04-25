<main class="shell py-6">
  <section class="panel p-5">
    <div class="flex items-center justify-between">
      <div>
        <div class="pill">Players</div>
        <h1 class="mt-3 text-3xl font-black">Space roster</h1>
      </div>
      <button class="btn">Add anonymous player</button>
    </div>
    <div class="mt-5 grid gap-3">
      {#each ['Alice', 'Bob', 'Pappa', 'Ingrid'] as player}
        <article class="flex items-center justify-between rounded-2xl border border-white/10 bg-white/5 p-4">
          <div>
            <strong>{player}</strong>
            <p class="mt-1 text-sm text-slate-300">Ready for singles and doubles.</p>
          </div>
          <button class="btn btn-secondary">Edit</button>
        </article>
      {/each}
    </div>
  </section>
</main>

