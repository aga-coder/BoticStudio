"""Legt ein Konto für die Anmeldung am Dashboard an.

    python -m ablauf.konto --firma testfirma-echt --person admin --email chef@firma.de

Das Passwort wird zweimal verdeckt abgefragt, nie als Argument übergeben
(sonst stünde es im Verlauf der Kommandozeile) und nie ausgegeben. Die Person
muss es in der Firma schon geben. Umgebung: BOTICSTUDIO_ARBEITSDATEN.

Registrieren mit Bestätigungslink kommt später, wenn BoticStudio Mails
versenden kann.
"""

from __future__ import annotations

import argparse
import getpass
import os
import sys

from .konten import Anmeldung, KontoFehler, PostgresKonten


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--firma", required=True)
    parser.add_argument("--person", required=True, help="ID der Person, wie der Pförtner sie kennt")
    parser.add_argument("--email", required=True)
    args = parser.parse_args()

    dsn = os.environ.get("BOTICSTUDIO_ARBEITSDATEN")
    if not dsn:
        print("BOTICSTUDIO_ARBEITSDATEN fehlt.", file=sys.stderr)
        return 2
    passwort = getpass.getpass("Passwort: ")
    if getpass.getpass("Passwort wiederholen: ") != passwort:
        print("Die beiden Passwörter sind verschieden.", file=sys.stderr)
        return 1
    try:
        konten = PostgresKonten(dsn)
    except RuntimeError as f:
        print(f, file=sys.stderr)
        return 1
    try:
        Anmeldung(konten).lege_konto_an(args.firma, args.person, args.email, passwort)
    except (KontoFehler, RuntimeError) as f:
        print(f, file=sys.stderr)
        return 1
    finally:
        konten.schliesse()
    print(f"Konto angelegt: {args.firma}/{args.person}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
