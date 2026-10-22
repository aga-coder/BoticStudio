-- Arbeitsdaten: Ergebnisse der Abläufe, die das Dashboard zeigt.
--
-- Gespeichert wird nur ein Verweis auf die Mail (Bereich, UID, Message-ID),
-- nie Betreff, Absender oder Text. Die Mail bleibt im Postfach.
--
-- Schreiben darf nur der Ablauf (Rolle boticstudio_ablauf). Er hat keinen
-- Zugriff auf kern und audit. Auch hier trennt Row-Level Security mit FORCE
-- die Firmen.

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'boticstudio_ablauf') THEN
        CREATE ROLE boticstudio_ablauf NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE;
    END IF;
END
$$;

CREATE SCHEMA arbeit;

CREATE TABLE arbeit.einordnungen (
    firma        text NOT NULL REFERENCES kern.firmen (id),
    bereich      text NOT NULL CHECK (bereich LIKE 'postfach/%'),
    uid          text NOT NULL CHECK (uid <> ''),
    message_id   text NOT NULL DEFAULT '',
    -- eingeordnet | nicht_eingeordnet | fehler
    stand        text NOT NULL CHECK (stand IN ('eingeordnet', 'nicht_eingeordnet', 'fehler')),
    kategorie    text,
    begruendung  text NOT NULL DEFAULT '' CHECK (length(begruendung) <= 500),
    unsicher     boolean NOT NULL DEFAULT true,
    markierungen text[] NOT NULL DEFAULT '{}',
    modell       text NOT NULL DEFAULT '',
    aufgabe      text NOT NULL,
    audit_nr     bigint,
    eingeordnet_am timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (firma, bereich, uid),
    CHECK (stand <> 'eingeordnet' OR kategorie IS NOT NULL)
);

ALTER TABLE arbeit.einordnungen ENABLE ROW LEVEL SECURITY;
ALTER TABLE arbeit.einordnungen FORCE ROW LEVEL SECURITY;
CREATE POLICY mandant ON arbeit.einordnungen USING (firma = kern.aktuelle_firma()) WITH CHECK (firma = kern.aktuelle_firma());

GRANT USAGE ON SCHEMA arbeit TO boticstudio_ablauf;
GRANT USAGE ON SCHEMA kern TO boticstudio_ablauf;
GRANT EXECUTE ON FUNCTION kern.aktuelle_firma() TO boticstudio_ablauf;
GRANT SELECT, INSERT, UPDATE ON arbeit.einordnungen TO boticstudio_ablauf;
