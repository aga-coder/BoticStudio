import { useCallback, useEffect, useMemo, useState } from "react";
import { holeEinordnungen, holeZustand, starteEinordnen, type Lauf, type Zeile, type Zustand } from "./api";

type Filter = { art: "alle" } | { art: "kategorie"; name: string } | { art: "hand" } | { art: "fehler" };

const ABFRAGE_MS = 1500;

function zeit(iso: string | null): string {
  if (!iso) return "";
  return new Intl.DateTimeFormat("de-DE", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" }).format(new Date(iso));
}

function postfachName(bereich: string): string {
  const ohne = bereich.replace(/^postfach\//, "");
  const i = ohne.lastIndexOf("/");
  return i > 0 ? `${ohne.slice(0, i)} · ${ohne.slice(i + 1)}` : ohne;
}

function passt(z: Zeile, f: Filter): boolean {
  switch (f.art) {
    case "alle":
      return true;
    case "kategorie":
      return z.stand === "eingeordnet" && z.kategorie === f.name;
    case "hand":
      return z.stand === "nicht_eingeordnet";
    case "fehler":
      return z.stand === "fehler";
  }
}

function gleich(a: Filter, b: Filter): boolean {
  return a.art === b.art && (a.art !== "kategorie" || (b.art === "kategorie" && a.name === b.name));
}

function LaufZeile({ lauf, laeuft }: { lauf: Lauf | null; laeuft: boolean }) {
  if (laeuft) return <p className="lauf">Ordnet gerade ein …</p>;
  if (!lauf) return <p className="lauf">Noch nicht eingeordnet. Der Knopf liest die Mails der letzten 7 Tage (höchstens 100).</p>;
  if (lauf.abbruch) {
    return (
      <p className="lauf lauf-fehler" role="alert">
        Abgebrochen ({zeit(lauf.ende)}): {lauf.abbruch}
      </p>
    );
  }
  const b = lauf.bericht;
  return (
    <p className="lauf">
      Zuletzt {zeit(lauf.ende)}: {b?.gelesen ?? 0} gelesen, {b?.eingeordnet ?? 0} eingeordnet
      {b?.nicht_eingeordnet ? `, ${b.nicht_eingeordnet} von Hand prüfen` : ""}
      {b?.fehler ? `, ${b.fehler} mit Fehler` : ""}.
    </p>
  );
}

function Eintrag({ z }: { z: Zeile }) {
  const marke =
    z.stand === "eingeordnet" ? z.kategorie : z.stand === "nicht_eingeordnet" ? "Von Hand prüfen" : "Fehler";
  return (
    <li className={`eintrag eintrag-${z.stand}`}>
      <div className="eintrag-kopf">
        <span className={`marke marke-${z.stand}`}>{marke}</span>
        <span className="zeit">{zeit(z.eingeordnet_am)}</span>
      </div>
      {z.art9_verdacht ? (
        <p className="hinweis hinweis-art9">
          Verdacht auf besondere Daten (Art. 9 DSGVO). Diese Mail ging an kein Modell außerhalb der EU. Bitte von Hand
          einordnen.
        </p>
      ) : (
        z.begruendung && <p className="begruendung">{z.begruendung}</p>
      )}
      {z.stand === "eingeordnet" && z.unsicher && <p className="hinweis">Das Modell war sich nicht sicher.</p>}
      <p className="verweis">
        Mail-Nr. {z.uid} in {postfachName(z.bereich)}
        {z.nachweis != null && <> · Nachweis Nr. {z.nachweis}</>}
      </p>
    </li>
  );
}

export function App() {
  const [zustand, setZustand] = useState<Zustand | null>(null);
  const [zeilen, setZeilen] = useState<Zeile[]>([]);
  const [fehler, setFehler] = useState("");
  const [filter, setFilter] = useState<Filter>({ art: "alle" });

  const lade = useCallback(async () => {
    try {
      const [z, e] = await Promise.all([holeZustand(), holeEinordnungen()]);
      setZustand(z);
      setZeilen(e);
      setFehler("");
    } catch (f) {
      setFehler(f instanceof Error ? f.message : "Der Dienst ist nicht erreichbar.");
    }
  }, []);

  useEffect(() => {
    void lade();
  }, [lade]);

  const laeuft = zustand?.laeuft ?? false;
  useEffect(() => {
    if (!laeuft) return;
    const t = setInterval(async () => {
      try {
        const z = await holeZustand();
        setZustand(z);
        if (!z.laeuft) setZeilen(await holeEinordnungen());
      } catch (f) {
        setFehler(f instanceof Error ? f.message : "Der Dienst ist nicht erreichbar.");
      }
    }, ABFRAGE_MS);
    return () => clearInterval(t);
  }, [laeuft]);

  const knopf = async () => {
    try {
      setZustand(await starteEinordnen());
      setFehler("");
    } catch (f) {
      setFehler(f instanceof Error ? f.message : "Das Einordnen ließ sich nicht starten.");
    }
  };

  const kacheln = useMemo(() => {
    const namen = [...(zustand?.kategorien ?? [])];
    for (const z of zeilen) if (z.stand === "eingeordnet" && z.kategorie && !namen.includes(z.kategorie)) namen.push(z.kategorie);
    const liste: { titel: string; filter: Filter; anzahl: number; art: string }[] = namen.map((name) => ({
      titel: name,
      filter: { art: "kategorie", name },
      anzahl: zeilen.filter((z) => passt(z, { art: "kategorie", name })).length,
      art: "kategorie",
    }));
    liste.push({ titel: "Von Hand prüfen", filter: { art: "hand" }, anzahl: zeilen.filter((z) => z.stand === "nicht_eingeordnet").length, art: "hand" });
    const mitFehler = zeilen.filter((z) => z.stand === "fehler").length;
    if (mitFehler) liste.push({ titel: "Fehler", filter: { art: "fehler" }, anzahl: mitFehler, art: "fehler" });
    return liste;
  }, [zustand, zeilen]);

  const sichtbar = zeilen.filter((z) => passt(z, filter));

  return (
    <div className="seite">
      <header className="kopf">
        <div>
          <h1>Posteingang</h1>
          {zustand && <p className="postfach">{postfachName(zustand.postfach)}</p>}
        </div>
        <button className="knopf" onClick={knopf} disabled={!zustand || laeuft}>
          {laeuft ? "Ordnet ein …" : "Jetzt einordnen"}
        </button>
      </header>

      {fehler && (
        <p className="lauf lauf-fehler" role="alert">
          {fehler}
        </p>
      )}
      {zustand && <LaufZeile lauf={zustand.letzter_lauf} laeuft={laeuft} />}

      <nav className="kacheln" aria-label="Nach Kategorie filtern">
        {kacheln.map((k) => {
          const aktiv = gleich(filter, k.filter);
          return (
            <button
              key={k.titel}
              className={`kachel kachel-${k.art}${aktiv ? " aktiv" : ""}`}
              aria-pressed={aktiv}
              onClick={() => setFilter(aktiv ? { art: "alle" } : k.filter)}
            >
              <span className="kachel-zahl">{k.anzahl}</span>
              <span className="kachel-titel">{k.titel}</span>
            </button>
          );
        })}
      </nav>

      <section aria-label="Mails">
        {sichtbar.length === 0 ? (
          <p className="leer">{zeilen.length === 0 ? "Noch keine Mails eingeordnet." : "Keine Mails in dieser Auswahl."}</p>
        ) : (
          <ul className="liste">
            {sichtbar.map((z) => (
              <Eintrag key={`${z.bereich}/${z.uid}`} z={z} />
            ))}
          </ul>
        )}
      </section>

      <footer className="fuss">
        <p>
          Gespeichert ist nur ein Verweis auf jede Mail. Betreff, Absender und Text bleiben im Postfach und werden hier
          noch nicht angezeigt.
        </p>
        <p>
          Die Prüfung auf besondere Daten ist ein erster Filter mit einer Wortliste, keine Garantie. Was die Wortliste
          nicht erkennt, geht an das Modell.
        </p>
      </footer>
    </div>
  );
}
