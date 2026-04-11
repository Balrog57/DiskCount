from __future__ import annotations

import httpx

from diskcount.domain import Deal
from diskcount.sources.feed import parse_feed


class DealabsRssSource:
    name = "dealabs"

    def __init__(self, urls: list[str]) -> None:
        self.urls = urls

    async def fetch(self, client: httpx.AsyncClient) -> list[Deal]:
        deals: list[Deal] = []
        for url in self.urls:
            response = await client.get(url)
            response.raise_for_status()
            deals.extend(parse_dealabs_feed(response.text))
        return deals


def parse_dealabs_feed(feed_text: str) -> list[Deal]:
    return parse_feed(feed_text, DealabsRssSource.name, default_condition="new")
