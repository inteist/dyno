#!/usr/bin/env python3
"""Opt-in sanitized Chat Completions capture (Python stdlib, no CI dependency).

DYNO_LIVE_OPENAI=1 OPENAI_BASE_URL=http://host:8000/v1 DYNO_MODEL=served-id \
  scripts/capture-compatible-fixtures.py /path/to/new/capture

Sends one short request, never probes admin routes or alters cache/lifecycle.
Output rewrites model/request IDs and retains only benchmark-relevant fields.
Review the capture before moving it into adapter testdata. No error bodies,
headers, credentials, endpoint host, or environment dumps are written.
"""
import http.client
import json
import os
from pathlib import Path
import sys
import urllib.error
import urllib.parse
import urllib.request


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def contains_secret(value, key):
    if not key:
        return False
    if isinstance(value, str):
        return key in value
    if isinstance(value, dict):
        return any(contains_secret(k, key) or contains_secret(v, key) for k, v in value.items())
    if isinstance(value, list):
        return any(contains_secret(v, key) for v in value)
    return False


def sanitized_usage(value):
    if value is None:
        return None
    if not isinstance(value, dict):
        raise ValueError("malformed usage")
    clean = {}
    details = {
        "prompt_tokens_details": ("cached_tokens", "audio_tokens"),
        "completion_tokens_details": ("reasoning_tokens", "audio_tokens", "accepted_prediction_tokens", "rejected_prediction_tokens"),
    }
    for field in ("prompt_tokens", "completion_tokens", "total_tokens"):
        if field in value:
            count = value[field]
            if count is not None and (type(count) is not int or count < 0):
                raise ValueError("malformed usage count")
            clean[field] = count
    for field, allowed in details.items():
        if field not in value:
            continue
        if value[field] is None:
            clean[field] = None
            continue
        if not isinstance(value[field], dict):
            raise ValueError("malformed usage details")
        clean[field] = {}
        for name in allowed:
            if name not in value[field]:
                continue
            count = value[field][name]
            if count is not None and (type(count) is not int or count < 0):
                raise ValueError("malformed usage detail count")
            clean[field][name] = count
    return clean


def write_private(path, text):
    # Exclusive creation and private permissions, independent of the caller's umask.
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as output:
        output.write(text)


