-- Ablaufstände: wo ein Lauf des Orchestrators gerade steht (Checkpoints).
--
-- Der Orchestrator hält bei "wartet" an und macht nach der Freigabe genau
-- dort weiter, auch nach einem Neustart. Dafür speichert er seinen Stand hier.
--
-- Der Inhalt ist verschlüsselt (AES-256-GCM, an die Firma gebunden). Die
-- Datenbank sieht nur, zu welcher Firma und welchem Lauf eine Zeile gehört.
-- Ist ein Lauf fertig, löscht der Orchestrator seine Zeilen.
--
-- Wie überall trennt Row-Level Security mit FORCE die Firmen.

CREATE TABLE arbeit.ablaufstaende (
    firma          text NOT NULL REFERENCES kern.firmen (id),
    lauf           text NOT NULL CHECK (lauf <> ''),
    zweig          text NOT NULL DEFAULT '',
    stand_id       text NOT NULL CHECK (stand_id <> ''),
    vorher_id      text,
    art            text NOT NULL,
    inhalt         bytea NOT NULL,
    meta_art       text NOT NULL,
    meta           bytea NOT NULL,
    gespeichert_am timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (firma, lauf, zweig, stand_id)
);

-- Zwischenergebnisse eines Schritts, bevor der nächste Stand gespeichert ist.
CREATE TABLE arbeit.ablaufschritte (
    firma    text NOT NULL REFERENCES kern.firmen (id),
    lauf     text NOT NULL,
    zweig    text NOT NULL DEFAULT '',
    stand_id text NOT NULL,
    schritt  text NOT NULL,
    pfad     text NOT NULL DEFAULT '',
    nr       integer NOT NULL,
    kanal    text NOT NULL,
    art      text NOT NULL,
    inhalt   bytea NOT NULL,
    PRIMARY KEY (firma, lauf, zweig, stand_id, schritt, nr)
);

ALTER TABLE arbeit.ablaufstaende  ENABLE ROW LEVEL SECURITY;
ALTER TABLE arbeit.ablaufstaende  FORCE ROW LEVEL SECURITY;
ALTER TABLE arbeit.ablaufschritte ENABLE ROW LEVEL SECURITY;
ALTER TABLE arbeit.ablaufschritte FORCE ROW LEVEL SECURITY;
CREATE POLICY mandant ON arbeit.ablaufstaende  USING (firma = kern.aktuelle_firma()) WITH CHECK (firma = kern.aktuelle_firma());
CREATE POLICY mandant ON arbeit.ablaufschritte USING (firma = kern.aktuelle_firma()) WITH CHECK (firma = kern.aktuelle_firma());

-- Der Orchestrator darf seine eigenen Stände auch löschen (Datensparsamkeit).
GRANT SELECT, INSERT, UPDATE, DELETE ON arbeit.ablaufstaende, arbeit.ablaufschritte TO boticstudio_ablauf;
