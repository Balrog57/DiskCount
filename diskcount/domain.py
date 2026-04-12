from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timezone
from decimal import Decimal
from hashlib import sha256
from typing import Any, Literal
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit

Condition = Literal["new", "used"]
MediaType = Literal["rotational", "solid_state"]
DriveCategory = Literal[
    "external_3_5",
    "external_2_5",
    "internal_3_5",
    "internal_2_5",
    "internal_hybrid",
    "internal_sas",
    "external_ssd",
    "internal_ssd",
    "m2_sata",
    "m2_nvme",
    "u2_u3",
]
DriveInterface = Literal["sata", "sas", "nvme", "usb"]


def utc_now() -> datetime:
    return datetime.now(timezone.utc)


def canonical_url(url: str | None) -> str:
    if not url:
        return ""
    split = urlsplit(url)
    kept_query = [
        (key, value)
        for key, value in parse_qsl(split.query, keep_blank_values=True)
        if not key.lower().startswith(("tag", "utm_", "ascsubtag"))
    ]
    return urlunsplit((split.scheme, split.netloc.lower(), split.path.rstrip("/"), urlencode(kept_query), ""))


@dataclass(frozen=True)
class Deal:
    source: str
    title: str
    url: str
    price_eur: Decimal
    price_per_tb: Decimal
    capacity_tb: Decimal
    condition: Condition | None
    media_type: MediaType | None
    external_id: str | None = None
    form_factor: str | None = None
    technology: str | None = None
    drive_category: DriveCategory | None = None
    interfaces: tuple[DriveInterface, ...] = ()
    observed_at: datetime = field(default_factory=utc_now)
    raw: dict[str, Any] = field(default_factory=dict)

    @property
    def product_id(self) -> str:
        if self.external_id:
            return f"{self.source}:{self.external_id}".lower()
        identity = canonical_url(self.url) or f"{self.title}:{self.capacity_tb}:{self.source}"
        digest = sha256(identity.encode("utf-8")).hexdigest()[:24]
        return f"{self.source}:url:{digest}"


@dataclass(frozen=True)
class NotificationDecision:
    should_notify: bool
    reason: str
    discount_pct: Decimal | None = None
    baseline_price_per_tb: Decimal | None = None
