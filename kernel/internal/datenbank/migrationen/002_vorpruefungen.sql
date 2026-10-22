-- Vorprüfungen: Was eine prüfende Fähigkeit zu einem Inhalt festgestellt hat.
--
-- Gespeichert wird nur der Fingerabdruck (SHA-256) des Inhalts, nie der
-- Inhalt selbst. Einträge werden nur angehängt: Eine Markierung kann
-- niemand nachträglich entfernen, auch der Pförtner nicht.

CREATE TABLE kern.vorpruefungen (
    firma         text NOT NULL REFERENCES kern.firmen (id),
    fingerabdruck text NOT NULL CHECK (fingerabdruck LIKE 'sha256:%'),
    faehigkeit    text NOT NULL,
    markierungen  text[] NOT NULL DEFAULT '{}',
    geprueft_am   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX vorpruefungen_fingerabdruck ON kern.vorpruefungen (firma, fingerabdruck);

ALTER TABLE kern.vorpruefungen ENABLE ROW LEVEL SECURITY;
ALTER TABLE kern.vorpruefungen FORCE ROW LEVEL SECURITY;
CREATE POLICY mandant ON kern.vorpruefungen USING (firma = kern.aktuelle_firma()) WITH CHECK (firma = kern.aktuelle_firma());

GRANT SELECT, INSERT ON kern.vorpruefungen TO boticstudio_pfoertner;
