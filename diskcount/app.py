from __future__ import annotations

import asyncio
import logging

from aiogram import Bot

from .bot import build_dispatcher, configure_bot_commands
from .config import Settings
from .db import Repository, create_db_engine
from .notifier import TelegramNotifier
from .scanner import Scanner, scheduler_loop
from .sources import build_sources


async def run_bot(settings: Settings) -> None:
    if not settings.telegram_bot_token:
        raise RuntimeError("TELEGRAM_BOT_TOKEN is required for 'run'.")

    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
    engine = create_db_engine(settings.database_url)
    repository = Repository(engine)
    repository.init()

    bot = Bot(settings.telegram_bot_token)
    await configure_bot_commands(bot, settings)
    sources = build_sources(settings)
    scanner = Scanner(settings, repository, sources, notifier=TelegramNotifier(bot))
    dispatcher = build_dispatcher(settings, repository, scanner)

    scheduler_task = asyncio.create_task(scheduler_loop(scanner, settings.poll_interval_seconds))
    try:
        await dispatcher.start_polling(bot)
    finally:
        scheduler_task.cancel()
        await bot.session.close()
        try:
            await scheduler_task
        except asyncio.CancelledError:
            pass
