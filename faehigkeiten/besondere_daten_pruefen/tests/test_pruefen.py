import pytest

from besondere_daten_pruefen.pruefen import MARKIERUNG, pruefe


@pytest.mark.parametrize(
    "text, bereich",
    [
        ("Anbei meine Krankmeldung für diese Woche.", "gesundheit"),
        ("Die AU-Bescheinigung reiche ich nach.", "gesundheit"),
        ("Ich bin SCHWANGER und brauche einen anderen Termin.", "gesundheit"),
        ("Der Befund vom Arzt liegt bei.", "gesundheit"),
        ("Wegen meiner Behinderung brauche ich einen barrierefreien Zugang.", "gesundheit"),
        ("Bitte die Kirchensteuer korrigieren, ich bin evangelisch.", "religion"),
        ("Während Ramadan bitte keine Termine am Abend.", "religion"),
        ("Ich bin Mitglied bei Verdi.", "gewerkschaft"),
        ("Das hat mit meiner politischen   Überzeugung zu tun.", "politik"),
        ("Er wurde wegen seiner Hautfarbe abgelehnt.", "herkunft"),
        ("Meine sexuelle Orientierung geht Sie nichts an.", "sexualleben"),
        ("Der Gentest ist abgeschlossen.", "genetik_biometrie"),
        ("Ich war im Krankenhaus.", "gesundheit"),
        ("Gute   Besserung!", "gesundheit"),
    ],
)
def test_treffer_wird_markiert(text, bereich):
    ergebnis = pruefe({"betreff": "", "text": text})
    assert ergebnis["markierungen"] == [MARKIERUNG]
    assert bereich in ergebnis["hinweise"]


@pytest.mark.parametrize(
    "text",
    [
        "Bitte um ein Angebot für 200 Stück.",
        "Die Lieferung kam beschädigt an, ich möchte mich beschweren.",
        "Wann haben Sie geöffnet?",
        "Er sucht noch einen Termin.",
    ],
)
def test_ohne_treffer_keine_markierung(text):
    ergebnis = pruefe({"betreff": "Frage", "text": text})
    assert ergebnis["markierungen"] == []
    assert ergebnis["hinweise"] == []


def test_betreff_zaehlt_mit():
    assert pruefe({"betreff": "Krankmeldung", "text": "siehe Anhang"})["markierungen"] == [MARKIERUNG]


def test_kein_wort_im_ergebnis():
    # Gemeldet werden nur Bereiche, nie die gefundenen Textstellen.
    ergebnis = pruefe("Meine Diagnose: Depression.")
    assert "diagnose" not in str(ergebnis).lower()
    assert "depression" not in str(ergebnis).lower()


def test_nur_am_wortanfang():
    assert pruefe("Das Zuverdienen ist erlaubt.")["markierungen"] == []
    # Kurze Wörter nur als ganzes Wort.
    assert pruefe("Ihr Verdienst wurde überwiesen")["markierungen"] == []
    assert pruefe("Ich bin bei ver.di")["markierungen"] == ["art9_verdacht"]


def test_falsche_eingabe():
    with pytest.raises(ValueError):
        pruefe(42)
