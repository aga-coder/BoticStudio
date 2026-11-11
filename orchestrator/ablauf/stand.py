"""Ablaufstände: wo ein Lauf des Orchestrators gerade steht (Checkpoints).

Der Orchestrator hält bei "wartet" an. Damit er nach der Freigabe genau dort
weitermacht, auch nach einem Neustart, speichert er seinen Stand in PostgreSQL.

- Getrennt nach Firma: Jeder Zugriff setzt die Firma, Row-Level Security zeigt
  und nimmt nur deren Zeilen. Ein Stand-Speicher gehört zu genau einer Firma.
- Verschlüsselt: Der Inhalt ist mit AES-256-GCM verschlüsselt und an die Firma
  gebunden. Eine Zeile, die jemand in eine andere Firma kopiert, lässt sich
  nicht entschlüsseln. Die Datenbank sieht nur Firma und Lauf.
- Der Schlüssel steht in einer Datei außerhalb des Repos
  (BOTICSTUDIO_ABLAUFSTAND_SCHLUESSEL_DATEI), nie in einem Log. Anlegen:

      python -m ablauf.stand --neu /ausserhalb/des/repos/ablaufstand.schluessel

Ein Schlüssel gilt bisher für alle Firmen. Je Firma ein eigener Schlüssel aus
dem Schlüsseldienst (STACKIT KMS) kommt mit dem Hosting.
"""

from __future__ import annotations

import argparse
import base64
import os
import secrets
import sys
import threading
from collections.abc import Iterator, Sequence
from pathlib import Path
from typing import Any

from langchain_core.runnables import RunnableConfig
from langgraph.checkpoint.base import (
    WRITES_IDX_MAP,
    BaseCheckpointSaver,
    ChannelVersions,
    Checkpoint,
    CheckpointMetadata,
    CheckpointTuple,
    get_checkpoint_id,
    get_checkpoint_metadata,
)
from langgraph.checkpoint.memory import InMemorySaver
from langgraph.checkpoint.serde.base import CipherProtocol
from langgraph.checkpoint.serde.encrypted import EncryptedSerializer
from langgraph.checkpoint.serde.jsonplus import JsonPlusSerializer

from .geheimdatei import schreibe_geheim

UMGEBUNG_SCHLUESSEL = "BOTICSTUDIO_ABLAUFSTAND_SCHLUESSEL_DATEI"


class StandFehler(RuntimeError):
    """Text ohne Inhalte, Schlüssel oder Verbindungsangaben."""


def neuer_schluessel() -> str:
    return base64.urlsafe_b64encode(secrets.token_bytes(32)).decode()


def lies_schluessel(pfad: str) -> bytes:
    try:
        with open(pfad, encoding="utf-8") as f:
            roh = base64.urlsafe_b64decode(f.read().strip())
    except (OSError, ValueError):
        raise StandFehler("Schlüssel für Ablaufstände nicht lesbar.") from None
    if len(roh) != 32:
        raise StandFehler("Schlüssel für Ablaufstände ungültig.")
    return roh


class _FirmenChiffre(CipherProtocol):
    """AES-256-GCM. Die Firma geht als Zusatzangabe in die Prüfsumme ein."""

    def __init__(self, schluessel: bytes, firma: str) -> None:
        if len(schluessel) != 32:
            raise StandFehler("Schlüssel für Ablaufstände ungültig.")
        self._schluessel, self._firma = schluessel, firma.encode()

    def __repr__(self) -> str:
        return "_FirmenChiffre()"

    def _aes(self, nonce: bytes):
        from Crypto.Cipher import AES

        aes = AES.new(self._schluessel, AES.MODE_GCM, nonce=nonce)
        aes.update(self._firma)
        return aes

    def encrypt(self, plaintext: bytes) -> tuple[str, bytes]:
        nonce = secrets.token_bytes(12)
        geheim, pruefsumme = self._aes(nonce).encrypt_and_digest(plaintext)
        return "aesgcm", nonce + pruefsumme + geheim

    def decrypt(self, ciphername: str, ciphertext: bytes) -> bytes:
        if ciphername != "aesgcm" or len(ciphertext) < 28:
            raise StandFehler("Ablaufstand nicht lesbar.")
        try:
            return self._aes(ciphertext[:12]).decrypt_and_verify(ciphertext[28:], ciphertext[12:28])
        except ValueError:
            # Falscher Schlüssel, andere Firma oder veränderte Zeile.
            raise StandFehler("Ablaufstand nicht lesbar.") from None


class ArbeitsspeicherStand(InMemorySaver):
    """Für Tests und den Betrieb ohne Datenbank. Nach einem Neustart ist alles weg."""

    def laeufe(self) -> list[str]:
        """Alle Läufe mit Stand, egal welcher Firma. Der Orchestrator zeigt nur die der eigenen."""
        return [lauf for lauf, zweige in list(self.storage.items()) if zweige]


