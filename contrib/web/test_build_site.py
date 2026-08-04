from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path


class BuildSiteTest(unittest.TestCase):
    def build_site(self, config_values: dict[str, str]) -> tuple[str, str]:
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
