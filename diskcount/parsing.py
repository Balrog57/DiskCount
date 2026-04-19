from __future__ import annotations

import re
import unicodedata
from decimal import Decimal, InvalidOperation
from urllib.parse import urlsplit

from .domain import Condition, DriveCategory, DriveInterface, MediaType

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


def normalize_drive_category(text: str | None, media_type: MediaType | None = None) -> DriveCategory | None:
    folded = ascii_fold(text).replace('"', "")
    compact = folded.replace(".", "").replace("-", " ")
    is_external = any(word in compact for word in ("external", "externe", "usb"))
    is_internal = any(word in compact for word in ("internal", "interne", "m2", "m 2", "u2", "u 2", "u3", "u 3"))

    if media_type == "solid_state":
        if any(word in compact for word in ("m2 nvme", "m 2 nvme", "nvme")):
            return "m2_nvme"
        if any(word in compact for word in ("m2 sata", "m 2 sata")):
            return "m2_sata"
        if any(word in compact for word in ("u2", "u 2", "u3", "u 3")):
            return "u2_u3"
        if is_external:
            return "external_ssd"
        if is_internal:
            return "internal_ssd"
        return None

    if "hybrid" in compact or "sshd" in compact:
        return "internal_hybrid"
    if "sas" in compact:
        return "internal_sas"
    if is_external and "25" in compact:
        return "external_2_5"
    if is_external:
        return "external_3_5"
    if is_internal and "25" in compact:
        return "internal_2_5"
    if is_internal:
        return "internal_3_5"
    return None


def normalize_interfaces(text: str | None) -> tuple[DriveInterface, ...]:
    folded = ascii_fold(text)
    interfaces: list[DriveInterface] = []
    for value, patterns in (
        ("nvme", ("nvme", "pcie", "pci-e")),
        ("sata", ("sata",)),
        ("sas", ("sas",)),
        ("usb", ("usb",)),
    ):
        if any(pattern in folded for pattern in patterns):
            interfaces.append(value)  # type: ignore[arg-type]
    return tuple(interfaces)


def extract_asin(url: str | None) -> str | None:
    if not url:
        return None
    match = ASIN_RE.search(urlsplit(url).path)
    if match:
        return match.group("asin").upper()
    return None
