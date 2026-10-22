"""Läuft nur mit Testdatenbank (siehe README): BOTICSTUDIO_TEST_ARBEITSDATEN
(Rolle boticstudio_ablauf) und BOTICSTUDIO_TEST_ARBEITSDATEN_EIGENTUEMER."""

import os

import pytest

from ablauf.speicher import Einordnung, PostgresSpeicher

DSN = os.environ.get("BOTICSTUDIO_TEST_ARBEITSDATEN")
EIGENTUEMER = os.environ.get("BOTICSTUDIO_TEST_ARBEITSDATEN_EIGENTUEMER")
pytestmark = pytest.mark.skipif(not (DSN and EIGENTUEMER), reason="keine Testdatenbank eingerichtet")


@pytest.fixture
def eigentuemer():
    import psycopg

    with psycopg.connect(EIGENTUEMER, autocommit=True) as v:
        for f in ("py-a", "py-b"):
            with v.transaction():
                v.execute("SELECT set_config('boticstudio.firma', %s, true)", (f,))
                v.execute("DELETE FROM arbeit.einordnungen WHERE firma = %s", (f,))
                v.execute("INSERT INTO kern.firmen (id) VALUES (%s) ON CONFLICT DO NOTHING", (f,))
        yield v


def zeilen(v, firma):
    with v.transaction():
        v.execute("SELECT set_config('boticstudio.firma', %s, true)", (firma,))
        return v.execute("SELECT uid, stand, kategorie FROM arbeit.einordnungen ORDER BY uid").fetchall()


def test_speichern_und_ueberschreiben(eigentuemer):
    sp = PostgresSpeicher(DSN)
    sp.merke(Einordnung("py-a", "postfach/x/INBOX", "1", "<1>", "fehler", "t1", begruendung="x"))
    sp.merke(Einordnung("py-a", "postfach/x/INBOX", "1", "<1>", "eingeordnet", "t2", kategorie="Anfrage", unsicher=False))
    sp.merke(Einordnung("py-b", "postfach/y/INBOX", "1", "<1>", "nicht_eingeordnet", "t3", markierungen=["art9_verdacht"]))
    sp.schliesse()
    assert zeilen(eigentuemer, "py-a") == [("1", "eingeordnet", "Anfrage")]
    assert zeilen(eigentuemer, "py-b") == [("1", "nicht_eingeordnet", None)]


def test_falsches_passwort_ohne_passwort_im_fehler():
    falsch = DSN.replace("nur-fuer-tests-ablauf", "GanzGeheim987")
    with pytest.raises(RuntimeError) as f:
        PostgresSpeicher(falsch)
    assert "GanzGeheim987" not in str(f.value)
