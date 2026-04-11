from __future__ import annotations

import argparse
import asyncio
import logging
from decimal import Decimal

from .app import run_bot
from .config import Settings
from .domain import Deal
from .db import Repository, create_db_engine
from .scanner import Scanner
from .sources import build_sources


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="diskcount")
    subparsers = parser.add_subparsers(dest="command", required=True)

    subparsers.add_parser("init-db", help="Create database tables.")

    scan_parser = subparsers.add_parser("scan", help="Run one scan.")
    scan_parser.add_argument("--dry-run", action="store_true", help="Do not send notifications or persist observations.")

    check_parser = subparsers.add_parser("check", help="Alias for scan --dry-run.")
    check_parser.add_argument("--persist", action="store_true", help="Persist observations like a normal scan.")

    list_parser = subparsers.add_parser("list", help="List current best offers from enabled sources.")
    list_parser.add_argument("--limit", type=int, default=20, help="Maximum number of offers to print.")
    list_parser.add_argument("--min-tb", type=Decimal, default=None, help="Minimum capacity in TB.")
    list_parser.add_argument("--max-eur-tb", type=Decimal, default=None, help="Maximum EUR/TB.")
    list_parser.add_argument("--media", choices=["rotational", "solid_state"], default=None, help="Storage media filter.")
    list_parser.add_argument("--condition", choices=["new", "used"], default=None, help="Condition filter.")

    subparsers.add_parser("run", help="Start the Telegram bot and scheduler.")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    settings = Settings()

    if args.command == "run":
        asyncio.run(run_bot(settings))
        return 0

    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
    engine = create_db_engine(settings.database_url)
    repository = Repository(engine)
    repository.init()

    if args.command == "init-db":
        print("Database initialized.")
        return 0

    if args.command in {"scan", "check"}:
        scanner = Scanner(settings, repository, build_sources(settings))
        dry_run = args.dry_run if args.command == "scan" else not args.persist
        report = asyncio.run(scanner.run_once(dry_run=dry_run))
        print(
            f"Scan complete: fetched={report.fetched} matched={report.matched} "
            f"notified={report.notified} dry_run_notifications={report.dry_run_notifications} errors={len(report.errors)}"
        )
        for error in report.errors:
            print(f"ERROR {error}")
        return 1 if report.errors else 0

    if args.command == "list":
        scanner = Scanner(settings, repository, build_sources(settings))
        deals, errors = asyncio.run(scanner.fetch_deals())
        for deal in _filter_deals(deals, args.min_tb, args.max_eur_tb, args.media, args.condition)[: args.limit]:
            print(
                f"{deal.price_per_tb:.2f} EUR/To | {deal.price_eur:.2f} EUR | "
                f"{deal.capacity_tb:g} To | {deal.condition or 'n/a'} | {deal.media_type or 'n/a'} | "
                f"{deal.source} | {deal.title} | {deal.url}"
            )
        for error in errors:
            print(f"ERROR {error}")
        return 1 if errors else 0

    raise AssertionError(f"Unhandled command {args.command}")


def _filter_deals(
    deals: list[Deal],
    min_tb: Decimal | None,
    max_eur_tb: Decimal | None,
    media: str | None,
    condition: str | None,
) -> list[Deal]:
    filtered: list[Deal] = []
    for deal in deals:
        if min_tb is not None and deal.capacity_tb < min_tb:
            continue
        if max_eur_tb is not None and deal.price_per_tb > max_eur_tb:
            continue
        if media is not None and deal.media_type != media:
            continue
        if condition is not None and deal.condition != condition:
            continue
        filtered.append(deal)
    return sorted(filtered, key=lambda deal: (deal.price_per_tb, -deal.capacity_tb))
