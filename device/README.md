# Guide: Auto-start Sensor Service on Boot

To regularly send sensor data to Azure, our program needs to be running consistently. You can start it manually with `python main.py` each time, but a more efficient way is to set it up to start automatically at boot. Follow the steps below to set this up.

## 📄 Step 1: Customise Service Template

Open `sensor.service.template`:
- Replace `<<PATH_TO_DEVICE_PROGRAM>>` with the full path to your `main.py`.
- Replace `<<WORKING_DIRECTORY>>` with the directory where your `main.py` is located.
- Replace `<<USER>>` with the username that will run the script.

Then, rename the file to `sensor.service` and save your changes in the `/etc/systemd/system/` directory.

## 🔄 Step 2: Load & Enable the Sensor Service

Enable the service to run at boot:

```
sudo systemctl daemon-reload
sudo systemctl enable sensor.service
```

## 🚀 Step 3: Start the Sensor Service

Start the service immediately with:

```
sudo systemctl start sensor.service
```

## 🔍 Step 4: Check the Status

Check the status of the service with:

```
sudo systemctl status sensor.service
```

## 🔄 Step 5: Restart After Changes

If you update `main.py` and want the changes to take effect, restart the service:

```
sudo systemctl restart sensor.service
```

That's it! `main.py` should now run on boot, sending sensor data to Azure. 🎉

Note: Replace `sensor.service` with your service file's name if different.