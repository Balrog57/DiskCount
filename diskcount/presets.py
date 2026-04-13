from __future__ import annotations

CAPACITY_PRESETS: dict[str, tuple[str, float | None, float | None, str]] = {
    "all": ("Toute capacite", None, None, "all"),
    "ssd_lt_256": ("SSD <256 Go", None, 0.256, "solid_state"),
    "ssd_256": ("SSD ~256 Go", 0.24, 0.30, "solid_state"),
    "ssd_512": ("SSD ~512 Go", 0.48, 0.60, "solid_state"),
    "ssd_1": ("SSD ~1 To", 0.9, 1.2, "solid_state"),
    "ssd_2": ("SSD ~2 To", 1.8, 2.4, "solid_state"),
    "ssd_4": ("SSD ~4 To", 3.6, 4.8, "solid_state"),
    "ssd_gt_4": ("SSD >4 To", 4.0, None, "solid_state"),
    "hdd_lt_4": ("HDD <4 To", None, 4.0, "rotational"),
    "hdd_4_8": ("HDD 4-8 To", 4.0, 8.0, "rotational"),
    "hdd_8_12": ("HDD 8-12 To", 8.0, 12.0, "rotational"),
    "hdd_12_16": ("HDD 12-16 To", 12.0, 16.0, "rotational"),
    "hdd_16_20": ("HDD 16-20 To", 16.0, 20.0, "rotational"),
    "hdd_20_24": ("HDD 20-24 To", 20.0, 24.0, "rotational"),
    "hdd_24_30": ("HDD 24-30 To", 24.0, 30.0, "rotational"),
    "hdd_gt_30": ("HDD >30 To", 30.0, None, "rotational"),
}

HDD_CAPACITY_KEYS = (
    "hdd_lt_4",
    "hdd_4_8",
    "hdd_8_12",
    "hdd_12_16",
    "hdd_16_20",
    "hdd_20_24",
    "hdd_24_30",
    "hdd_gt_30",
)
SSD_CAPACITY_KEYS = ("ssd_lt_256", "ssd_256", "ssd_512", "ssd_1", "ssd_2", "ssd_4", "ssd_gt_4")