def main():
    if os.environ.get("DYNO_LIVE_OPENAI") != "1":
        raise ValueError("set DYNO_LIVE_OPENAI=1 to explicitly allow live requests")
    if len(sys.argv) != 2:
        raise ValueError("usage: capture-compatible-fixtures.py NEW_OUTPUT_DIRECTORY")
    base = os.environ.get("OPENAI_BASE_URL", "")
    u = urllib.parse.urlsplit(base)
    if u.scheme not in ("http", "https") or not u.netloc or u.username or u.password or "?" in base or "#" in base:
        raise ValueError("set a credential-free full http(s) OPENAI_BASE_URL")
    model = os.environ.get("DYNO_MODEL", "")
    if not model:
        raise ValueError("set DYNO_MODEL to a served ID")
    out = Path(sys.argv[1])
    if out.exists():
        raise ValueError("output directory must be new; existing fixtures are never overwritten")
    key = os.environ.get(os.environ.get("DYNO_API_KEY_ENV", "OPENAI_API_KEY"), "")
    field = os.environ.get("DYNO_TOKEN_LIMIT_FIELD", "max_tokens")
    usage = os.environ.get("DYNO_USAGE", "required")
    if field not in ("max_tokens", "max_completion_tokens") or usage not in ("required", "off"):
        raise ValueError("invalid token-limit or usage policy")
    payload = {"model": model, "messages": [{"role": "user", "content": "Count from one to fifty in English words."}], "stream": True, "n": 1, field: 16, "temperature": 0, "seed": 42}
    if usage == "required":
        payload["stream_options"] = {"include_usage": True}
    headers = {"Content-Type": "application/json"}
    if key:
        headers["Authorization"] = "Bearer " + key
    request = urllib.request.Request(base.rstrip("/") + "/chat/completions", data=json.dumps(payload).encode(), headers=headers)
    opener = urllib.request.build_opener(NoRedirect)
    with opener.open(request, timeout=60) as response:
        raw = response.read(2 * 1024 * 1024 + 1)
    if len(raw) > 2 * 1024 * 1024:
        raise ValueError("capture exceeded 2 MiB")
    frames = []
    content_parts = []
    done = False
    for frame in raw.decode("utf-8-sig").replace("\r\n", "\n").split("\n\n"):
        data = "\n".join(line[5:].lstrip(" ") for line in frame.splitlines() if line.startswith("data:"))
        if not data:
            continue
        if data == "[DONE]":
            frames.append("data: [DONE]\n\n")
            done = True
            break
        obj = json.loads(data)
        if not isinstance(obj, dict):
            raise ValueError("stream frame must be an object")
        if "error" in obj:
            raise ValueError("server returned an embedded error; no capture saved")
        clean = {"id": "fixture", "object": "chat.completion.chunk", "created": 0, "model": "fixture-model"}
        clean["choices"] = []
        choices = obj.get("choices", [])
        if not isinstance(choices, list):
            raise ValueError("choices must be an array")
        for choice in choices:
            if not isinstance(choice, dict):
                raise ValueError("choice must be an object")
            if "index" in choice and (type(choice["index"]) is not int or choice["index"] < 0):
                raise ValueError("malformed choice index")
            if "finish_reason" in choice and choice["finish_reason"] is not None and not isinstance(choice["finish_reason"], str):
                raise ValueError("malformed finish reason")
            if choice.get("finish_reason") not in (None, "", "stop", "length", "tool_calls", "content_filter", "function_call"):
                raise ValueError("unknown finish reason")
            item = {k: choice[k] for k in ("index", "finish_reason") if k in choice}
            if "delta" in choice:
                if not isinstance(choice["delta"], dict):
                    raise ValueError("delta must be an object")
                for field in ("role", "content"):
                    if field in choice["delta"] and choice["delta"][field] is not None and not isinstance(choice["delta"][field], str):
                        raise ValueError("malformed delta field")
                if choice["delta"].get("role") not in (None, "", "assistant"):
                    raise ValueError("unknown delta role")
                item["delta"] = {k: choice["delta"][k] for k in ("role", "content") if k in choice["delta"]}
            content = item.get("delta", {}).get("content")
            if isinstance(content, str):
                content_parts.append(content)
            clean["choices"].append(item)
        if "usage" in obj:
            clean["usage"] = sanitized_usage(obj["usage"])
        text = json.dumps(clean, ensure_ascii=False)
        if contains_secret(clean, key) or (key and key in text):
            raise ValueError("response echoed credential; no capture saved")
        frames.append("data: " + text + "\n\n")
    if not done:
        raise ValueError("stream lacked terminal marker; no capture saved")
    if key and key in "".join(content_parts):
        raise ValueError("response echoed credential across chunks; no capture saved")
    manifest = {"sdk": "github.com/openai/openai-go/v3@v3.71.2", "server_kind": os.environ.get("DYNO_SERVER_KIND", "unknown"), "server_version": os.environ.get("DYNO_SERVER_VERSION", "unknown"), "model_revision": os.environ.get("DYNO_MODEL_REVISION", "unknown"), "usage": usage, "token_limit_field": field, "scope": "one live inference response; cache/reasoning policy unverified"}
    manifest_text = json.dumps(manifest, indent=2) + "\n"
    if contains_secret(manifest, key) or (key and key in manifest_text):
        raise ValueError("credential appeared in manifest; no capture saved")
    # Validate both serialized files before creating anything (lone surrogates).
    "".join(frames).encode("utf-8")
    manifest_text.encode("utf-8")
    out.mkdir(parents=True, mode=0o700)
    write_private(out / "chat.sse", "".join(frames))
    write_private(out / "manifest.json", manifest_text)
    print("Sanitized capture saved. Review content and provenance before committing fixtures.")


def run():
    try:
        main()
        return 0
    except urllib.error.HTTPError as exc:
        print(f"Capture failed: HTTP {exc.code}; response body omitted", file=sys.stderr)
        return 1
    except (urllib.error.URLError, TimeoutError, http.client.HTTPException):
        print("Capture failed: endpoint transport unavailable", file=sys.stderr)
        return 1
    except (ValueError, OSError, TypeError, KeyError, RecursionError):
        # Configuration/parse errors can contain response bytes; never print them.
        print("Capture failed: invalid configuration, filesystem, or stream (details omitted for safety)", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(run())
