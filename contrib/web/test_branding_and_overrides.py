from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path


class BrandingAndOverridesTest(unittest.TestCase):
    def source(self) -> Path:
        return Path(__file__).resolve().parent

    def build(
        self,
        relay_config: str,
        *,
        site_overrides: dict[str, str] | None = None,
        rules_override: str | None = None,
    ) -> Path:
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        config = root / "site.json"
        relay = root / "config.yml"
        output = root / "public"

        values = {
            "site_name": "Test Relay",
            "tagline": "Test tagline",
            "operator_name": "Legacy Operator",
            "contact_url": "mailto:test@example.com",
            "activitypub_contact": "",
            "activitypub_contact_url": "",
            "source_url": "https://github.com/thystra/Activity-Relay",
            "status_url": "/status.json",
            "language": "en",
            "logo_url": "",
            "logo_alt": "Test Relay logo",
            "banner_url": "",
            "banner_alt": "Test Relay banner",
        }
        if site_overrides:
            values.update(site_overrides)

        config.write_text(json.dumps(values), encoding="utf-8")
        relay.write_text(relay_config, encoding="utf-8")

        command = [
            "python3",
            str(self.source() / "build-site.py"),
            "--source",
            str(self.source()),
            "--config",
            str(config),
            "--relay-config",
            str(relay),
            "--output",
            str(output),
        ]

        if rules_override is not None:
            content = root / "content"
            content.mkdir()
            (content / "rules.html").write_text(
                rules_override,
                encoding="utf-8",
            )
            command.extend(["--content-overrides", str(content)])

        subprocess.run(command, check=True)
        return output

    def test_relay_config_drives_branding_and_operator(self) -> None:
        output = self.build(
            'RELAY_ICON: "https://example.org/icon.png"\n'
            'RELAY_IMAGE: "https://example.org/banner.png"\n'
            'FEDIVERSE_OPERATOR_ID: "@operator@friendica.example"\n'
            'FEDIVERSE_OPERATOR_URL: '
            '"https://friendica.example/profile/operator"\n'
        )
        index = (output / "index.html").read_text(encoding="utf-8")
        javascript = (output / "assets/relay.js").read_text(
            encoding="utf-8"
        )

        self.assertIn(
            'class="site-logo" src="https://example.org/icon.png"',
            index,
        )
        self.assertIn(
            'class="site-banner"><img src="https://example.org/banner.png"',
            index,
        )
        self.assertIn(
            'rel="me" href="https://friendica.example/profile/operator"',
            index,
        )
        self.assertIn("@operator@friendica.example", index)
        self.assertNotIn("Status loaded from", javascript)
        self.assertIn('setStatusMessage("", true);', javascript)

    def test_underscore_alias_and_plain_operator_id(self) -> None:
        output = self.build(
            'FEDIVERSE_OPERATOR_ID: "@operator@example.org"\n'
        )
        index = (output / "index.html").read_text(encoding="utf-8")
        self.assertIn(
            "Operated by <span>@operator@example.org</span>",
            index,
        )

    def test_site_json_branding_overrides_relay_config(self) -> None:
        output = self.build(
            'RELAY_ICON: "https://example.org/relay-icon.png"\n'
            'RELAY_IMAGE: "https://example.org/relay-banner.png"\n',
            site_overrides={
                "logo_url": "https://example.org/site-icon.png",
                "banner_url": "https://example.org/site-banner.png",
            },
        )
        index = (output / "index.html").read_text(encoding="utf-8")
        self.assertIn("site-icon.png", index)
        self.assertIn("site-banner.png", index)
        self.assertNotIn("relay-icon.png", index)
        self.assertNotIn("relay-banner.png", index)

    def test_rules_override(self) -> None:
        output = self.build(
            'FEDIVERSE_OPERATOR_ID: "@operator@example.org"\n',
            rules_override="<h1>Operator rules override</h1>",
        )
        rules = (output / "rules/index.html").read_text(
            encoding="utf-8"
        )
        self.assertIn("Operator rules override", rules)


if __name__ == "__main__":
    unittest.main()
