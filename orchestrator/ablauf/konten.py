"""Konten und Sitzungen für die Anmeldung am Dashboard.

- Passwörter nur als scrypt-Hash (Salz je Konto, Parameter im Hash).
- Sitzungen: ein zufälliger Schlüssel im Cookie, gespeichert nur als SHA-256.
  Eine Sitzung läuft nach 8 Stunden ab, und nach 60 Minuten ohne Nutzung.
- Nach 5 Fehlversuchen ist ein Konto 15 Minuten gesperrt. Die Antwort sagt
  nie, ob es die Adresse gibt oder ob nur das Passwort falsch war.
- In PostgreSQL trennt Row-Level Security die Firmen. Beim Anmelden findet
  der Hash der E-Mail-Adresse die Firma, danach läuft alles in ihr.

Passwörter, Schlüssel und E-Mail-Adressen stehen in keinem Log und in keiner
Fehlermeldung.
"""

from __future__ import annotations

import base64
import hashlib
import hmac
import secrets
import threading
from dataclasses import dataclass, replace
from datetime import datetime, timedelta, timezone
from typing import Protocol

MIN_LAENGE = 12
MAX_FEHLVERSUCHE = 5
SPERRE = timedelta(minutes=15)
LAUFZEIT = timedelta(hours=8)
LEERLAUF = timedelta(minutes=60)

# scrypt nach OWASP: N=2^17, r=8, p=1 (rund 128 MiB je Prüfung).
SCRYPT_N, SCRYPT_R, SCRYPT_P = 2**17, 8, 1


class KontoFehler(Exception):
    """Ein Fehler, dessen Text gefahrlos an den Nutzer gehen darf."""


@dataclass
class Konto:
    firma: str
    person: str
    email: str
    passwort_hash: str
    aktiv: bool = True
    fehlversuche: int = 0
    gesperrt_bis: datetime | None = None


@dataclass
class Sitzung:
    firma: str
    person: str
    laeuft_ab: datetime
    zuletzt: datetime


def _jetzt() -> datetime:
    return datetime.now(timezone.utc)


def normalisiere(email: str) -> str:
    return email.strip().lower()


def email_hash(email: str) -> bytes:
    return hashlib.sha256(normalisiere(email).encode()).digest()


def hashe_passwort(passwort: str, n: int = SCRYPT_N, r: int = SCRYPT_R, p: int = SCRYPT_P) -> str:
    salz = secrets.token_bytes(16)
    wert = hashlib.scrypt(passwort.encode(), salt=salz, n=n, r=r, p=p, maxmem=256 * 1024 * 1024, dklen=32)
    b64 = lambda b: base64.b64encode(b).decode()  # noqa: E731
    return f"scrypt${n}${r}${p}${b64(salz)}${b64(wert)}"


def pruefe_passwort(passwort: str, gespeichert: str) -> bool:
    try:
        art, n, r, p, salz, wert = gespeichert.split("$")
        if art != "scrypt":
            return False
        neu = hashlib.scrypt(
            passwort.encode(), salt=base64.b64decode(salz), n=int(n), r=int(r), p=int(p),
            maxmem=256 * 1024 * 1024, dklen=32,
        )
    except (ValueError, TypeError):
        return False
    return hmac.compare_digest(neu, base64.b64decode(wert))


def pruefe_neues_passwort(passwort: str) -> None:
    if len(passwort) < MIN_LAENGE:
        raise KontoFehler(f"Das Passwort braucht mindestens {MIN_LAENGE} Zeichen.")


def _token_hash(token: str) -> bytes:
    return hashlib.sha256(token.encode()).digest()


def baue_cookie_wert(firma: str, token: str) -> str:
    return f"{firma}.{token}"


def zerlege_cookie_wert(wert: str) -> tuple[str, str] | None:
    firma, punkt, token = wert.rpartition(".")
    if not punkt or not firma or len(token) < 32:
        return None
    return firma, token


class KontenSpeicher(Protocol):
    def finde_firma(self, ehash: bytes) -> tuple[str, str] | None: ...
    def konto(self, firma: str, person: str) -> Konto | None: ...
    def lege_an(self, konto: Konto) -> None: ...
    def merke_versuch(self, firma: str, person: str, fehlversuche: int, gesperrt_bis: datetime | None, erfolg: bool) -> None: ...
    def neue_sitzung(self, firma: str, thash: bytes, person: str, laeuft_ab: datetime, jetzt: datetime) -> None: ...
    def sitzung(self, firma: str, thash: bytes) -> Sitzung | None: ...
    def nutze_sitzung(self, firma: str, thash: bytes, zuletzt: datetime) -> None: ...
    def beende_sitzung(self, firma: str, thash: bytes) -> None: ...


