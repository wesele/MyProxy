import os
import json
import ssl
import urllib.request
import urllib.error
from flask import Flask, render_template, request, jsonify, redirect, url_for, Response

app = Flask(__name__)

API_BASE = os.environ.get("API_BASE", "http://127.0.0.1:8080/admin/api")


def _ssl_ctx():
    if API_BASE.startswith("https"):
        ctx = ssl.create_default_context()
        ctx.check_hostname = False
        ctx.verify_mode = ssl.CERT_NONE
        return ctx
    return None


def api_call(method, path, data=None):
    url = API_BASE + path
    headers = {"Content-Type": "application/json"}
    body = json.dumps(data).encode() if data else None
    req = urllib.request.Request(url, data=body, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, context=_ssl_ctx()) as resp:
            return json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        return {"error": e.read().decode()}
    except Exception as e:
        return {"error": str(e)}


@app.route("/admin/login")
def login_page():
    return render_template("login.html")


@app.route("/admin")
@app.route("/admin/")
def index():
    return redirect(url_for("dashboard"))


@app.route("/admin/dashboard")
def dashboard():
    return render_template("dashboard.html")


@app.route("/admin/api/stats")
def stats():
    hours = request.args.get("hours", "24")
    model = request.args.get("model", "")
    start = request.args.get("start", "")
    end = request.args.get("end", "")
    path = f"/stats?hours={hours}&model={model}"
    if start:
        path += f"&start={start}"
    if end:
        path += f"&end={end}"
    result = api_call("GET", path)
    return jsonify(result)


@app.route("/admin/api/logs")
def model_logs():
    model = request.args.get("model", "")
    hours = request.args.get("hours", "24")
    limit = request.args.get("limit", "100")
    start = request.args.get("start", "")
    end = request.args.get("end", "")
    path = f"/logs?model={model}&hours={hours}&limit={limit}"
    if start:
        path += f"&start={start}"
    if end:
        path += f"&end={end}"
    result = api_call("GET", path)
    return jsonify(result)


@app.route("/admin/providers")
def providers():
    data = api_call("GET", "/providers")
    return render_template("providers.html", providers=data if isinstance(data, list) else [])


@app.route("/admin/providers/add", methods=["GET", "POST"])
def provider_add():
    if request.method == "POST":
        models_raw = request.form.get("models_json", "[]")
        try:
            models = json.loads(models_raw)
        except json.JSONDecodeError:
            models = []
        keys_raw = request.form.get("keys_json", "[]")
        try:
            keys = json.loads(keys_raw)
        except json.JSONDecodeError:
            keys = []
        payload = {
            "name": request.form["name"],
            "provider_type": request.form["provider_type"],
            "base_url": request.form["base_url"],
            "models": models,
            "keys": keys,
            "priority": int(request.form.get("priority", 0)),
        }
        if keys:
            payload["api_key"] = keys[0].get("key_value", "")
        elif request.form.get("api_key"):
            payload["api_key"] = request.form["api_key"]
        api_call("POST", "/providers", payload)
        return redirect(url_for("providers"))
    return render_template("provider_form.html", provider=None)


@app.route("/admin/providers/<int:pid>/edit", methods=["GET", "POST"])
def provider_edit(pid):
    if request.method == "POST":
        models_raw = request.form.get("models_json", "[]")
        try:
            models = json.loads(models_raw)
        except json.JSONDecodeError:
            models = []
        keys_raw = request.form.get("keys_json", "[]")
        try:
            keys = json.loads(keys_raw)
        except json.JSONDecodeError:
            keys = []
        payload = {
            "name": request.form["name"],
            "provider_type": request.form["provider_type"],
            "base_url": request.form["base_url"],
            "models": models,
            "keys": keys,
            "priority": int(request.form.get("priority", 0)),
        }
        if keys:
            payload["api_key"] = keys[0].get("key_value", "")
        elif request.form.get("api_key"):
            payload["api_key"] = request.form["api_key"]
        api_call("PUT", f"/providers/{pid}", payload)
        return redirect(url_for("providers"))
    providers_data = api_call("GET", "/providers")
    provider = None
    if isinstance(providers_data, list):
        for p in providers_data:
            if p.get("id") == pid:
                provider = p
                break
    return render_template("provider_form.html", provider=provider)


