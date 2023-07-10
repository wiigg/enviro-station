from subprocess import check_output


def get_cpu_temperature():
    """Get CPU temperature"""
    with open("/sys/class/thermal/thermal_zone0/temp", "r") as f:
        temp = f.read()
        temp = int(temp) / 1000.0
    return temp


def get_serial_number():
    """Get Raspberry Pi serial number"""
    with open("/proc/cpuinfo", "r") as f:
        for line in f:
            if line[0:6] == "Serial":
                return line.split(":")[1].strip()


def check_wifi():
    """Check Wi-Fi for connection"""
    if check_output(["hostname", "-I"]):
        return True
    else:
        return False