class Anmeldung:
    """Die Regeln der Anmeldung, unabhängig vom Speicher."""

    def __init__(self, speicher: KontenSpeicher, hashen=hashe_passwort) -> None:
        self.speicher = speicher
        self._hashen = hashen
        # Für unbekannte Adressen wird trotzdem ein Hash geprüft: gleiche Dauer, kein Hinweis.
        self._attrappe = hashen(secrets.token_urlsafe(16))

    def lege_konto_an(self, firma: str, person: str, email: str, passwort: str) -> None:
        pruefe_neues_passwort(passwort)
        email = normalisiere(email)
        if "@" not in email:
            raise KontoFehler("Das ist keine E-Mail-Adresse.")
        if self.speicher.finde_firma(email_hash(email)):
            raise KontoFehler("Für diese E-Mail-Adresse gibt es schon ein Konto.")
        self.speicher.lege_an(Konto(firma, person, email, self._hashen(passwort)))

    def melde_an(self, email: str, passwort: str, jetzt: datetime | None = None) -> tuple[Sitzung, str]:
        """Liefert Sitzung und Cookie-Wert. Jeder Fehler hat denselben Text."""
        jetzt = jetzt or _jetzt()
        nein = KontoFehler("Anmeldung fehlgeschlagen. Nach mehreren Fehlversuchen ist das Konto 15 Minuten gesperrt.")
        treffer = self.speicher.finde_firma(email_hash(email))
        konto = self.speicher.konto(*treffer) if treffer else None
        if konto is None:
            pruefe_passwort(passwort, self._attrappe)
            raise nein
        if konto.gesperrt_bis and konto.gesperrt_bis > jetzt:
            pruefe_passwort(passwort, self._attrappe)
            raise nein
        if not pruefe_passwort(passwort, konto.passwort_hash) or not konto.aktiv:
            fehl = konto.fehlversuche + 1
            sperre = jetzt + SPERRE if fehl >= MAX_FEHLVERSUCHE else None
            self.speicher.merke_versuch(konto.firma, konto.person, 0 if sperre else fehl, sperre, erfolg=False)
            raise nein
        self.speicher.merke_versuch(konto.firma, konto.person, 0, None, erfolg=True)
        token = secrets.token_urlsafe(32)
        sitzung = Sitzung(konto.firma, konto.person, jetzt + LAUFZEIT, jetzt)
        self.speicher.neue_sitzung(konto.firma, _token_hash(token), konto.person, sitzung.laeuft_ab, jetzt)
        return sitzung, baue_cookie_wert(konto.firma, token)

    def pruefe_sitzung(self, cookie_wert: str, jetzt: datetime | None = None) -> Sitzung | None:
        jetzt = jetzt or _jetzt()
        teile = zerlege_cookie_wert(cookie_wert)
        if not teile:
            return None
        firma, token = teile
        thash = _token_hash(token)
        s = self.speicher.sitzung(firma, thash)
        if s is None:
            return None
        if s.laeuft_ab <= jetzt or s.zuletzt + LEERLAUF <= jetzt:
            self.speicher.beende_sitzung(firma, thash)
            return None
        konto = self.speicher.konto(firma, s.person)
        if konto is None or not konto.aktiv:
            return None
        self.speicher.nutze_sitzung(firma, thash, jetzt)
        return replace(s, zuletzt=jetzt)

    def melde_ab(self, cookie_wert: str) -> None:
        teile = zerlege_cookie_wert(cookie_wert)
        if teile:
            self.speicher.beende_sitzung(teile[0], _token_hash(teile[1]))


class ListenKonten:
    """Für Tests."""

    def __init__(self) -> None:
        self._sperre = threading.Lock()
        self.konten: dict[tuple[str, str], Konto] = {}
        self.namen: dict[bytes, tuple[str, str]] = {}
        self.sitzungen: dict[tuple[str, bytes], Sitzung] = {}

    def finde_firma(self, ehash: bytes) -> tuple[str, str] | None:
        return self.namen.get(ehash)

    def konto(self, firma: str, person: str) -> Konto | None:
        k = self.konten.get((firma, person))
        return replace(k) if k else None

    def lege_an(self, konto: Konto) -> None:
        with self._sperre:
            self.konten[(konto.firma, konto.person)] = konto
            self.namen[email_hash(konto.email)] = (konto.firma, konto.person)

    def merke_versuch(self, firma, person, fehlversuche, gesperrt_bis, erfolg) -> None:
        with self._sperre:
            k = self.konten[(firma, person)]
            k.fehlversuche, k.gesperrt_bis = fehlversuche, gesperrt_bis

    def neue_sitzung(self, firma, thash, person, laeuft_ab, jetzt) -> None:
        with self._sperre:
            self.sitzungen[(firma, thash)] = Sitzung(firma, person, laeuft_ab, jetzt)

    def sitzung(self, firma, thash) -> Sitzung | None:
        s = self.sitzungen.get((firma, thash))
        return replace(s) if s else None

    def nutze_sitzung(self, firma, thash, zuletzt) -> None:
        with self._sperre:
            if (firma, thash) in self.sitzungen:
                self.sitzungen[(firma, thash)].zuletzt = zuletzt

    def beende_sitzung(self, firma, thash) -> None:
        with self._sperre:
            self.sitzungen.pop((firma, thash), None)


