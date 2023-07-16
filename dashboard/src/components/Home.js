import Graph from "./Graph";
import DataCard from "./DataCard";
import { getLatestData, getPercentageChange } from "../utils/data";
import tempicon from "../images/temperature.png";
import humicon from "../images/humidity.png";
import presicon from "../images/pressure.png";
import pmicon from "../images/pm.png";
import gasicon from "../images/gas.png";

const Home = ({ messages }) => {
  const latestTemperature = getLatestData(messages, "temperature");
  const latestHumidity = getLatestData(messages, "humidity");
  const latestPressure = getLatestData(messages, "pressure");

  const temperatureChange = getPercentageChange(messages, "temperature");
  const humidityChange = getPercentageChange(messages, "humidity");
  const pressureChange = getPercentageChange(messages, "pressure");

  return (
    <div className="bg-gray-100 pt-2 pb-4 px-4 md:pt-4 md:px-8 border border-gray-300">
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4 my-4">
        <DataCard
          title={"Temperature"}
          data={latestTemperature}
          symbol={"°C"}
          change={temperatureChange}
          icon={tempicon}
        />
        <DataCard
          title={"Humidity"}
          data={latestHumidity}
          symbol={"%"}
          change={humidityChange}
          icon={humicon}
        />
        <DataCard
          title={"Pressure"}
          data={latestPressure}
          symbol={"hPa"}
          change={pressureChange}
          icon={presicon}
        />
      </div>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 my-4">
        <Graph
          title={"Particulate Matter"}
          keys={["pm1", "pm25", "pm10"]}
          data={messages}
          symbol={"µg/m³"}
          icon={pmicon}
        />
        <Graph
          title={"Gas"}
          keys={["nh3", "oxidised", "reduced"]}
          data={messages}
          symbol={"kΩ"}
          icon={gasicon}
        />
      </div>
    </div>
  );
};

export default Home;
