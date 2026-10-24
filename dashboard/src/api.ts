// Verbindung zum Dashboard-Dienst (python -m ablauf.dashboard).
// Er liefert nur Arbeitsdaten: einen Verweis auf jede Mail, nie ihren Inhalt.

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

async function lies<T>(antwort: Response): Promise<T> {
  const daten = await antwort.json().catch(() => ({}));
  if (!antwort.ok) {
    throw new Error((daten as { fehler?: string }).fehler ?? `Der Dienst antwortet mit ${antwort.status}.`);
  }
  return daten as T;
}

export async function holeZustand(): Promise<Zustand> {
  return lies<Zustand>(await fetch("/api/zustand"));
}

export async function holeEinordnungen(): Promise<Zeile[]> {
  return (await lies<{ einordnungen: Zeile[] }>(await fetch("/api/einordnungen"))).einordnungen;
}

export async function starteEinordnen(): Promise<Zustand> {
  // Die eigene Kopfzeile zeigt dem Dienst, dass der Knopf hier gedrückt wurde.
  const antwort = await fetch("/api/einordnen", {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-BoticStudio-Aktion": "1" },
    body: "{}",
  });
  return lies<Zustand>(antwort);
}