class PostgresStaende:
    """Eine Verbindung für alle Firmen. `fuer(firma)` gibt den Stand-Speicher einer Firma."""

    def __init__(self, dsn: str, schluessel: bytes) -> None:
        import psycopg

        if len(schluessel) != 32:
            raise StandFehler("Schlüssel für Ablaufstände ungültig.")
        self._schluessel = schluessel
        self._sperre = threading.RLock()
        try:
            self._verbindung = psycopg.connect(dsn, autocommit=False)
        except psycopg.Error:
            # Der Text der Ausnahme kann die Verbindungsangabe enthalten.
            raise StandFehler("Datenbank für Ablaufstände nicht erreichbar.") from None

    def fuer(self, firma: str) -> PostgresStand:
        if not firma:
            raise StandFehler("Ablaufstand ohne Firma.")
        return PostgresStand(self, firma)

    def schliesse(self) -> None:
        self._verbindung.close()


_STAND = "lauf, zweig, stand_id, vorher_id, art, inhalt, meta_art, meta"


class PostgresStand(BaseCheckpointSaver[str]):
    """Checkpoints genau einer Firma. Aufgebaut wie der SQLite-Speicher von LangGraph."""

    def __init__(self, staende: PostgresStaende, firma: str) -> None:
        super().__init__(serde=EncryptedSerializer(_FirmenChiffre(staende._schluessel, firma), JsonPlusSerializer()))
        self._staende, self.firma = staende, firma

    def _vorgang(self):
        """Transaktion mit gesetzter Firma. Fehlertexte der Datenbank gehen nicht hinaus."""
        return _Vorgang(self._staende, self.firma)

    def _tupel(self, cur, zeile) -> CheckpointTuple:
        lauf, zweig, stand_id, vorher_id, art, inhalt, meta_art, meta = zeile
        cur.execute(
            "SELECT schritt, kanal, art, inhalt FROM arbeit.ablaufschritte"
            " WHERE lauf = %s AND zweig = %s AND stand_id = %s ORDER BY pfad, schritt, nr",
            (lauf, zweig, stand_id),
        )
        schreibungen = [(s, k, self.serde.loads_typed((a, bytes(i)))) for s, k, a, i in cur.fetchall()]

        def cfg(id_: str) -> RunnableConfig:
            return {"configurable": {"thread_id": lauf, "checkpoint_ns": zweig, "checkpoint_id": id_}}

        return CheckpointTuple(
            cfg(stand_id),
            self.serde.loads_typed((art, bytes(inhalt))),
            self.serde.loads_typed((meta_art, bytes(meta))),
            cfg(vorher_id) if vorher_id else None,
            schreibungen,
        )

    def get_tuple(self, config: RunnableConfig) -> CheckpointTuple | None:
        lauf = str(config["configurable"]["thread_id"])
        zweig = config["configurable"].get("checkpoint_ns", "")
        with self._vorgang() as cur:
            if stand_id := get_checkpoint_id(config):
                cur.execute(
                    f"SELECT {_STAND} FROM arbeit.ablaufstaende WHERE lauf = %s AND zweig = %s AND stand_id = %s",
                    (lauf, zweig, stand_id),
                )
            else:
                cur.execute(
                    f"SELECT {_STAND} FROM arbeit.ablaufstaende WHERE lauf = %s AND zweig = %s ORDER BY stand_id DESC LIMIT 1",
                    (lauf, zweig),
                )
            zeile = cur.fetchone()
            return self._tupel(cur, zeile) if zeile else None

    def list(
        self,
        config: RunnableConfig | None,
        *,
        filter: dict[str, Any] | None = None,
        before: RunnableConfig | None = None,
        limit: int | None = None,
    ) -> Iterator[CheckpointTuple]:
        wo, werte = ["true"], []
        if config is not None:
            wo.append("lauf = %s")
            werte.append(str(config["configurable"]["thread_id"]))
            if (zweig := config["configurable"].get("checkpoint_ns")) is not None:
                wo.append("zweig = %s")
                werte.append(zweig)
        if before is not None and (vor := get_checkpoint_id(before)):
            wo.append("stand_id < %s")
            werte.append(vor)
        with self._vorgang() as cur:
            cur.execute(f"SELECT {_STAND} FROM arbeit.ablaufstaende WHERE {' AND '.join(wo)} ORDER BY stand_id DESC", werte)
            tupel = [self._tupel(cur, z) for z in cur.fetchall()]
        # Die Metadaten sind verschlüsselt, also erst hier filtern.
        if filter:
            tupel = [t for t in tupel if all(t.metadata.get(k) == v for k, v in filter.items())]
        yield from tupel[:limit] if limit is not None else tupel

    def put(
        self, config: RunnableConfig, checkpoint: Checkpoint, metadata: CheckpointMetadata, new_versions: ChannelVersions
    ) -> RunnableConfig:
        lauf = str(config["configurable"]["thread_id"])
        zweig = config["configurable"].get("checkpoint_ns", "")
        art, inhalt = self.serde.dumps_typed(checkpoint)
        meta_art, meta = self.serde.dumps_typed(get_checkpoint_metadata(config, metadata))
        with self._vorgang() as cur:
            cur.execute(
                """INSERT INTO arbeit.ablaufstaende (firma, lauf, zweig, stand_id, vorher_id, art, inhalt, meta_art, meta)
                   VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s)
                   ON CONFLICT (firma, lauf, zweig, stand_id) DO UPDATE SET vorher_id = EXCLUDED.vorher_id,
                     art = EXCLUDED.art, inhalt = EXCLUDED.inhalt, meta_art = EXCLUDED.meta_art, meta = EXCLUDED.meta""",
                (self.firma, lauf, zweig, checkpoint["id"], config["configurable"].get("checkpoint_id"), art, inhalt, meta_art, meta),
            )
        return {"configurable": {"thread_id": lauf, "checkpoint_ns": zweig, "checkpoint_id": checkpoint["id"]}}

    def put_writes(self, config: RunnableConfig, writes: Sequence[tuple[str, Any]], task_id: str, task_path: str = "") -> None:
        c = config["configurable"]
        # Besondere Kanäle (Fehler, Unterbrechung) ersetzen ihren alten Eintrag, alles andere bleibt, wie es zuerst war.
        bei_konflikt = (
            "DO UPDATE SET kanal = EXCLUDED.kanal, art = EXCLUDED.art, inhalt = EXCLUDED.inhalt, pfad = EXCLUDED.pfad"
            if all(w[0] in WRITES_IDX_MAP for w in writes)
            else "DO NOTHING"
        )
        with self._vorgang() as cur:
            for nr, (kanal, wert) in enumerate(writes):
                art, inhalt = self.serde.dumps_typed(wert)
                cur.execute(
                    f"""INSERT INTO arbeit.ablaufschritte (firma, lauf, zweig, stand_id, schritt, pfad, nr, kanal, art, inhalt)
                        VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s)
                        ON CONFLICT (firma, lauf, zweig, stand_id, schritt, nr) {bei_konflikt}""",
                    (
                        self.firma, str(c["thread_id"]), c.get("checkpoint_ns", ""), str(c["checkpoint_id"]),
                        task_id, task_path, WRITES_IDX_MAP.get(kanal, nr), kanal, art, inhalt,
                    ),
                )

    def delete_thread(self, thread_id: str) -> None:
        with self._vorgang() as cur:
            cur.execute("DELETE FROM arbeit.ablaufschritte WHERE lauf = %s", (str(thread_id),))
            cur.execute("DELETE FROM arbeit.ablaufstaende WHERE lauf = %s", (str(thread_id),))

    def laeufe(self) -> list[str]:
        """Die Läufe dieser Firma, die noch einen Stand haben (also nicht fertig sind)."""
        with self._vorgang() as cur:
            cur.execute("SELECT lauf FROM arbeit.ablaufstaende GROUP BY lauf ORDER BY min(gespeichert_am), lauf")
            return [z[0] for z in cur.fetchall()]


