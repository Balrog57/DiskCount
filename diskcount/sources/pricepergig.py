from __future__ import annotations

from decimal import Decimal, InvalidOperation
from typing import Any

import httpx

from diskcount.domain import Deal
from diskcount.parsing import (
    extract_asin,
    normalize_condition,
    normalize_drive_category,
    normalize_interfaces,
    normalize_media_type,
)


class PricePerGigSource:
    name = "pricepergig"

    def __init__(
        self,
        api_url: str,
        marketplace: str = "amazon.fr",
        max_results: int = 200,
    ) -> None:
        self.api_url = api_url
        self.marketplace = marketplace
        self.max_results = max_results

    async def fetch(self, client: httpx.AsyncClient) -> list[Deal]:
        deals: list[Deal] = []
        page_size = min(50, max(1, self.max_results))
        for offset in range(0, self.max_results, page_size):
            response = await client.get(
                self.api_url,
                params={
                    "marketplace": f"eq.{self.marketplace}",
                    "technology": "in.(HDD,SSD)",
                    "order": "price_per_tb.asc,capacity_gb.desc",
                    "limit": str(page_size),
                    "offset": str(offset),
                },
            )
            response.raise_for_status()
            payload = response.json()
            page_deals = parse_pricepergig_api(payload)
            deals.extend(page_deals)
            if len(page_deals) < page_size:
                break
        return deals


def _decimal(value: Any) -> Decimal | None:
    if value is None or value == "":
        return None
    try:
        return Decimal(str(value))
    except (InvalidOperation, ValueError):
        return None


def parse_pricepergig_api(payload: Any) -> list[Deal]:
    if not isinstance(payload, list):
        return []

    deals: list[Deal] = []
    for item in payload:
        if not isinstance(item, dict):
            continue

        title = str(item.get("name") or "").strip()
        url = str(item.get("url") or "").strip()
        price_eur = _decimal(item.get("price"))
        price_per_tb = _decimal(item.get("price_per_tb"))
        capacity_gb = _decimal(item.get("capacity_gb"))
        if not title or not url or price_eur is None or price_per_tb is None or capacity_gb is None:
            continue

        currency = str(item.get("currency") or "").strip().upper()
        if currency and currency not in {"EUR", "\u20ac"}:
            continue

        technology = str(item.get("technology") or "")
        interface = str(item.get("interface") or "")
        form_factor = str(item.get("form_factor") or "")
        haystack = " ".join([title, technology, interface, form_factor, str(item.get("tags") or "")])
        media_type = normalize_media_type(haystack)
        if media_type not in ("rotational", "solid_state"):
            continue

        capacity_tb = (capacity_gb / Decimal("1000")).quantize(Decimal("0.001"))
        condition = normalize_condition(str(item.get("condition") or ""))
        drive_category = normalize_drive_category(" ".join([form_factor, technology, title]), media_type)
        interfaces = normalize_interfaces(" ".join([interface, title]))
        external_id = str(item.get("id") or "").strip() or extract_asin(url)

        deals.append(
            Deal(
                source=PricePerGigSource.name,
                external_id=external_id or None,
                title=title,
                url=url,
                price_eur=price_eur.quantize(Decimal("0.01")),
                price_per_tb=price_per_tb.quantize(Decimal("0.01")),
                capacity_tb=capacity_tb,
                condition=condition,
                media_type=media_type,
                form_factor=form_factor or None,
                technology=technology or None,
                drive_category=drive_category,
                interfaces=interfaces,
                raw={
                    "brand": item.get("brand"),
                    "model": item.get("model"),
                    "marketplace": item.get("marketplace"),
                    "seller_name": item.get("seller_name"),
                    "last_updated": item.get("last_updated"),
                    "tags": item.get("tags"),
                    "warranty": item.get("warranty"),
                    "image_url": item.get("image_url"),
                },
            )
        )

    return deals
