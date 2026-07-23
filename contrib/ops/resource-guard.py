#!/usr/bin/env python3
"""Check relay storage budgets and mail only on alert state changes."""

from __future__ import annotations

import argparse
import fcntl
import json
import os
import re
import shutil
import socket
import subprocess
import urllib.request
from datetime import datetime
from pathlib import Path


def load_flat_yaml(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or ":" not in line:
            continue
        key, value = line.split(":", 1)
        value = value.strip()
        if value and value[0:1] in {'"', "'"} and value[-1:] == value[0]:
            value = value[1:-1]
        values[key.strip()] = value
    return values


def directory_bytes(path: Path) -> int:
    total = 0
    for root, dirs, files in os.walk(path, followlinks=False):
        dirs[:] = [name for name in dirs if not (Path(root) / name).is_symlink()]
        for name in files:
            try:
                total += (Path(root) / name).stat(follow_symlinks=False).st_size
            except FileNotFoundError:
                pass
    return total


def percent(used: int, limit: int) -> float:
    return (used * 100.0 / limit) if limit > 0 else 0.0


def configured_bool(value: str) -> bool:
    return value.strip().lower() in {"1", "true", "yes", "on"}


def parse_size(value: str) -> int:
    match = re.fullmatch(r"\s*(\d+(?:\.\d+)?)\s*([kmgtp]?i?b)?\s*", value, re.IGNORECASE)
    if not match:
        raise ValueError(f"invalid size {value!r}; use bytes or units such as MB, GiB, or TB")
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
    for unit, multiplier in (("TB", 1000**4), ("GB", 1000**3), ("MB", 1000**2), ("KB", 1000)):
        if size >= multiplier:
            return f"{size / multiplier:.2f}{unit}"
    return f"{size}B"


def send_mail(recipient: str, subject: str, body: str, backend: str, command: str) -> None:
    if backend == "mail":
        subprocess.run([command, "-s", subject, recipient], input=body + "\n", text=True, check=True)
        return
    if backend == "sendmail":
        message = f"To: {recipient}\nSubject: {subject}\n\n{body}\n"
        subprocess.run([command, "-t"], input=message, text=True, check=True)
        return
    raise ValueError("MAIL_BACKEND must be 'mail' or 'sendmail'")


def load_domains(status_url: str) -> list[str]:
    request = urllib.request.Request(status_url, headers={"Accept": "application/json"})
    with urllib.request.urlopen(request, timeout=10) as response:
        data = json.load(response)
    domains = data.get("connected_instances", {}).get("domains", [])
    if not isinstance(domains, list):
        raise ValueError("status response has no connected domain list")
    return sorted({str(domain).strip().lower() for domain in domains if str(domain).strip()})


def format_changes(label: str, domains: list[str], limit: int = 100) -> str:
    if not domains:
        return f"{label}: none"
    visible = domains[:limit]
    lines = [f"{label} ({len(domains)}):", *(f"  {domain}" for domain in visible)]
    if len(domains) > limit:
        lines.append(f"  ... and {len(domains) - limit} more")
    return "\n".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", type=Path, default=Path("config.yml"))
    parser.add_argument("--state-dir", type=Path, default=Path("/var/lib/activity-relay-guard"))
    parser.add_argument("--mailer-command", help="Override MAIL_COMMAND")
    parser.add_argument("--no-mail", action="store_true")
    args = parser.parse_args()

    config = load_flat_yaml(args.config)
    mail_backend = config.get("MAIL_BACKEND", "mail").strip().lower()
    default_mail_command = "/usr/bin/mail" if mail_backend == "mail" else "/usr/sbin/sendmail"
    mail_command = args.mailer_command or config.get("MAIL_COMMAND", default_mail_command).strip()
    if mail_backend not in {"mail", "sendmail"}:
        raise SystemExit("MAIL_BACKEND must be 'mail' or 'sendmail'")
    warning = float(config.get("RESOURCE_WARNING_PERCENT", "75"))
    critical = float(config.get("RESOURCE_CRITICAL_PERCENT", "100"))
    if not 0 < warning < critical <= 100:
        raise SystemExit("resource thresholds must satisfy 0 < warning < critical <= 100")

    args.state_dir.mkdir(parents=True, exist_ok=True)
    lock = (args.state_dir / "lock").open("w", encoding="utf-8")
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)

    findings: list[tuple[str, float, str]] = []
    for label in ("STORAGE", "CACHE"):
        path_value = config.get(f"{label}_DIR", "").strip()
        limit_value = config.get(f"{label}_LIMIT", "0").strip()
        try:
            limit = parse_size(limit_value)
        except (KeyError, ValueError) as error:
            findings.append(("critical", 100.0, f"{label}: invalid limit: {error}"))
            continue
        if not path_value:
            findings.append(("critical", 100.0, f"{label}: path or positive byte limit is not configured"))
            continue
        path = Path(path_value)
        if not path.is_dir():
            findings.append(("critical", 100.0, f"{label}: {path} is missing or not mounted"))
            continue
        if configured_bool(config.get(f"{label}_REQUIRE_MOUNT", "false")):
            mount_path = Path(config.get(f"{label}_MOUNT_POINT", path_value).strip())
            if not mount_path.is_mount():
                findings.append(("critical", 100.0, f"{label}: {mount_path} is not a mount point"))
                continue
        used = directory_bytes(path)
        budget_pct = percent(used, limit)
        disk = shutil.disk_usage(path)
        filesystem_pct = percent(disk.used, disk.total)
        effective = max(budget_pct, filesystem_pct)
        state = "critical" if effective >= critical else "warning" if effective >= warning else "ok"
        detail = (
            f"{label}: {path}; directory {format_size(used)} / {format_size(limit)} "
            f"({budget_pct:.1f}% of cap); filesystem {filesystem_pct:.1f}% used, "
            f"{format_size(disk.free)} free"
        )
        findings.append((state, effective, detail))

    rank = {"ok": 0, "warning": 1, "critical": 2}
    overall = max((item[0] for item in findings), key=rank.get)
    state_file = args.state_dir / "state"
    previous = state_file.read_text(encoding="utf-8").strip() if state_file.exists() else "unknown"
    report = "\n".join(item[2] for item in findings)
    host = socket.getfqdn()
    print(f"Activity-Relay resource guard on {host}: {overall}\n{report}")

    if overall != previous:
        recipient = config.get("ADMIN_EMAIL", "root").strip() or "root"
        if not args.no_mail:
            send_mail(
                recipient,
                f"[{overall.upper()}] Activity-Relay storage on {host}",
                f"State changed from {previous} to {overall}.\n\n{report}",
                mail_backend,
                mail_command,
            )
        state_file.write_text(overall + "\n", encoding="utf-8")

    if configured_bool(config.get("DAILY_SUMMARY_EMAIL", "false")):
        now = datetime.now().astimezone()
        summary_hour = int(config.get("DAILY_SUMMARY_HOUR", "8"))
        if not 0 <= summary_hour <= 23:
            raise SystemExit("DAILY_SUMMARY_HOUR must be from 0 through 23")
        summary_date_file = args.state_dir / "summary-date"
        last_summary_date = (
            summary_date_file.read_text(encoding="utf-8").strip()
            if summary_date_file.exists()
            else ""
        )
        if now.hour >= summary_hour and last_summary_date != now.date().isoformat():
            domains_file = args.state_dir / "summary-domains.json"
            previous_domains = (
                json.loads(domains_file.read_text(encoding="utf-8"))
                if domains_file.exists()
                else None
            )
            status_url = config.get("SUMMARY_STATUS_URL", "http://127.0.0.1:8080/status.json").strip()
            try:
                domains = load_domains(status_url)
                if previous_domains is None:
                    changes = "Changed servers: no previous summary baseline"
                else:
                    added = sorted(set(domains) - set(previous_domains))
                    removed = sorted(set(previous_domains) - set(domains))
                    changes = "\n".join(
                        (format_changes("Added", added), format_changes("Removed", removed))
                    )
                server_report = f"Connected servers: {len(domains)}\n{changes}"
            except Exception as error:
                domains = None
                server_report = f"Connected servers: unavailable ({error})"

            recipient = config.get("ADMIN_EMAIL", "root").strip() or "root"
            summary = f"Activity-Relay daily summary for {host}\n\n{server_report}\n\n{report}"
            if not args.no_mail:
                send_mail(
                    recipient,
                    f"[SUMMARY] Activity-Relay on {host} - {now.date().isoformat()}",
                    summary,
                    mail_backend,
                    mail_command,
                )
            if domains is not None:
                domains_file.write_text(json.dumps(domains, indent=2) + "\n", encoding="utf-8")
            summary_date_file.write_text(now.date().isoformat() + "\n", encoding="utf-8")
            print(summary)

    return rank[overall]


if __name__ == "__main__":
    raise SystemExit(main())
