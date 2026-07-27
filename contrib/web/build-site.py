#!/usr/bin/env python3
"Build the static Activity-Relay landing site without external dependencies."

from __future__ import annotations

import argparse
import hashlib
import html
import json
import shutil
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urlparse


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
    data.setdefault("logo_url", "")
    data.setdefault("logo_alt", data["site_name"])
    data.setdefault("activitypub_contact", "")
    data.setdefault("activitypub_contact_url", "")

    activitypub_contact = str(data["activitypub_contact"]).strip()
    activitypub_contact_url = str(data["activitypub_contact_url"]).strip()
    if activitypub_contact_url and not activitypub_contact:
        raise SystemExit(
            "activitypub_contact_url requires activitypub_contact"
        )
    if activitypub_contact_url:
        parsed_contact_url = urlparse(activitypub_contact_url)
        if (
            parsed_contact_url.scheme != "https"
            or not parsed_contact_url.netloc
        ):
            raise SystemExit(
                "activitypub_contact_url must be an absolute HTTPS URL"
            )

    data["activitypub_contact"] = activitypub_contact
    data["activitypub_contact_url"] = activitypub_contact_url
    return {key: str(value) for key, value in data.items()}


def replace_tokens(text: str, values: dict[str, str]) -> str:
    for key, value in values.items():
        text = text.replace("{{" + key + "}}", value)
    return text


def asset_version(source: Path) -> str:
    digest = hashlib.sha256()
    for path in sorted((source / "assets").rglob("*")):
        if path.is_file():
            digest.update(path.relative_to(source).as_posix().encode("utf-8"))
            digest.update(b"\0")
            digest.update(path.read_bytes())
            digest.update(b"\0")
    return digest.hexdigest()[:16]


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
        "ASSET_VERSION": asset_version(source),
    }

    activitypub_contact = html.escape(config["activitypub_contact"])
    activitypub_contact_url = html.escape(
        config["activitypub_contact_url"],
        quote=True,
    )
    if activitypub_contact:
        activitypub_label = f"ActivityPub contact: {activitypub_contact}"
        if activitypub_contact_url:
            escaped["ACTIVITYPUB_CONTACT_HTML"] = (
                ' · <a href="'
                + activitypub_contact_url
                + '">'
                + activitypub_label
                + "</a>"
            )
        else:
            escaped["ACTIVITYPUB_CONTACT_HTML"] = (
                " · <span>" + activitypub_label + "</span>"
            )
    else:
        escaped["ACTIVITYPUB_CONTACT_HTML"] = ""
    logo_url = html.escape(config["logo_url"], quote=True)
    escaped["LOGO"] = (
        '<img class="site-logo" src="'
        + logo_url
        + '" alt="'
        + html.escape(config["logo_alt"], quote=True)
        + '">' if logo_url else ""
    )
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
