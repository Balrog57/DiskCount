from decimal import Decimal

from diskcount.bot import (
    AlertDraft,
    _alert_id_and_price,
    _user_id_and_label,
    apply_capacity_preset_to_draft,
    apply_price_preset_to_draft,
    build_alert_capacity_keyboard,
    build_alert_category_keyboard,
    build_alert_edit_keyboard,
    build_alert_price_keyboard,
    build_bot_commands,
    build_draft_keyboard,
    build_main_keyboard,
    build_menu_keyboard,
    create_alert_from_draft,
    draft_to_alert_args,
    is_authorized,
    is_env_admin,
    parse_alert_args,
)
from diskcount.config import Settings
from diskcount.db import Repository, create_db_engine


def test_parse_alert_args() -> None:
    args = parse_alert_args(
        "name=NAS min_tb=16 max_eur_tb=20 media=rotational condition=new,used "
        "sources=diskprices,ebay,leboncoin"
    )
    assert args.name == "NAS"
    assert args.min_capacity_tb == 16
    assert args.max_price_per_tb == Decimal("20")
    assert args.media_types == ["rotational"]
    assert args.conditions == ["new", "used"]
    assert args.sources == ["diskprices", "ebay", "leboncoin"]


def test_parse_alert_args_ssd_price_per_gb_and_diskprice_filters() -> None:
    args = parse_alert_args(
        "name=SSD min_tb=2 max_eur_gb=0.08 media=solid_state condition=new "
        "category=m2_nvme,external_ssd interface=nvme,usb"
    )
    assert args.max_price_per_tb == Decimal("80.00")
    assert args.media_types == ["solid_state"]
    assert args.drive_categories == ["m2_nvme", "external_ssd"]
    assert args.interfaces == ["nvme", "usb"]


def test_auth_accepts_admin_and_static_allow_list() -> None:
    settings = Settings(telegram_allowed_user_ids=[123])
    assert is_authorized(settings, None, 123)
    assert not is_authorized(settings, None, 456)

    admin_settings = Settings(telegram_admin_user_ids=[456])
    assert is_env_admin(admin_settings, 456)
    assert is_authorized(admin_settings, None, 456)


def test_auth_accepts_database_allowed_user() -> None:
    repository = Repository(create_db_engine("sqlite:///:memory:"))
    repository.init()
    repository.upsert_authorized_user(789, "Invite NAS")
    assert is_authorized(Settings(), repository, 789)


def test_parse_alert_id_and_price() -> None:
    assert _alert_id_and_price("1 18,5") == (1, Decimal("18.5"))
    assert _alert_id_and_price("1 none") == (1, None)
    assert _alert_id_and_price("bad") is None


def test_parse_user_id_and_label() -> None:
    assert _user_id_and_label("123 Jean Dupont") == (123, "Jean Dupont")
    assert _user_id_and_label("bad Jean") is None


def test_build_bot_commands() -> None:
    user_commands = [command.command for command in build_bot_commands()]
    admin_commands = [command.command for command in build_bot_commands(include_admin=True)]
    assert "menu" in user_commands
    assert "create" in user_commands
    assert "add" in user_commands
    assert "status" in user_commands
    assert "allow" not in user_commands
    assert "allow" in admin_commands
    assert "revoke" in admin_commands


def test_build_main_keyboard() -> None:
    user_buttons = [button.text for row in build_main_keyboard().keyboard for button in row]
    admin_buttons = [button.text for row in build_main_keyboard(include_admin=True).keyboard for button in row]
    assert "/menu" in user_buttons
    assert "/alerts" in user_buttons
    assert "/allow" not in user_buttons
    assert "/allow" in admin_buttons
    assert "/revoke" in admin_buttons


def test_build_menu_keyboard_navigation() -> None:
    home_buttons = [button.text for row in build_menu_keyboard().inline_keyboard for button in row]
    alert_buttons = [button.text for row in build_menu_keyboard("alerts").inline_keyboard for button in row]
    help_buttons = [button.text for row in build_menu_keyboard("help").inline_keyboard for button in row]
    admin_home_buttons = [button.text for row in build_menu_keyboard(include_admin=True).inline_keyboard for button in row]

    assert "Creer une alerte" in home_buttons
    assert "Mes alertes" in home_buttons
    assert "Sources" not in home_buttons
    assert "Capacites" in help_buttons
    assert "Prix" in help_buttons
    assert "Sources backend" in help_buttons
    assert "Fallback /add" in help_buttons
    assert "Admin" not in home_buttons
    assert "Admin" in admin_home_buttons
    assert "Mes alertes" in alert_buttons
    assert "Precedent" in alert_buttons
    assert "Accueil" in alert_buttons


