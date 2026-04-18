# /// script
# requires-python = ">=3.10"
# dependencies = [
#     "sentence-transformers>=3.0",
#     "numpy>=1.24",
# ]
# ///
"""
Embedding sidecar — stateless embedding server over a Unix domain socket.

Run with: uv run embed/sidecar.py [--socket /tmp/pigeon-embed.sock] [--model all-MiniLM-L6-v2]

Loads a sentence-transformers model once and serves embed/compare requests.
Protocol: newline-delimited JSON, one request per connection.

Requests:
    Embed only:
        → {"text": "..."}
        ← {"embedding": [0.08, -0.29, ...]}

    Compare (with previous embedding):
        → {"text": "...", "prev_embedding": [0.12, -0.34, ...]}
        ← {"embedding": [0.08, -0.29, ...], "sim": 0.43}

Usage:
    python sidecar.py [--socket /tmp/pigeon-embed.sock] [--model all-MiniLM-L6-v2]
"""

import argparse
import json
import logging
import os
import signal
import socket
import sys

import numpy as np
from sentence_transformers import SentenceTransformer

logger = logging.getLogger("embed-sidecar")

DEFAULT_SOCKET = "/tmp/pigeon-embed.sock"
DEFAULT_MODEL = "all-MiniLM-L6-v2"


class EmbedSidecar:
    """Stateless embedding server. Loads model once, serves over Unix socket."""

    def __init__(self, socket_path: str, model_name: str):
        self.socket_path = socket_path
        self.model = SentenceTransformer(model_name)
        self.dims = self.model.get_sentence_embedding_dimension()
        logger.info("model loaded: %s (%d dims)", model_name, self.dims)

    def handle(self, req: dict) -> dict:
        """Handle a single request. Pure function — no side effects."""
        text = req.get("text")
        if not text:
            return {"error": "missing 'text' field"}

        embedding = self.model.encode(text, show_progress_bar=False)
        resp = {"embedding": embedding.tolist()}

        prev = req.get("prev_embedding")
        if prev is not None:
            prev = np.array(prev, dtype=np.float32)
            sim = float(np.dot(embedding, prev) / (np.linalg.norm(embedding) * np.linalg.norm(prev)))
            resp["sim"] = sim

        return resp

    def serve(self):
        """Listen on Unix socket, handle one request per connection."""
        if os.path.exists(self.socket_path):
            os.unlink(self.socket_path)

        srv = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        srv.bind(self.socket_path)
        srv.listen(8)
        logger.info("listening on %s", self.socket_path)

        # Clean up socket file on shutdown.
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
        """Read one JSON request, write one JSON response, close."""
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
    parser.add_argument("--socket", default=DEFAULT_SOCKET, help="Unix socket path")
    parser.add_argument("--model", default=DEFAULT_MODEL, help="sentence-transformers model name")
    args = parser.parse_args()

    logging.basicConfig(level=logging.INFO, format="%(name)s: %(message)s")
    sidecar = EmbedSidecar(args.socket, args.model)
    sidecar.serve()


if __name__ == "__main__":
    main()
