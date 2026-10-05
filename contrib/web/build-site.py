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


PROFILE_SCALAR_LIMIT = 256
PROFILE_NOTE_LIMIT = 1024
PROFILE_LIST_ITEMS = 16
PROFILE_LIST_ITEM_LIMIT = 128
PROFILE_URL_LIMIT = 2048
SUPPORT_ENTRIES_LIMIT = 8
SUPPORT_TITLE_LIMIT = 80
SUPPORT_VALUE_LIMIT = 512
SUPPORT_URL_LIMIT = 2048

PROFILE_SCALAR_KEYS = {
    "participation_mode",
    "availability",
    "relay_type",
    "contact_fediverse",
    "contact_email",
    "contact_url",
    "participation_url",
    "notes",
}
PROFILE_LIST_KEYS = {"languages", "countries", "regions", "topics"}


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


def yaml_block(path: Path | None, key: str) -> list[str]:
    if path is None or not path.exists():
        return []

    lines = path.read_text(encoding="utf-8").splitlines()
    header_index = None
    for index, raw_line in enumerate(lines):
        if not raw_line or raw_line[0].isspace():
            continue
        stripped = raw_line.strip()
        if stripped.startswith("#"):
            continue
        name, separator, value = raw_line.partition(":")
        if separator and name.strip() == key:
            if header_index is not None:
                raise SystemExit(f"Duplicate {key} block in relay configuration")
            if strip_yaml_comment(value.strip()):
                raise SystemExit(f"{key} must be a YAML block")
            header_index = index

    if header_index is None:
        return []

    block: list[str] = []
    for raw_line in lines[header_index + 1 :]:
        stripped = raw_line.strip()
        if not stripped or stripped.startswith("#"):
            block.append(raw_line)
            continue
        if not raw_line[0].isspace():
            break
        if "\t" in raw_line[: len(raw_line) - len(raw_line.lstrip())]:
            raise SystemExit(f"{key} indentation must use spaces")
        block.append(raw_line)
    return block


def split_inline_yaml_list(value: str) -> list[str]:
    value = strip_yaml_comment(value.strip())
    if value == "[]":
        return []
    if not (value.startswith("[") and value.endswith("]")):
        raise SystemExit("Profile list values must use a YAML sequence")

    content = value[1:-1].strip()
    if not content:
        return []

    items: list[str] = []
    current: list[str] = []
    quote = ""
    escaped = False
    for character in content:
        if escaped:
            current.append(character)
            escaped = False
            continue
        if character == "\\" and quote == '"':
            current.append(character)
            escaped = True
            continue
        if quote:
            current.append(character)
            if character == quote:
                quote = ""
            continue
        if character in {"'", '"'}:
            quote = character
            current.append(character)
            continue
        if character == ",":
            item = parse_simple_yaml_scalar("".join(current).strip())
            if not item:
                raise SystemExit("Profile list values may not contain empty items")
            items.append(item)
            current = []
            continue
        current.append(character)
    if quote:
        raise SystemExit("Unterminated quote in profile list")
    item = parse_simple_yaml_scalar("".join(current).strip())
    if not item:
        raise SystemExit("Profile list values may not contain empty items")
    items.append(item)
    return items


def contains_control(value: str) -> bool:
    return any(ord(character) < 0x20 or ord(character) == 0x7F for character in value)


def normalize_profile_text(value: str, maximum: int, key: str) -> str:
    value = value.strip()
    if not value:
        return ""
    if len(value.encode("utf-8")) > maximum or contains_control(value):
        raise SystemExit(f"DIRECTORY_PROFILE {key} is invalid")
    return value


def normalize_profile_list(values: list[str], key: str) -> list[str]:
    if len(values) > PROFILE_LIST_ITEMS:
        raise SystemExit(f"DIRECTORY_PROFILE {key} has too many values")
    normalized: set[str] = set()
    for value in values:
        value = normalize_profile_text(value, PROFILE_LIST_ITEM_LIMIT, key)
        if not value:
            raise SystemExit(f"DIRECTORY_PROFILE {key} contains an empty value")
        normalized.add(value)
    return sorted(normalized)


def normalize_profile_url(value: str, key: str) -> str:
    value = normalize_profile_text(value, PROFILE_URL_LIMIT, key)
    if not value:
        return ""
    parsed = urlparse(value)
    if (
        parsed.scheme != "https"
        or not parsed.netloc
        or parsed.username is not None
        or parsed.password is not None
        or parsed.query
        or parsed.fragment
    ):
        raise SystemExit(f"DIRECTORY_PROFILE {key} must be an absolute HTTPS URL without credentials, query, or fragment")
    return value


