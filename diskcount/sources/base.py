from __future__ import annotations

from typing import Protocol

import httpx

from diskcount.domain import Deal


class Source(Protocol):
    name: str

    async def fetch(self, client: httpx.AsyncClient) -> list[Deal]:
        raise NotImplementedError
