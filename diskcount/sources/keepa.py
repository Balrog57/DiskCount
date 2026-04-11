from __future__ import annotations

from decimal import Decimal
from typing import Any

import httpx

from diskcount.domain import Deal
from diskcount.parsing import normalize_media_type, parse_capacity_tb


class KeepaSource:
    name = "keepa"

    def __init__(self, api_key: str, asins: list[str], domain: int) -> None:
        self.api_key = api_key
        self.asins = asins
        self.domain = domain

    async def fetch(self, client: httpx.AsyncClient) -> list[Deal]:
        response = await client.get(
            "https://api.keepa.com/product",
            params={
                "key": self.api_key,
                "domain": self.domain,
                "asin": ",".join(self.asins),
                "stats": 90,
            },
        )
        response.raise_for_status()
        return parse_keepa_response(response.json())


def _first_positive_cent_value(values: Any) -> int | None:
    if not isinstance(values, list):
        return None
    for value in values[:4]:
        if isinstance(value, int) and value > 0:
            return value
    return None


def parse_keepa_response(payload: dict[str, Any]) -> list[Deal]:
    deals: list[Deal] = []
    for product in payload.get("products", []):
        title = str(product.get("title") or "")
        asin = str(product.get("asin") or "")
        stats = product.get("stats") or {}
        cents = _first_positive_cent_value(stats.get("current"))
        capacity_tb = parse_capacity_tb(title)
        media_type = normalize_media_type(title)
        if not title or not asin or cents is None or capacity_tb is None or capacity_tb <= 0 or media_type is None:
            continue

        price_eur = (Decimal(cents) / Decimal("100")).quantize(Decimal("0.01"))
        price_per_tb = (price_eur / capacity_tb).quantize(Decimal("0.01"))
        url = f"https://www.amazon.fr/dp/{asin}"
        deals.append(
            Deal(
                source=KeepaSource.name,
                external_id=asin,
                title=title,
                url=url,
                price_eur=price_eur,
                price_per_tb=price_per_tb,
                capacity_tb=capacity_tb,
                condition="new",
                media_type=media_type,
                raw={"stats": stats},
            )
        )
    return deals
