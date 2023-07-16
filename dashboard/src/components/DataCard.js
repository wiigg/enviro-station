import upicon from "../images/arrow-up.png";
import downicon from "../images/arrow-down.png";

const DataCard = ({ title, data, symbol, change, icon }) => {
  let changeClass = "";
  let changeIcon = null;
  if (change > 0) {
    changeClass = "text-green-400";
    changeIcon = upicon;
  } else if (change < 0) {
    changeClass = "text-red-400";
    changeIcon = downicon;
  } else {
    changeClass = "text-yellow-400";
    changeIcon = null;
  }

  return (
    <div className="bg-gray-800 text-white rounded-lg shadow-md p-6 w-full flex flex-col items-center justify-center">
      <div className="flex items-center">
        <img src={icon} alt="icon" className="h-6 mr-2 mb-4" />{" "}
        <h2 className="text-lg mb-4 uppercase tracking-wider">{title}</h2>
      </div>
      <div className="flex items-baseline">
        <span className="text-6xl font-bold">{data}</span>
        <span className="text-base">{symbol}</span>
      </div>
      <div className={`flex items-baseline ${changeClass}`}>
        <img src={changeIcon} alt="" className="h-4 mr-1" />
        <span className="mt-2 text-2xl">{change}</span>
        <span className="text-base">%</span>
      </div>
      <span className="text-xs mt-2 text-gray-400">
        vs. 30 minute moving average
      </span>
    </div>
  );
};

export default DataCard;
