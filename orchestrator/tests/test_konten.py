from datetime import timedelta
from functools import partial

import pytest

from ablauf.konten import (
    LAUFZEIT, LEERLAUF, SPERRE, Anmeldung, KontoFehler, ListenKonten, hashe_passwort, pruefe_passwort,
)

SCHNELL = partial(hashe_passwort, n=2**10)  # nur für Tests, im Betrieb 2^17
PASSWORT = "richtig-langes-Passwort"
NEIN = "Anmeldung fehlgeschlagen"


@pytest.fixture
def an():
    a = Anmeldung(ListenKonten(), hashen=SCHNELL)
    a.lege_konto_an("firma-a", "ben", "Ben@Firma-A.de ", PASSWORT)
    return a


def test_hash_ohne_klartext_und_mit_salz():
    h1, h2 = hashe_passwort("geheimes-passwort", n=2**10), hashe_passwort("geheimes-passwort", n=2**10)
    assert h1.startswith("scrypt$1024$8$1$") and "geheimes" not in h1 and h1 != h2
    assert pruefe_passwort("geheimes-passwort", h1) and not pruefe_passwort("falsch", h1)
    assert not pruefe_passwort("x", "kaputt") and not pruefe_passwort("x", "md5$1$2$3$4$5")


def test_betrieb_nutzt_starke_parameter():
    assert hashe_passwort("x" * 12).startswith("scrypt$131072$8$1$")


def test_kurzes_passwort_und_doppelte_adresse(an):
    with pytest.raises(KontoFehler, match="mindestens 12"):
        an.lege_konto_an("firma-a", "eva", "eva@firma-a.de", "kurz")
    with pytest.raises(KontoFehler, match="schon ein Konto"):
        an.lege_konto_an("firma-b", "x", "ben@firma-a.de", PASSWORT)


def test_anmelden_und_sitzung(an):
    sitzung, wert = an.melde_an("  BEN@firma-a.de", PASSWORT)
    assert (sitzung.firma, sitzung.person) == ("firma-a", "ben")
    assert wert.startswith("firma-a.") and PASSWORT not in wert
    s = an.pruefe_sitzung(wert)
    assert s and s.person == "ben"
    # Im Speicher steht nur der Hash des Schlüssels.
    token = wert.split(".", 1)[1]
    assert all(token.encode() not in k[1] for k in an.speicher.sitzungen)


def test_gleiche_antwort_fuer_unbekannt_und_falsch(an):
    with pytest.raises(KontoFehler) as unbekannt:
        an.melde_an("niemand@firma-a.de", PASSWORT)
    with pytest.raises(KontoFehler) as falsch:
        an.melde_an("ben@firma-a.de", "falsches-passwort")
    assert str(unbekannt.value) == str(falsch.value) and NEIN in str(falsch.value)


def test_sperre_nach_fuenf_fehlversuchen(an):
    _, jetzt = an.melde_an("ben@firma-a.de", PASSWORT)[0], None
    for _ in range(5):
        with pytest.raises(KontoFehler):
            an.melde_an("ben@firma-a.de", "falsches-passwort")
    with pytest.raises(KontoFehler):
        an.melde_an("ben@firma-a.de", PASSWORT)  # gesperrt, auch mit richtigem Passwort
    konto = an.speicher.konto("firma-a", "ben")
    spaeter = konto.gesperrt_bis + timedelta(seconds=1)
    assert konto.gesperrt_bis - SPERRE < spaeter
    sitzung, _ = an.melde_an("ben@firma-a.de", PASSWORT, jetzt=spaeter)
    assert sitzung.person == "ben" and an.speicher.konto("firma-a", "ben").fehlversuche == 0


def test_sitzung_laeuft_ab(an):
    sitzung, wert = an.melde_an("ben@firma-a.de", PASSWORT)
    t = sitzung.zuletzt
    # Wer regelmäßig arbeitet, bleibt bis zum Ende der Laufzeit angemeldet.
    schritt = LEERLAUF - timedelta(minutes=1)
    while t + schritt < sitzung.laeuft_ab:
        t += schritt
        assert an.pruefe_sitzung(wert, jetzt=t)
    assert an.pruefe_sitzung(wert, jetzt=sitzung.zuletzt + LAUFZEIT) is None


def test_leerlauf_beendet_sitzung(an):
    sitzung, wert = an.melde_an("ben@firma-a.de", PASSWORT)
    assert an.pruefe_sitzung(wert, jetzt=sitzung.zuletzt + LEERLAUF) is None
    assert an.speicher.sitzungen == {}


def test_abmelden_und_gefaelschte_cookies(an):
    _, wert = an.melde_an("ben@firma-a.de", PASSWORT)
    token = wert.split(".", 1)[1]
    assert an.pruefe_sitzung("firma-b." + token) is None
    assert an.pruefe_sitzung("firma-a.kurz") is None and an.pruefe_sitzung("") is None
    an.melde_ab(wert)
    assert an.pruefe_sitzung(wert) is None


def test_inaktives_konto(an):
    _, wert = an.melde_an("ben@firma-a.de", PASSWORT)
    an.speicher.konten[("firma-a", "ben")].aktiv = False
    assert an.pruefe_sitzung(wert) is None
    with pytest.raises(KontoFehler):
        an.melde_an("ben@firma-a.de", PASSWORT)
