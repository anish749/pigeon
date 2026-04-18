# /// script
# requires-python = ">=3.10"
# dependencies = [
#     "sentence-transformers>=3.0",
#     "pytest>=8.0",
# ]
# ///
"""Tests for the embedding sidecar."""

import json
import socket
import tempfile
import threading

import numpy as np
import pytest

from sidecar import UnixHTTPServer, make_handler


class FakeModel:
    """Deterministic model that returns a fixed-size vector derived from input length."""

    def __init__(self, dims=4):
        self.dims = dims

    def encode(self, text, show_progress_bar=False):
        # Produce a deterministic vector from the text so tests can assert on similarity.
        h = hash(text) & 0xFFFFFFFF
        rng = np.random.RandomState(h)
        vec = rng.randn(self.dims).astype(np.float32)
        return vec / np.linalg.norm(vec)

    def get_sentence_embedding_dimension(self):
        return self.dims


@pytest.fixture
def server():
    """Start a UnixHTTPServer with a fake model on a temp socket."""
    sock_path = tempfile.mktemp(suffix=".sock")
    model = FakeModel(dims=4)
    srv = UnixHTTPServer(sock_path, make_handler(model))
    thread = threading.Thread(target=srv.serve_forever, daemon=True)
    thread.start()
    yield sock_path
    srv.shutdown()
    srv.server_close()


def http_post(sock_path, path, body):
    """Send an HTTP POST over the Unix socket and return (status, parsed json)."""
    conn = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    conn.connect(sock_path)

    payload = json.dumps(body).encode()
    request = (
        f"POST {path} HTTP/1.1\r\n"
        f"Host: localhost\r\n"
        f"Content-Type: application/json\r\n"
        f"Content-Length: {len(payload)}\r\n"
        f"Connection: close\r\n"
        f"\r\n"
    ).encode() + payload

    conn.sendall(request)

    response = b""
    while True:
        chunk = conn.recv(4096)
        if not chunk:
            break
        response += chunk
    conn.close()

    header_end = response.index(b"\r\n\r\n")
    status_line = response[:response.index(b"\r\n")].decode()
    status = int(status_line.split(" ", 2)[1])
    body_bytes = response[header_end + 4:]
    return status, json.loads(body_bytes)


def test_embed_returns_vector(server):
    status, resp = http_post(server, "/embed", {"text": "hello world"})
    assert status == 200
    assert "embedding" in resp
    assert len(resp["embedding"]) == 4
    assert all(isinstance(x, float) for x in resp["embedding"])


def test_embed_missing_text(server):
    status, resp = http_post(server, "/embed", {"foo": "bar"})
    assert status == 400
    assert "error" in resp


def test_embed_deterministic(server):
    _, resp1 = http_post(server, "/embed", {"text": "same input"})
    _, resp2 = http_post(server, "/embed", {"text": "same input"})
    assert resp1["embedding"] == resp2["embedding"]


def test_embed_different_texts_differ(server):
    _, resp1 = http_post(server, "/embed", {"text": "topic A: machine learning"})
    _, resp2 = http_post(server, "/embed", {"text": "topic B: cooking recipes"})
    assert resp1["embedding"] != resp2["embedding"]


def test_wrong_path_returns_404(server):
    conn = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    conn.connect(server)
    payload = json.dumps({"text": "hello"}).encode()
    request = (
        f"POST /wrong HTTP/1.1\r\n"
        f"Host: localhost\r\n"
        f"Content-Type: application/json\r\n"
        f"Content-Length: {len(payload)}\r\n"
        f"Connection: close\r\n"
        f"\r\n"
    ).encode() + payload
    conn.sendall(request)
    response = b""
    while True:
        chunk = conn.recv(4096)
        if not chunk:
            break
        response += chunk
    conn.close()
    status_line = response[:response.index(b"\r\n")].decode()
    status = int(status_line.split(" ", 2)[1])
    assert status == 404
