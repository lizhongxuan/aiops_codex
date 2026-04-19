#!/usr/bin/env python3
import argparse
import json
import os
import sys
import urllib.error
import urllib.request
from typing import Dict, List, Optional


DEFAULT_BASE_URL = "http://127.0.0.1:8317/v1"
DEFAULT_MODEL = "gpt-5.4"


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Stream chat completions from a local CLIProxyAPI instance."
    )
    parser.add_argument(
        "prompt",
        nargs="+",
        help="Prompt text to send to the model.",
    )
    parser.add_argument(
        "--base-url",
        default=os.getenv("CLIPROXYAPI_BASE_URL")
        or os.getenv("OPENAI_BASE_URL")
        or DEFAULT_BASE_URL,
        help=f"OpenAI-compatible base URL. Default: {DEFAULT_BASE_URL}",
    )
    parser.add_argument(
        "--api-key",
        default=os.getenv("CLIPROXYAPI_API_KEY") or os.getenv("OPENAI_API_KEY"),
        help="API key. Defaults to CLIPROXYAPI_API_KEY or OPENAI_API_KEY.",
    )
    parser.add_argument(
        "--model",
        default=os.getenv("CLIPROXYAPI_MODEL")
        or os.getenv("OPENAI_MODEL")
        or DEFAULT_MODEL,
        help=f"Model name. Default: {DEFAULT_MODEL}",
    )
    parser.add_argument(
        "--system",
        default=None,
        help="Optional system message.",
    )
    return parser


def normalize_base_url(base_url: str) -> str:
    return base_url.rstrip("/")


def build_messages(prompt: str, system_message: Optional[str]) -> List[Dict]:
    messages = []
    if system_message:
        messages.append({"role": "system", "content": system_message})
    messages.append({"role": "user", "content": prompt})
    return messages


def stream_chat(base_url: str, api_key: str, model: str, messages: List[Dict]) -> int:
    if not api_key:
        print(
            "Missing API key. Set CLIPROXYAPI_API_KEY or OPENAI_API_KEY, "
            "or pass --api-key.",
            file=sys.stderr,
        )
        return 2

    url = f"{normalize_base_url(base_url)}/chat/completions"
    payload = {
        "model": model,
        "stream": True,
        "messages": messages,
    }
    body = json.dumps(payload).encode("utf-8")
    request = urllib.request.Request(
        url,
        data=body,
        method="POST",
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
        },
    )

    try:
        with urllib.request.urlopen(request, timeout=300) as response:
            printed = False
            for raw_line in response:
                line = raw_line.decode("utf-8", errors="replace").strip()
                if not line or not line.startswith("data: "):
                    continue

                data = line[6:]
                if data == "[DONE]":
                    break

                event = json.loads(data)
                for choice in event.get("choices", []):
                    delta = choice.get("delta", {})
                    content = delta.get("content")
                    if content:
                        sys.stdout.write(content)
                        sys.stdout.flush()
                        printed = True

            if printed:
                sys.stdout.write("\n")
            else:
                print("Stream finished, but no text content was received.", file=sys.stderr)
                return 1
    except urllib.error.HTTPError as exc:
        details = exc.read().decode("utf-8", errors="replace")
        print(f"HTTP {exc.code}: {details}", file=sys.stderr)
        return 1
    except urllib.error.URLError as exc:
        print(f"Connection failed: {exc.reason}", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        print("\nInterrupted.", file=sys.stderr)
        return 130

    return 0


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()
    prompt = " ".join(args.prompt)
    messages = build_messages(prompt, args.system)
    return stream_chat(args.base_url, args.api_key, args.model, messages)


if __name__ == "__main__":
    raise SystemExit(main())
