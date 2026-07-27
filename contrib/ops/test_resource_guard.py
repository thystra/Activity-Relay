from __future__ import annotations

import importlib.util
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

MODULE_PATH = Path(__file__).with_name("resource-guard.py")
SPEC = importlib.util.spec_from_file_location("activity_relay_resource_guard", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
guard = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(guard)


class ResourceGuardSummaryTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.storage = self.root / "storage"
        self.cache = self.root / "cache"
        self.state = self.root / "state"
        self.storage.mkdir()
        self.cache.mkdir()
        self.state.mkdir()
        (self.state / "state").write_text("ok\n", encoding="utf-8")
        self.config = self.root / "config.yml"

    def write_config(self, schedule: str) -> None:
        self.config.write_text(
            "\n".join(
                [
                    f"STORAGE_DIR: {self.storage}",
                    f"CACHE_DIR: {self.cache}",
                    "STORAGE_LIMIT: 1GB",
                    "CACHE_LIMIT: 1GB",
                    "RESOURCE_WARNING_PERCENT: 75",
                    "RESOURCE_CRITICAL_PERCENT: 100",
                    "ADMIN_EMAIL: root",
                    "MAIL_BACKEND: mail",
                    "MAIL_COMMAND: /usr/bin/mail",
                    "MAIL_TIMEOUT_SECONDS: 60",
                    "DAILY_SUMMARY_EMAIL: true",
                    schedule.rstrip(),
                    "SUMMARY_STATUS_URL: http://127.0.0.1/status.json",
                    "",
                ]
            ),
            encoding="utf-8",
        )

    def run_guard(self, *args: str, now: str) -> int:
        return guard.main(
            [
                "--config",
                str(self.config),
                "--state-dir",
                str(self.state),
                "--now",
                now,
                *args,
            ]
        )

    def state_json(self) -> dict:
        return json.loads(
            (self.state / "summary-slots.json").read_text(encoding="utf-8")
        )

    def test_block_list_and_legacy_time_parsing(self) -> None:
        self.write_config(
            'DAILY_SUMMARY_TIMES:\n'
            '  - "08:00"\n'
            '  - "14:30"\n'
            '  - "08:00"'
        )
        config = guard.load_simple_yaml(self.config)
        times, legacy = guard.parse_summary_times(config)
        self.assertEqual(times, ["08:00", "14:30"])
        self.assertFalse(legacy)

        self.write_config(
            "DAILY_SUMMARY_HOUR: 8\n"
            "DAILY_SUMMARY_MINUTE: 30"
        )
        config = guard.load_simple_yaml(self.config)
        times, legacy = guard.parse_summary_times(config)
        self.assertEqual(times, ["08:30"])
        self.assertTrue(legacy)

    def test_latest_due_slot_skips_earlier_slots(self) -> None:
        record = {"sent_slots": {}, "skipped_slots": {}}
        now = guard.parse_now("2026-07-27T15:05:00-04:00")
        selected, skipped = guard.due_slot(
            ["08:00", "12:00", "14:30", "20:00"],
            now,
            record,
        )
        self.assertEqual(selected, "14:30")
        self.assertEqual(skipped, ["08:00", "12:00"])

    @mock.patch.object(guard.socket, "getfqdn", return_value="relay.example")
    @mock.patch.object(guard, "load_domains", return_value=["a.example"])
    @mock.patch.object(guard, "send_mail")
    def test_changing_time_creates_new_slot_same_day(
        self,
        send_mail: mock.Mock,
        load_domains: mock.Mock,
        getfqdn: mock.Mock,
    ) -> None:
        self.write_config('DAILY_SUMMARY_TIMES:\n  - "13:00"')
        result = self.run_guard(now="2026-07-27T13:02:00-04:00")
        self.assertEqual(result, 0)
        self.assertIn(
            "13:00",
            self.state_json()["days"]["2026-07-27"]["sent_slots"],
        )

        self.write_config('DAILY_SUMMARY_TIMES:\n  - "14:00"')
        result = self.run_guard(now="2026-07-27T14:03:00-04:00")
        self.assertEqual(result, 0)
        record = self.state_json()["days"]["2026-07-27"]
        self.assertIn("13:00", record["sent_slots"])
        self.assertIn("14:00", record["sent_slots"])
        self.assertEqual(send_mail.call_count, 2)

    @mock.patch.object(guard.socket, "getfqdn", return_value="relay.example")
    @mock.patch.object(guard, "load_domains", return_value=["a.example"])
    @mock.patch.object(guard, "send_mail")
    def test_catch_up_email_records_and_describes_skipped_slots(
        self,
        send_mail: mock.Mock,
        load_domains: mock.Mock,
        getfqdn: mock.Mock,
    ) -> None:
        self.write_config(
            'DAILY_SUMMARY_TIMES:\n'
            '  - "08:00"\n'
            '  - "12:00"\n'
            '  - "14:30"'
        )
        result = self.run_guard(now="2026-07-27T15:00:00-04:00")
        self.assertEqual(result, 0)
        record = self.state_json()["days"]["2026-07-27"]
        self.assertIn("14:30", record["sent_slots"])
        self.assertEqual(
            sorted(record["skipped_slots"]),
            ["08:00", "12:00"],
        )
        body = send_mail.call_args.args[2]
        self.assertIn(
            "Skipped earlier scheduled report slots: 08:00, 12:00",
            body,
        )
        self.assertIn("--show-summary-state", body)
        self.assertIn("--reset-summary-slot 08:00 --force", body)
        self.assertIn("cannot recreate the historical state", body)

    @mock.patch.object(guard.socket, "getfqdn", return_value="relay.example")
    @mock.patch.object(guard, "load_domains", return_value=["a.example"])
    @mock.patch.object(guard, "send_mail")
    def test_no_mail_does_not_consume_scheduled_slot(
        self,
        send_mail: mock.Mock,
        load_domains: mock.Mock,
        getfqdn: mock.Mock,
    ) -> None:
        self.write_config('DAILY_SUMMARY_TIMES:\n  - "08:00"')
        result = self.run_guard(
            "--no-mail",
            now="2026-07-27T08:05:00-04:00",
        )
        self.assertEqual(result, 0)
        self.assertFalse((self.state / "summary-slots.json").exists())
        send_mail.assert_not_called()

    @mock.patch.object(guard.socket, "getfqdn", return_value="relay.example")
    @mock.patch.object(guard, "load_domains", return_value=["a.example"])
    @mock.patch.object(
        guard,
        "send_mail",
        side_effect=guard.MailDeliveryError("timed out"),
    )
    def test_mail_failure_does_not_consume_scheduled_slot(
        self,
        send_mail: mock.Mock,
        load_domains: mock.Mock,
        getfqdn: mock.Mock,
    ) -> None:
        self.write_config('DAILY_SUMMARY_TIMES:\n  - "08:00"')
        result = self.run_guard(now="2026-07-27T08:05:00-04:00")
        self.assertEqual(result, 3)
        self.assertFalse((self.state / "summary-slots.json").exists())
        self.assertFalse((self.state / "summary-domains.json").exists())

    @mock.patch.object(guard.socket, "getfqdn", return_value="relay.example")
    @mock.patch.object(guard, "load_domains", return_value=["a.example"])
    @mock.patch.object(guard, "send_mail")
    def test_manual_summary_does_not_consume_scheduled_slot(
        self,
        send_mail: mock.Mock,
        load_domains: mock.Mock,
        getfqdn: mock.Mock,
    ) -> None:
        self.write_config('DAILY_SUMMARY_TIMES:\n  - "08:00"')
        result = self.run_guard(
            "--send-summary-now",
            now="2026-07-27T07:30:00-04:00",
        )
        self.assertEqual(result, 0)
        state = self.state_json()
        self.assertNotIn("2026-07-27", state["days"])
        self.assertEqual(len(state["manual_summaries"]), 1)

        result = self.run_guard(now="2026-07-27T08:03:00-04:00")
        self.assertEqual(result, 0)
        self.assertIn(
            "08:00",
            self.state_json()["days"]["2026-07-27"]["sent_slots"],
        )

    @mock.patch.object(guard.socket, "getfqdn", return_value="relay.example")
    @mock.patch.object(guard, "load_domains", return_value=["a.example"])
    @mock.patch.object(guard, "send_mail")
    def test_reset_skipped_slot_makes_it_eligible(
        self,
        send_mail: mock.Mock,
        load_domains: mock.Mock,
        getfqdn: mock.Mock,
    ) -> None:
        self.write_config(
            'DAILY_SUMMARY_TIMES:\n'
            '  - "08:00"\n'
            '  - "14:00"'
        )
        self.run_guard(now="2026-07-27T14:03:00-04:00")
        record = self.state_json()["days"]["2026-07-27"]
        self.assertIn("08:00", record["skipped_slots"])

        result = self.run_guard(
            "--reset-summary-slot",
            "08:00",
            "--force",
            now="2026-07-27T14:04:00-04:00",
        )
        self.assertEqual(result, 0)
        record = self.state_json()["days"]["2026-07-27"]
        self.assertNotIn("08:00", record["skipped_slots"])

        self.run_guard(now="2026-07-27T14:05:00-04:00")
        record = self.state_json()["days"]["2026-07-27"]
        self.assertIn("08:00", record["sent_slots"])

    def test_legacy_date_maps_only_for_legacy_hour(self) -> None:
        self.write_config("DAILY_SUMMARY_HOUR: 8")
        (self.state / "summary-date").write_text(
            "2026-07-27\n",
            encoding="utf-8",
        )
        state = guard.default_summary_state()
        now = guard.parse_now("2026-07-27T09:00:00-04:00")
        changed = guard.migrate_legacy_summary_date(
            self.state,
            state,
            ["08:00"],
            True,
            now,
        )
        self.assertTrue(changed)
        record = state["days"]["2026-07-27"]
        self.assertIn("08:00", record["sent_slots"])
        self.assertFalse((self.state / "summary-date").exists())
        self.assertTrue((self.state / "summary-date.migrated").exists())


if __name__ == "__main__":
    unittest.main()
