from __future__ import annotations

import base64
import time
from decimal import Decimal
from typing import Any

import httpx

from diskcount.domain import Deal
from diskcount.parsing import decimal_from_text, normalize_condition, normalize_media_type, parse_capacity_tb


class EbayBrowseSource:
    name = "ebay"

    def __init__(
        self,
        client_id: str,
        client_secret: str,
        search_queries: list[str],
        marketplace_id: str = "EBAY_FR",
        scope: str = "https://api.ebay.com/oauth/api_scope",
        limit: int = 50,
        category_ids: list[str] | None = None,
    ) -> None:
        self.client_id = client_id
        self.client_secret = client_secret
        self.search_queries = search_queries
        self.marketplace_id = marketplace_id
        self.scope = scope
        self.limit = limit
        self.category_ids = category_ids or []
        self._token: str | None = None
        self._token_expires_at = 0.0

    async def fetch(self, client: httpx.AsyncClient) -> list[Deal]:
        token = await self._get_token(client)
        deals: list[Deal] = []
        headers = {
            "Authorization": f"Bearer {token}",
            "X-EBAY-C-MARKETPLACE-ID": self.marketplace_id,
        }
        for query in self.search_queries:
            params: dict[str, str] = {"q": query, "limit": str(self.limit)}
            if self.category_ids:
                params["category_ids"] = ",".join(self.category_ids)
            response = await client.get(
                "https://api.ebay.com/buy/browse/v1/item_summary/search",
                headers=headers,
                params=params,
            )
            response.raise_for_status()
            deals.extend(parse_ebay_search_response(response.json(), source_name=self.name))
        return deals

    async def _get_token(self, client: httpx.AsyncClient) -> str:
        now = time.time()
        if self._token and now < self._token_expires_at - 60:
            return self._token
        credentials = base64.b64encode(f"{self.client_id}:{self.client_secret}".encode("utf-8")).decode("ascii")
        response = await client.post(
            "https://api.ebay.com/identity/v1/oauth2/token",
            headers={
                "Authorization": f"Basic {credentials}",
                "Content-Type": "application/x-www-form-urlencoded",
            },
            data={"grant_type": "client_credentials", "scope": self.scope},
        )
        response.raise_for_status()
        payload = response.json()
        self._token = str(payload["access_token"])
        self._token_expires_at = now + int(payload.get("expires_in", 7200))
        return self._token


def parse_ebay_search_response(payload: dict[str, Any], source_name: str = "ebay") -> list[Deal]:
    deals: list[Deal] = []
    for item in payload.get("itemSummaries", []):
        title = str(item.get("title") or "")
        subtitle = str(item.get("subtitle") or "")
        short_description = str(item.get("shortDescription") or "")
        haystack = f"{title} {subtitle} {short_description}"
        price = item.get("price") or item.get("currentBidPrice") or {}
        currency = str(price.get("currency") or "").upper()
        if currency and currency != "EUR":
            continue
        price_eur = decimal_from_text(str(price.get("value") or ""))
        capacity_tb = parse_capacity_tb(haystack)
        media_type = normalize_media_type(haystack)
        if price_eur is None or capacity_tb is None or capacity_tb <= 0 or media_type is None:
            continue
        condition = normalize_condition(str(item.get("condition") or ""))
        price_per_tb = (price_eur / capacity_tb).quantize(Decimal("0.01"))
        item_id = str(item.get("itemId") or item.get("legacyItemId") or "")
        url = str(item.get("itemWebUrl") or "")
        deals.append(
            Deal(
                source=source_name,
                external_id=item_id or url,
                title=title,
                url=url,
                price_eur=price_eur.quantize(Decimal("0.01")),
                price_per_tb=price_per_tb,
                capacity_tb=capacity_tb,
                condition=condition,
                media_type=media_type,
                raw={"marketplace": item.get("itemLocation"), "seller": item.get("seller")},
            )
        )
    return deals