def test_build_alert_edit_keyboards() -> None:
    repository = Repository(create_db_engine("sqlite:///:memory:"))
    repository.init()
    alert = repository.create_alert(
        chat_id=42,
        owner_user_id=1001,
        name="NAS",
        min_capacity_tb=16,
        max_capacity_tb=20,
        conditions=["new"],
        media_types=["rotational"],
        drive_categories=["internal_3_5"],
        interfaces=["sata"],
        sources=["diskprices"],
        max_price_per_tb=Decimal("20.00"),
        min_discount_pct=5.0,
        cooldown_hours=24,
    )
    edit_buttons = [button.text for row in build_alert_edit_keyboard(alert).inline_keyboard for button in row]
    category_buttons = [button.text for row in build_alert_category_keyboard(alert).inline_keyboard for button in row]
    capacity_buttons = [button.text for row in build_alert_capacity_keyboard(alert).inline_keyboard for button in row]
    price_buttons = [button.text for row in build_alert_price_keyboard(alert).inline_keyboard for button in row]
    assert "Type" in edit_buttons
    assert "Etat" in edit_buttons
    assert "Capacite" in edit_buttons
    assert "Prix" in edit_buttons
    assert "Sources" not in edit_buttons
    assert "[x] Internal 3.5" in category_buttons
    assert "[x] HDD 16-20 To" in capacity_buttons
    assert "[x] HDD <=20 EUR/To" in price_buttons
    assert "Accueil" in category_buttons


def test_alert_draft_presets_and_create() -> None:
    repository = Repository(create_db_engine("sqlite:///:memory:"))
    repository.init()

    draft = AlertDraft()
    apply_capacity_preset_to_draft(draft, "ssd_2")
    apply_price_preset_to_draft(draft, "s008")
    draft.conditions = ["new"]
    draft.drive_categories = ["m2_nvme"]
    draft.interfaces = ["nvme"]
    draft.sources = []

    args = draft_to_alert_args(draft)
    assert args.capacity_presets == ["hdd_16_20", "ssd_2"]
    assert args.min_capacity_tb is None
    assert args.max_capacity_tb is None
    assert args.media_types == ["rotational", "solid_state"]
    assert args.max_price_per_tb == Decimal("80")

    alert = create_alert_from_draft(repository, chat_id=42, owner_user_id=1001, draft=draft)
    assert alert.owner_user_id == 1001
    assert alert.conditions == ["new"]
    assert alert.drive_categories == ["m2_nvme"]
    assert alert.interfaces == ["nvme"]
    assert alert.sources == []
    assert alert.capacity_presets == ["hdd_16_20", "ssd_2"]


def test_draft_keyboard_marks_selected_values() -> None:
    draft = AlertDraft(step="price", media_types=["solid_state"], max_price_per_tb=Decimal("80"))
    price_buttons = [button.text for row in build_draft_keyboard(draft).inline_keyboard for button in row]
    assert "[x] SSD <=0.08 EUR/Go" in price_buttons

    draft.step = "capacity"
    draft.capacity_presets = []
    apply_capacity_preset_to_draft(draft, "hdd_16_20")
    capacity_buttons = [button.text for row in build_draft_keyboard(draft).inline_keyboard for button in row]
    assert "[x] HDD 16-20 To" in capacity_buttons


def test_capacity_keyboard_supports_multiple_selected_ranges() -> None:
    draft = AlertDraft(step="capacity", capacity_presets=[])
    apply_capacity_preset_to_draft(draft, "hdd_16_20")
    apply_capacity_preset_to_draft(draft, "hdd_20_24")
    capacity_buttons = [button.text for row in build_draft_keyboard(draft).inline_keyboard for button in row]
    assert "[x] HDD 16-20 To" in capacity_buttons
    assert "[x] HDD 20-24 To" in capacity_buttons
