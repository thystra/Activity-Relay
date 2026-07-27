#!/usr/bin/env python3
# /usr/lib/activity-relay/resource-guard.py
"""Check relay storage budgets and send scheduled operational summaries."""

from __future__ import annotations

import argparse
import fcntl
import json
import os
import re
import shutil
import socket
import subprocess
import sys
import urllib.request
from datetime import datetime
from pathlib import Path
from typing import Any

SUMMARY_STATE_VERSION = 1
SUMMARY_STATE_RETENTION_DAYS = 31


class MailDeliveryError(RuntimeError):
    """Raised when the configured local mail transport fails."""


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
        if character == "#" and (index == 0 or value[index - 1].isspace()):
            return value[:index].rstrip()
    return value.rstrip()


def parse_yaml_value(value: str) -> str | list[str]:
    value = strip_yaml_comment(value.strip())
    if not value:
        return ""
    if value.startswith("["):
        try:
            parsed = json.loads(value)
        except json.JSONDecodeError as error:
            raise ValueError(f"invalid inline list {value!r}") from error
        if not isinstance(parsed, list):
            raise ValueError(f"expected a list, found {type(parsed).__name__}")
        return [str(item) for item in parsed]
    if value.startswith('"') and value.endswith('"'):
        try:
            return str(json.loads(value))
        except json.JSONDecodeError as error:
            raise ValueError(f"invalid quoted value {value!r}") from error
    if value.startswith("'") and value.endswith("'"):
        return value[1:-1].replace("''", "'")
    return value


def load_simple_yaml(path: Path) -> dict[str, str | list[str]]:
    """Read top-level scalar values and simple block lists without PyYAML."""
    values: dict[str, str | list[str]] = {}
    lines = path.read_text(encoding="utf-8").splitlines()
    index = 0

    while index < len(lines):
        raw = lines[index]
        stripped = raw.strip()
        if (
            not stripped
            or stripped.startswith("#")
            or raw[:1].isspace()
            or ":" not in raw
        ):
            index += 1
            continue

        key, value = raw.split(":", 1)
        key = key.strip()
        value = strip_yaml_comment(value.strip())

        if value:
            values[key] = parse_yaml_value(value)
            index += 1
            continue

        items: list[str] = []
        cursor = index + 1
        while cursor < len(lines):
            child = lines[cursor]
            child_stripped = child.strip()
            if not child_stripped or child_stripped.startswith("#"):
                cursor += 1
                continue
            if not child[:1].isspace():
                break
            if child_stripped.startswith("-"):
                item = child_stripped[1:].strip()
                parsed = parse_yaml_value(item)
                if isinstance(parsed, list):
                    raise ValueError(
                        f"nested lists are not supported for {key}"
                    )
                items.append(parsed)
            cursor += 1

        values[key] = items if items else ""
        index = cursor

    return values


def config_scalar(
    config: dict[str, str | list[str]],
    key: str,
    default: str = "",
) -> str:
    value = config.get(key, default)
    if isinstance(value, list):
        raise ValueError(f"{key} must be a scalar value")
    return value


def configured_bool(value: str) -> bool:
    return value.strip().lower() in {"1", "true", "yes", "on"}


def parse_size(value: str) -> int:
    match = re.fullmatch(
        r"\s*(\d+(?:\.\d+)?)\s*([kmgtp]?i?b)?\s*",
        value,
        re.IGNORECASE,
    )
    if not match:
        raise ValueError(
            f"invalid size {value!r}; use bytes or units such as MB, GiB, or TB"
        )
    amount = float(match.group(1))
    unit = (match.group(2) or "B").upper()
    multipliers = {
        "B": 1,
        "KB": 1000,
        "MB": 1000**2,
        "GB": 1000**3,
        "TB": 1000**4,
        "PB": 1000**5,
        "KIB": 1024,
        "MIB": 1024**2,
        "GIB": 1024**3,
        "TIB": 1024**4,
        "PIB": 1024**5,
    }
    size = int(amount * multipliers[unit])
    if size <= 0:
        raise ValueError("size must be greater than zero")
    return size


