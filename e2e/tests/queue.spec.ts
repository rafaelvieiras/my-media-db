import { expect, test } from "../fixtures";

// Journey for the execution queue screen (ADR-0040, issue #62). It does not
// depend on the library state and it does not trigger a run: it is idempotent
// regarding the queue (it works with an empty queue, with a run in progress, or
// with a recent history), the same spirit as the webhook spec.

test("queue page: reachable from the dashboard and shows the state of the queue", async ({
  page,
}) => {
  await page.goto("/dashboard");
  await page.getByRole("link", { name: "Queue" }).click();
  await expect(page).toHaveURL(/\/dashboard\/queue$/);
  await expect(
    page.getByRole("heading", { level: 1, name: "Queue" }),
  ).toBeVisible();

  // The screen always answers one of the four shapes: nothing running (empty
  // state), something running now, something waiting, or a recent history.
  const vazia = page.getByText("Nothing running");
  const rodando = page.getByRole("heading", { name: "Running now" });
  const esperando = page.getByRole("heading", { name: "Waiting" });
  const recentes = page.getByRole("heading", { name: "Recently finished" });
  await expect(
    vazia.or(rodando).or(esperando).or(recentes).first(),
  ).toBeVisible();
});

test("queue page: a recent run shows a duration and its trail opens", async ({
  page,
}) => {
  await page.goto("/dashboard/queue");
  await expect(
    page.getByRole("heading", { level: 1, name: "Queue" }),
  ).toBeVisible();

  const recentes = page.getByRole("heading", { name: "Recently finished" });
  if (!(await recentes.isVisible())) {
    // No run finished on this server yet (fresh install): the screen still works,
    // there is simply nothing to expand.
    await expect(page.getByText("Nothing running")).toBeVisible();
    return;
  }

  // The card of a finished run carries how long it took ("took 3min").
  const cartao = page
    .getByRole("listitem")
    .filter({ hasText: /took |levou / })
    .first();
  await expect(cartao).toBeVisible();

  // The trail is fetched on demand: before the click, only the button exists.
  const abrir = cartao.getByRole("button", {
    name: /Show what it did|Ver o que fez/,
  });
  await abrir.click();
  await expect(
    cartao
      .getByRole("button", { name: /Hide what it did|Ocultar o que fez/ })
      .first(),
  ).toBeVisible();
});
