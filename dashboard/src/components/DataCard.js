const DataCard = ({ title, data, symbol, change }) => {
  let changeClass = "";
  if (change > 0) {
    change = `+${change}`;
    changeClass = "text-green-400";
  } else if (change < 0) {
    changeClass = "text-red-400";
  } else {
    changeClass = "text-yellow-400";
  }

  return (
    <div className="bg-gray-800 text-white rounded-lg shadow-md p-6 w-full flex flex-col items-center justify-center">
      <h2 className="text-lg mb-4 uppercase tracking-wider">{title}</h2>
      <div className="flex items-baseline">
        <span className="text-6xl font-bold">{data}</span>
        <span className="text-base">{symbol}</span>
      </div>
      <div className={`flex items-baseline ${changeClass}`}>
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