def format_size(size: int) -> str:
    for unit, multiplier in (
        ("TB", 1000**4),
        ("GB", 1000**3),
        ("MB", 1000**2),
        ("KB", 1000),
    ):
        if size >= multiplier:
            return f"{size / multiplier:.2f}{unit}"
    return f"{size}B"


def directory_bytes(path: Path) -> int:
    total = 0
    for root, dirs, files in os.walk(path, followlinks=False):
        dirs[:] = [
            name
            for name in dirs
            if not (Path(root) / name).is_symlink()
        ]
        for name in files:
            try:
                total += (
                    Path(root) / name
                ).stat(follow_symlinks=False).st_size
            except FileNotFoundError:
                pass
    return total


def percent(used: int, limit: int) -> float:
    return (used * 100.0 / limit) if limit > 0 else 0.0


def parse_summary_time(value: str) -> tuple[int, int, str]:
    value = value.strip()
    match = re.fullmatch(r"([01]\d|2[0-3]):([0-5]\d)", value)
    if not match:
        raise ValueError(
            f"invalid summary time {value!r}; use zero-padded 24-hour HH:MM"
        )
    hour = int(match.group(1))
    minute = int(match.group(2))
    return hour, minute, f"{hour:02d}:{minute:02d}"


def parse_summary_times(
    config: dict[str, str | list[str]],
) -> tuple[list[str], bool]:
    explicit = config.get("DAILY_SUMMARY_TIMES")
    legacy_used = False

    if explicit not in (None, "", []):
        if isinstance(explicit, list):
            raw_times = explicit
        else:
            raw_times = [
                item.strip()
                for item in str(explicit).split(",")
                if item.strip()
            ]
        if "DAILY_SUMMARY_HOUR" in config:
            print(
                "WARNING: DAILY_SUMMARY_HOUR is ignored because "
                "DAILY_SUMMARY_TIMES is configured.",
                file=sys.stderr,
            )
    else:
        legacy_used = True
        hour = int(config_scalar(config, "DAILY_SUMMARY_HOUR", "8"))
        minute = int(config_scalar(config, "DAILY_SUMMARY_MINUTE", "0"))
        if not 0 <= hour <= 23:
            raise ValueError("DAILY_SUMMARY_HOUR must be from 0 through 23")
        if not 0 <= minute <= 59:
            raise ValueError("DAILY_SUMMARY_MINUTE must be from 0 through 59")
        raw_times = [f"{hour:02d}:{minute:02d}"]

    canonical = {
        parse_summary_time(str(value))[2]
        for value in raw_times
    }
    if not canonical:
        raise ValueError("DAILY_SUMMARY_TIMES must contain at least one time")
    return sorted(canonical), legacy_used


def send_mail(
    recipient: str,
    subject: str,
    body: str,
    backend: str,
    command: str,
    timeout_seconds: float,
) -> None:
    try:
        if backend == "mail":
            subprocess.run(
                [command, "-s", subject, recipient],
                input=body + "\n",
                text=True,
                check=True,
                timeout=timeout_seconds,
            )
            return
        if backend == "sendmail":
            message = (
                f"To: {recipient}\n"
                f"Subject: {subject}\n"
                "\n"
                f"{body}\n"
            )
            subprocess.run(
                [command, "-t"],
                input=message,
                text=True,
                check=True,
                timeout=timeout_seconds,
            )
            return
    except subprocess.TimeoutExpired as error:
        raise MailDeliveryError(
            f"mail command timed out after {timeout_seconds:g} seconds"
        ) from error
    except subprocess.CalledProcessError as error:
        raise MailDeliveryError(
            f"mail command exited with status {error.returncode}"
        ) from error
    except OSError as error:
        raise MailDeliveryError(
            f"could not execute mail command {command}: {error}"
        ) from error

    raise ValueError("MAIL_BACKEND must be 'mail' or 'sendmail'")


