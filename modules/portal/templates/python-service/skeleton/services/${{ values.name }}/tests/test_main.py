import os

os.environ["METRICS_PORT"] = "off"  # no metrics server in tests

from fastapi.testclient import TestClient  # noqa: E402

from app.main import SERVICE, app  # noqa: E402

client = TestClient(app)


def test_root():
    response = client.get("/")
    assert response.status_code == 200
    assert response.json()["service"] == SERVICE
    assert response.headers["X-Content-Type-Options"] == "nosniff"


def test_health():
    assert client.get("/healthz").status_code == 200
    assert client.get("/readyz").status_code == 200


def test_unknown_path():
    assert client.get("/missing").status_code == 404
