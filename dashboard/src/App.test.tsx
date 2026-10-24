import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { App } from "./App";
import type { Ich, Koepfe, Zeile, Zustand } from "./api";

const ZUSTAND: Zustand = {
  firma: "testfirma",
  postfach: "postfach/info@firma.de/INBOX",
  kategorien: ["Anfrage", "Beschwerde", "Sonstiges"],
  laeuft: false,
  letzter_lauf: null,
};

function zeile(uid: string, mehr: Partial<Zeile>): Zeile {
  return {
    bereich: "postfach/info@firma.de/INBOX", uid, stand: "eingeordnet", kategorie: "Anfrage", begruendung: "",
    unsicher: false, art9_verdacht: false, modell: "m", nachweis: 1, eingeordnet_am: "2026-10-24T08:00:00+00:00", ...mehr,
  };
}

const ZEILEN = [
  zeile("1", { begruendung: "Kunde möchte ein Angebot.", nachweis: 11 }),
  zeile("2", { kategorie: "Beschwerde", begruendung: "Ware kam beschädigt an." }),
  zeile("3", { stand: "nicht_eingeordnet", kategorie: null, art9_verdacht: true, begruendung: "Bitte von Hand einordnen." }),
];

let zustand: Zustand;
let koepfe: Koepfe;
let ich: Ich;
let angemeldet: boolean;
let aufrufe: { pfad: string; init?: RequestInit }[];

beforeEach(() => {
  zustand = { ...ZUSTAND };
  koepfe = {
    bereich: "postfach/info@firma.de/INBOX",
    koepfe: { "1": { von: "Max Muster <max@kunde.de>", betreff: "Angebot für 200 Stück", datum: "2026-10-23T09:15:00+02:00" } },
    fehler: "",
  };
  aufrufe = [];
  // Standard: Testmodus ohne Anmeldung.
  ich = { anmeldung_noetig: false, angemeldet: false, person: "p", firma: "testfirma" };
  angemeldet = true;
  vi.stubGlobal("fetch", vi.fn(async (pfad: string, init?: RequestInit) => {
    aufrufe.push({ pfad, init });
    const json = (w: unknown, status = 200) => new Response(JSON.stringify(w), { status });
    if (pfad === "/api/ich") return json(ich.anmeldung_noetig ? { ...ich, angemeldet, person: angemeldet ? "chefin" : null } : ich);
    if (pfad === "/api/anmelden") {
      const { passwort } = JSON.parse(String(init?.body));
      if (passwort !== "richtig-richtig") return json({ fehler: "Anmeldung fehlgeschlagen." }, 401);
      angemeldet = true;
      return json({ anmeldung_noetig: true, angemeldet: true, person: "chefin", firma: "testfirma" });
    }
    if (pfad === "/api/abmelden") {
      angemeldet = false;
      return json({ angemeldet: false });
    }
    if (!angemeldet) return json({ fehler: "Bitte anmelden.", anmelden: true }, 401);
    if (pfad === "/api/zustand") return json(zustand);
    if (pfad === "/api/einordnungen") return json({ einordnungen: ZEILEN });
    if (pfad === "/api/koepfe") return json(koepfe);
    if (pfad === "/api/einordnen") {
      zustand = { ...zustand, laeuft: true };
      return json(zustand, 202);
    }
    return json({ fehler: "Unbekannt." }, 404);
  }));
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

test("zeigt Zahlen je Kategorie und die Mails", async () => {
  render(<App />);
  expect(await screen.findByText("Kunde möchte ein Angebot.")).toBeTruthy();
  const kachel = (titel: string) => screen.getByText(titel, { selector: ".kachel-titel" }).parentElement!;
  expect(kachel("Anfrage").textContent).toBe("1Anfrage");
  expect(kachel("Beschwerde").textContent).toBe("1Beschwerde");
  expect(kachel("Sonstiges").textContent).toBe("0Sonstiges");
  expect(kachel("Von Hand prüfen").textContent).toBe("1Von Hand prüfen");
  expect(screen.getByText(/info@firma.de · INBOX/, { selector: ".postfach" })).toBeTruthy();
  expect(screen.getByText(/Nachweis Nr. 11/)).toBeTruthy();
});

test("Art.-9-Verdacht wird deutlich gezeigt, ohne Versprechen", async () => {
  render(<App />);
  expect(await screen.findByText(/ging an kein Modell außerhalb der EU/)).toBeTruthy();
  expect(screen.getByText(/keine Garantie/)).toBeTruthy();
});

test("Kachel filtert die Liste, zweiter Druck zeigt wieder alle", async () => {
  render(<App />);
  await screen.findByText("Kunde möchte ein Angebot.");
  const kachel = screen.getByText("Beschwerde", { selector: ".kachel-titel" }).parentElement!;
  fireEvent.click(kachel);
  expect(screen.queryByText("Kunde möchte ein Angebot.")).toBeNull();
  expect(screen.getByText("Ware kam beschädigt an.")).toBeTruthy();
  fireEvent.click(kachel);
  expect(screen.getByText("Kunde möchte ein Angebot.")).toBeTruthy();
});

test("Knopf startet das Einordnen mit eigener Kopfzeile und wartet auf das Ende", async () => {
  render(<App />);
  const knopf = await screen.findByRole("button", { name: "Jetzt einordnen" });
  await waitFor(() => expect((knopf as HTMLButtonElement).disabled).toBe(false));
  vi.useFakeTimers();
  await act(async () => {
    fireEvent.click(knopf);
  });
  const start = aufrufe.find((a) => a.pfad === "/api/einordnen")!;
  expect(start.init?.method).toBe("POST");
  expect((start.init?.headers as Record<string, string>)["X-BoticStudio-Aktion"]).toBe("1");
  expect(screen.getByRole("button", { name: "Ordnet ein …" })).toBeTruthy();

  zustand = {
    ...zustand, laeuft: false,
    letzter_lauf: { beginn: "2026-10-24T08:00:00+00:00", ende: "2026-10-24T08:01:00+00:00", abbruch: "",
      bericht: { aufgabe: "a", gelesen: 3, eingeordnet: 2, nicht_eingeordnet: 1, fehler: 0, kategorien: {} } },
  };
  const vorher = aufrufe.filter((a) => a.pfad === "/api/einordnungen").length;
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1600);
  });
  expect(screen.getByRole("button", { name: "Jetzt einordnen" })).toBeTruthy();
  expect(screen.getByText(/3 gelesen, 2 eingeordnet, 1 von Hand prüfen/)).toBeTruthy();
  expect(aufrufe.filter((a) => a.pfad === "/api/einordnungen").length).toBe(vorher + 1);
});

