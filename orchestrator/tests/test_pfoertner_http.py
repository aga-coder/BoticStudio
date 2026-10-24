import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import pytest

from ablauf.pfoertner import HttpPfoertner, PfoertnerFehler

SCHLUESSEL = "k" * 43


class FalscherPfoertner(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_POST(self):  # noqa: N802
        laenge = int(self.headers.get("Content-Length", "0"))
        anfrage = json.loads(self.rfile.read(laenge))
        if self.headers.get("Authorization") != "Bearer " + SCHLUESSEL:
            status, antwort = 401, {"fehler": "dienst nicht angemeldet"}
        elif anfrage["anfrage"]["firma"] != "a":
            status, antwort = 403, {"fehler": "dienst darf für diese firma nicht anfragen"}
        else:
            status, antwort = 200, {"entscheidung": {"ergebnis": "erlaubt"}}
        daten = json.dumps(antwort).encode()
        self.send_response(status)
        self.send_header("Content-Length", str(len(daten)))
        self.end_headers()
        self.wfile.write(daten)


@pytest.fixture
def adresse():
    server = ThreadingHTTPServer(("127.0.0.1", 0), FalscherPfoertner)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    yield f"http://127.0.0.1:{server.server_address[1]}"
    server.shutdown()
    server.server_close()


def schluesseldatei(tmp_path, inhalt=SCHLUESSEL):
    pfad = tmp_path / "dienst.schluessel"
    pfad.write_text(inhalt + "\n")
    return str(pfad)


def test_schickt_den_schluessel_mit(adresse, tmp_path):
    pf = HttpPfoertner.aus_umgebung({"BOTICSTUDIO_PFOERTNER": adresse, "BOTICSTUDIO_DIENSTSCHLUESSEL_DATEI": schluesseldatei(tmp_path)})
    assert pf.fuehre({"firma": "a"}, {})["entscheidung"]["ergebnis"] == "erlaubt"
    assert SCHLUESSEL not in repr(pf)


def test_falscher_schluessel_und_fremde_firma_klar_gemeldet(adresse, tmp_path):
    pf = HttpPfoertner(adresse, schluessel="x" * 43)
    with pytest.raises(PfoertnerFehler, match="kennt diesen Dienst nicht") as f:
        pf.fuehre({"firma": "a"}, {})
    assert "x" * 10 not in str(f.value)
    pf = HttpPfoertner(adresse, schluessel=SCHLUESSEL)
    with pytest.raises(PfoertnerFehler, match="nicht für diese Firma"):
        pf.fuehre({"firma": "b"}, {})


def test_ohne_schluessel_startet_nichts(tmp_path):
    with pytest.raises(PfoertnerFehler, match="BOTICSTUDIO_DIENSTSCHLUESSEL_DATEI fehlt"):
        HttpPfoertner.aus_umgebung({})
    with pytest.raises(PfoertnerFehler, match="nicht lesbar"):
        HttpPfoertner.aus_umgebung({"BOTICSTUDIO_DIENSTSCHLUESSEL_DATEI": str(tmp_path / "fehlt")})
    with pytest.raises(PfoertnerFehler, match="ungültig") as f:
        HttpPfoertner.aus_umgebung({"BOTICSTUDIO_DIENSTSCHLUESSEL_DATEI": schluesseldatei(tmp_path, "kurz")})
    assert "kurz" not in str(f.value)
