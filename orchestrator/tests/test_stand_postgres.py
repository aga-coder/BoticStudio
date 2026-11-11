"""Ablaufstände in PostgreSQL: Neustart, Trennung der Firmen, Verschlüsselung.

Läuft nur mit Testdatenbank (siehe README)."""

import os
import secrets

import pytest

from ablauf.orchestrator import LaufUnbekannt
from ablauf.stand import PostgresStaende, StandFehler, lies_schluessel, main, neuer_schluessel
from tests.test_orchestrator import FreigabePfoertner, neu

DSN = os.environ.get("BOTICSTUDIO_TEST_ARBEITSDATEN")
EIGENTUEMER = os.environ.get("BOTICSTUDIO_TEST_ARBEITSDATEN_EIGENTUEMER")
pytestmark = pytest.mark.skipif(not (DSN and EIGENTUEMER), reason="keine Testdatenbank eingerichtet")

SCHLUESSEL = secrets.token_bytes(32)
GEHEIM = "Sehr vertraulicher Entwurf 4711"
TABELLEN = ("arbeit.ablaufschritte", "arbeit.ablaufstaende")


@pytest.fixture
def eigentuemer():
    import psycopg

    with psycopg.connect(EIGENTUEMER, autocommit=True) as v:
        for f in ("py-a", "py-b"):
            with v.transaction():
                v.execute("SELECT set_config('boticstudio.firma', %s, true)", (f,))
                v.execute("INSERT INTO kern.firmen (id) VALUES (%s) ON CONFLICT DO NOTHING", (f,))
                for t in TABELLEN:
                    v.execute(f"DELETE FROM {t}")
        yield v


def zeilen(v, firma, sql):
    with v.transaction():
        v.execute("SELECT set_config('boticstudio.firma', %s, true)", (firma,))
        return v.execute(sql).fetchall()


@pytest.fixture
def staende(eigentuemer):
    s = PostgresStaende(DSN, SCHLUESSEL)
    yield s
    s.schliesse()


def test_weitermachen_nach_neustart(eigentuemer, staende):
    pf = FreigabePfoertner()
    lauf = neu(pf, staende.fuer).starte("probe", "py-a", "ben", {"text": GEHEIM})
    assert lauf.stand == "wartet"
    assert staende.fuer("py-a").laeufe() == [lauf.id]
    staende.schliesse()

    # Neuer Prozess: neue Verbindung, neuer Orchestrator, derselbe Schlüssel.
    pf.entscheide("f-1", True)
    nachher = PostgresStaende(DSN, SCHLUESSEL)
    try:
        o = neu(pf, nachher.fuer)
        assert o.stand("py-a", lauf.id).wartet_auf[0]["anzeige"] == {"text": GEHEIM}
        ende = o.setze_fort("py-a", lauf.id)
        assert ende.stand == "fertig" and pf.gesendet == [GEHEIM]
        assert pf.aufrufe.count("probe_zaehlen") == 1
        # Fertig: Der Stand ist gelöscht.
        assert nachher.fuer("py-a").laeufe() == []
    finally:
        nachher.schliesse()
    for t in TABELLEN:
        assert zeilen(eigentuemer, "py-a", f"SELECT count(*) FROM {t}") == [(0,)]


def test_firmen_sind_getrennt(eigentuemer, staende):
    pf = FreigabePfoertner()
    o = neu(pf, staende.fuer)
    lauf = o.starte("probe", "py-a", "ben", {"text": GEHEIM})
    pf.entscheide("f-1", True)

    # Firma b sieht den Lauf nicht und kann ihn nicht fortsetzen.
    assert staende.fuer("py-b").laeufe() == []
    with pytest.raises(LaufUnbekannt):
        o.setze_fort("py-b", lauf.id)
    assert pf.gesendet == []
    # Auch eine Abfrage ganz ohne Firmenfilter sieht nur die eigene Firma, ohne Firma nichts.
    for t in TABELLEN:
        assert zeilen(eigentuemer, "py-b", f"SELECT count(*) FROM {t}") == [(0,)]
        assert zeilen(eigentuemer, "", f"SELECT count(*) FROM {t}") == [(0,)]
        assert zeilen(eigentuemer, "py-a", f"SELECT count(*) FROM {t}")[0][0] > 0
    # Der Ablauf kann keine Zeile in eine andere Firma schreiben.
    import psycopg

    with psycopg.connect(DSN) as v, pytest.raises(psycopg.Error):
        v.execute("SELECT set_config('boticstudio.firma', 'py-a', true)")
        v.execute("UPDATE arbeit.ablaufstaende SET firma = 'py-b'")


