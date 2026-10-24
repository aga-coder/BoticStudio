// Verbindung zum Dashboard-Dienst (python -m ablauf.dashboard).
// Gespeichert ist nur ein Verweis auf jede Mail. Betreff, Absender und Datum
// holt der Dienst live über den Pförtner, den Text einer Mail nie.

export type Stand = "eingeordnet" | "nicht_eingeordnet" | "fehler";

export interface Zeile {
  bereich: string;
  uid: string;
  stand: Stand;
  kategorie: string | null;
  begruendung: string;
  unsicher: boolean;
  art9_verdacht: boolean;
  modell: string;
  nachweis: number | null;
  eingeordnet_am: string | null;
}

export interface Bericht {
  aufgabe: string;
  gelesen: number;
  eingeordnet: number;
  nicht_eingeordnet: number;
  fehler: number;
  kategorien: Record<string, number>;
}

export interface Lauf {
  beginn: string;
  ende: string | null;
  bericht: Bericht | null;
  abbruch: string;
}

export interface Zustand {
  firma: string;
  postfach: string;
  kategorien: string[];
  laeuft: boolean;
  letzter_lauf: Lauf | null;
}

export interface Kopf {
  von: string;
  betreff: string;
  datum: string;
}

export interface Koepfe {
  bereich: string;
  koepfe: Record<string, Kopf>;
  fehler: string;
}

export interface Ich {
  anmeldung_noetig: boolean;
  angemeldet: boolean;
  person: string | null;
  firma: string | null;
}

/** Die Sitzung ist abgelaufen oder fehlt: zurück zur Anmeldung. */
export class NichtAngemeldet extends Error {}

const AUS_DEM_DASHBOARD = { "X-BoticStudio-Aktion": "1" };

async function lies<T>(antwort: Response): Promise<T> {
  const daten = await antwort.json().catch(() => ({}));
  if (!antwort.ok) {
    const { fehler, anmelden } = daten as { fehler?: string; anmelden?: boolean };
    const text = fehler ?? `Der Dienst antwortet mit ${antwort.status}.`;
    throw anmelden ? new NichtAngemeldet(text) : new Error(text);
  }
  return daten as T;
}

export async function holeIch(): Promise<Ich> {
  return lies<Ich>(await fetch("/api/ich"));
}

export async function meldeAn(email: string, passwort: string): Promise<Ich> {
  // Der Dienst setzt den Sitzungs-Cookie (HttpOnly, für dieses Skript unsichtbar).
  const antwort = await fetch("/api/anmelden", {
    method: "POST",
    headers: { "Content-Type": "application/json", ...AUS_DEM_DASHBOARD },
    body: JSON.stringify({ email, passwort }),
  });
  return lies<Ich>(antwort);
}

export async function meldeAb(): Promise<void> {
  await lies(await fetch("/api/abmelden", { method: "POST", headers: AUS_DEM_DASHBOARD, body: "{}" }));
}

export async function holeZustand(): Promise<Zustand> {
  return lies<Zustand>(await fetch("/api/zustand"));
}

export async function holeEinordnungen(): Promise<Zeile[]> {
  return (await lies<{ einordnungen: Zeile[] }>(await fetch("/api/einordnungen"))).einordnungen;
}

export async function holeKoepfe(): Promise<Koepfe> {
  // Löst einen Abruf am Postfach aus, deshalb mit derselben Kopfzeile wie der Knopf.
  return lies<Koepfe>(await fetch("/api/koepfe", { headers: AUS_DEM_DASHBOARD }));
}

export async function starteEinordnen(): Promise<Zustand> {
  // Die eigene Kopfzeile zeigt dem Dienst, dass der Knopf hier gedrückt wurde.
  const antwort = await fetch("/api/einordnen", {
    method: "POST",
    headers: { "Content-Type": "application/json", ...AUS_DEM_DASHBOARD },
    body: "{}",
  });
  return lies<Zustand>(antwort);
}