class PostgresKonten:
    """Jeder Zugriff setzt zuerst die Firma; Row-Level Security sorgt für den Rest."""

    def __init__(self, dsn: str) -> None:
        import psycopg

        self._sperre = threading.Lock()
        try:
            self._v = psycopg.connect(dsn, autocommit=False)
        except psycopg.Error:
            raise RuntimeError("Konten-Datenbank nicht erreichbar.") from None

    def _in_firma(self, firma: str | None, sql: str, werte: tuple = (), holen: bool = False):
        import psycopg

        try:
            with self._sperre, self._v.transaction(), self._v.cursor() as cur:
                if firma is not None:
                    cur.execute("SELECT set_config('boticstudio.firma', %s, true)", (firma,))
                cur.execute(sql, werte)
                return cur.fetchall() if holen else None
        except psycopg.errors.UniqueViolation:
            raise KontoFehler("Für diese E-Mail-Adresse gibt es schon ein Konto.") from None
        except psycopg.errors.ForeignKeyViolation:
            raise KontoFehler("Diese Person gibt es in der Firma nicht.") from None
        except psycopg.Error:
            raise RuntimeError("Konten-Datenbank: Vorgang fehlgeschlagen.") from None

    def finde_firma(self, ehash: bytes) -> tuple[str, str] | None:
        z = self._in_firma(None, "SELECT firma, person FROM konto.anmeldenamen WHERE email_hash = %s", (ehash,), True)
        return (z[0][0], z[0][1]) if z else None

    def konto(self, firma: str, person: str) -> Konto | None:
        z = self._in_firma(
            firma,
            "SELECT firma, person, email, passwort_hash, aktiv, fehlversuche, gesperrt_bis FROM konto.konten WHERE person = %s",
            (person,), True,
        )
        return Konto(*z[0]) if z else None

    def lege_an(self, konto: Konto) -> None:
        import psycopg

        try:
            with self._sperre, self._v.transaction(), self._v.cursor() as cur:
                cur.execute("SELECT set_config('boticstudio.firma', %s, true)", (konto.firma,))
                cur.execute(
                    "INSERT INTO konto.konten (firma, person, email, passwort_hash) VALUES (%s, %s, %s, %s)",
                    (konto.firma, konto.person, konto.email, konto.passwort_hash),
                )
                cur.execute(
                    "INSERT INTO konto.anmeldenamen (email_hash, firma, person) VALUES (%s, %s, %s)",
                    (email_hash(konto.email), konto.firma, konto.person),
                )
        except psycopg.errors.UniqueViolation:
            raise KontoFehler("Für diese E-Mail-Adresse oder Person gibt es schon ein Konto.") from None
        except psycopg.errors.ForeignKeyViolation:
            raise KontoFehler("Diese Person gibt es in der Firma nicht.") from None
        except psycopg.Error:
            raise RuntimeError("Konten-Datenbank: Konto ließ sich nicht anlegen.") from None

    def merke_versuch(self, firma, person, fehlversuche, gesperrt_bis, erfolg) -> None:
        self._in_firma(
            firma,
            """UPDATE konto.konten SET fehlversuche = %s, gesperrt_bis = %s,
                 letzte_anmeldung = CASE WHEN %s THEN now() ELSE letzte_anmeldung END
               WHERE person = %s""",
            (fehlversuche, gesperrt_bis, erfolg, person),
        )

    def neue_sitzung(self, firma, thash, person, laeuft_ab, jetzt) -> None:
        self._in_firma(
            firma,
            "INSERT INTO konto.sitzungen (firma, token_hash, person, angelegt_am, zuletzt, laeuft_ab) VALUES (%s, %s, %s, %s, %s, %s)",
            (firma, thash, person, jetzt, jetzt, laeuft_ab),
        )

    def sitzung(self, firma, thash) -> Sitzung | None:
        z = self._in_firma(
            firma, "SELECT firma, person, laeuft_ab, zuletzt FROM konto.sitzungen WHERE token_hash = %s", (thash,), True
        )
        return Sitzung(*z[0]) if z else None

    def nutze_sitzung(self, firma, thash, zuletzt) -> None:
        self._in_firma(firma, "UPDATE konto.sitzungen SET zuletzt = %s WHERE token_hash = %s", (zuletzt, thash))

    def beende_sitzung(self, firma, thash) -> None:
        self._in_firma(firma, "DELETE FROM konto.sitzungen WHERE token_hash = %s", (thash,))

    def schliesse(self) -> None:
        self._v.close()
