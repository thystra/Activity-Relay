#!/usr/bin/env python3
# File: contrib/ops/test_fep_ae0c_cases.py

from __future__ import annotations

import json
import unittest
from pathlib import Path
from urllib.parse import urlparse


REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
CASES_PATH = REPOSITORY_ROOT / "testdata" / "fep-ae0c" / "cases.json"
COVERAGE_PATH = REPOSITORY_ROOT / "testdata" / "fep-ae0c" / "coverage.json"
TWO_RELAY_CONTRACT_PATH = (
    REPOSITORY_ROOT
    / "testdata"
    / "fep-ae0c"
    / "two-relay-probe-contract.json"
)

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
EXECUTABLE_COVERAGE = {"executable-new", "existing-executable"}


class FEPAE0CCasesTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.document = json.loads(CASES_PATH.read_text(encoding="utf-8"))
        cls.allowed = cls.document["allowed_values"]
        cls.cases = cls.document["cases"]
        cls.coverage_document = json.loads(
            COVERAGE_PATH.read_text(encoding="utf-8")
        )
        cls.coverage = cls.coverage_document["coverage"]
        cls.two_relay_contract = json.loads(
            TWO_RELAY_CONTRACT_PATH.read_text(encoding="utf-8")
        )

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

    def test_coverage_metadata(self) -> None:
        self.assertEqual(self.coverage_document["schema_version"], 1)
        self.assertEqual(
            self.coverage_document["activity_relay_baseline"],
            "v2.5.0",
        )
        self.assertEqual(
            self.coverage_document["fixture_catalog"],
            "testdata/fep-ae0c/cases.json",
        )

    def test_every_fixture_has_exactly_one_coverage_record(self) -> None:
        case_ids = {case["id"] for case in self.cases}
        coverage_ids = [entry["case_id"] for entry in self.coverage]

        self.assertEqual(len(coverage_ids), len(set(coverage_ids)))
        self.assertEqual(case_ids, set(coverage_ids))

    def test_coverage_statuses_and_evidence(self) -> None:
        allowed_statuses = set(
            self.coverage_document["allowed_statuses"]
        )

        for entry in self.coverage:
            with self.subTest(case=entry["case_id"]):
                self.assertIn(entry["status"], allowed_statuses)
                self.assertIsInstance(entry.get("notes"), str)
                self.assertTrue(entry["notes"].strip())

                tests = entry.get("tests")
                self.assertIsInstance(tests, list)

                if entry["status"] in EXECUTABLE_COVERAGE:
                    self.assertGreater(len(tests), 0)
                    self.assertNotIn("command", entry)
                    for test_name in tests:
                        self.assertRegex(
                            test_name,
                            r"^Test[A-Za-z0-9_]+$",
                        )
                elif entry["status"] in {
                    "diagnostic-executable",
                    "process-invariant",
                }:
                    self.assertEqual(tests, [])
                    command = entry.get("command")
                    self.assertIsInstance(command, str)
                    self.assertTrue(command.strip())
                    command_path = REPOSITORY_ROOT / command
                    self.assertTrue(command_path.is_file(), command)
                    self.assertTrue(command_path.stat().st_mode & 0o111)
                    if entry["status"] == "process-invariant":
                        self.assertEqual(
                            entry.get("expected_classification"),
                            "no_reflection_observed",
                        )
                else:
                    self.assertEqual(tests, [])
                    self.assertNotIn("command", entry)

    def test_two_relay_probe_contract(self) -> None:
        contract = self.two_relay_contract
        self.assertEqual(contract["schema_version"], 1)
        self.assertEqual(
            contract["fixture"],
            "repeat-id-two-relay-loop",
        )
        topology = contract["topology"]
        self.assertEqual(topology["relay_processes"], 2)
        self.assertEqual(topology["worker_processes"], 2)
        self.assertEqual(topology["redis_instances"], 2)
        self.assertTrue(topology["independent_actor_keys"])
        self.assertTrue(topology["trusted_tls_frontends"])
        self.assertTrue(topology["signed_origin_server"])
        self.assertIn(
            "reflection_threshold_reached",
            contract["classifications"],
        )
        self.assertGreaterEqual(
            len(contract["required_observations"]),
            6,
        )
        self.assertGreaterEqual(
            len(contract.get("runtime_invariants", [])),
            5,
        )
        expectations = contract.get("passing_expectations", {})
        self.assertEqual(
            expectations.get("classification"),
            "no_reflection_observed",
        )
        self.assertEqual(
            expectations.get("generated_cross_relay_posts"),
            0,
        )


if __name__ == "__main__":
    unittest.main()

# EOF: contrib/ops/test_fep_ae0c_cases.py
