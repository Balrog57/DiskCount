from __future__ import annotations

from decimal import Decimal

import feedparser
import httpx

from diskcount.domain import Condition, Deal
from diskcount.parsing import normalize_condition, normalize_media_type, parse_capacity_tb, parse_price_eur


class FeedSource:
    def __init__(self, name: str, urls: list[str], default_condition: Condition | None = None) -> None:
        self.name = name
        self.urls = urls
        self.default_condition = default_condition

    async def fetch(self, client: httpx.AsyncClient) -> list[Deal]:
        deals: list[Deal] = []
        for url in self.urls:
            response = await client.get(url)
            response.raise_for_status()
            deals.extend(parse_feed(response.text, self.name, self.default_condition))
        return deals


def parse_feed(feed_text: str, source_name: str, default_condition: Condition | None = None) -> list[Deal]:
    parsed = feedparser.parse(feed_text)
    deals: list[Deal] = []
    for entry in parsed.entries:
        title = getattr(entry, "title", "")
        link = getattr(entry, "link", "")
        summary = getattr(entry, "summary", "")
        haystack = f"{title} {summary}"

        price_eur = parse_price_eur(haystack)
        capacity_tb = parse_capacity_tb(haystack)
        if price_eur is None or capacity_tb is None or capacity_tb <= 0:
            continue

        media_type = normalize_media_type(haystack)
        if media_type is None:
            continue

        condition = normalize_condition(haystack) or default_condition
        price_per_tb = (price_eur / capacity_tb).quantize(Decimal("0.01"))
        external_id = getattr(entry, "id", None) or getattr(entry, "guid", None) or link

        deals.append(
            Deal(
                source=source_name,
                external_id=str(external_id),
                title=title,
                url=link,
                price_eur=price_eur.quantize(Decimal("0.01")),
                price_per_tb=price_per_tb,
                capacity_tb=capacity_tb,
                condition=condition,
                media_type=media_type,
                raw={"summary": summary},
            )
        )
    return deals
