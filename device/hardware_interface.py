import ST7735
from pms5003 import PMS5003, ReadTimeoutError, ChecksumMismatchError
from bme280 import BME280
from PIL import Image, ImageDraw, ImageFont
from fonts.ttf import RobotoMedium as UserFont

try:
    from smbus2 import SMBus
except ImportError:
    from smbus import SMBus

import logging

from utilities import get_cpu_temperature, check_wifi, get_serial_number


class HardwareInterface:
    def __init__(self):
        """Initialise hardware interface"""
        self.bus = SMBus(1)
        self.bme280 = BME280(i2c_dev=self.bus)
        self.pms5003 = PMS5003()

        self.font_size = 16
        self.font = ImageFont.truetype(UserFont, self.font_size)

        # Create LCD instance
        disp = ST7735.ST7735(
            port=0, cs=1, dc=9, backlight=12, rotation=270, spi_speed_hz=10000000
        )

        # Initialize display
        disp.begin()

        self.width = disp.width
        self.height = disp.height

        self.comp_factor = 2.25

    def read_values(self):
        """Read values from BME280 and PMS5003 and return as dict"""
        values = {}
        cpu_temp = get_cpu_temperature()
        raw_temp = self.bme280.get_temperature()
        comp_temp = raw_temp - ((cpu_temp - raw_temp) / self.comp_factor)
        values["temperature"] = "{:.2f}".format(comp_temp)
        values["pressure"] = "{:.2f}".format(self.bme280.get_pressure() * 100)
        values["humidity"] = "{:.2f}".format(self.bme280.get_humidity())
        try:
            pm_values = self.pms5003.read()
            values["P2"] = str(pm_values.pm_ug_per_m3(2.5))
            values["P1"] = str(pm_values.pm_ug_per_m3(10))
        except (ReadTimeoutError, ChecksumMismatchError):
            logging.info("Failed to read PMS5003. Reseting and retrying.")
            self.pms5003.reset()
            pm_values = self.pms5003.read()
            values["P2"] = str(pm_values.pm_ug_per_m3(2.5))
            values["P1"] = str(pm_values.pm_ug_per_m3(10))
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
        size_x, size_y = draw.textsize(message, self.font)
        x = (self.WIDTH - size_x) / 2
        y = (self.HEIGHT / 2) - (size_y / 2)
        draw.rectangle((0, 0, 160, 80), back_colour)
        draw.text((x, y), message, font=self.font, fill=text_colour)
        self.disp.display(img)
