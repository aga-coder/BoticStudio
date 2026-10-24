import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import {
  holeEinordnungen,
  holeIch,
  holeKoepfe,
  holeZustand,
  meldeAb,
  meldeAn,
  NichtAngemeldet,
  starteEinordnen,
  type Ich,
  type Koepfe,
  type Kopf,
  type Lauf,
  type Zeile,
  type Zustand,
} from "./api";

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

function Eintrag({ z, kopf, koepfeGeladen }: { z: Zeile; kopf?: Kopf; koepfeGeladen: boolean }) {
  const marke =
    z.stand === "eingeordnet" ? z.kategorie : z.stand === "nicht_eingeordnet" ? "Von Hand prüfen" : "Fehler";
  return (
    <li className={`eintrag eintrag-${z.stand}`}>
      <div className="eintrag-kopf">
        <span className={`marke marke-${z.stand}`}>{marke}</span>
        <span className="zeit">{zeit(kopf?.datum || z.eingeordnet_am)}</span>
      </div>
      {kopf ? (
        <>
          <p className="betreff">{kopf.betreff || "(ohne Betreff)"}</p>
          <p className="absender">{kopf.von}</p>
        </>
      ) : (
        koepfeGeladen && <p className="hinweis">Betreff und Absender nicht abrufbar (Mail älter als 31 Tage oder nicht mehr im Ordner).</p>
      )}
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

function Anmelden({ angemeldet }: { angemeldet: (ich: Ich) => void }) {
  const [email, setEmail] = useState("");
  const [passwort, setPasswort] = useState("");
  const [fehler, setFehler] = useState("");
  const [sendet, setSendet] = useState(false);

  const absenden = async (e: FormEvent) => {
    e.preventDefault();
    setSendet(true);
    try {
      const ich = await meldeAn(email, passwort);
      setPasswort("");
      angemeldet(ich);
    } catch (f) {
      setFehler(f instanceof Error ? f.message : "Anmeldung fehlgeschlagen.");
    } finally {
      setSendet(false);
    }
  };

  return (
    <div className="seite">
      <form className="anmelden" onSubmit={absenden}>
        <h1>Anmelden</h1>
        <label>
          E-Mail
          <input type="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} />
        </label>
        <label>
          Passwort
          <input
            type="password"
            autoComplete="current-password"
            required
            value={passwort}
            onChange={(e) => setPasswort(e.target.value)}
          />
        </label>
        {fehler && (
          <p className="lauf lauf-fehler" role="alert">
            {fehler}
          </p>
        )}
        <button className="knopf" type="submit" disabled={sendet}>
          {sendet ? "Prüft …" : "Anmelden"}
        </button>
      </form>
    </div>
  );
}

export function App() {
  const [ich, setIch] = useState<Ich | null>(null);
  const [fehler, setFehler] = useState("");

  const pruefe = useCallback(async () => {
    try {
      setIch(await holeIch());
      setFehler("");
    } catch (f) {
      setFehler(f instanceof Error ? f.message : "Der Dienst ist nicht erreichbar.");
    }
  }, []);

  useEffect(() => {
    void pruefe();
  }, [pruefe]);

  const abgelaufen = useCallback(() => setIch({ anmeldung_noetig: true, angemeldet: false, person: null, firma: null }), []);
  const abmelden = useCallback(async () => {
    try {
      await meldeAb();
    } finally {
      abgelaufen();
    }
  }, [abgelaufen]);

  if (!ich) {
    return (
      <div className="seite">
        {fehler && (
          <p className="lauf lauf-fehler" role="alert">
            {fehler}
          </p>
        )}
      </div>
    );
  }
  if (ich.anmeldung_noetig && !ich.angemeldet) return <Anmelden angemeldet={setIch} />;

  return <Posteingang ich={ich} abmelden={ich.anmeldung_noetig ? abmelden : undefined} abgelaufen={abgelaufen} />;
}

function Posteingang({ ich, abmelden, abgelaufen }: { ich: Ich; abmelden?: () => void; abgelaufen: () => void }) {
  const [zustand, setZustand] = useState<Zustand | null>(null);
  const [zeilen, setZeilen] = useState<Zeile[]>([]);
  const [fehler, setFehler] = useState("");
  const [filter, setFilter] = useState<Filter>({ art: "alle" });
  const [koepfe, setKoepfe] = useState<Koepfe | null>(null);

  // Ist die Sitzung abgelaufen, geht es zurück zur Anmeldung statt zu einer Fehlermeldung.
  const zeigeFehler = useCallback(
    (f: unknown, sonst: string) => {
      if (f instanceof NichtAngemeldet) abgelaufen();
      else setFehler(f instanceof Error ? f.message : sonst);
    },
    [abgelaufen],
  );

  // Betreff und Absender kommen getrennt und dürfen fehlen: Ist das Postfach
  // nicht erreichbar, bleibt die Liste trotzdem sichtbar.
  const ladeKoepfe = useCallback(async () => {
    try {
      setKoepfe(await holeKoepfe());
    } catch (f) {
      if (f instanceof NichtAngemeldet) return abgelaufen();
      setKoepfe({ bereich: "", koepfe: {}, fehler: f instanceof Error ? f.message : "Nicht erreichbar." });
    }
  }, [abgelaufen]);

  const lade = useCallback(async () => {
    try {
      const [z, e] = await Promise.all([holeZustand(), holeEinordnungen()]);
      setZustand(z);
      setZeilen(e);
      setFehler("");
      if (e.length) void ladeKoepfe();
    } catch (f) {
      zeigeFehler(f, "Der Dienst ist nicht erreichbar.");
    }
  }, [ladeKoepfe, zeigeFehler]);

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
        if (!z.laeuft) {
          setZeilen(await holeEinordnungen());
          void ladeKoepfe();
        }
      } catch (f) {
        zeigeFehler(f, "Der Dienst ist nicht erreichbar.");
      }
    }, ABFRAGE_MS);
    return () => clearInterval(t);
  }, [laeuft, ladeKoepfe, zeigeFehler]);

  const knopf = async () => {
    try {
      setZustand(await starteEinordnen());
      setFehler("");
    } catch (f) {
      zeigeFehler(f, "Das Einordnen ließ sich nicht starten.");
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
      {abmelden && (
        <p className="ich">
          Angemeldet als {ich.person}
          <button className="verweis-knopf" onClick={abmelden}>
            Abmelden
          </button>
        </p>
      )}

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

      {koepfe?.fehler && (
        <p className="lauf lauf-hinweis" role="status">
          Betreff und Absender gerade nicht abrufbar: {koepfe.fehler}
        </p>
      )}

      <section aria-label="Mails">
        {sichtbar.length === 0 ? (
          <p className="leer">{zeilen.length === 0 ? "Noch keine Mails eingeordnet." : "Keine Mails in dieser Auswahl."}</p>
        ) : (
          <ul className="liste">
            {sichtbar.map((z) => (
              <Eintrag
                key={`${z.bereich}/${z.uid}`}
                z={z}
                kopf={koepfe && koepfe.bereich === z.bereich ? koepfe.koepfe[z.uid] : undefined}
                koepfeGeladen={!!koepfe && !koepfe.fehler}
              />
            ))}
          </ul>
        )}
      </section>

      <footer className="fuss">
        <p>
          Gespeichert ist nur ein Verweis auf jede Mail. Betreff und Absender holt das Dashboard bei jedem Laden live aus
          dem Postfach, über den Pförtner, und speichert sie nicht. Den Text einer Mail zeigt es nicht.
        </p>
        <p>
          Die Prüfung auf besondere Daten ist ein erster Filter mit einer Wortliste, keine Garantie. Was die Wortliste
          nicht erkennt, geht an das Modell.
        </p>
      </footer>
    </div>
  );
}
