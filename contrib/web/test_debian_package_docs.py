# File: contrib/web/test_debian_package_docs.py

from __future__ import annotations

import unittest
from pathlib import Path


class DebianPackageDocumentationTest(unittest.TestCase):
    def test_native_website_initialization_creates_site_json(self) -> None:
        repository = Path(__file__).resolve().parents[2]
        readme = (repository / "debian" / "README.Debian").read_text(
            encoding="utf-8"
        )
        install_manifest = (
            repository / "debian" / "activity-relay.install"
        ).read_text(encoding="utf-8")

        self.assertIn(
            "contrib/web/site.json.example etc/activity-relay-web",
            install_manifest,
        )

        rebuild_wrapper = (
            repository / "contrib" / "web" / "activity-relay-rebuild-site"
        ).read_text(encoding="utf-8")

        copy_sequence = (
            "   cp -an /usr/share/activity-relay/web/. "
            "/etc/activity-relay-web/\n"
            "\n"
            "   cp --update=none \\\n"
            "       /etc/activity-relay-web/site.json.example \\\n"
            "       /etc/activity-relay-web/site.json\n"
        )

        self.assertEqual(readme.count(copy_sequence), 1)
        self.assertIn(
            'config_file="${ACTIVITY_RELAY_WEB_CONFIG:-'
            '/etc/activity-relay-web/site.json}"',
            rebuild_wrapper,
        )
        self.assertLess(
            readme.index(copy_sequence),
            readme.index(
                "Edit /etc/activity-relay-web/site.json and build the site"
            ),
        )


if __name__ == "__main__":
    unittest.main()

# EOF: contrib/web/test_debian_package_docs.py
