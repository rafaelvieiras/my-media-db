import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { expect, test } from "../fixtures";
import { resetBiblioteca } from "../lib/reset";

// OWNER ORDER editor (ADR-0033). A release that put two aired episodes in ONE numbered
// entry has no provider to describe it, so the owner declares it in the drawer of the
// series screen: start from the base order, merge the episodes the release glued
// together, save. The journey goes through the real UI against the real binary — the
// keyboard-free path is the point here, not the API (journeys_ordem_dono_test.go covers
// the API contract and the scan).
const pastas: string[] = [];

// semeiaSerie creates a unique folder under /tmp with the season files of the fake
// provider tree ("Springfield Tales"/71663: S01E01..E03).
function semeiaSerie(arquivos: string[]): string {
  const dir = mkdtempSync(join(tmpdir(), "mmdb-e2e-dono-"));
  pastas.push(dir);
  const season = join(dir, "Springfield Tales", "Season 01");
  mkdirSync(season, { recursive: true });
  for (const nome of arquivos) {
    writeFileSync(
      join(dir, "Springfield Tales", "Season 01", nome),
      Buffer.alloc(2048, 7),
    );
  }
  return dir;
}

// limparDatasources removes all datasources through the API (the reset does not touch them).
async function limparDatasources(
  request: import("@playwright/test").APIRequestContext,
) {
  const res = await request.get("/api/v1/library");
  const body = await res.json();
  for (const s of body.items ?? []) {
    await request.delete(`/api/v1/library/${s.id}`);
  }
}

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request);
  await limparDatasources(request);
});

test.afterEach(() => {
  for (const dir of pastas.splice(0)) {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("the owner merges two episodes into one unit of their release", async ({
  page,
  request,
}) => {
  const dir = semeiaSerie([
    "Springfield Tales S01E01.mkv",
    "Springfield Tales S01E02.mkv",
    "Springfield Tales S01E03.mkv",
  ]);

  // The datasource is created through the API (its own form has its own journey): what
  // this test drives is the drawer, not the library screen.
  const res = await request.post("/api/v1/library", {
    data: { path: dir, type: "tv", content_type: "series" },
  });
  expect(res.status()).toBe(201);
  const ds = await res.json();
  expect((await request.post(`/api/v1/library/${ds.id}/scan`)).status()).toBe(
    202,
  );
  await expect
    .poll(
      async () =>
        (await (await request.get(`/api/v1/library/${ds.id}/scan`)).json())
          .running,
      { timeout: 30_000 },
    )
    .toBe(false);

  // The series was auto-imported by the scan; its screen is where the order lives.
  const lista = await (
    await request.get("/api/v1/entities?type=tv&limit=20")
  ).json();
  const serie = (lista.items ?? []).find((e: { title?: string }) =>
    (e.title ?? "").includes("Springfield"),
  );
  expect(serie).toBeTruthy();

  await page.goto(`/entities/${serie.id}`);
  await expect(
    page.getByRole("heading", { level: 1, name: /Springfield/ }),
  ).toBeVisible();

  // The drawer opens empty and offers to start from the order the series uses today.
  await page.getByRole("button", { name: "Episode order" }).click();
  await expect(
    page.getByText(
      "No unit yet. Start from an order to get every episode numbered.",
    ),
  ).toBeVisible();
  await page.getByRole("button", { name: /^Start from the .* order$/ }).click();

  // The base order of the fake tree: six episodes (three in each season), so six units
  // of one episode each.
  await expect(page.getByText("1 episode")).toHaveCount(6);

  // The release put E01 and E02 in one entry: select both and merge.
  const caixas = page.getByRole("checkbox");
  await caixas.nth(0).check();
  await caixas.nth(1).check();
  await page.getByRole("button", { name: "Merge 2 selected" }).click();
  await expect(page.getByText("2 episodes")).toBeVisible();

  await page.getByLabel("Name of the order").fill("HBO 2004");
  await page.getByRole("button", { name: "Save order" }).click();
  await expect(
    page.getByText(
      "No unit yet. Start from an order to get every episode numbered.",
    ),
  ).toBeHidden();

  // Saved: the API agrees, and the tree now projects the OWNER order — the merged unit
  // shows both episodes under the same number.
  const gravada = await (
    await request.get(`/api/v1/entities/${serie.id}/owner-order`)
  ).json();
  expect(gravada.name).toBe("HBO 2004");
  // Six episodes, one merge: five units, and the first one holds the two merged ones.
  expect(gravada.units).toHaveLength(5);
  expect(gravada.units[0].episodes).toHaveLength(2);

  const arvore = await (
    await request.get(`/api/v1/entities/${serie.id}/episodes`)
  ).json();
  expect(arvore.ordering).toBe("owner");
  const mesclados = (arvore.groups ?? [])
    .flatMap(
      (g: { episodes?: { numbering?: { merged_episodes?: number } }[] }) =>
        g.episodes ?? [],
    )
    .filter(
      (e: { numbering?: { merged_episodes?: number } }) =>
        e.numbering?.merged_episodes === 2,
    );
  expect(mesclados).toHaveLength(2);

  // And removing it puts the series back on the provider order.
  await page.getByRole("button", { name: "Episode order" }).click();
  await page.getByRole("button", { name: "Remove order" }).click();
  await expect
    .poll(
      async () =>
        (
          await (
            await request.get(`/api/v1/entities/${serie.id}/owner-order`)
          ).json()
        ).units.length,
      { timeout: 10_000 },
    )
    .toBe(0);
});
