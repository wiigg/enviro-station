try:
    import st7735 as ST7735
except ImportError:
    import ST7735
import os
from pms5003 import PMS5003, ReadTimeoutError, ChecksumMismatchError
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


class DeviceInterface:
    def __init__(self):
        """Initialise device interface"""
        self.bus = SMBus(1)
        self.bme280 = BME280(i2c_dev=self.bus)
        self.pms5003 = PMS5003()
        self.gas = gas.read_all()

        self.font_size = 16
        self.font = load_status_font(self.font_size)

        # Create LCD instance
        self.disp = ST7735.ST7735(
            port=0, cs=1, dc=9, backlight=12, rotation=270, spi_speed_hz=10000000
        )

        # Initialize display
        self.disp.begin()

        # Width and height to calculate text position
        self.WIDTH = self.disp.width
        self.HEIGHT = self.disp.height

        self.comp_factor = env_float("DEVICE_TEMP_COMP_FACTOR", 1.45)
        self.temp_offset_c = env_float("DEVICE_TEMP_OFFSET_C", 0.6)
        self.last_pm_values = {"pm1": "0", "pm2": "0", "pm10": "0"}

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
                break  # if no exception, break the loop
            except (ReadTimeoutError, ChecksumMismatchError):
                logging.info("Failed to read PMS5003. Resetting and retrying.")
                self.pms5003.reset()
        else:
            logging.warning(
                "Using cached PM values after PMS5003 read failures: %s",
                self.last_pm_values,
            )

        return values

    def display_status(self):
        """Display Raspberry Pi serial and Wi-Fi status"""
        wifi_status = "connected" if check_wifi() else "disconnected"
        text_colour = (255, 255, 255)
        back_colour = (0, 170, 170) if check_wifi() else (85, 15, 15)
        id = get_serial_number()
        message = "{}\nWi-Fi: {}".format(id, wifi_status)
        img = Image.new("RGB", (self.WIDTH, self.HEIGHT), color=(0, 0, 0))
        draw = ImageDraw.Draw(img)
        left, top, right, bottom = draw.multiline_textbbox((0, 0), message, font=self.font)
        size_x = right - left
        size_y = bottom - top
        x = (self.WIDTH - size_x) / 2
        y = (self.HEIGHT / 2) - (size_y / 2)
        draw.rectangle((0, 0, 160, 80), back_colour)
        draw.multiline_text((x, y), message, font=self.font, fill=text_colour)
        self.disp.display(img)
