#!/usr/bin/env python3
# Build the optional Activity-Relay static website.

from __future__ import annotations

import argparse
import hashlib
import html
import json
import re
import shutil
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urlparse


def load_json_config(path: Path) -> dict[str, str]:
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
        raise SystemExit(
            f"Missing website configuration keys: {', '.join(missing)}"
        )

    data.setdefault("logo_url", "")
    data.setdefault("logo_alt", data["site_name"])
    data.setdefault("banner_url", "")
    data.setdefault("banner_alt", f'{data["site_name"]} banner')
    data.setdefault("activitypub_contact", "")
    data.setdefault("activitypub_contact_url", "")

    activitypub_contact = str(data["activitypub_contact"]).strip()
    activitypub_contact_url = str(data["activitypub_contact_url"]).strip()

    if activitypub_contact_url and not activitypub_contact:
        raise SystemExit(
            "activitypub_contact_url requires activitypub_contact"
        )

    if activitypub_contact_url:
        parsed = urlparse(activitypub_contact_url)
        if parsed.scheme != "https" or not parsed.netloc:
            raise SystemExit(
                "activitypub_contact_url must be an absolute HTTPS URL"
            )

    data["activitypub_contact"] = activitypub_contact
    data["activitypub_contact_url"] = activitypub_contact_url

    return {key: str(value) for key, value in data.items()}


def strip_yaml_comment(value: str) -> str:
    quote = ""
    escaped = False

    for index, character in enumerate(value):
        if escaped:
            escaped = False
            continue
        if character == "\\" and quote == '"':
            escaped = True
            continue
        if quote:
            if character == quote:
                quote = ""
            continue
        if character in {"'", '"'}:
            quote = character
            continue
        if character == "#" and (
            index == 0 or value[index - 1].isspace()
        ):
            return value[:index].rstrip()

    return value.rstrip()


def parse_simple_yaml_scalar(value: str) -> str:
    value = strip_yaml_comment(value.strip())
    if not value:
        return ""

    if value.startswith('"') and value.endswith('"'):
        try:
            parsed = json.loads(value)
        except json.JSONDecodeError as error:
            raise SystemExit(
                f"Invalid quoted YAML scalar: {value}"
            ) from error
        return str(parsed)

    if value.startswith("'") and value.endswith("'"):
        return value[1:-1].replace("''", "'")

    return value


def load_relay_site_metadata(path: Path | None) -> dict[str, str]:
    if path is None or not path.exists():
        return {}

    aliases = {
        "RELAY_ICON": "RELAY_ICON",
        "RELAY_IMAGE": "RELAY_IMAGE",
        "FEDIVERSE_OPERATOR_ID": "FEDIVERSE_OPERATOR_ID",
        "FEDIVERSE-OPERATOR-ID": "FEDIVERSE_OPERATOR_ID",
        "FEDIVERSE_OPERATOR_URL": "FEDIVERSE_OPERATOR_URL",
        "FEDIVERSE-OPERATOR-URL": "FEDIVERSE_OPERATOR_URL",
    }
    result: dict[str, str] = {}

    for raw_line in path.read_text(encoding="utf-8").splitlines():
        if not raw_line or raw_line[0].isspace():
            continue

        stripped = raw_line.strip()
        if not stripped or stripped.startswith("#"):
            continue

        key, separator, value = raw_line.partition(":")
        key = key.strip()
        canonical = aliases.get(key)
        if separator and canonical:
            result[canonical] = parse_simple_yaml_scalar(value)

    return result


def validate_public_url(value: str, key: str) -> str:
    value = value.strip()
    if not value:
        return ""

    if value.startswith("/"):
        return value

    parsed = urlparse(value)
    if parsed.scheme != "https" or not parsed.netloc:
        raise SystemExit(
            f"{key} must be an absolute HTTPS URL or a root-relative path"
        )

    return value


def validate_operator_id(value: str) -> str:
    value = value.strip()
    if not value:
        return ""

    if (
        not value.startswith("@")
        or value.count("@") != 2
        or any(character.isspace() for character in value)
    ):
        raise SystemExit(
            "FEDIVERSE_OPERATOR_ID must use @nickname@server.example form"
        )

    return value


def replace_tokens(text: str, values: dict[str, str]) -> str:
    for key, value in values.items():
        text = text.replace("{{" + key + "}}", value)
    return text


def asset_version(source: Path, asset_overrides: Path | None) -> str:
    digest = hashlib.sha256()

    roots = [source / "assets"]
    if asset_overrides is not None and asset_overrides.is_dir():
        roots.append(asset_overrides)

    for root in roots:
        for path in sorted(root.rglob("*")):
            if not path.is_file():
                continue
            digest.update(path.relative_to(root).as_posix().encode("utf-8"))
            digest.update(b"\0")
            digest.update(path.read_bytes())
            digest.update(b"\0")

    return digest.hexdigest()[:16]