def load_domains(status_url: str) -> list[str]:
    request = urllib.request.Request(
        status_url,
        headers={"Accept": "application/json"},
    )
    with urllib.request.urlopen(request, timeout=10) as response:
        data = json.load(response)
    domains = data.get("connected_instances", {}).get("domains", [])
    if not isinstance(domains, list):
        raise ValueError("status response has no connected domain list")
    return sorted(
        {
            str(domain).strip().lower()
            for domain in domains
            if str(domain).strip()
        }
    )


def format_changes(
    label: str,
    domains: list[str],
    limit: int = 100,
) -> str:
    if not domains:
        return f"{label}: none"
    visible = domains[:limit]
    lines = [
        f"{label} ({len(domains)}):",
        *(f"  {domain}" for domain in visible),
    ]
    if len(domains) > limit:
        lines.append(f"  ... and {len(domains) - limit} more")
    return "\n".join(lines)


def atomic_write_text(path: Path, text: str) -> None:
    temporary = path.with_name(path.name + ".tmp")
    temporary.write_text(text, encoding="utf-8")
    os.replace(temporary, path)


def default_summary_state() -> dict[str, Any]:
    return {
        "version": SUMMARY_STATE_VERSION,
        "days": {},
        "manual_summaries": [],
    }


def load_summary_state(path: Path) -> dict[str, Any]:
    if not path.exists():
        return default_summary_state()
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        raise ValueError("summary state must be a JSON object")
    if data.get("version") != SUMMARY_STATE_VERSION:
        raise ValueError(
            f"unsupported summary state version {data.get('version')!r}"
        )
    if not isinstance(data.get("days"), dict):
        raise ValueError("summary state days must be an object")
    if not isinstance(data.get("manual_summaries", []), list):
        raise ValueError("summary state manual_summaries must be a list")
    data.setdefault("manual_summaries", [])
    return data


def save_summary_state(path: Path, state: dict[str, Any]) -> None:
    atomic_write_text(
        path,
        json.dumps(state, indent=2, sort_keys=True) + "\n",
    )


def day_record(
    state: dict[str, Any],
    local_date: str,
) -> dict[str, Any]:
    days = state.setdefault("days", {})
    record = days.setdefault(
        local_date,
        {
            "sent_slots": {},
            "skipped_slots": {},
        },
    )
    record.setdefault("sent_slots", {})
    record.setdefault("skipped_slots", {})
    return record


def prune_summary_state(state: dict[str, Any]) -> None:
    days = state.setdefault("days", {})
    for date_key in sorted(days)[:-SUMMARY_STATE_RETENTION_DAYS]:
        del days[date_key]
    manual = state.setdefault("manual_summaries", [])
    if len(manual) > 100:
        del manual[:-100]


def migrate_legacy_summary_date(
    state_dir: Path,
    state: dict[str, Any],
    summary_times: list[str],
    legacy_used: bool,
    now: datetime,
) -> bool:
    legacy_path = state_dir / "summary-date"
    if not legacy_path.exists():
        return False

    legacy_date = legacy_path.read_text(encoding="utf-8").strip()
    if not legacy_date:
        legacy_path.rename(state_dir / "summary-date.migrated-empty")
        return True

    record = day_record(state, legacy_date)
    migration = record.setdefault(
        "legacy_summary_date",
        {
            "migrated_at": now.isoformat(),
            "source": str(legacy_path),
        },
    )

    if legacy_used and legacy_date == now.date().isoformat():
        slot = summary_times[0]
        record["sent_slots"].setdefault(
            slot,
            {
                "processed_at": now.isoformat(),
                "source": "legacy-summary-date",
            },
        )
        migration["mapped_to_slot"] = slot
    else:
        migration["mapped_to_slot"] = None

    destination = state_dir / "summary-date.migrated"
    if destination.exists():
        destination = state_dir / (
            "summary-date.migrated." + now.strftime("%Y%m%d%H%M%S")
        )
    legacy_path.rename(destination)
    migration["archived_as"] = str(destination)
    return True


def due_slot(
    summary_times: list[str],
    now: datetime,
    record: dict[str, Any],
) -> tuple[str | None, list[str]]:
    current_minutes = now.hour * 60 + now.minute
    sent = record.get("sent_slots", {})
    skipped = record.get("skipped_slots", {})
    due = []

    for slot in summary_times:
        hour, minute, canonical = parse_summary_time(slot)
        if hour * 60 + minute <= current_minutes:
            if canonical not in sent and canonical not in skipped:
                due.append(canonical)

    if not due:
        return None, []
    return due[-1], due[:-1]


