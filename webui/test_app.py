import json
import io
from unittest.mock import patch, MagicMock

import pytest
from flask import url_for

from app import api_call, app


@pytest.fixture
def client():
    app.config["TESTING"] = True
    with app.test_client() as c:
        yield c


@pytest.fixture
def mock_urlopen():
    mock_resp = MagicMock()
    mock_resp.__enter__ = MagicMock(return_value=mock_resp)
    mock_resp.__exit__ = MagicMock(return_value=False)
    mock_resp.read.return_value = b"{}"

    with patch("app.urllib.request.urlopen", return_value=mock_resp) as mock:
        yield mock, mock_resp


@pytest.fixture
def mock_api_call():
    with patch("app.api_call") as mock:
        yield mock


# ---- Dashboard routes ----

def test_index_redirects_to_dashboard(client):
    resp = client.get("/admin")
    assert resp.status_code == 302
    assert "/admin/dashboard" in resp.headers["Location"]


def test_index_slash_redirects_to_dashboard(client):
    resp = client.get("/admin/")
    assert resp.status_code == 302
    assert "/admin/dashboard" in resp.headers["Location"]


def test_dashboard_returns_200(client):
    resp = client.get("/admin/dashboard")
    assert resp.status_code == 200


# ---- Provider routes ----

def test_providers_list_with_data(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = json.dumps([
        {"id": 1, "name": "OpenAI", "provider_type": "openai", "base_url": "https://api.openai.com"}
    ]).encode()

    with app.test_client() as client:
        resp = client.get("/admin/providers")
        assert resp.status_code == 200
        assert b"OpenAI" in resp.data


def test_providers_list_empty(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = b"{}"

    with app.test_client() as client:
        resp = client.get("/admin/providers")
        assert resp.status_code == 200


def test_providers_add_form(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = b"{}"

    with app.test_client() as client:
        resp = client.get("/admin/providers/add")
        assert resp.status_code == 200


def test_providers_add_post_creates_and_redirects(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = json.dumps({"id": 1, "name": "TestProvider"}).encode()

    with app.test_client() as client:
        resp = client.post("/admin/providers/add", data={
            "name": "TestProvider",
            "provider_type": "openai",
            "base_url": "https://api.openai.com",
            "api_key": "sk-test",
            "models_json": '["gpt-4"]',
            "priority": "1",
        }, follow_redirects=False)
        assert resp.status_code == 302
        assert "/admin/providers" in resp.headers["Location"]

        # Verify the API call was made with correct payload
        call_args = mock_open.call_args
        assert call_args is not None
        req = call_args[0][0]
        sent_body = json.loads(req.data)
        assert sent_body["name"] == "TestProvider"
        assert sent_body["provider_type"] == "openai"
        assert sent_body["models"] == ["gpt-4"]


def test_provider_edit_form_with_data(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    provider_list = json.dumps([
        {"id": 1, "name": "OpenAI", "provider_type": "openai", "base_url": "https://api.openai.com"}
    ]).encode()
    mock_resp.read.return_value = provider_list

    with app.test_client() as client:
        resp = client.get("/admin/providers/1/edit")
        assert resp.status_code == 200
        assert b"OpenAI" in resp.data


def test_provider_edit_form_nonexistent(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = json.dumps([
        {"id": 2, "name": "Other", "provider_type": "openai", "base_url": "https://example.com"}
    ]).encode()

    with app.test_client() as client:
        resp = client.get("/admin/providers/999/edit")
        assert resp.status_code == 200


def test_provider_edit_post_updates_and_redirects(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = json.dumps({"id": 1, "name": "Updated"}).encode()

    with app.test_client() as client:
        resp = client.post("/admin/providers/1/edit", data={
            "name": "Updated",
            "provider_type": "openai",
            "base_url": "https://api.openai.com",
            "api_key": "",
            "models_json": '["gpt-3.5"]',
            "priority": "0",
        }, follow_redirects=False)
        assert resp.status_code == 302
        assert "/admin/providers" in resp.headers["Location"]

        call_args = mock_open.call_args
        req = call_args[0][0]
        assert req.method == "PUT"


def test_provider_delete_redirects(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = b"{}"

    with app.test_client() as client:
        resp = client.post("/admin/providers/1/delete", follow_redirects=False)
        assert resp.status_code == 302
        assert "/admin/providers" in resp.headers["Location"]

        call_args = mock_open.call_args
        req = call_args[0][0]
        assert req.method == "DELETE"


# ---- Provider utility routes ----

def test_provider_export_returns_json(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = json.dumps([
        {"id": 1, "name": "OpenAI"}
    ]).encode()

    with app.test_client() as client:
        resp = client.get("/admin/providers/export")
        assert resp.status_code == 200
        assert resp.content_type == "application/json"
        data = json.loads(resp.data)
        assert isinstance(data, list)
        assert data[0]["name"] == "OpenAI"


def test_provider_export_error_propagated(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = json.dumps({"error": "backend error"}).encode()

    with app.test_client() as client:
        resp = client.get("/admin/providers/export")
        assert resp.status_code == 200
        data = json.loads(resp.data)
        assert "error" in data


def test_provider_import_with_file(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = json.dumps({"imported": 2}).encode()

    with app.test_client() as client:
        data = {
            "file": (io.BytesIO(json.dumps([
                {"name": "P1", "provider_type": "openai", "base_url": "https://a.com"},
                {"name": "P2", "provider_type": "openai", "base_url": "https://b.com"},
            ]).encode()), "providers.json"),
        }
        resp = client.post("/admin/providers/import", data=data,
                           content_type="multipart/form-data")
        assert resp.status_code == 200
        result = json.loads(resp.data)
        assert "imported" in result


def test_provider_import_with_file_object_format(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = json.dumps({"imported": 3}).encode()

    with app.test_client() as client:
        data = {
            "file": (io.BytesIO(json.dumps({
                "providers": [
                    {"name": "P1", "provider_type": "openai", "base_url": "https://a.com"},
                    {"name": "P2", "provider_type": "openai", "base_url": "https://b.com"},
                ]
            }).encode()), "providers.json"),
        }
        resp = client.post("/admin/providers/import", data=data,
                           content_type="multipart/form-data")
        assert resp.status_code == 200


def test_provider_import_without_file(client):
    resp = client.post("/admin/providers/import")
    assert resp.status_code == 200
    data = json.loads(resp.data)
    assert data["error"] == "No file provided"


def test_provider_import_invalid_json(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = b"{}"

    with app.test_client() as client:
        data = {
            "file": (io.BytesIO(b"not valid json"), "providers.json"),
        }
        resp = client.post("/admin/providers/import", data=data,
                           content_type="multipart/form-data")
        assert resp.status_code == 200
        result = json.loads(resp.data)
        assert "error" in result


def test_provider_fetch_models(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = json.dumps({"models": [{"id": "gpt-4"}]}).encode()

    with app.test_client() as client:
        resp = client.post("/admin/providers/fetch-models",
                           json={"base_url": "https://api.openai.com", "api_key": "sk-test"},
                           content_type="application/json")
        assert resp.status_code == 200
        data = json.loads(resp.data)
        assert "models" in data


def test_provider_test_route(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = json.dumps({"ok": True}).encode()

    with app.test_client() as client:
        resp = client.post("/admin/providers/test",
                           json={"base_url": "https://api.openai.com", "api_key": "sk-test"},
                           content_type="application/json")
        assert resp.status_code == 200
        data = json.loads(resp.data)
        assert data["ok"] is True


# ---- API docs routes ----

def test_api_docs_swagger(client):
    resp = client.get("/admin/api")
    assert resp.status_code == 200


def test_api_openapi_json(client):
    resp = client.get("/admin/api/openapi.json")
    assert resp.status_code == 200
    data = json.loads(resp.data)
    assert isinstance(data, dict)


# ---- Tools route ----

def test_tools_returns_200(client):
    resp = client.get("/admin/tools")
    assert resp.status_code == 200


# ---- Models route ----

def test_models_returns_200_with_providers(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = json.dumps([
        {"id": 1, "name": "OpenAI", "provider_type": "openai", "base_url": "https://api.openai.com"}
    ]).encode()

    with app.test_client() as client:
        resp = client.get("/admin/models")
        assert resp.status_code == 200
        assert b"OpenAI" in resp.data


def test_models_handles_empty_providers(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = b"{}"

    with app.test_client() as client:
        resp = client.get("/admin/models")
        assert resp.status_code == 200


# ---- Stats and logs ----

def test_stats_returns_data(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = json.dumps({"total_requests": 42}).encode()

    with app.test_client() as client:
        resp = client.get("/admin/api/stats")
        assert resp.status_code == 200
        data = json.loads(resp.data)
        assert data["total_requests"] == 42


def test_stats_with_query_params(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = json.dumps({"total_requests": 10}).encode()

    with app.test_client() as client:
        resp = client.get("/admin/api/stats?hours=48&model=gpt-4&start=2024-01-01&end=2024-01-02")
        assert resp.status_code == 200

        # Verify the path includes the query params
        call_args = mock_open.call_args
        req = call_args[0][0]
        assert "hours=48" in req.full_url
        assert "model=gpt-4" in req.full_url
        assert "start=2024-01-01" in req.full_url
        assert "end=2024-01-02" in req.full_url


def test_logs_returns_data(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = json.dumps({"logs": [{"id": 1}]}).encode()

    with app.test_client() as client:
        resp = client.get("/admin/api/logs")
        assert resp.status_code == 200
        data = json.loads(resp.data)
        assert "logs" in data


def test_logs_with_query_params(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = json.dumps({"logs": []}).encode()

    with app.test_client() as client:
        resp = client.get("/admin/api/logs?model=gpt-4&hours=48&limit=50")
        assert resp.status_code == 200

        call_args = mock_open.call_args
        req = call_args[0][0]
        assert "model=gpt-4" in req.full_url
        assert "hours=48" in req.full_url
        assert "limit=50" in req.full_url


# ---- api_call function tests ----

def test_api_call_success():
    mock_resp = MagicMock()
    mock_resp.__enter__ = MagicMock(return_value=mock_resp)
    mock_resp.__exit__ = MagicMock(return_value=False)
    mock_resp.read.return_value = json.dumps({"id": 1, "name": "Test"}).encode()

    with patch("app.urllib.request.urlopen", return_value=mock_resp) as mock_open:
        result = api_call("GET", "/providers")
        assert result == {"id": 1, "name": "Test"}
        mock_open.assert_called_once()


def test_api_call_http_error():
    import urllib.error
    mock_resp = MagicMock()
    mock_resp.read.return_value = b'{"status": "not found"}'
    mock_resp.code = 404
    http_error = urllib.error.HTTPError(
        url="http://127.0.0.1:8080/admin/api/providers/999",
        code=404,
        msg="Not Found",
        hdrs={},
        fp=io.BytesIO(b'{"status": "not found"}'),
    )

    with patch("app.urllib.request.urlopen", side_effect=http_error):
        result = api_call("GET", "/providers/999")
        assert "error" in result


def test_api_call_network_error():
    with patch("app.urllib.request.urlopen", side_effect=ConnectionError("refused")):
        result = api_call("GET", "/providers")
        assert "error" in result
        assert "refused" in str(result["error"])


def test_api_call_with_data():
    mock_resp = MagicMock()
    mock_resp.__enter__ = MagicMock(return_value=mock_resp)
    mock_resp.__exit__ = MagicMock(return_value=False)
    mock_resp.read.return_value = json.dumps({"id": 2, "name": "Created"}).encode()

    with patch("app.urllib.request.urlopen", return_value=mock_resp) as mock_open:
        payload = {"name": "New Provider", "provider_type": "openai"}
        result = api_call("POST", "/providers", payload)
        assert result == {"id": 2, "name": "Created"}
        call_args = mock_open.call_args
        req = call_args[0][0]
        assert req.method == "POST"
        assert json.loads(req.data) == payload


def test_api_call_without_data():
    mock_resp = MagicMock()
    mock_resp.__enter__ = MagicMock(return_value=mock_resp)
    mock_resp.__exit__ = MagicMock(return_value=False)
    mock_resp.read.return_value = json.dumps({"status": "ok"}).encode()

    with patch("app.urllib.request.urlopen", return_value=mock_resp) as mock_open:
        result = api_call("GET", "/status")
        assert result == {"status": "ok"}
        call_args = mock_open.call_args
        req = call_args[0][0]
        assert req.data is None


# ---- Provider API key route ----

def test_provider_api_key_returns_key(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = json.dumps({
        "id": 1, "name": "OpenAI", "api_key": "sk-secret"
    }).encode()

    with app.test_client() as client:
        resp = client.get("/admin/api/providers/1/api-key")
        assert resp.status_code == 200
        data = json.loads(resp.data)
        assert data["api_key"] == "sk-secret"

        call_args = mock_open.call_args
        req = call_args[0][0]
        assert "show_key=1" in req.full_url


# ---- Provider add with invalid JSON ----

def test_providers_add_invalid_models_json(mock_urlopen):
    mock_open, mock_resp = mock_urlopen
    mock_resp.read.return_value = json.dumps({"id": 1}).encode()

    with app.test_client() as client:
        resp = client.post("/admin/providers/add", data={
            "name": "Test",
            "provider_type": "openai",
            "base_url": "https://api.example.com",
            "api_key": "sk-test",
            "models_json": "not-valid-json",
            "priority": "0",
        }, follow_redirects=False)
        assert resp.status_code == 302
        assert "/admin/providers" in resp.headers["Location"]