def read_content(
    source: Path,
    content_overrides: Path | None,
    filename: str,
) -> str:
    if content_overrides is not None:
        override = content_overrides / filename
        if override.is_file():
            return override.read_text(encoding="utf-8")

    return (source / "content" / filename).read_text(encoding="utf-8")


def copy_asset_overrides(
    asset_overrides: Path | None,
    destination: Path,
) -> None:
    if asset_overrides is None or not asset_overrides.is_dir():
        return

    for source_path in asset_overrides.rglob("*"):
        if not source_path.is_file():
            continue
        relative = source_path.relative_to(asset_overrides)
        output_path = destination / relative
        output_path.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source_path, output_path)


def operator_html(
    operator_id: str,
    operator_url: str,
    fallback_name: str,
) -> str:
    label = operator_id or fallback_name
    escaped_label = html.escape(label)

    if operator_url:
        return (
            '<a rel="me" href="'
            + html.escape(operator_url, quote=True)
            + '">'
            + escaped_label
            + "</a>"
        )

    return f"<span>{escaped_label}</span>"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument(
        "--source",
        type=Path,
        default=Path(__file__).resolve().parent,
        help="Package or checkout website source directory",
    )
    parser.add_argument(
        "--relay-config",
        type=Path,
        help="Optional Activity-Relay YAML configuration",
    )
    parser.add_argument(
        "--content-overrides",
        type=Path,
        help="Optional operator content override directory",
    )
    parser.add_argument(
        "--asset-overrides",
        type=Path,
        help="Optional operator asset override directory",
    )
    args = parser.parse_args()

    config = load_json_config(args.config)
    relay_metadata = load_relay_site_metadata(args.relay_config)

    if not config["logo_url"]:
        config["logo_url"] = relay_metadata.get("RELAY_ICON", "")
    if not config["banner_url"]:
        config["banner_url"] = relay_metadata.get("RELAY_IMAGE", "")

    config["logo_url"] = validate_public_url(
        config["logo_url"],
        "logo_url or RELAY_ICON",
    )
    config["banner_url"] = validate_public_url(
        config["banner_url"],
        "banner_url or RELAY_IMAGE",
    )

    operator_id = validate_operator_id(
        relay_metadata.get("FEDIVERSE_OPERATOR_ID", "")
        or config["activitypub_contact"]
    )
    operator_url = validate_public_url(
        relay_metadata.get("FEDIVERSE_OPERATOR_URL", "")
        or config["activitypub_contact_url"],
        "FEDIVERSE_OPERATOR_URL",
    )

    source = args.source.resolve()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)

    escaped = {
        "SITE_NAME": html.escape(config["site_name"]),
        "TAGLINE": html.escape(config["tagline"]),
        "CONTACT_URL": html.escape(config["contact_url"], quote=True),
        "SOURCE_URL": html.escape(config["source_url"], quote=True),
        "STATUS_URL": html.escape(config["status_url"], quote=True),
        "LANGUAGE": html.escape(config["language"], quote=True),
        "OPERATOR_NAME": html.escape(config["operator_name"]),
        "YEAR": str(datetime.now(timezone.utc).year),
        "ASSET_VERSION": asset_version(source, args.asset_overrides),
        "OPERATOR_ID_HTML": operator_html(
            operator_id,
            operator_url,
            config["operator_name"],
        ),
    }

    logo_url = html.escape(config["logo_url"], quote=True)
    escaped["LOGO"] = (
        '<img class="site-logo" src="'
        + logo_url
        + '" alt="'
        + html.escape(config["logo_alt"], quote=True)
        + '">'
        if logo_url
        else ""
    )

    banner_url = html.escape(config["banner_url"], quote=True)
    escaped["BANNER"] = (
        '<figure class="site-banner">'
        '<img src="'
        + banner_url
        + '" alt="'
        + html.escape(config["banner_alt"], quote=True)
        + '">'
        "</figure>"
        if banner_url
        else ""
    )

    page_template = (
        source / "templates/page.html"
    ).read_text(encoding="utf-8")

    footer = replace_tokens(
        read_content(source, args.content_overrides, "footer.html"),
        escaped,
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
            read_content(source, args.content_overrides, content_file),
            values,
        )
        values["CONTENT"] = content
        values["FOOTER"] = footer
        rendered = replace_tokens(page_template, values)
        unresolved_tokens = sorted(
            set(re.findall(r"{{([A-Z_][A-Z_]*)}}", rendered))
        )
        if unresolved_tokens:
            raise SystemExit(
                "Unresolved website template tokens in "
                + content_file
                + ": "
                + ", ".join(unresolved_tokens)
            )

        destination = output if slug == "" else output / slug
        destination.mkdir(parents=True, exist_ok=True)
        (destination / "index.html").write_text(
            rendered,
            encoding="utf-8",
        )

    assets_destination = output / "assets"
    if assets_destination.exists():
        shutil.rmtree(assets_destination)

    shutil.copytree(source / "assets", assets_destination)
    copy_asset_overrides(args.asset_overrides, assets_destination)

    print(f"Built relay site in {output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
