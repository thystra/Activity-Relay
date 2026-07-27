from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path


class RebuildSiteCLITest(unittest.TestCase):
    def test_explicit_source_config_and_output(self) -> None:
        source = Path(__file__).resolve().parent
        wrapper = source / "rebuild-site.sh"

        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = root / "site.json"
            output = root / "public-html" / "relay"

            config.write_text(
                json.dumps(
                    {
                        "site_name": "Path Test Relay",
                        "tagline": "Custom output test",
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
                    str(wrapper),
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
            self.assertIn("Path Test Relay", index)

    def test_help(self) -> None:
        source = Path(__file__).resolve().parent
        result = subprocess.run(
            [str(source / "rebuild-site.sh"), "--help"],
            text=True,
            capture_output=True,
            check=True,
        )
        self.assertIn("--output DIR", result.stdout)


if __name__ == "__main__":
    unittest.main()
