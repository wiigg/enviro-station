try:
    import st7735 as ST7735
except ImportError:
    import ST7735
import os
from pms5003 import (
    PMS5003,
    ChecksumMismatchError,
    ReadTimeoutError,
    SerialTimeoutError,
)
from bme280 import BME280
from enviroplus import gas
from PIL import Image, ImageDraw, ImageFont
import time

try:
    from smbus2 import SMBus
except ImportError:
    from smbus import SMBus

import logging

from device_utilities import get_cpu_temperature, check_wifi, get_serial_number


def load_status_font(size):
    system_font = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
    if os.path.exists(system_font):
        return ImageFont.truetype(system_font, size)
    return ImageFont.load_default()


def env_float(name, fallback):
    raw_value = os.getenv(name)
    if raw_value is None:
        return fallback
    try:
        return float(raw_value)
    except ValueError:
        return fallback


def env_int(name, fallback):
    raw_value = os.getenv(name)
    if raw_value is None:
        return fallback
    try:
        return int(raw_value)
    except ValueError:
        return fallback


def clamp(value, lower, upper):
    return max(lower, min(upper, value))


class DeviceInterface:
    def __init__(self):
        """Initialise device interface"""
        self.bus = SMBus(1)
        self.bme280 = BME280(i2c_dev=self.bus)
        self.pms5003 = PMS5003()
        self.gas = gas.read_all()

        self.font_size = env_int("DEVICE_DISPLAY_FONT_SIZE", 11)
        self.font = load_status_font(self.font_size)
        self.small_font = load_status_font(max(8, self.font_size - 2))

        # Create LCD instance
        self.disp = ST7735.ST7735(
            port=0, cs=1, dc=9, backlight=12, rotation=270, spi_speed_hz=10000000
        )

        # Initialize display
        self.disp.begin()
        self._set_backlight(clamp(env_float("DEVICE_DISPLAY_BACKLIGHT", 0.35), 0, 1))

        # Width and height to calculate text position
        self.WIDTH = self.disp.width
        self.HEIGHT = self.disp.height

        self.comp_factor = env_float("DEVICE_TEMP_COMP_FACTOR", 1.45)
        self.temp_offset_c = env_float("DEVICE_TEMP_OFFSET_C", 0.6)
        self.last_pm_values = {"pm1": "0", "pm2": "0", "pm10": "0"}
        self.serial_label = self._compact_serial(get_serial_number())
        self.wifi_check_interval_seconds = max(1, env_int("DEVICE_WIFI_CHECK_INTERVAL_SECONDS", 30))
        self.last_wifi_checked_at = 0
        self.cached_wifi_connected = False

    def read_values(self):
        """Read values from BME280 and PMS5003 and return as dict"""
        # Refresh gas readings for each call to ensure up-to-date values
        self.gas = gas.read_all()
        cpu_temp = get_cpu_temperature()
        raw_temp = self.bme280.get_temperature()
        comp_temp = raw_temp - ((cpu_temp - raw_temp) / self.comp_factor) - self.temp_offset_c

        values = {
            "timestamp": "{:.0f}".format(time.time()),
            "temperature": "{:.2f}".format(comp_temp),
            "pressure": "{:.2f}".format(self.bme280.get_pressure() * 100),
            "humidity": "{:.2f}".format(self.bme280.get_humidity()),
            "oxidised": "{:.2f}".format(self.gas.oxidising / 1000),
            "reduced": "{:.2f}".format(self.gas.reducing / 1000),
            "nh3": "{:.2f}".format(self.gas.nh3 / 1000),
            "pm_available": False,
        }
        values.update(self.last_pm_values)

        for _ in range(2):  # try twice
            try:
                pm_values = self.pms5003.read()
                self.last_pm_values = {
                    "pm1": str(pm_values.pm_ug_per_m3(1.0)),
                    "pm2": str(pm_values.pm_ug_per_m3(2.5)),
                    "pm10": str(pm_values.pm_ug_per_m3(10)),
                }
                values.update(self.last_pm_values)
                values["pm_available"] = True
                break  # if no exception, break the loop
            except (ReadTimeoutError, SerialTimeoutError, ChecksumMismatchError):
                logging.info("Failed to read PMS5003. Resetting and retrying.")
                self.pms5003.reset()
        else:
            logging.warning(
                "Using cached PM values after PMS5003 read failures: %s",
                self.last_pm_values,
            )

        return values

    def display_status(self, values=None):
        """Display latest sensor metrics with a dark, low-glare palette."""
        wifi_connected = self._wifi_connected()
        values = values or {}
        alert_state = self._display_alert_state(values, wifi_connected)

        background = (0, 0, 0) if alert_state == "ok" else (24, 0, 0)
        panel = (10, 10, 10) if alert_state == "ok" else (34, 4, 4)
        text_colour = (198, 190, 178)
        muted_colour = (110, 104, 96)
        accent_colour = (125, 24, 24) if alert_state == "ok" else (190, 38, 38)

        img = Image.new("RGB", (self.WIDTH, self.HEIGHT), color=background)
        draw = ImageDraw.Draw(img)
        draw.rectangle((0, 0, self.WIDTH, 13), fill=panel)
        draw.rectangle((0, 0, 3, self.HEIGHT), fill=accent_colour)

        wifi_label = "WiFi" if wifi_connected else "OFFLINE"
        draw.text((7, 1), wifi_label, font=self.small_font, fill=muted_colour)
        draw.text((self.WIDTH - 54, 1), self.serial_label, font=self.small_font, fill=muted_colour)

        metrics = [
            ("PM2.5", self._format_metric(values, "pm2", 0)),
            ("PM10", self._format_metric(values, "pm10", 0)),
            ("TEMP", self._format_metric(values, "temperature", 1, "C")),
            ("HUM", self._format_metric(values, "humidity", 0, "%")),
        ]

        positions = [(7, 18), (83, 18), (7, 49), (83, 49)]
        for (label, value), (x, y) in zip(metrics, positions):
            draw.text((x, y), label, font=self.small_font, fill=muted_colour)
            draw.text((x, y + 11), value, font=self.font, fill=text_colour)

        self.disp.display(img)

    def _set_backlight(self, level):
        set_backlight = getattr(self.disp, "set_backlight", None)
        if not callable(set_backlight):
            logging.info("Display driver does not expose backlight dimming")
            return
        try:
            set_backlight(level)
        except TypeError:
            try:
                set_backlight(level > 0)
            except Exception as exc:
                logging.warning("Failed to set display backlight: %s", exc)
        except Exception as exc:
            logging.warning("Failed to set display backlight: %s", exc)

    def _wifi_connected(self):
        now = time.monotonic()
        if now - self.last_wifi_checked_at >= self.wifi_check_interval_seconds:
            self.cached_wifi_connected = check_wifi()
            self.last_wifi_checked_at = now
        return self.cached_wifi_connected

    def _compact_serial(self, serial_number):
        serial_number = str(serial_number or "")
        if len(serial_number) <= 8:
            return serial_number
        return serial_number[-8:]

    def _display_alert_state(self, values, wifi_connected):
        if not wifi_connected or values.get("pm_available") is False:
            return "alert"
        pm2 = self._numeric_value(values, "pm2")
        pm10 = self._numeric_value(values, "pm10")
        if pm2 is not None and pm2 > 15:
            return "alert"
        if pm10 is not None and pm10 > 45:
            return "alert"
        return "ok"

    def _format_metric(self, values, key, decimals, suffix=""):
        if key in ("pm1", "pm2", "pm10") and values.get("pm_available") is False:
            return "--"
        value = self._numeric_value(values, key)
        if value is None:
            return "--"
        return f"{value:.{decimals}f}{suffix}"

    def _numeric_value(self, values, key):
        raw_value = values.get(key)
        if raw_value is None:
            return None
        try:
            return float(raw_value)
        except (TypeError, ValueError):
            return None
