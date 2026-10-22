-- Grundlage: Firmen, Personen, Erlaubnisse, Aufgaben und das Audit.
--
-- Mandantentrennung: Jede Tabelle mit Firmendaten hat Row-Level Security mit
-- FORCE, damit sie auch für den Eigentümer der Tabellen gilt. Sichtbar ist nur,
-- was zur Firma des laufenden Vorgangs gehört (Einstellung boticstudio.firma,
-- vom Pförtner je Transaktion mit SET LOCAL gesetzt). Ist sie nicht gesetzt,
-- ist nichts sichtbar und nichts schreibbar.
--
-- Der Pförtner meldet sich als boticstudio_pfoertner an: kein Superuser, kein
-- BYPASSRLS, nicht Eigentümer, kein DELETE, im Audit nur Lesen und Anhängen.

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'boticstudio_pfoertner') THEN
        CREATE ROLE boticstudio_pfoertner NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE;
    END IF;
END
$$;

CREATE SCHEMA kern;
CREATE SCHEMA audit;

CREATE FUNCTION kern.aktuelle_firma() RETURNS text
    LANGUAGE sql STABLE
    AS $$ SELECT NULLIF(current_setting('boticstudio.firma', true), '') $$;

CREATE TABLE kern.firmen (
    id          text PRIMARY KEY CHECK (id <> ''),
    angelegt_am timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE kern.ziele (
    firma            text NOT NULL REFERENCES kern.firmen (id),
    art              text NOT NULL,
    adresse          text NOT NULL,
    region           text NOT NULL,
    einwilligung_von text,
    einwilligung_am  timestamptz,
    PRIMARY KEY (firma, art, adresse),
    CHECK (region = 'eu' OR einwilligung_von IS NOT NULL)
);

CREATE TABLE kern.personen (
    firma text NOT NULL REFERENCES kern.firmen (id),
    id    text NOT NULL CHECK (id <> ''),
    rolle text NOT NULL CHECK (rolle IN ('admin', 'leitend', 'ausfuehrend')),
    PRIMARY KEY (firma, id)
);

CREATE TABLE kern.erlaubnisse (
    firma       text NOT NULL,
    id          text NOT NULL CHECK (id <> ''),
    recht       text NOT NULL,
    bereich     text NOT NULL,
    dauer       text NOT NULL CHECK (dauer IN ('einmalig', 'dauerhaft')),
    gueltig_bis timestamptz,
    fuer        text[] NOT NULL DEFAULT '{}',
    erteilt_von text NOT NULL,
    erteilt_am  timestamptz NOT NULL,
    verbraucht  boolean NOT NULL DEFAULT false,
    widerrufen  boolean NOT NULL DEFAULT false,
    PRIMARY KEY (firma, id),
    FOREIGN KEY (firma, erteilt_von) REFERENCES kern.personen (firma, id)
);
CREATE INDEX erlaubnisse_recht ON kern.erlaubnisse (firma, recht);

CREATE TABLE kern.aufgaben (
    firma     text NOT NULL REFERENCES kern.firmen (id),
    id        text NOT NULL CHECK (id <> ''),
    gestoppt  boolean NOT NULL DEFAULT false,
    schritte  integer NOT NULL DEFAULT 0,
    versionen jsonb NOT NULL DEFAULT '{}',
    PRIMARY KEY (firma, id)
);

-- Audit: nur anhängen. Je Firma eine Kette; "ketten" hält das letzte Glied,
-- damit gleichzeitige Vorgänge derselben Firma sauber hintereinander anhängen.
CREATE TABLE audit.ketten (
    firma        text PRIMARY KEY,
    letzte_nr    bigint NOT NULL,
    letzter_hash text NOT NULL
);

CREATE TABLE audit.eintraege (
    firma       text NOT NULL,
    nr          bigint NOT NULL,
    zeit        timestamptz NOT NULL,
    art         text NOT NULL,
    inhalt      jsonb NOT NULL,
    vorher_hash text NOT NULL,
    hash        text NOT NULL,
    PRIMARY KEY (firma, nr)
);

CREATE FUNCTION audit.unveraenderlich() RETURNS trigger
    LANGUAGE plpgsql
    AS $$ BEGIN RAISE EXCEPTION 'audit ist nur zum anhängen'; END $$;

CREATE TRIGGER nur_anhaengen BEFORE UPDATE OR DELETE ON audit.eintraege
    FOR EACH ROW EXECUTE FUNCTION audit.unveraenderlich();
CREATE TRIGGER nie_leeren BEFORE TRUNCATE ON audit.eintraege
    FOR EACH STATEMENT EXECUTE FUNCTION audit.unveraenderlich();

-- Row-Level Security, auch für den Eigentümer.
ALTER TABLE kern.firmen      ENABLE ROW LEVEL SECURITY;
ALTER TABLE kern.firmen      FORCE ROW LEVEL SECURITY;
ALTER TABLE kern.ziele       ENABLE ROW LEVEL SECURITY;
ALTER TABLE kern.ziele       FORCE ROW LEVEL SECURITY;
ALTER TABLE kern.personen    ENABLE ROW LEVEL SECURITY;
ALTER TABLE kern.personen    FORCE ROW LEVEL SECURITY;
ALTER TABLE kern.erlaubnisse ENABLE ROW LEVEL SECURITY;
ALTER TABLE kern.erlaubnisse FORCE ROW LEVEL SECURITY;
ALTER TABLE kern.aufgaben    ENABLE ROW LEVEL SECURITY;
ALTER TABLE kern.aufgaben    FORCE ROW LEVEL SECURITY;
ALTER TABLE audit.ketten     ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit.ketten     FORCE ROW LEVEL SECURITY;
ALTER TABLE audit.eintraege  ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit.eintraege  FORCE ROW LEVEL SECURITY;

CREATE POLICY mandant ON kern.firmen      USING (id = kern.aktuelle_firma())    WITH CHECK (id = kern.aktuelle_firma());
CREATE POLICY mandant ON kern.ziele       USING (firma = kern.aktuelle_firma()) WITH CHECK (firma = kern.aktuelle_firma());
CREATE POLICY mandant ON kern.personen    USING (firma = kern.aktuelle_firma()) WITH CHECK (firma = kern.aktuelle_firma());
CREATE POLICY mandant ON kern.erlaubnisse USING (firma = kern.aktuelle_firma()) WITH CHECK (firma = kern.aktuelle_firma());
CREATE POLICY mandant ON kern.aufgaben    USING (firma = kern.aktuelle_firma()) WITH CHECK (firma = kern.aktuelle_firma());
CREATE POLICY mandant ON audit.ketten     USING (firma = kern.aktuelle_firma()) WITH CHECK (firma = kern.aktuelle_firma());
CREATE POLICY mandant ON audit.eintraege  USING (firma = kern.aktuelle_firma()) WITH CHECK (firma = kern.aktuelle_firma());

-- Rechte der Pförtner-Rolle: kein DELETE, im Audit kein UPDATE.
GRANT USAGE ON SCHEMA kern, audit TO boticstudio_pfoertner;
GRANT EXECUTE ON FUNCTION kern.aktuelle_firma() TO boticstudio_pfoertner;
GRANT SELECT, INSERT, UPDATE ON kern.firmen, kern.ziele, kern.personen, kern.erlaubnisse, kern.aufgaben TO boticstudio_pfoertner;
GRANT SELECT, INSERT, UPDATE ON audit.ketten TO boticstudio_pfoertner;
GRANT SELECT, INSERT ON audit.eintraege TO boticstudio_pfoertner;
