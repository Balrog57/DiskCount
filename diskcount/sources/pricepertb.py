from __future__ import annotations

from decimal import Decimal

import httpx
from bs4 import BeautifulSoup

from diskcount.domain import Deal
from diskcount.parsing import (
    extract_asin,
    normalize_condition,
    normalize_drive_category,
    normalize_interfaces,
    normalize_media_type,
    parse_capacity_tb,
    parse_price_eur,
)


class PricePerTBSource:
    name = "pricepertb"

    def __init__(self, urls: list[str]) -> None:
        self.urls = urls

    async def fetch(self, client: httpx.AsyncClient) -> list[Deal]:
        deals: list[Deal] = []
        for url in self.urls:
            response = await client.get(url)
            response.raise_for_status()
            deals.extend(parse_pricepertb_html(response.text))
        return deals


def _cell_text(cell) -> str:
    return cell.get_text(" ", strip=True)


def parse_pricepertb_html(html: str) -> list[Deal]:
    soup = BeautifulSoup(html, "html.parser")
    deals: list[Deal] = []

    for row in soup.select("tr"):
        cells = row.find_all(["td", "th"])
        if len(cells) < 8:
            continue
        texts = [_cell_text(cell) for cell in cells]
        if any("Price" in text or "Prix" in text for text in texts[:2]):
            continue

        if len(cells) >= 9:
            price_per_tb_text = texts[1]
            price_text = texts[2]
            capacity_text = texts[3]
            warranty_text = texts[4]
            form_factor_text = texts[5]
            technology_text = texts[6]
            condition_text = texts[7]
            link_cell = cells[8]
        else:
            price_per_tb_text = texts[0]
            price_text = texts[1]
            capacity_text = texts[2]
            warranty_text = texts[3]
            form_factor_text = texts[4]
            technology_text = texts[5]
            condition_text = texts[6]
            link_cell = cells[7]

        price_per_tb = parse_price_eur(price_per_tb_text)
        price_eur = parse_price_eur(price_text)
        capacity_tb = parse_capacity_tb(capacity_text)
        if price_per_tb is None or price_eur is None or capacity_tb is None or capacity_tb <= 0:
            continue

        media_type = normalize_media_type(" ".join([technology_text, form_factor_text]))
        if media_type not in ("rotational", "solid_state"):
            continue

        link = link_cell.find("a", href=True)
        if not link:
            continue
        url = str(link["href"])
        title = link.get_text(" ", strip=True)
        category_text = " ".join([form_factor_text, technology_text, title])
        condition = normalize_condition(condition_text)
        drive_category = normalize_drive_category(category_text, media_type)
        interfaces = normalize_interfaces(category_text)

        deals.append(
            Deal(
                source=PricePerTBSource.name,
                external_id=extract_asin(url),
                title=title,
                url=url,
                price_eur=price_eur.quantize(Decimal("0.01")),
                price_per_tb=price_per_tb.quantize(Decimal("0.01")),
                capacity_tb=capacity_tb,
                condition=condition,
                media_type=media_type,
                form_factor=form_factor_text or None,
                technology=technology_text or None,
                drive_category=drive_category,
                interfaces=interfaces,
                raw={"warranty": warranty_text},
            )
        )

    return deals