def load_directory_profile(path: Path | None) -> dict[str, object]:
    block = yaml_block(path, "DIRECTORY_PROFILE")
    if not block:
        return {}

    meaningful = [line for line in block if line.strip() and not line.strip().startswith("#")]
    if not meaningful:
        return {}
    base_indent = min(len(line) - len(line.lstrip(" ")) for line in meaningful)
    if base_indent <= 0:
        raise SystemExit("DIRECTORY_PROFILE entries must be indented")

    profile: dict[str, object] = {}
    index = 0
    while index < len(block):
        raw_line = block[index]
        stripped = raw_line.strip()
        if not stripped or stripped.startswith("#"):
            index += 1
            continue
        indent = len(raw_line) - len(raw_line.lstrip(" "))
        if indent != base_indent:
            raise SystemExit("DIRECTORY_PROFILE has invalid indentation")
        key, separator, raw_value = raw_line.strip().partition(":")
        if not separator or key in profile or key not in PROFILE_SCALAR_KEYS | PROFILE_LIST_KEYS:
            raise SystemExit("DIRECTORY_PROFILE contains an unknown or duplicate field")

        raw_value = strip_yaml_comment(raw_value.strip())
        if key in PROFILE_LIST_KEYS:
            if raw_value:
                values = split_inline_yaml_list(raw_value)
                index += 1
            else:
                values = []
                index += 1
                while index < len(block):
                    child = block[index]
                    child_stripped = child.strip()
                    if not child_stripped or child_stripped.startswith("#"):
                        index += 1
                        continue
                    child_indent = len(child) - len(child.lstrip(" "))
                    if child_indent <= base_indent:
                        break
                    if not child_stripped.startswith("- "):
                        raise SystemExit(f"DIRECTORY_PROFILE {key} must be a YAML sequence")
                    value = parse_simple_yaml_scalar(child_stripped[2:].strip())
                    if not value:
                        raise SystemExit(f"DIRECTORY_PROFILE {key} contains an empty value")
                    values.append(value)
                    index += 1
            profile[key] = normalize_profile_list(values, key)
            continue

        if not raw_value:
            raise SystemExit(f"DIRECTORY_PROFILE {key} must be a scalar string")
        value = parse_simple_yaml_scalar(raw_value)
        maximum = PROFILE_NOTE_LIMIT if key == "notes" else PROFILE_SCALAR_LIMIT
        if key in {"contact_url", "participation_url"}:
            profile[key] = normalize_profile_url(value, key)
        else:
            profile[key] = normalize_profile_text(value, maximum, key)
        index += 1

    return profile


def render_profile_list(values: object) -> str:
    if not isinstance(values, list):
        return ""
    return ", ".join(html.escape(str(value)) for value in values)


def relay_profile_html(profile: dict[str, object]) -> str:
    if not profile or not any(profile.values()):
        return ""

    def scalar(key: str) -> str:
        return html.escape(str(profile.get(key, "")))

    def row(label: str, value: str, link: str = "") -> str:
        if not value:
            return ""
        rendered = value
        if link:
            rendered = (
                '<a rel="noreferrer" href="'
                + html.escape(link, quote=True)
                + '">'
                + value
                + "</a>"
            )
        return "<div><dt>" + html.escape(label) + "</dt><dd>" + rendered + "</dd></div>"

    information = "".join(
        [
            row("Registration status", scalar("participation_mode")),
            row("Availability", scalar("availability")),
            row("Fediverse contact", scalar("contact_fediverse")),
            row("Email", scalar("contact_email")),
            row("Contact", scalar("contact_url"), str(profile.get("contact_url", ""))),
            row("Notes", scalar("notes")),
        ]
    )

    relay_type = scalar("relay_type")
    topics = render_profile_list(profile.get("topics", []))
    focus_rows = row("Relay type", relay_type) + row("Topics", topics)

    language_rows = "".join(
        [
            row("Languages", render_profile_list(profile.get("languages", []))),
            row("Countries", render_profile_list(profile.get("countries", []))),
            row("Regions", render_profile_list(profile.get("regions", []))),
        ]
    )
    focus_intro = ""
    if language_rows:
        focus_intro = (
            '<p class="relay-focus-intro">This relay is focused on the following '
            "languages, countries, and/or regions:</p>"
            '<dl class="profile-grid relay-focus-locations">'
            + language_rows
            + "</dl>"
        )

    focus = ""
    if focus_rows or focus_intro:
        focus = (
            '<div class="relay-profile-section relay-focus">'
            "<h3>Relay focus</h3>"
            + ('<dl class="profile-grid">' + focus_rows + "</dl>" if focus_rows else "")
            + focus_intro
            + "</div>"
        )

    return (
        '<section class="panel relay-profile-panel" aria-labelledby="relay-profile-heading">'
        '<h2 id="relay-profile-heading">Relay information</h2>'
        + ('<dl class="profile-grid">' + information + "</dl>" if information else "")
        + focus
        + "</section>"
    )