@app.route("/admin/providers/<int:pid>/delete", methods=["POST"])
def provider_delete(pid):
    api_call("DELETE", f"/providers/{pid}")
    return redirect(url_for("providers"))


@app.route("/admin/tokens")
def tokens():
    return render_template("tokens.html")


@app.route("/admin/tools")
def tools():
    return render_template("tools.html")


@app.route("/admin/api/openapi.json")
def openapi_spec():
    import json
    import os
    spec_path = os.path.join(os.path.dirname(__file__), "static", "openapi.json")
    with open(spec_path) as f:
        spec = json.load(f)
    return jsonify(spec)


@app.route("/admin/api")
def api_docs():
    return render_template("api.html")


@app.route("/admin/models")
def models():
    data = api_call("GET", "/providers")
    # Extract Go backend base URL from API_BASE (strip /admin/api suffix)
    go_base = API_BASE.rstrip("/admin/api")
    return render_template("models.html", providers=data if isinstance(data, list) else [], go_base=go_base)


@app.route("/admin/models/test", methods=["POST"])
def model_test():
    import http.client
    data = request.get_json()
    body = json.dumps(data).encode()
    is_https = API_BASE.startswith("https")
    port = int(os.environ.get("GO_PORT", 8080))
    try:
        if is_https:
            conn = http.client.HTTPSConnection("127.0.0.1", port, timeout=600, context=_ssl_ctx())
        else:
            conn = http.client.HTTPConnection("127.0.0.1", port, timeout=600)
        conn.request("POST", "/admin/api/models/test", body, {"Content-Type": "application/json"})
        resp = conn.getresponse()
        def generate():
            while True:
                line = resp.readline()
                if not line:
                    break
                yield line
            conn.close()
        return Response(generate(), mimetype="text/event-stream", direct_passthrough=True)
    except Exception as e:
        err = json.dumps({"done": True, "error": str(e)})
        return Response(f"data: {err}\n\n", mimetype="text/event-stream")


@app.route("/admin/providers/export")
def provider_export():
    result = api_call("GET", "/providers/export")
    if isinstance(result, dict) and "error" in result:
        return jsonify(result)
    return Response(
        json.dumps(result, indent=2, default=str),
        mimetype="application/json",
        headers={"Content-Disposition": "attachment; filename=providers-backup.json"}
    )


@app.route("/admin/providers/import", methods=["POST"])
def provider_import():
    file = request.files.get("file")
    if not file:
        return jsonify({"error": "No file provided"})
    try:
        data = json.load(file)
    except Exception as e:
        return jsonify({"error": f"Invalid JSON: {e}"})
    if isinstance(data, dict) and "providers" in data:
        providers = data["providers"]
    elif isinstance(data, list):
        providers = data
    else:
        return jsonify({"error": "Invalid format: expected a JSON array or object with providers key"})
    result = api_call("POST", "/providers/import", {"providers": providers})
    return jsonify(result)


@app.route("/admin/providers/fetch-models", methods=["POST"])
def provider_fetch_models():
    data = request.get_json()
    result = api_call("POST", "/providers/fetch-models", data)
    return jsonify(result)


@app.route("/admin/providers/test", methods=["POST"])
def provider_test():
    data = request.get_json()
    result = api_call("POST", "/providers/test", data)
    return jsonify(result)


@app.route("/admin/api/providers/<int:pid>/api-key")
def provider_api_key(pid):
    result = api_call("GET", f"/providers/{pid}?show_key=1")
    return jsonify(result)


if __name__ == "__main__":
    port = int(os.environ.get("FLASK_PORT", 5100))
    app.run(host="127.0.0.1", port=port, debug=False)
