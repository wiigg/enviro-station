# Guide: Auto-start Sensor Data Service on Boot

To regularly send sensor data to the cloud, our program needs to be running consistently. You can start it manually with `python main.py` each time, but a more efficient way is to set it up to start automatically at boot. Follow the steps below to set this up.

## 📄 Step 1: Customise Service Template

Open `template.service`:
- Replace `<<PATH_TO_DEVICE_PROGRAM>>` with the full path to your `main.py`.
- Replace `<<WORKING_DIRECTORY>>` with the directory where your `main.py` is located.
- Replace `<<USER>>` with the username that will run the script.

Then, save your changes as `sensor-data.service` in the `/etc/systemd/system/` directory.

## 🔄 Step 2: Load & Enable the Sensor Data Service

Enable the service to run at boot:

```
sudo systemctl daemon-reload
sudo systemctl enable sensor-data.service
```

## 🚀 Step 3: Start the Sensor Data Service

Start the service immediately with:

```
sudo systemctl start sensor-data.service
```

## 🔍 Step 4: Check the Status

Check the status of the service with:

```
sudo systemctl status sensor-data.service
```

## 🔄 Step 5: Restart After Changes

If you update `main.py` and want the changes to take effect, restart the service:

```
sudo systemctl restart sensor-data.service
```

That's it! `main.py` should now run on boot, sending sensor data to the cloud. 🎉

Note: Replace `sensor-data.service` with your service file's name if different.