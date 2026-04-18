# /// script
# requires-python = ">=3.10"
# dependencies = [
#     "sentence-transformers>=3.0",
# ]
# ///
"""
Embedding sidecar — stateless HTTP embedding server over a Unix domain socket.

Run with: uv run embed/sidecar.py --socket <path> --model <model-name>

    POST /embed  {"text": "..."}  →  {"embedding": [0.08, -0.29, ...]}
"""

import argparse
import json
import logging
import os
import signal
import socket
import sys
from http.server import HTTPServer, BaseHTTPRequestHandler

from sentence_transformers import SentenceTransformer

logger = logging.getLogger("embed-sidecar")


def make_handler(model: SentenceTransformer):
    """Create a request handler class bound to the given model."""

    class Handler(BaseHTTPRequestHandler):
        def do_POST(self):
            if self.path != "/embed":
                self.send_error(404)
                return

            length = int(self.headers.get("Content-Length", 0))
            body = json.loads(self.rfile.read(length))

            text = body.get("text")
            if not text:
                self._json_response(400, {"error": "missing 'text' field"})
                return

            embedding = model.encode(text, show_progress_bar=False)
            self._json_response(200, {"embedding": embedding.tolist()})

        def _json_response(self, status, obj):
            data = json.dumps(obj).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def log_message(self, fmt, *args):
            logger.debug(fmt, *args)

    return Handler


class UnixHTTPServer(HTTPServer):
    address_family = socket.AF_UNIX

    def __init__(self, socket_path, handler):
        self.socket_path = socket_path
        if os.path.exists(socket_path):
            os.unlink(socket_path)
        super().__init__(socket_path, handler)

    def server_close(self):
        super().server_close()
        if os.path.exists(self.socket_path):
            os.unlink(self.socket_path)


def main():
    parser = argparse.ArgumentParser(description="Embedding sidecar server")
    parser.add_argument("--socket", required=True, help="Unix socket path")
    parser.add_argument("--model", required=True, help="sentence-transformers model name")
    args = parser.parse_args()

    logging.basicConfig(level=logging.INFO, format="%(name)s: %(message)s")

    logger.info("loading model: %s", args.model)
    model = SentenceTransformer(args.model)
    logger.info("model loaded: %d dims", model.get_sentence_embedding_dimension())

    srv = UnixHTTPServer(args.socket, make_handler(model))
    logger.info("listening on %s", args.socket)

    def shutdown(signum, frame):
        logger.info("shutting down")
        srv.server_close()
        sys.exit(0)

    signal.signal(signal.SIGTERM, shutdown)
    signal.signal(signal.SIGINT, shutdown)

    srv.serve_forever()


if __name__ == "__main__":
    main()
