-- Konten und Sitzungen für die Anmeldung am Dashboard.
--
-- Ein Konto gehört zu genau einer Person einer Firma (kern.personen). Das
-- Passwort steht nur als scrypt-Hash da, eine Sitzung nur als SHA-256 des
-- Schlüssels im Cookie. Auch hier trennt Row-Level Security mit FORCE die
-- Firmen.
--
-- Beim Anmelden ist die Firma noch nicht bekannt. Dafür gibt es eine
-- Zuordnung vom Hash der E-Mail-Adresse zur Firma (konto.anmeldenamen). Sie
-- ist für alle lesbar, aber sie enthält keine Adresse, nur deren Hash. Alles
-- andere läuft erst, wenn die Firma gesetzt ist.

CREATE SCHEMA konto;

CREATE TABLE konto.konten (
    firma            text NOT NULL,
    person           text NOT NULL,
    email            text NOT NULL CHECK (email = lower(email) AND email LIKE '%_@_%'),
    passwort_hash    text NOT NULL CHECK (passwort_hash LIKE 'scrypt$%'),
    aktiv            boolean NOT NULL DEFAULT true,
    fehlversuche     integer NOT NULL DEFAULT 0 CHECK (fehlversuche >= 0),
    gesperrt_bis     timestamptz,
    angelegt_am      timestamptz NOT NULL DEFAULT now(),
    letzte_anmeldung timestamptz,
    PRIMARY KEY (firma, person),
    FOREIGN KEY (firma, person) REFERENCES kern.personen (firma, id)
);

CREATE TABLE konto.anmeldenamen (
    email_hash bytea PRIMARY KEY CHECK (length(email_hash) = 32),
    firma      text NOT NULL,
    person     text NOT NULL,
    FOREIGN KEY (firma, person) REFERENCES konto.konten (firma, person)
);

CREATE TABLE konto.sitzungen (
    firma       text NOT NULL,
    token_hash  bytea NOT NULL CHECK (length(token_hash) = 32),
    person      text NOT NULL,
    angelegt_am timestamptz NOT NULL DEFAULT now(),
    zuletzt     timestamptz NOT NULL DEFAULT now(),
    laeuft_ab   timestamptz NOT NULL,
    PRIMARY KEY (firma, token_hash),
    FOREIGN KEY (firma, person) REFERENCES konto.konten (firma, person)
);

ALTER TABLE konto.konten ENABLE ROW LEVEL SECURITY;
ALTER TABLE konto.konten FORCE ROW LEVEL SECURITY;
CREATE POLICY mandant ON konto.konten USING (firma = kern.aktuelle_firma()) WITH CHECK (firma = kern.aktuelle_firma());

ALTER TABLE konto.sitzungen ENABLE ROW LEVEL SECURITY;
ALTER TABLE konto.sitzungen FORCE ROW LEVEL SECURITY;
CREATE POLICY mandant ON konto.sitzungen USING (firma = kern.aktuelle_firma()) WITH CHECK (firma = kern.aktuelle_firma());

-- Lesen ohne gesetzte Firma, aber nur über den Hash. Schreiben nur für die eigene Firma.
ALTER TABLE konto.anmeldenamen ENABLE ROW LEVEL SECURITY;
ALTER TABLE konto.anmeldenamen FORCE ROW LEVEL SECURITY;
CREATE POLICY suchen ON konto.anmeldenamen FOR SELECT USING (true);
CREATE POLICY anlegen ON konto.anmeldenamen FOR INSERT WITH CHECK (firma = kern.aktuelle_firma());

-- Das Dashboard läuft als boticstudio_ablauf. Konten löschen kann es nicht,
-- nur Sitzungen (Abmelden).
GRANT USAGE ON SCHEMA konto TO boticstudio_ablauf;
GRANT SELECT, INSERT, UPDATE ON konto.konten TO boticstudio_ablauf;
GRANT SELECT, INSERT ON konto.anmeldenamen TO boticstudio_ablauf;
GRANT SELECT, INSERT, UPDATE, DELETE ON konto.sitzungen TO boticstudio_ablauf;
