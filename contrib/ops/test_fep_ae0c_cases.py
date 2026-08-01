#!/usr/bin/env python3
# File: contrib/ops/test_fep_ae0c_cases.py

from __future__ import annotations

import json
import unittest
from pathlib import Path
from urllib.parse import urlparse


REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
CASES_PATH = REPOSITORY_ROOT / "testdata" / "fep-ae0c" / "cases.json"

REQUIRED_CASE_FIELDS = {
    "id",
    "description",
    "profile",
    "status",
    "decision",
    "distribution",
    "current_v2_5",
    "v3_assertion",
}

PAYLOAD_FIELDS = {"activity", "actor", "http_request", "scenario"}


class FEPAE0CCasesTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.document = json.loads(CASES_PATH.read_text(encoding="utf-8"))
        cls.allowed = cls.document["allowed_values"]
        cls.cases = cls.document["cases"]

    def test_document_metadata(self) -> None:
        self.assertEqual(self.document["schema_version"], 1)
        self.assertEqual(
            self.document["source"]["fep"],
            "https://w3id.org/fep/ae0c",
        )
        self.assertEqual(
            self.document["source"]["activity_relay_baseline"],
            "v2.5.0",
        )
        self.assertGreaterEqual(len(self.cases), 16)

    def test_case_ids_are_unique_and_portable(self) -> None:
        identifiers = [case["id"] for case in self.cases]
        self.assertEqual(len(identifiers), len(set(identifiers)))
        for identifier in identifiers:
            self.assertRegex(identifier, r"^[a-z0-9]+(?:-[a-z0-9]+)*$")

    def test_required_fields_and_enums(self) -> None:
        for case in self.cases:
            with self.subTest(case=case.get("id")):
                self.assertTrue(REQUIRED_CASE_FIELDS.issubset(case))
                self.assertIn(case["profile"], self.allowed["profile"])
                self.assertIn(case["status"], self.allowed["status"])
                self.assertIn(case["decision"], self.allowed["decision"])
                self.assertIn(
                    case["distribution"],
                    self.allowed["distribution"],
                )
                self.assertEqual(
                    len(PAYLOAD_FIELDS.intersection(case)),
                    1,
                    "each case must have exactly one payload/scenario field",
                )

    def test_activity_and_actor_payloads_are_activitystreams_shaped(self) -> None:
        for case in self.cases:
            payload = case.get("activity") or case.get("actor")
            if payload is None:
                continue
            with self.subTest(case=case["id"]):
                self.assertIn("@context", payload)
                self.assertIsInstance(payload.get("id"), str)
                self.assertIsInstance(payload.get("type"), str)
                self.assertTrue(urlparse(payload["id"]).scheme)
                if "actor" in payload and isinstance(payload["actor"], str):
                    self.assertTrue(urlparse(payload["actor"]).scheme)

    def test_decisions_include_preservation_extensions_and_open_questions(self) -> None:
        decisions = {case["decision"] for case in self.cases}
        self.assertTrue(
            {"preserve", "document-extension", "investigate", "defer"}
            .issubset(decisions)
        )

    def test_audit_required_cases_are_not_claimed_as_covered(self) -> None:
        for case in self.cases:
            if case["status"] == "audit-required":
                with self.subTest(case=case["id"]):
                    self.assertEqual(case["decision"], "investigate")


if __name__ == "__main__":
    unittest.main()

# EOF: contrib/ops/test_fep_ae0c_cases.py