def load_support_entries(path: Path | None) -> list[dict[str, str]]:
    block = yaml_block(path, "SUPPORT")
    if not block:
        return []
    meaningful = [line for line in block if line.strip() and not line.strip().startswith("#")]
    if not meaningful:
        return []
    base_indent = min(len(line) - len(line.lstrip(" ")) for line in meaningful)
    if base_indent <= 0:
        raise SystemExit("SUPPORT entries must be indented")

    entries: list[dict[str, str]] = []
    current: dict[str, str] | None = None
    for raw_line in block:
        stripped = raw_line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        indent = len(raw_line) - len(raw_line.lstrip(" "))
        if indent == base_indent:
            if not stripped.startswith("- "):
                raise SystemExit("SUPPORT must be a YAML sequence")
            if current is not None:
                entries.append(current)
            current = {}
            field_text = stripped[2:].strip()
        else:
            if current is None or indent <= base_indent:
                raise SystemExit("SUPPORT has invalid indentation")
            field_text = stripped
        key, separator, raw_value = field_text.partition(":")
        key = key.strip()
        if not separator or key not in {"title", "url", "value"} or current is None or key in current:
            raise SystemExit("SUPPORT contains an unknown or duplicate field")
        value = parse_simple_yaml_scalar(raw_value.strip())
        current[key] = value
    if current is not None:
        entries.append(current)

    if len(entries) > SUPPORT_ENTRIES_LIMIT:
        raise SystemExit("SUPPORT has too many entries")

    normalized: list[dict[str, str]] = []
    for index, entry in enumerate(entries, start=1):
        title = entry.get("title", "").strip()
        link = entry.get("url", "").strip()
        value = entry.get("value", "").strip()
        if (
            not title
            or len(title.encode("utf-8")) > SUPPORT_TITLE_LIMIT
            or contains_control(title)
            or (not link) == (not value)
        ):
            raise SystemExit(f"SUPPORT entry {index} is invalid")
        if link:
            if len(link.encode("utf-8")) > SUPPORT_URL_LIMIT or contains_control(link):
                raise SystemExit(f"SUPPORT entry {index} is invalid")
            parsed = urlparse(link)
            if parsed.scheme != "https" or not parsed.netloc or parsed.username is not None or parsed.password is not None:
                raise SystemExit(f"SUPPORT entry {index} url must be an absolute HTTPS URL without credentials")
        if value and (len(value.encode("utf-8")) > SUPPORT_VALUE_LIMIT or contains_control(value)):
            raise SystemExit(f"SUPPORT entry {index} is invalid")
        normalized.append({"title": title, "url": link, "value": value})
    return normalized


def support_html(entries: list[dict[str, str]]) -> str:
    if not entries:
        return ""
    methods = []
    for entry in entries:
        title = html.escape(entry["title"])
        if entry["url"]:
            heading = (
                '<a rel="noreferrer" href="'
                + html.escape(entry["url"], quote=True)
                + '">'
                + title
                + "</a>"
            )
            detail = ""
        else:
            heading = title
            detail = "<dd><code>" + html.escape(entry["value"]) + "</code></dd>"
        methods.append("<div><dt>" + heading + "</dt>" + detail + "</div>")
    return (
        '<details class="panel support-panel">'
        "<summary>Support this relay</summary>"
        '<p class="muted">Optional ways to support the operation of this relay.</p>'
        '<dl class="support-methods">'
        + "".join(methods)
        + "</dl></details>"
    )


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
    relay_profile = load_directory_profile(args.relay_config)
    support_entries = load_support_entries(args.relay_config)

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
        "RELAY_PROFILE": relay_profile_html(relay_profile),
        "SUPPORT_BLOCK": support_html(support_entries),
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