test("Abbruch wird als Fehler angezeigt", async () => {
  zustand = { ...zustand, letzter_lauf: { beginn: "x", ende: "2026-10-24T08:01:00+00:00", bericht: null, abbruch: "Pförtner nicht erreichbar." } };
  render(<App />);
  expect((await screen.findByRole("alert")).textContent).toContain("Pförtner nicht erreichbar.");
});

test("zeigt Betreff und Absender live, mit eigener Kopfzeile abgefragt", async () => {
  render(<App />);
  expect(await screen.findByText("Angebot für 200 Stück")).toBeTruthy();
  expect(screen.getByText("Max Muster <max@kunde.de>")).toBeTruthy();
  const abruf = aufrufe.find((a) => a.pfad === "/api/koepfe")!;
  expect((abruf.init?.headers as Record<string, string>)["X-BoticStudio-Aktion"]).toBe("1");
  // Für Mails ohne Kopfzeilen ein ehrlicher Hinweis statt leerer Zeile
  expect(screen.getAllByText(/nicht abrufbar \(Mail älter als 31 Tage/).length).toBe(2);
});

test("Postfach nicht erreichbar: Liste bleibt, Hinweis erscheint", async () => {
  koepfe = { bereich: "", koepfe: {}, fehler: "Anmeldung am Postfach fehlgeschlagen." };
  render(<App />);
  expect(await screen.findByText(/gerade nicht abrufbar: Anmeldung am Postfach fehlgeschlagen/)).toBeTruthy();
  expect(screen.getByText("Kunde möchte ein Angebot.")).toBeTruthy();
});

function mitAnmeldung(schon: boolean) {
  ich = { anmeldung_noetig: true, angemeldet: schon, person: null, firma: null };
  angemeldet = schon;
}

test("ohne Sitzung erscheint die Anmeldung, Mails werden nicht abgefragt", async () => {
  mitAnmeldung(false);
  render(<App />);
  expect(await screen.findByRole("heading", { name: "Anmelden" })).toBeTruthy();
  expect(aufrufe.map((a) => a.pfad)).toEqual(["/api/ich"]);
});

test("falsches Passwort zeigt den Fehler, richtiges öffnet den Posteingang", async () => {
  mitAnmeldung(false);
  render(<App />);
  fireEvent.change(await screen.findByLabelText("E-Mail"), { target: { value: "chefin@firma.de" } });
  fireEvent.change(screen.getByLabelText("Passwort"), { target: { value: "falsch" } });
  fireEvent.click(screen.getByRole("button", { name: "Anmelden" }));
  expect((await screen.findByRole("alert")).textContent).toContain("Anmeldung fehlgeschlagen.");

  fireEvent.change(screen.getByLabelText("Passwort"), { target: { value: "richtig-richtig" } });
  fireEvent.click(screen.getByRole("button", { name: "Anmelden" }));
  expect(await screen.findByText("Kunde möchte ein Angebot.")).toBeTruthy();
  expect(screen.getByText(/Angemeldet als chefin/)).toBeTruthy();
  const anmelden = aufrufe.find((a) => a.pfad === "/api/anmelden")!;
  expect((anmelden.init?.headers as Record<string, string>)["X-BoticStudio-Aktion"]).toBe("1");
});

test("Abmelden führt zurück zur Anmeldung", async () => {
  mitAnmeldung(true);
  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Abmelden" }));
  expect(await screen.findByRole("heading", { name: "Anmelden" })).toBeTruthy();
  expect(aufrufe.some((a) => a.pfad === "/api/abmelden")).toBe(true);
});

test("abgelaufene Sitzung führt zur Anmeldung statt zu einer Fehlermeldung", async () => {
  mitAnmeldung(true);
  render(<App />);
  const knopf = await screen.findByRole("button", { name: "Jetzt einordnen" });
  await waitFor(() => expect((knopf as HTMLButtonElement).disabled).toBe(false));
  angemeldet = false;
  fireEvent.click(knopf);
  expect(await screen.findByRole("heading", { name: "Anmelden" })).toBeTruthy();
});

test("im Testmodus gibt es keinen Abmelden-Knopf", async () => {
  render(<App />);
  await screen.findByText("Kunde möchte ein Angebot.");
  expect(screen.queryByRole("button", { name: "Abmelden" })).toBeNull();
});
