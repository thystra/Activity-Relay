from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path


class BuildSiteTest(unittest.TestCase):
    def test_dashboard_bundle_is_versioned_and_contains_participant_ui(self) -> None:
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
                        "source_url": "https://github.com/thystra/Activity-Relay",
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

            index = (output / "index.html").read_text(encoding="utf-8")
            self.assertIn("Participating servers", index)
            self.assertIn('id="relay-receiving-count"', index)
            self.assertRegex(index, r"/assets/relay\.css\?v=[0-9a-f]{16}")
            self.assertRegex(index, r"/assets/relay\.js\?v=[0-9a-f]{16}")

            javascript = (output / "assets/relay.js").read_text(encoding="utf-8")
            self.assertIn("receiving_instances", javascript)
            self.assertIn("if (!publisherList) return;", javascript)


if __name__ == "__main__":
    unittest.main()