def storage_findings(
    config: dict[str, str | list[str]],
) -> tuple[list[tuple[str, float, str]], str, str]:
    warning = float(config_scalar(config, "RESOURCE_WARNING_PERCENT", "75"))
    critical = float(
        config_scalar(config, "RESOURCE_CRITICAL_PERCENT", "100")
    )
    if not 0 < warning < critical <= 100:
        raise ValueError(
            "resource thresholds must satisfy "
            "0 < warning < critical <= 100"
        )

    findings: list[tuple[str, float, str]] = []
    for label in ("STORAGE", "CACHE"):
        path_value = config_scalar(config, f"{label}_DIR", "").strip()
        limit_value = config_scalar(config, f"{label}_LIMIT", "0").strip()
        try:
            limit = parse_size(limit_value)
        except ValueError as error:
            findings.append(
                (
                    "critical",
                    100.0,
                    f"{label}: invalid limit: {error}",
                )
            )
            continue
        if not path_value:
            findings.append(
                (
                    "critical",
                    100.0,
                    f"{label}: path or positive byte limit is not configured",
                )
            )
            continue

        path = Path(path_value)
        if not path.is_dir():
            findings.append(
                (
                    "critical",
                    100.0,
                    f"{label}: {path} is missing or not mounted",
                )
            )
            continue

        if configured_bool(
            config_scalar(config, f"{label}_REQUIRE_MOUNT", "false")
        ):
            mount_path = Path(
                config_scalar(
                    config,
                    f"{label}_MOUNT_POINT",
                    path_value,
                ).strip()
            )
            if not mount_path.is_mount():
                findings.append(
                    (
                        "critical",
                        100.0,
                        f"{label}: {mount_path} is not a mount point",
                    )
                )
                continue

        used = directory_bytes(path)
        budget_pct = percent(used, limit)
        disk = shutil.disk_usage(path)
        filesystem_pct = percent(disk.used, disk.total)
        effective = max(budget_pct, filesystem_pct)
        state = (
            "critical"
            if effective >= critical
            else "warning"
            if effective >= warning
            else "ok"
        )
        detail = (
            f"{label}: {path}; directory {format_size(used)} / "
            f"{format_size(limit)} ({budget_pct:.1f}% of cap); "
            f"filesystem {filesystem_pct:.1f}% used, "
            f"{format_size(disk.free)} free"
        )
        findings.append((state, effective, detail))

    rank = {"ok": 0, "warning": 1, "critical": 2}
    overall = max((item[0] for item in findings), key=rank.get)
    report = "\n".join(item[2] for item in findings)
    return findings, overall, report


def server_report(
    config: dict[str, str | list[str]],
    domains_file: Path,
) -> tuple[list[str] | None, str]:
    previous_domains = (
        json.loads(domains_file.read_text(encoding="utf-8"))
        if domains_file.exists()
        else None
    )
    status_url = config_scalar(
        config,
        "SUMMARY_STATUS_URL",
        "http://127.0.0.1:8080/status.json",
    ).strip()
    try:
        domains = load_domains(status_url)
        if previous_domains is None:
            changes = "Changed servers: no previous summary baseline"
        else:
            added = sorted(set(domains) - set(previous_domains))
            removed = sorted(set(previous_domains) - set(domains))
            changes = "\n".join(
                (
                    format_changes("Added", added),
                    format_changes("Removed", removed),
                )
            )
        return domains, (
            f"Connected servers: {len(domains)}\n"
            f"{changes}"
        )
    except Exception as error:
        return None, f"Connected servers: unavailable ({error})"


