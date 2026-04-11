from __future__ import annotations

import re
import unicodedata
from decimal import Decimal, InvalidOperation
from urllib.parse import urlsplit

from .domain import Condition, MediaType

CAPACITY_RE = re.compile(r"(?P<value>\d+(?:[,.]\d+)?)\s*(?P<unit>t[bo]|g[bo]|tb|gb)\b", re.IGNORECASE)
EURO_RE = re.compile(
    r"(?:(?:€|eur)\s*(?P<prefix>\d[\d\s\u00a0.]*(?:[,.]\d{1,3})?)|"
    r"(?P<suffix>\d[\d\s\u00a0.]*(?:[,.]\d{1,3})?)\s*(?:€|eur))",
    re.IGNORECASE,
)
ASIN_RE = re.compile(r"(?:/dp/|/gp/product/|/product/)(?P<asin>[A-Z0-9]{10})(?:[/?#]|$)", re.IGNORECASE)


def ascii_fold(value: str | None) -> str:
    if not value:
        return ""
    normalized = unicodedata.normalize("NFKD", value)
    return normalized.encode("ascii", "ignore").decode("ascii").lower()


def decimal_from_text(value: str | None) -> Decimal | None:
    if value is None:
        return None
    cleaned = value.strip().replace("\u00a0", " ").replace(" ", "")
    if not cleaned:
        return None
    if "," in cleaned and "." in cleaned:
        cleaned = cleaned.replace(".", "").replace(",", ".")
    else:
        cleaned = cleaned.replace(",", ".")
    cleaned = re.sub(r"[^0-9.]", "", cleaned)
    if not cleaned:
        return None
    try:
        return Decimal(cleaned)
    except InvalidOperation:
        return None


def parse_price_eur(text: str | None) -> Decimal | None:
    if not text:
        return None
    match = EURO_RE.search(text)
    if match:
        return decimal_from_text(match.group("prefix") or match.group("suffix"))
    return decimal_from_text(text)


def parse_capacity_tb(text: str | None) -> Decimal | None:
    if not text:
        return None
    match = CAPACITY_RE.search(text)
    if not match:
        return None
    value = decimal_from_text(match.group("value"))
    if value is None:
        return None
    unit = ascii_fold(match.group("unit"))
    if unit.startswith("g"):
        return value / Decimal("1000")
    return value


def normalize_condition(text: str | None) -> Condition | None:
    folded = ascii_fold(text)
    if any(word in folded for word in ("used", "occasion", "reconditionne", "refurbished")):
        return "used"
    if any(word in folded for word in ("new", "neuf", "neuve")):
        return "new"
    return None


def normalize_media_type(text: str | None) -> MediaType | None:
    folded = ascii_fold(text)
    if any(word in folded for word in ("ssd", "nvme", "solid state")):
        return "solid_state"
    if any(word in folded for word in ("hdd", "disque dur", "hard drive", "7200rpm", "5400rpm", "3.5", "2.5")):
        return "rotational"
    return None


def extract_asin(url: str | None) -> str | None:
    if not url:
        return None
    match = ASIN_RE.search(urlsplit(url).path)
    if match:
        return match.group("asin").upper()
    return None
