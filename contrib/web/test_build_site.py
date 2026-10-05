from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path


class BuildSiteTest(unittest.TestCase):
    def build_site(
        self,
        config_values: dict[str, str],
        relay_config_body: str | None = None,
    ) -> tuple[str, str]:
        source = Path(__file__).resolve().parent
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        config = root / "site.json"
        output = root / "public"

        values = {
            "site_name": "Test Relay",
            "tagline": "A test relay",
            "operator_name": "Test Operator",
            "contact_url": "mailto:test@example.com",
            "source_url": "https://github.com/thystra/Activity-Relay",
            "status_url": "/status.json",
            "language": "en",
        }
        values.update(config_values)
        config.write_text(json.dumps(values), encoding="utf-8")

        command = [
            "python3",
            str(source / "build-site.py"),
            "--source",
            str(source),
            "--config",
            str(config),
            "--output",
            str(output),
        ]
        if relay_config_body is not None:
            relay_config = root / "config.yml"
            relay_config.write_text(relay_config_body, encoding="utf-8")
            command.extend(["--relay-config", str(relay_config)])

        subprocess.run(command, check=True)

        return (
            (output / "index.html").read_text(encoding="utf-8"),
            (output / "assets/relay.js").read_text(encoding="utf-8"),
        )

    def test_dashboard_bundle_and_activitypub_contact(self) -> None:
        index, javascript = self.build_site(
            {
                "activitypub_contact": "@operator@social.example",
                "activitypub_contact_url": (
                    "https://social.example/@operator?x=1&y=2"
                ),
            }
        )

        self.assertIn("Participating servers", index)
        self.assertIn('id="relay-receiving-count"', index)
        self.assertIn("data-relay-policy-status", index)
        self.assertIn('id="relay-policy-label"', index)
        self.assertIn('id="relay-policy-description"', index)
        self.assertGreaterEqual(
            index.count('data-status-url="/status.json"'),
            2,
        )
        self.assertRegex(index, r"/assets/relay\.css\?v=[0-9a-f]{16}")
        self.assertRegex(index, r"/assets/relay\.js\?v=[0-9a-f]{16}")
        self.assertIn(
            'href="https://social.example/@operator?x=1&amp;y=2"',
            index,
        )
        self.assertIn(
            "@operator@social.example",
            index,
        )
        self.assertIn("receiving_instances", javascript)
        self.assertIn("public_address_distribution_policy", javascript)
        self.assertIn("public_address_distribution_label", javascript)
        self.assertIn("explicit_public_only", javascript)
        self.assertIn("public_and_unlisted", javascript)
        self.assertIn("if (!dashboard && !policyStatus) return;", javascript)
        self.assertIn("renderPolicy(data);", javascript)
        self.assertIn("renderPolicyUnavailable();", javascript)
        self.assertIn("if (!publisherList) return;", javascript)
        self.assertIn('setStatusMessage("", true);', javascript)
        self.assertIn(
            "Unable to load relay status:",
            javascript,
        )
        self.assertNotIn(
            "Status loaded from ${statusURL}.",
            javascript,
        )

    def test_footer_uses_configured_status_url(self) -> None:
        index, _ = self.build_site(
            {"status_url": "/alternate-status.json"}
        )
        self.assertGreaterEqual(
            index.count('data-status-url="/alternate-status.json"'),
            2,
        )

    def test_activitypub_contact_is_optional(self) -> None:
        index, _ = self.build_site({})
        self.assertNotIn("ActivityPub contact:", index)

    def test_activitypub_contact_can_be_plain_text(self) -> None:
        index, _ = self.build_site(
            {"activitypub_contact": "@operator@social.example"}
        )
        self.assertIn(
            "<span>@operator@social.example</span>",
            index,
        )

    def test_activitypub_contact_url_requires_handle(self) -> None:
        result = self.run_invalid_config(
            {
                "activitypub_contact": "",
                "activitypub_contact_url": (
                    "https://social.example/@operator"
                ),
            }
        )
        self.assertIn(
            "activitypub_contact_url requires activitypub_contact",
            result.stderr,
        )

    def test_activitypub_contact_url_requires_https(self) -> None:
        result = self.run_invalid_config(
            {
                "activitypub_contact": "@operator@social.example",
                "activitypub_contact_url": (
                    "http://social.example/@operator"
                ),
            }
        )
        self.assertIn(
            "activitypub_contact_url must be an absolute HTTPS URL",
            result.stderr,
        )



    def test_directory_profile_renders_near_top_with_focus_grouping(self) -> None:
        index, _ = self.build_site(
            {},
            """
DIRECTORY_PROFILE:
  participation_mode: open
  availability: public
  relay_type: unrestricted
  languages: [EN]
  countries:
    - US
    - UK
  regions: [Americas]
  topics: [general]
  contact_fediverse: "@alan@friendica.argentwolf.org"
  contact_email: webmaster@argentwolf.org
  contact_url: https://www.wolfandraven.blog
  participation_url: https://relay.argentwolf.org
  notes: "Public relay & community <welcome>"
""",
        )
        profile_position = index.index('id="relay-profile-heading"')
        status_position = index.index('data-relay-dashboard')
        self.assertLess(profile_position, status_position)
        for required in [
            "Relay information",
            "Registration status",
            "Relay focus",
            "This relay is focused on the following languages, countries, and/or regions:",
            "unrestricted",
            "general",
            "EN",
            "UK, US",
            "Americas",
            "@alan@friendica.argentwolf.org",
            "webmaster@argentwolf.org",
            'href="https://www.wolfandraven.blog"',
            "Public relay &amp; community &lt;welcome&gt;",
        ]:
            self.assertIn(required, index)
        self.assertNotIn("Public relay & community <welcome>", index)
        self.assertNotIn("https://relay.argentwolf.org", index)

    def test_directory_profile_suppresses_location_focus_when_unspecified(self) -> None:
        index, _ = self.build_site(
            {},
            """
DIRECTORY_PROFILE:
  participation_mode: open
  availability: public
  relay_type: general
  languages: []
  countries: []
  regions: []
  topics: [general]
  contact_fediverse: ""
  contact_email: ""
  contact_url: ""
  participation_url: ""
  notes: ""
""",
        )
        self.assertIn("Relay focus", index)
        self.assertIn("Topics", index)
        self.assertNotIn(
            "This relay is focused on the following languages, countries, and/or regions:",
            index,
        )
        self.assertNotIn("<dt>Languages</dt>", index)
        self.assertNotIn("<dt>Countries</dt>", index)
        self.assertNotIn("<dt>Regions</dt>", index)

    def test_support_block_is_optional_collapsed_and_escaped(self) -> None:
        empty, _ = self.build_site({})
        self.assertNotIn("Support this relay", empty)

        index, _ = self.build_site(
            {},
            """
SUPPORT:
  - title: "Liberapay & friends"
    url: "https://support.example/path?x=1&y=2"
  - title: "Wallet <primary>"
    value: "bc1qexample&value"
""",
        )
        self.assertIn('<details class="panel support-panel">', index)
        self.assertIn("Support this relay", index)
        self.assertIn("Optional ways to support the operation of this relay.", index)
        self.assertIn("Liberapay &amp; friends", index)
        self.assertIn('href="https://support.example/path?x=1&amp;y=2"', index)
        self.assertIn("Wallet &lt;primary&gt;", index)
        self.assertIn("bc1qexample&amp;value", index)
        self.assertNotIn("Wallet <primary>", index)

    def test_invalid_support_entry_fails_static_site_build_only(self) -> None:
        source = Path(__file__).resolve().parent
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = root / "site.json"
            relay_config = root / "config.yml"
            output = root / "public"
            config.write_text(
                json.dumps(
                    {
                        "site_name": "Test Relay",
                        "tagline": "A test relay",
                        "operator_name": "Test Operator",
                        "contact_url": "mailto:test@example.com",
                        "source_url": "https://github.com/thystra/Activity-Relay",
                        "status_url": "/status.json",
                        "language": "en",
                    }
                ),
                encoding="utf-8",
            )
            relay_config.write_text(
                """
SUPPORT:
  - title: Broken
    url: http://support.example/
""",
                encoding="utf-8",
            )
            result = subprocess.run(
                [
                    "python3",
                    str(source / "build-site.py"),
                    "--source",
                    str(source),
                    "--config",
                    str(config),
                    "--relay-config",
                    str(relay_config),
                    "--output",
                    str(output),
                ],
                text=True,
                capture_output=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("must be an absolute HTTPS URL", result.stderr)

    def test_operator_name_content_token_is_rendered(self) -> None:
        source = Path(__file__).resolve().parent
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = root / "site.json"
            output = root / "public"
            config.write_text(
                json.dumps(
                    {
                        "site_name": "Test Relay",
                        "tagline": "A test relay",
                        "operator_name": "Test Operator",
                        "contact_url": "mailto:test@example.com",
                        "source_url": (
                            "https://github.com/thystra/Activity-Relay"
                        ),
                        "status_url": "/status.json",
                        "language": "en",
                    }
                ),
                encoding="utf-8",
            )
            subprocess.run(
                [
                    "python3",
                    str(source / "build-site.py"),
                    "--source",
                    str(source),
                    "--config",
                    str(config),
                    "--output",
                    str(output),
                ],
                check=True,
            )
            about = (output / "about/index.html").read_text(
                encoding="utf-8"
            )
            privacy = (output / "privacy/index.html").read_text(
                encoding="utf-8"
            )
            self.assertIn("Test Operator", about)
            self.assertIn("Test Operator", privacy)
            self.assertNotIn("{{OPERATOR_NAME}}", about)
            self.assertNotIn("{{OPERATOR_NAME}}", privacy)

    def test_unknown_content_token_is_rejected(self) -> None:
        source = Path(__file__).resolve().parent
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = root / "site.json"
            output = root / "public"
            overrides = root / "content"
            overrides.mkdir()
            (overrides / "home.html").write_text(
                "<p>{{UNKNOWN_TOKEN}}</p>",
                encoding="utf-8",
            )
            config.write_text(
                json.dumps(
                    {
                        "site_name": "Test Relay",
                        "tagline": "A test relay",
                        "operator_name": "Test Operator",
                        "contact_url": "mailto:test@example.com",
                        "source_url": (
                            "https://github.com/thystra/Activity-Relay"
                        ),
                        "status_url": "/status.json",
                        "language": "en",
                    }
                ),
                encoding="utf-8",
            )
            result = subprocess.run(
                [
                    "python3",
                    str(source / "build-site.py"),
                    "--source",
                    str(source),
                    "--config",
                    str(config),
                    "--output",
                    str(output),
                    "--content-overrides",
                    str(overrides),
                ],
                text=True,
                capture_output=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(
                (
                    "Unresolved website template tokens in "
                    "home.html: UNKNOWN_TOKEN"
                ),
                result.stderr,
            )

    def test_container_includes_website_source(self) -> None:
        source = Path(__file__).resolve().parent
        dockerfile = source.parents[1] / "Dockerfile"
        text = dockerfile.read_text(encoding="utf-8")
        self.assertIn(
            "/usr/share/activity-relay/web",
            text,
        )

    def test_custom_status_example_uses_external_script(self) -> None:
        source = Path(__file__).resolve().parent
        html = (source / "examples/status-widget.html").read_text(
            encoding="utf-8"
        )
        javascript = (source / "examples/status-widget.js").read_text(
            encoding="utf-8"
        )
        self.assertIn('src="/status-widget.js"', html)
        self.assertNotIn("<script>", html)
        self.assertIn('fetch("/status.json"', javascript)
        self.assertIn("receiving_instances", javascript)

    def run_invalid_config(
        self,
        config_values: dict[str, str],
    ) -> subprocess.CompletedProcess[str]:
        source = Path(__file__).resolve().parent
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = root / "site.json"
            output = root / "public"
            values = {
                "site_name": "Test Relay",
                "tagline": "A test relay",
                "operator_name": "Test Operator",
                "contact_url": "mailto:test@example.com",
                "source_url": (
                    "https://github.com/thystra/Activity-Relay"
                ),
                "status_url": "/status.json",
                "language": "en",
            }
            values.update(config_values)
            config.write_text(json.dumps(values), encoding="utf-8")

            result = subprocess.run(
                [
                    "python3",
                    str(source / "build-site.py"),
                    "--source",
                    str(source),
                    "--config",
                    str(config),
                    "--output",
                    str(output),
                ],
                text=True,
                capture_output=True,
            )
            self.assertNotEqual(result.returncode, 0)
            return result


if __name__ == "__main__":
    unittest.main()
