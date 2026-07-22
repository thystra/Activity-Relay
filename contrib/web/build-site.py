#!/usr/bin/env python3
"""Build the static Activity-Relay landing site without external dependencies."""

from __future__ import annotations

import argparse
import html
import json
import shutil
from datetime import datetime, timezone
from pathlib import Path


def load_config(path: Path) -> dict[str, str]:
    with path.open("r", encoding="utf-8") as handle:
        data = json.load(handle)
    required = {
        "site_name",
        "tagline",
        "operator_name",
        "contact_url",
        "source_url",
        "status_url",
        "language",
    }
    missing = sorted(required.difference(data))
    if missing:
        raise SystemExit(f"Missing configuration keys: {', '.join(missing)}")
    return {key: str(value) for key, value in data.items()}


def replace_tokens(text: str, values: dict[str, str]) -> str:
    for key, value in values.items():
        text = text.replace("{{" + key + "}}", value)
    return text


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument(
        "--source",
        type=Path,
        default=Path(__file__).resolve().parent,
        help="Directory containing templates/, content/, and assets/",
    )
    args = parser.parse_args()

    config = load_config(args.config)
    source = args.source.resolve()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)

    escaped = {
        "SITE_NAME": html.escape(config["site_name"]),
        "TAGLINE": html.escape(config["tagline"]),
        "OPERATOR_NAME": html.escape(config["operator_name"]),
        "CONTACT_URL": html.escape(config["contact_url"], quote=True),
        "SOURCE_URL": html.escape(config["source_url"], quote=True),
        "STATUS_URL": html.escape(config["status_url"], quote=True),
        "LANGUAGE": html.escape(config["language"], quote=True),
        "YEAR": str(datetime.now(timezone.utc).year),
    }

    page_template = (source / "templates/page.html").read_text(encoding="utf-8")
    footer = replace_tokens(
        (source / "content/footer.html").read_text(encoding="utf-8"), escaped
    )

    pages = {
        "": ("Home", "home.html"),
        "about": ("About", "about.html"),
        "rules": ("Rules", "rules.html"),
        "privacy": ("Privacy", "privacy.html"),
    }

    for slug, (title, content_file) in pages.items():
        values = dict(escaped)
        values["PAGE_TITLE"] = html.escape(title)
        content = replace_tokens(
            (source / "content" / content_file).read_text(encoding="utf-8"), values
        )
        values["CONTENT"] = content
        values["FOOTER"] = footer
        rendered = replace_tokens(page_template, values)

        destination = output if slug == "" else output / slug
        destination.mkdir(parents=True, exist_ok=True)
        (destination / "index.html").write_text(rendered, encoding="utf-8")

    assets_destination = output / "assets"
    if assets_destination.exists():
        shutil.rmtree(assets_destination)
    shutil.copytree(source / "assets", assets_destination)

    print(f"Built relay site in {output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
