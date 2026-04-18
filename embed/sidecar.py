# /// script
# requires-python = ">=3.10"
# dependencies = [
#     "sentence-transformers>=3.0",
# ]
# ///
"""
Embedding sidecar — stateless embedding server over a Unix domain socket.

Run with: uv run embed/sidecar.py --socket <path> --model <model-name>

Loads a sentence-transformers model once and serves embed requests.
Protocol: newline-delimited JSON, one request per connection.

    → {"text": "..."}
    ← {"embedding": [0.08, -0.29, ...]}
"""

import argparse
import json
import logging
import os
import signal
import socket
import sys

from sentence_transformers import SentenceTransformer

logger = logging.getLogger("embed-sidecar")


class EmbedSidecar:
    """Stateless embedding server. Loads model once, serves over Unix socket."""

    def __init__(self, socket_path: str, model_name: str):
        self.socket_path = socket_path
        self.model = SentenceTransformer(model_name)
        self.dims = self.model.get_sentence_embedding_dimension()
        logger.info("model loaded: %s (%d dims)", model_name, self.dims)

    def handle(self, req: dict) -> dict:
        text = req.get("text")
        if not text:
            return {"error": "missing 'text' field"}
        embedding = self.model.encode(text, show_progress_bar=False)
        return {"embedding": embedding.tolist()}

    def serve(self):
        if os.path.exists(self.socket_path):
            os.unlink(self.socket_path)

        srv = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        srv.bind(self.socket_path)
        srv.listen(8)
        logger.info("listening on %s", self.socket_path)

        def cleanup(signum, frame):
            logger.info("shutting down")
            srv.close()
            if os.path.exists(self.socket_path):
                os.unlink(self.socket_path)
            sys.exit(0)

        signal.signal(signal.SIGTERM, cleanup)
        signal.signal(signal.SIGINT, cleanup)

        while True:
            conn, _ = srv.accept()
            try:
                self._handle_conn(conn)
            except Exception:
                logger.exception("error handling connection")
            finally:
                conn.close()

    def _handle_conn(self, conn: socket.socket):
        data = b""
        while True:
            chunk = conn.recv(4096)
            if not chunk:
                break
            data += chunk
            if b"\n" in data:
                break

        line = data.split(b"\n", 1)[0]
        if not line:
            return

        req = json.loads(line)
        resp = self.handle(req)
        conn.sendall(json.dumps(resp).encode() + b"\n")


def main():
    parser = argparse.ArgumentParser(description="Embedding sidecar server")
    parser.add_argument("--socket", required=True, help="Unix socket path")
    parser.add_argument("--model", required=True, help="sentence-transformers model name")
    args = parser.parse_args()

    logging.basicConfig(level=logging.INFO, format="%(name)s: %(message)s")
    sidecar = EmbedSidecar(args.socket, args.model)
    sidecar.serve()


if __name__ == "__main__":
    main()