def skipped_report(
    skipped_slots: list[str],
    state_dir: Path,
) -> str:
    if not skipped_slots:
        return ""

    first = skipped_slots[0]
    lines = [
        "",
        "Skipped earlier scheduled report slots: "
        + ", ".join(skipped_slots),
        (
            "Only the most recent due slot is sent after downtime, so a burst "
            "of stale reports is avoided."
        ),
        (
            "No historical snapshot exists for skipped slots. Their records "
            f"are stored in {state_dir / 'summary-slots.json'}."
        ),
        "Inspect them with:",
        "  activity-relay-resource-guard --show-summary-state",
        "Send a current unscheduled summary with:",
        "  activity-relay-resource-guard --send-summary-now",
        (
            "To make a skipped slot eligible again today, run:"
        ),
        (
            "  activity-relay-resource-guard "
            f"--reset-summary-slot {first} --force"
        ),
        "  systemctl start activity-relay-resource-guard.service",
        (
            "A reset resend reflects current state; it cannot recreate the "
            "historical state at the skipped time."
        ),
    ]
    return "\n".join(lines)


def build_summary(
    host: str,
    now: datetime,
    server_status: str,
    resource_report: str,
    *,
    scheduled_slot: str | None,
    skipped_slots: list[str],
    state_dir: Path,
    manual: bool,
) -> str:
    generated = now.strftime("%Y-%m-%d %H:%M:%S %Z (%z)")
    schedule_line = (
        "Trigger: manual summary; scheduled slots were not consumed"
        if manual
        else (
            f"Scheduled slot: {scheduled_slot} server-local time; "
            "processed by the periodic timer"
        )
    )
    return (
        f"Activity-Relay operational summary for {host}\n\n"
        f"Generated: {generated}\n"
        f"{schedule_line}"
        f"{skipped_report(skipped_slots, state_dir)}\n\n"
        f"{server_status}\n\n"
        f"{resource_report}"
    )


def archive_summary(
    state_dir: Path,
    now: datetime,
    label: str,
    summary: str,
) -> str:
    directory = (
        state_dir
        / "summaries"
        / now.date().isoformat()
    )
    directory.mkdir(parents=True, exist_ok=True)
    safe_label = label.replace(":", "-")
    path = directory / f"{safe_label}.txt"
    if path.exists():
        path = directory / (
            f"{safe_label}-{now.strftime('%H%M%S')}.txt"
        )
    atomic_write_text(path, summary + "\n")
    return str(path)


def parse_now(value: str | None) -> datetime:
    if not value:
        return datetime.now().astimezone()
    parsed = datetime.fromisoformat(value)
    if parsed.tzinfo is None:
        parsed = parsed.astimezone()
    return parsed


def confirm_reset(force: bool, description: str) -> None:
    if force:
        return
    if not sys.stdin.isatty():
        raise SystemExit(
            f"{description} requires --force when stdin is not interactive"
        )
    answer = input(f"{description}? Type 'yes' to continue: ")
    if answer.strip().lower() != "yes":
        raise SystemExit("Reset cancelled")