class _Vorgang:
    def __init__(self, staende: PostgresStaende, firma: str) -> None:
        self._staende, self._firma = staende, firma

    def __enter__(self):
        import psycopg

        self._staende._sperre.acquire()
        try:
            self._tx = self._staende._verbindung.transaction()
            self._tx.__enter__()
            self._cur = self._staende._verbindung.cursor()
            # Row-Level Security zeigt und nimmt danach nur Zeilen dieser Firma.
            self._cur.execute("SELECT set_config('boticstudio.firma', %s, true)", (self._firma,))
        except psycopg.Error:
            self._staende._sperre.release()
            raise StandFehler("Ablaufstand: Datenbank nicht erreichbar.") from None
        return self._cur

    def __exit__(self, typ, wert, spur):
        import psycopg

        try:
            self._cur.close()
            self._tx.__exit__(typ, wert, spur)
        except psycopg.Error:
            raise StandFehler("Ablaufstand konnte nicht gespeichert werden.") from None
        finally:
            self._staende._sperre.release()
        if typ is not None and issubclass(typ, psycopg.Error):
            raise StandFehler("Ablaufstand konnte nicht gespeichert werden.") from None
        return False


def main() -> int:
    parser = argparse.ArgumentParser(description="Legt einen neuen Schlüssel für Ablaufstände an.")
    parser.add_argument("--neu", required=True, type=Path, help="neue Datei für den Schlüssel, außerhalb des Repos")
    args = parser.parse_args()
    if args.neu.exists():
        print("Die Datei gibt es schon. Ein neuer Schlüssel würde gespeicherte Stände unlesbar machen.", file=sys.stderr)
        return 1
    schreibe_geheim(args.neu, neuer_schluessel())
    print(f"Schlüssel gespeichert in {args.neu}. In {UMGEBUNG_SCHLUESSEL} eintragen.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