def test_inhalt_ist_verschluesselt_und_an_die_firma_gebunden(eigentuemer, staende):
    pf = FreigabePfoertner()
    lauf = neu(pf, staende.fuer).starte("probe", "py-a", "ben", {"text": GEHEIM})

    for t, spalten in (("arbeit.ablaufstaende", "inhalt, meta"), ("arbeit.ablaufschritte", "inhalt")):
        roh = zeilen(eigentuemer, "py-a", f"SELECT {spalten} FROM {t}")
        assert roh
        for zeile in roh:
            for feld in zeile:
                assert GEHEIM.encode() not in bytes(feld) and b"ben" not in bytes(feld)

    # Falscher Schlüssel: nicht lesbar, und der Fehlertext verrät nichts.
    falsch = PostgresStaende(DSN, secrets.token_bytes(32))
    try:
        with pytest.raises(StandFehler) as f:
            neu(pf, falsch.fuer).stand("py-a", lauf.id)
        assert str(f.value) == "Ablaufstand nicht lesbar."
    finally:
        falsch.schliesse()

    # Eine Zeile, die jemand mit Eigentümer-Rechten in Firma b kopiert, lässt sich dort nicht lesen.
    with eigentuemer.transaction():
        eigentuemer.execute("SELECT set_config('boticstudio.firma', 'py-a', true)")
        kopie = eigentuemer.execute(
            "SELECT lauf, zweig, stand_id, vorher_id, art, inhalt, meta_art, meta FROM arbeit.ablaufstaende"
        ).fetchall()
    with eigentuemer.transaction():
        eigentuemer.execute("SELECT set_config('boticstudio.firma', 'py-b', true)")
        for z in kopie:
            eigentuemer.execute(
                "INSERT INTO arbeit.ablaufstaende (firma, lauf, zweig, stand_id, vorher_id, art, inhalt, meta_art, meta)"
                " VALUES ('py-b', %s, %s, %s, %s, %s, %s, %s, %s)", z,
            )
    with pytest.raises(StandFehler):
        neu(pf, staende.fuer).stand("py-b", lauf.id)


def test_schluesseldatei(tmp_path, monkeypatch, capsys):
    pfad = tmp_path / "stand.schluessel"
    monkeypatch.setattr("sys.argv", ["stand", "--neu", str(pfad)])
    assert main() == 0
    schluessel = lies_schluessel(str(pfad))
    assert len(schluessel) == 32
    if os.name != "nt":
        assert pfad.stat().st_mode & 0o077 == 0
    # Der Schlüssel wird nicht ausgegeben und nicht überschrieben.
    assert pfad.read_text().strip() not in capsys.readouterr().out
    assert main() == 1 and lies_schluessel(str(pfad)) == schluessel
    for inhalt in ("", "kein base64 !!", neuer_schluessel()[:20]):
        pfad.write_text(inhalt)
        with pytest.raises(StandFehler):
            lies_schluessel(str(pfad))
    with pytest.raises(StandFehler):
        lies_schluessel(str(tmp_path / "fehlt"))


def test_falsches_passwort_ohne_passwort_im_fehler():
    from urllib.parse import urlsplit, urlunsplit

    teile = urlsplit(DSN)
    falsch = urlunsplit(teile._replace(netloc=f"{teile.username}:GanzGeheim987@{teile.hostname}:{teile.port or 5432}"))
    with pytest.raises(StandFehler) as f:
        PostgresStaende(falsch, SCHLUESSEL)
    assert "GanzGeheim987" not in str(f.value)
