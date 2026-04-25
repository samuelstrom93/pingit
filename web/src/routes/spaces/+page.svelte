<script lang="ts">
  let joinCode = '';
</script>

<main class="shell space-y-4 py-6">
  <section class="panel p-5">
    <div class="flex items-center justify-between">
      <div>
        <div class="pill">Spaces</div>
        <h1 class="mt-3 text-3xl font-black">Your clubs and groups</h1>
      </div>
      <button class="btn">Create space</button>
    </div>
    <div class="mt-5 grid gap-4 md:grid-cols-2">
      <article class="rounded-3xl border border-white/10 bg-white/5 p-5">
        <h2 class="text-xl font-bold">Bläckfiskligan</h2>
        <p class="mt-2 text-sm text-slate-300">Invite-only club with live matches and tournament history.</p>
      </article>
      <article class="rounded-3xl border border-dashed border-white/15 bg-white/5 p-5">
        <h2 class="text-xl font-bold">Join with code</h2>
        <input class="field mt-3" bind:value={joinCode} placeholder="Enter join code" />
        <button class="btn mt-3 w-full">Join</button>
      </article>
    </div>
  </section>
</main>

