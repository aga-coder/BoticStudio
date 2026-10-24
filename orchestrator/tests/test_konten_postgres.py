"""Läuft nur mit Testdatenbank (siehe README), wie test_speicher_postgres."""

import os
import uuid
from datetime import datetime, timedelta, timezone
from functools import partial
from urllib.parse import urlsplit, urlunsplit

import pytest

from ablauf.konten import Anmeldung, KontoFehler, PostgresKonten, email_hash, hashe_passwort

DSN = os.environ.get("BOTICSTUDIO_TEST_ARBEITSDATEN")
EIGENTUEMER = os.environ.get("BOTICSTUDIO_TEST_ARBEITSDATEN_EIGENTUEMER")
pytestmark = pytest.mark.skipif(not (DSN and EIGENTUEMER), reason="keine Testdatenbank eingerichtet")

SCHNELL = partial(hashe_passwort, n=2**10)
PASSWORT = "ein-langes-passwort"


@pytest.fixture
def eigentuemer():
    import psycopg

    # Anmeldenamen lassen sich nicht löschen (keine Regel dafür), deshalb neue Firmen je Test.
    global A, B
    lauf = uuid.uuid4().hex[:8]
    A, B = f"kt-a-{lauf}", f"kt-b-{lauf}"
    with psycopg.connect(EIGENTUEMER, autocommit=True) as v:
        for f in (A, B):
            with v.transaction():
                v.execute("SELECT set_config('boticstudio.firma', %s, true)", (f,))
                v.execute("INSERT INTO kern.firmen (id) VALUES (%s)", (f,))
                v.execute("INSERT INTO kern.personen (firma, id, rolle) VALUES (%s, 'chefin', 'admin')", (f,))
        yield v


@pytest.fixture
def konten(eigentuemer):
    k = PostgresKonten(DSN)
    yield k
    k.schliesse()


def test_anmelden_und_sitzung(konten):
    anm = Anmeldung(konten, hashen=SCHNELL)
    anm.lege_konto_an(A, "chefin", f"chefin@{A}.de", PASSWORT)
    sitzung, wert = anm.melde_an(f"Chefin@{A}.de", PASSWORT)
    assert (sitzung.firma, sitzung.person) == (A, "chefin")
    assert anm.pruefe_sitzung(wert).person == "chefin"
    anm.melde_ab(wert)
    assert anm.pruefe_sitzung(wert) is None


def test_leerlauf_in_der_datenbank(konten):
    anm = Anmeldung(konten, hashen=SCHNELL)
    anm.lege_konto_an(A, "chefin", f"chefin@{A}.de", PASSWORT)
    jetzt = datetime.now(timezone.utc)
    _, wert = anm.melde_an(f"chefin@{A}.de", PASSWORT, jetzt=jetzt)
    assert anm.pruefe_sitzung(wert, jetzt=jetzt + timedelta(minutes=59)) is not None
    assert anm.pruefe_sitzung(wert, jetzt=jetzt + timedelta(minutes=59 + 61)) is None


def test_gespeichert_nur_hashes(konten, eigentuemer):
    anm = Anmeldung(konten, hashen=SCHNELL)
    anm.lege_konto_an(A, "chefin", f"chefin@{A}.de", PASSWORT)
    _, wert = anm.melde_an(f"chefin@{A}.de", PASSWORT)
    token = wert.rpartition(".")[2]
    with eigentuemer.transaction():
        eigentuemer.execute("SELECT set_config('boticstudio.firma', %s, true)", (A,))
        alles = str(eigentuemer.execute(
            "SELECT k.*, s.* FROM konto.konten k JOIN konto.sitzungen s USING (firma, person)"
        ).fetchall())
    assert PASSWORT not in alles and token not in alles and "scrypt$" in alles


def test_firmen_sind_getrennt(konten):
    import psycopg

    anm = Anmeldung(konten, hashen=SCHNELL)
    anm.lege_konto_an(A, "chefin", f"chefin@{A}.de", PASSWORT)
    anm.lege_konto_an(B, "chefin", f"chefin@{B}.de", PASSWORT)
    _, wert_a = anm.melde_an(f"chefin@{A}.de", PASSWORT)
    # Den Schlüssel von A mit Firma B vorzeigen: findet nichts.
    assert anm.pruefe_sitzung(B + "." + wert_a.rpartition(".")[2]) is None
    assert konten.konto(B, "chefin").email == f"chefin@{B}.de"

    with psycopg.connect(DSN, autocommit=True) as v:
        with v.transaction():
            v.execute("SELECT set_config('boticstudio.firma', %s, true)", (A,))
            assert v.execute("SELECT DISTINCT firma FROM konto.konten").fetchall() == [(A,)]
            assert v.execute("SELECT DISTINCT firma FROM konto.sitzungen").fetchall() == [(A,)]
        # Ohne Firma: keine Konten sichtbar, aber die Suche nach dem Hash geht.
        with v.transaction():
            assert v.execute("SELECT count(*) FROM konto.konten").fetchone() == (0,)
            assert v.execute("SELECT firma FROM konto.anmeldenamen WHERE email_hash = %s", (email_hash(f"chefin@{B}.de"),)).fetchone() == (B,)
        # Einen Anmeldenamen für eine fremde Firma anlegen: verboten.
        with pytest.raises(psycopg.Error), v.transaction():
            v.execute("SELECT set_config('boticstudio.firma', %s, true)", (A,))
            v.execute("INSERT INTO konto.anmeldenamen VALUES (%s, %s, 'chefin')", (email_hash(f"neu@{B}.de"), B))
        # Konten löschen darf das Dashboard nicht.
        with pytest.raises(psycopg.Error), v.transaction():
            v.execute("SELECT set_config('boticstudio.firma', %s, true)", (A,))
            v.execute("DELETE FROM konto.konten")


def test_fehler_ohne_geheimnisse(konten):
    anm = Anmeldung(konten, hashen=SCHNELL)
    with pytest.raises(KontoFehler, match="Person gibt es"):
        anm.lege_konto_an(A, "unbekannt", f"x@{A}.de", PASSWORT)
    anm.lege_konto_an(A, "chefin", f"chefin@{A}.de", PASSWORT)
    with pytest.raises(KontoFehler) as f:
        anm.lege_konto_an(B, "chefin", f"chefin@{A}.de", PASSWORT)
    assert f"chefin@{A}.de" not in str(f.value)

    teile = urlsplit(DSN)
    falsch = urlunsplit(teile._replace(netloc=f"{teile.username}:GanzGeheim987@{teile.hostname}:{teile.port or 5432}"))
    with pytest.raises(RuntimeError) as f:
        PostgresKonten(falsch)
    assert "GanzGeheim987" not in str(f.value)