def run_locked(
    args: argparse.Namespace,
    config: dict[str, str | list[str]],
    now: datetime,
) -> int:
    state_path = args.state_dir / "summary-slots.json"
    state = load_summary_state(state_path)

    if args.show_summary_state:
        output = {
            "state_path": str(state_path),
            "archive_root": str(args.state_dir / "summaries"),
            "legacy_summary_date": (
                (args.state_dir / "summary-date").read_text(
                    encoding="utf-8"
                ).strip()
                if (args.state_dir / "summary-date").exists()
                else None
            ),
            "state": state,
        }
        print(json.dumps(output, indent=2, sort_keys=True))
        return 0

    local_date = now.date().isoformat()
    if args.reset_summary_slot:
        _, _, slot = parse_summary_time(args.reset_summary_slot)
        confirm_reset(
            args.force,
            f"Reset summary slot {slot} for {local_date}",
        )
        record = day_record(state, local_date)
        removed = False
        for key in ("sent_slots", "skipped_slots"):
            if slot in record[key]:
                del record[key][slot]
                removed = True
        prune_summary_state(state)
        save_summary_state(state_path, state)
        print(
            f"Summary slot {slot} for {local_date} "
            + ("was reset." if removed else "had no recorded state.")
        )
        return 0

    if args.reset_summary_state:
        confirm_reset(
            args.force,
            f"Reset all summary slots for {local_date}",
        )
        state.setdefault("days", {}).pop(local_date, None)
        prune_summary_state(state)
        save_summary_state(state_path, state)
        print(f"Summary slot state for {local_date} was reset.")
        return 0

    mail_backend = config_scalar(
        config,
        "MAIL_BACKEND",
        "mail",
    ).strip().lower()
    default_mail_command = (
        "/usr/bin/mail"
        if mail_backend == "mail"
        else "/usr/sbin/sendmail"
    )
    mail_command = (
        args.mailer_command
        or config_scalar(
            config,
            "MAIL_COMMAND",
            default_mail_command,
        ).strip()
    )
    if mail_backend not in {"mail", "sendmail"}:
        raise SystemExit("MAIL_BACKEND must be 'mail' or 'sendmail'")
    mail_timeout = float(
        config_scalar(config, "MAIL_TIMEOUT_SECONDS", "60")
    )
    if not 1 <= mail_timeout <= 3600:
        raise SystemExit(
            "MAIL_TIMEOUT_SECONDS must be from 1 through 3600"
        )

    try:
        _, overall, resource_report = storage_findings(config)
    except ValueError as error:
        raise SystemExit(str(error)) from error

    rank = {"ok": 0, "warning": 1, "critical": 2}
    host = socket.getfqdn()
    print(
        f"Activity-Relay resource guard on {host}: {overall}\n"
        f"{resource_report}"
    )

    is_summary_command = (
        args.send_summary_now or args.preview_summary
    )

    if not is_summary_command:
        state_file = args.state_dir / "state"
        previous = (
            state_file.read_text(encoding="utf-8").strip()
            if state_file.exists()
            else "unknown"
        )
        if overall != previous:
            recipient = (
                config_scalar(config, "ADMIN_EMAIL", "root").strip()
                or "root"
            )
            if not args.no_mail:
                try:
                    send_mail(
                        recipient,
                        f"[{overall.upper()}] "
                        f"Activity-Relay storage on {host}",
                        (
                            f"State changed from {previous} to {overall}."
                            f"\n\n{resource_report}"
                        ),
                        mail_backend,
                        mail_command,
                        mail_timeout,
                    )
                except MailDeliveryError as error:
                    print(f"ERROR: {error}", file=sys.stderr)
                    return 3
            state_file.write_text(
                overall + "\n",
                encoding="utf-8",
            )

    domains_file = args.state_dir / "summary-domains.json"

    if is_summary_command:
        domains, status = server_report(config, domains_file)
        summary = build_summary(
            host,
            now,
            status,
            resource_report,
            scheduled_slot=None,
            skipped_slots=[],
            state_dir=args.state_dir,
            manual=True,
        )
        print(summary)

        if args.preview_summary:
            return rank[overall]

        recipient = (
            config_scalar(config, "ADMIN_EMAIL", "root").strip()
            or "root"
        )
        try:
            send_mail(
                recipient,
                f"[SUMMARY NOW] Activity-Relay on {host} - "
                f"{now.date().isoformat()}",
                summary,
                mail_backend,
                mail_command,
                mail_timeout,
            )
        except MailDeliveryError as error:
            print(f"ERROR: {error}", file=sys.stderr)
            return 3

        if domains is not None:
            atomic_write_text(
                domains_file,
                json.dumps(domains, indent=2) + "\n",
            )
        archive = archive_summary(
            args.state_dir,
            now,
            "manual-" + now.strftime("%H%M%S"),
            summary,
        )
        state.setdefault("manual_summaries", []).append(
            {
                "processed_at": now.isoformat(),
                "archive": archive,
            }
        )
        prune_summary_state(state)
        save_summary_state(state_path, state)
        return rank[overall]

    if not configured_bool(
        config_scalar(config, "DAILY_SUMMARY_EMAIL", "false")
    ):
        return rank[overall]

    try:
        summary_times, legacy_used = parse_summary_times(config)
    except (TypeError, ValueError) as error:
        raise SystemExit(str(error)) from error

    migrated = migrate_legacy_summary_date(
        args.state_dir,
        state,
        summary_times,
        legacy_used,
        now,
    )
    if migrated:
        prune_summary_state(state)
        save_summary_state(state_path, state)
    record = day_record(state, local_date)
    selected_slot, skipped_slots = due_slot(
        summary_times,
        now,
        record,
    )
    if selected_slot is None:
        return rank[overall]

    domains, status = server_report(config, domains_file)
    summary = build_summary(
        host,
        now,
        status,
        resource_report,
        scheduled_slot=selected_slot,
        skipped_slots=skipped_slots,
        state_dir=args.state_dir,
        manual=False,
    )
    print(summary)

    if args.no_mail:
        print(
            "Summary preview only: --no-mail did not record or consume "
            f"scheduled slot {selected_slot}."
        )
        return rank[overall]

    recipient = (
        config_scalar(config, "ADMIN_EMAIL", "root").strip()
        or "root"
    )
    try:
        send_mail(
            recipient,
            f"[SUMMARY {selected_slot}] Activity-Relay on {host} - "
            f"{local_date}",
            summary,
            mail_backend,
            mail_command,
            mail_timeout,
        )
    except MailDeliveryError as error:
        print(f"ERROR: {error}", file=sys.stderr)
        return 3

    processed_at = now.isoformat()
    for skipped in skipped_slots:
        record["skipped_slots"][skipped] = {
            "skipped_at": processed_at,
            "caught_up_by": selected_slot,
            "reason": "a later scheduled slot was processed first",
        }
    archive = archive_summary(
        args.state_dir,
        now,
        selected_slot,
        summary,
    )
    record["sent_slots"][selected_slot] = {
        "processed_at": processed_at,
        "archive": archive,
    }
    if domains is not None:
        atomic_write_text(
            domains_file,
            json.dumps(domains, indent=2) + "\n",
        )
    prune_summary_state(state)
    save_summary_state(state_path, state)
    return rank[overall]


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    default_config = os.environ.get("ACTIVITY_RELAY_CONFIG")
    if not default_config:
        if Path("/etc/activity-relay/config.yml").exists():
            default_config = "/etc/activity-relay/config.yml"
        elif Path("/var/lib/relay/config.yml").exists():
            default_config = "/var/lib/relay/config.yml"
        else:
            default_config = "config.yml"

    parser.add_argument("--config", type=Path, default=Path(default_config))
    parser.add_argument(
        "--state-dir",
        type=Path,
        default=Path(
            os.environ.get(
                "ACTIVITY_RELAY_GUARD_STATE_DIR",
                "/var/lib/activity-relay-guard",
            )
        ),
    )
    parser.add_argument("--mailer-command", help="Override MAIL_COMMAND")
    parser.add_argument("--no-mail", action="store_true")
    parser.add_argument("--send-summary-now", action="store_true")
    parser.add_argument("--preview-summary", action="store_true")
    parser.add_argument("--show-summary-state", action="store_true")
    parser.add_argument("--reset-summary-slot", metavar="HH:MM")
    parser.add_argument("--reset-summary-state", action="store_true")
    parser.add_argument("--force", action="store_true")
    parser.add_argument(
        "--now",
        help=argparse.SUPPRESS,
    )
    args = parser.parse_args(argv)

    summary_actions = sum(
        bool(value)
        for value in (
            args.send_summary_now,
            args.preview_summary,
            args.show_summary_state,
            args.reset_summary_slot,
            args.reset_summary_state,
        )
    )
    if summary_actions > 1:
        parser.error("choose only one summary administration action")
    if args.send_summary_now and args.no_mail:
        parser.error(
            "--send-summary-now cannot be combined with --no-mail; "
            "use --preview-summary"
        )

    config = load_simple_yaml(args.config)
    now = parse_now(args.now)
    args.state_dir.mkdir(parents=True, exist_ok=True)
    lock_path = args.state_dir / "lock"
    with lock_path.open("w", encoding="utf-8") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            print(
                "Another Activity-Relay resource guard is already running."
            )
            return 0
        return run_locked(args, config, now)


if __name__ == "__main__":
    raise SystemExit(main())
