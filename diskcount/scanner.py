from __future__ import annotations

import asyncio
import logging
from dataclasses import dataclass, field

import httpx

from .config import Settings
from .db import Repository
from .domain import Deal, utc_now
from .notifier import Notifier
from .rules import alert_matches, should_notify
from .sources import Source

logger = logging.getLogger(__name__)


@dataclass
class ScanReport:
    fetched: int = 0
    matched: int = 0
    notified: int = 0
    dry_run_notifications: int = 0
    errors: list[str] = field(default_factory=list)


class Scanner:
    def __init__(
        self,
        settings: Settings,
        repository: Repository,
        sources: list[Source],
        notifier: Notifier | None = None,
    ) -> None:
        self.settings = settings
        self.repository = repository
        self.sources = sources
        self.notifier = notifier

    async def fetch_deals(self) -> tuple[list[Deal], list[str]]:
        deals: list[Deal] = []
        errors: list[str] = []
        headers = {"User-Agent": self.settings.user_agent}
        timeout = httpx.Timeout(self.settings.request_timeout_seconds)
        async with httpx.AsyncClient(headers=headers, timeout=timeout, follow_redirects=True) as client:
            for source in self.sources:
                try:
                    source_deals = await source.fetch(client)
                except Exception as exc:  # noqa: BLE001
                    message = f"{source.name}: {exc}"
                    logger.exception("Source fetch failed: %s", message)
                    errors.append(message)
                    continue
                logger.info("Fetched %s deals from %s", len(source_deals), source.name)
                deals.extend(source_deals)
        return deals, errors

    async def run_once(
        self,
        dry_run: bool = False,
        target_chat_id: int | None = None,
        target_owner_user_id: int | None = None,
    ) -> ScanReport:
        now = utc_now()
        deals, errors = await self.fetch_deals()
        report = ScanReport(fetched=len(deals), errors=errors)
        alerts = self.repository.list_alerts(
            chat_id=target_chat_id,
            owner_user_id=target_owner_user_id,
            only_enabled=True,
        )

        for deal in deals:
            baseline = self.repository.baseline_price_per_tb(deal.product_id, before=now)
            if not dry_run:
                self.repository.upsert_product(deal)
            for alert in alerts:
                if not alert_matches(alert, deal):
                    continue
                report.matched += 1
                last = self.repository.last_notification(alert.id, deal.product_id)
                decision = should_notify(
                    alert=alert,
                    deal=deal,
                    baseline_price_per_tb=baseline,
                    last_notification=last,
                    now=now,
                    significant_drop_pct=self.settings.notification_price_drop_pct,
                )
                if not decision.should_notify:
                    continue
                if dry_run:
                    report.dry_run_notifications += 1
                    continue
                if self.notifier is None:
                    continue
                await self.notifier.send_deal(alert.chat_id, alert, deal, decision)
                if self.settings.telegram_message_delay_seconds > 0:
                    await asyncio.sleep(self.settings.telegram_message_delay_seconds)
                self.repository.record_notification(alert, deal, decision.reason, decision.discount_pct, sent_at=now)
                report.notified += 1
            if not dry_run:
                self.repository.record_observation(deal, observed_at=now)

        return report


async def scheduler_loop(scanner: Scanner, interval_seconds: int) -> None:
    while True:
        report = await scanner.run_once()
        logger.info(
            "Scan completed: fetched=%s matched=%s notified=%s dry_run=%s errors=%s",
            report.fetched,
            report.matched,
            report.notified,
            report.dry_run_notifications,
            len(report.errors),
        )
        await asyncio.sleep(interval_seconds)
