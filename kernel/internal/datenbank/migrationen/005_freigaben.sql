-- Freigaben: Bestätigung eines Menschen für genau einen Schritt.
--
-- Sagt der Pförtner "wartet", legt er hier eine Zeile an. Sie nennt Aufgabe,
-- Fähigkeit, Bereich, Ziel und die Fingerabdrücke der Daten, nie die Daten
-- selbst. Eine leitende Person bestätigt oder lehnt ab. Danach läuft genau
-- dieser Schritt einmal, dann ist die Freigabe verbraucht.
--
-- Der Pförtner darf Zeilen anlegen und ihren Stand ändern, aber nicht löschen.
-- Jede Änderung steht zusätzlich im Audit.

CREATE TABLE kern.freigaben (
    firma           text NOT NULL,
    id              text NOT NULL CHECK (id <> ''),
    aufgabe         text NOT NULL,
    person          text NOT NULL,
    faehigkeit      text NOT NULL,
    version         text NOT NULL,
    bereich         text NOT NULL DEFAULT '',
    ziel            text NOT NULL DEFAULT '',
    schritt         text NOT NULL CHECK (schritt LIKE 'sha256:%'),
    daten           jsonb NOT NULL DEFAULT '{}',
    stand           text NOT NULL CHECK (stand IN ('offen', 'bestaetigt', 'abgelehnt', 'verbraucht')),
    angefragt_am    timestamptz NOT NULL,
    gueltig_bis     timestamptz NOT NULL,
    entschieden_von text,
    entschieden_am  timestamptz,
    PRIMARY KEY (firma, id),
    FOREIGN KEY (firma, person) REFERENCES kern.personen (firma, id),
    FOREIGN KEY (firma, entschieden_von) REFERENCES kern.personen (firma, id),
    -- Offen heißt: noch niemand hat entschieden. Jeder andere Stand braucht einen Menschen.
    CHECK ((stand = 'offen') = (entschieden_von IS NULL)),
    CHECK ((entschieden_von IS NULL) = (entschieden_am IS NULL))
);
CREATE INDEX freigaben_schritt ON kern.freigaben (firma, aufgabe, schritt, angefragt_am);
CREATE INDEX freigaben_offen ON kern.freigaben (firma, angefragt_am) WHERE stand = 'offen';

ALTER TABLE kern.freigaben ENABLE ROW LEVEL SECURITY;
ALTER TABLE kern.freigaben FORCE ROW LEVEL SECURITY;
CREATE POLICY mandant ON kern.freigaben USING (firma = kern.aktuelle_firma()) WITH CHECK (firma = kern.aktuelle_firma());

GRANT SELECT, INSERT, UPDATE ON kern.freigaben TO boticstudio_pfoertner;
