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


class DiskPricesSource:
    name = "diskprices"

    def __init__(self, url: str) -> None:
        self.url = url

    async def fetch(self, client: httpx.AsyncClient) -> list[Deal]:
        response = await client.get(self.url)
        response.raise_for_status()
        return parse_diskprices_html(response.text)


def _cell_text(cell) -> str:
    return cell.get_text(" ", strip=True)


def parse_diskprices_html(html: str) -> list[Deal]:
    soup = BeautifulSoup(html, "html.parser")
    deals: list[Deal] = []

    for row in soup.select("tr"):
        cells = row.find_all(["td", "th"])
        if len(cells) < 9:
            continue
        texts = [_cell_text(cell) for cell in cells]
        if "Price" in texts[0] or "Prix" in texts[0]:
            continue

        price_per_tb = parse_price_eur(texts[1])
        price_eur = parse_price_eur(texts[2])
        capacity_tb = parse_capacity_tb(texts[3])
        if price_per_tb is None or price_eur is None or capacity_tb is None:
            continue

        technology = texts[6] or None
        condition = normalize_condition(texts[7])
        media_type = normalize_media_type(technology) or normalize_media_type(" ".join(texts))
        if media_type not in ("rotational", "solid_state"):
            continue

        link = cells[-1].find("a", href=True)
        if not link:
            continue
        url = link["href"]
        title = link.get_text(" ", strip=True) or texts[-1]
        category_text = " ".join([texts[5], technology or "", title])
        drive_category = normalize_drive_category(category_text, media_type)
        interfaces = normalize_interfaces(category_text)

        deals.append(
            Deal(
                source=DiskPricesSource.name,
                external_id=extract_asin(url),
                title=title,
                url=url,
                price_eur=price_eur.quantize(Decimal("0.01")),
                price_per_tb=price_per_tb.quantize(Decimal("0.01")),
                capacity_tb=capacity_tb,
                condition=condition,
                media_type=media_type,
                form_factor=texts[5] or None,
                technology=technology,
                drive_category=drive_category,
                interfaces=interfaces,
                raw={
                    "price_per_gb": texts[0],
                    "warranty": texts[4],
                    "drive_category": drive_category,
                    "interfaces": list(interfaces),
                },
            )
        )

    return deals
