# Primitives-läget: bits-ui, shadcn-svelte, Tailwind v4

Faktadokument för wayfinder-ticket [#6](https://github.com/samuelstrom93/pingit/issues/6). Underlag till Primitives-beslutet (#7). Kontrollerat 2026-09-03 mot primärkällor (npm-registry, officiella docs, GitHub). Nuläge i repot: SvelteKit 2 + Svelte 5, Tailwind 3.4, `bits-ui@1.0.0-next.87`, `tailwind-variants`, `tailwind-merge`, `tailwindcss-animate`, `@vite-pwa/sveltekit`; endast button/card/input/label från shadcn-svelte.

## (a) bits-ui

- **Stabil release finns.** npm dist-tags: `latest: 2.19.0`, `next: 1.0.0-next.98` — `next`-taggen är stale, stable körde om den för länge sedan. Källa: `npm view bits-ui dist-tags`, <https://www.npmjs.com/package/bits-ui>.
- Tidslinje (npm-registry): `1.0.0-next.87` (vår pin) 2025-02-06 → `1.0.0-next.98` (sista prerelease) 2025-02-12 → **`1.1.0` första stabila** 2025-02-13 (ingen bar `1.0.0` publicerades) → `1.8.0` sista 1.x 2025-05-24 → **`2.1.0`** 2025-05-27 → **`2.19.0`** 2026-08-20.
- **Underhåll: mycket aktivt.** ~190 publicerade versioner från next.87 till 2.19.0 (~18,5 månader), patchar så sent som 2026-08-19/20. huntabyte aktiv primär maintainer; ~3,5k stars. Källa: <https://github.com/huntabyte/bits-ui>.
- **Svelte 5: ja, enbart.** `bits-ui@2.19.0` peer dep `svelte: ^5.33.0` (golvet höjdes i 2.1.0 för Attachments + `$props.id()`). Källa: `npm view bits-ui@latest peerDependencies`, <https://www.bits-ui.com/docs/migration-guide>.
- **Breaking changes next.87 → stabil 1.x/2.x** (kumulativ migrationsguide, <https://www.bits-ui.com/docs/migration-guide>; guiden diffar inte per prerelease-bygge, men next.87 ligger nära slutet av fönstret så det mesta bör redan vara på plats — verifiera per komponent vid uppgradering):
  - `el` → `ref`; `asChild` → `child`-snippet; `let:`-direktiv → snippet-props.
  - Transition-props borttagna — `child`-snippet + `forceMount` + native Svelte-transitions.
  - Automatisk portalling borttagen (Dialog/Popover/Select/Combobox/menyer) — explicit `Portal` krävs.
  - Accordion/Select/Combobox/Slider: nytt obligatoriskt `type`-prop; `selected` → `value`.
  - Checkbox: `Indicator`/`Input` borttagna, `checked` boolean-only; Tooltip: kräver `Tooltip.Provider`; Pin Input: helt ny API; menyer: `Label` → `GroupHeading`.

## (b) shadcn-svelte

- npm: `latest: 1.6.0` (2026-09-01), `next: 1.0.0-next.19`. Kadensen stabil: `1.0.1` (2025-06-08) → `1.6.0`, ungefär månadsvis/varannan vecka. Källa: `npm view shadcn-svelte time`.
- **Svelte 5: fullt stöd.** Egen migrationsguide finns; docs/demo kör Svelte 5. Källa: <https://shadcn-svelte.com/docs/migration/svelte-5>.
- **Tailwind v4: officiellt stöd.** CLI:t scaffoldar nya projekt med Tailwind v4 + Svelte 5 (`@theme`/`@theme inline`). Migration av befintligt v3-projekt är manuell: kör `npx @tailwindcss/upgrade`, lägg till `@tailwindcss/vite` i `vite.config.ts`, skriv om `app.css` till CSS-first. Uttryckligen icke-brytande: *"Your existing apps with Tailwind v3 will continue to work"* — nya CLI-tillagda komponenter följer det Tailwind-major som `components.json` pekar på. Källa: <https://shadcn-svelte.com/docs/migration/tailwind-v4>.
- Samma guide: `tailwindcss-animate` deprecatad till förmån för **`tw-animate-css`** i v4-projekt.
- `shadcn-svelte@latest` beror själv på `tailwind-merge ^3.6.0` (v4-kompatibla majorn). Källa: `npm view shadcn-svelte@latest dependencies`.
- **Komponenttäckning: ~80+ komponenter** (formulär, overlays, navigation, data display m.m.) — stor superset av de fyra vi har. Källa: <https://shadcn-svelte.com/docs/components>.

## (c) Tailwind 3.4 → 4: migrationsväg

- Versioner: `4.0.0` 2025-01-21, `4.1.0` 2025-04-01, latest `4.3.3`. 3.4-linjen: `3.4.19` publicerad 2025-12-10 med dist-tag **`v3-lts`** — Tailwind Labs patchar fortfarande v3. Källa: `npm view tailwindcss dist-tags/time`.
- **CSS-first-modellen** (<https://tailwindcss.com/docs/upgrade-guide>): `@tailwind base/components/utilities` → `@import "tailwindcss"`; tema flyttar till CSS via `@theme { --color-…: … }` (CSS-variabler som tokens); `tailwind.config.js` fungerar men autoladdas inte (`@config "…"` krävs); för Vite/SvelteKit rekommenderas `@tailwindcss/vite`-pluginet. Egna utilities via `@utility`; `theme()`-anrop ersätts av `var(--color-…)`.
- **Uppgraderingsverktyg:** `npx @tailwindcss/upgrade` (Node 20+) — automatiserar deps, config→CSS och klass-renames; docs rekommenderar egen branch + diff-granskning.
- **Browser-golv:** Safari 16.4+, Chrome 111+, Firefox 128+ (kräver `@property`, `color-mix()`). Äldre targets = stanna på v3.4.
- **Utvalda breaking changes:** `shadow-sm`→`shadow-xs`, `ring`→`ring-3` (default ringbredd 3px→1px), `outline-none`→`outline-hidden`; `bg-opacity-*`/`text-opacity-*` borttagna (opacity-modifier istället); border/ring-defaultfärg gray-200→`currentColor`; `!flex`→`flex!`; `hover:` gated på `@media (hover: hover)`; inget Sass/Less/Stylus-stöd.
- **Depkompatibilitet:**
  - `tailwind-variants` latest `3.3.1`: peer `tailwindcss: "*"`, `tailwind-merge >=3.0.0` — v4-ok. Källa: `npm view tailwind-variants@latest peerDependencies`.
  - `tailwind-merge`: major **3.x** (latest `3.6.0`) är byggd för v4:s klass-semantik; `tailwind-merge-2`-taggen (`2.6.1`) hålls vid liv för v3-projekt.
  - `tailwindcss-animate`: sista release `1.0.7` (2023-08-28), bygger på legacy JS-plugin-API; ersätts av **`tw-animate-css`** (latest `1.4.0`, ren CSS/`@utility`, uttryckligen "TailwindCSS v4.0 compatible replacement"; README flaggar kommande breaking v2.0.0). Källa: <https://github.com/Wombosvideo/tw-animate-css>.
  - `@vite-pwa/sveltekit` latest `1.1.0` (2025-11-27): inga Tailwind-beroenden alls, oberoende Vite-plugin. Ingen dokumenterad konflikt med `@tailwindcss/vite` hittad (obs: frånvaro av bevis, inte ett auktoritativt kompatibilitetslöfte). Källa: `npm view @vite-pwa/sveltekit dependencies/peerDependencies`.

## (d) Uppgraderingskostnad nu vs stanna på v3

- **Stanna på v3 är billigt nu:** Tailwind 3.4 har aktiv LTS-linje (`v3-lts` → 3.4.19, dec 2025); shadcn-svelte:s v3-spår fungerar och är default-fallback; tailwind-merge/tailwind-variants tvingar inte fram v4.
- **Stanna blir dyrare över tid:** `tailwindcss-animate` är död (2023) och uttryckligen deprecatad av shadcn-svelte-docs; bits-ui:s och shadcn-svelte:s docs/exempel antar i ökande grad Svelte 5 + v4 CSS-first, så nyare komponenter kopieras inte längre rakt av in i ett v3-projekt.
- **Uppgraderingens delar är separerbara:** shadcn-svelte behandlar Svelte 5-migration och Tailwind v4-migration som separata steg; bits-ui next.87 → 2.x är den tredje, oberoende axeln (störst API-yta: portalling, snippets, `type`-props — men bara 4 komponenter berörs i dag).
- **Ej fullt verifierat:** exakt diff next.87→next.98; @vite-pwa × @tailwindcss/vite-samspel (endast frånvaro av rapporterade problem); shadcn-svelte:s exakta registry-versionsnummer.
