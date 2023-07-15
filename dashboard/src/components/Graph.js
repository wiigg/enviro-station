import { useState } from "react";
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
  Label,
} from "recharts";

const Graph = ({ title, keys, data, symbol }) => {
  const lines = [
    { key: keys[0], colour: "#FF7F50" },
    { key: keys[1], colour: "#FDD835" },
    { key: keys[2], colour: "#BA55D3" },
  ];

  const [lineProps, setLineProps] = useState(
    lines.reduce(
      (a, { key }) => {
        a[key] = false;
        return a;
      },
      { hover: null }
    )
  );

  const handleLegendMouseEnter = (e) => {
    if (!lineProps[e.dataKey]) {
      setLineProps({ ...lineProps, hover: e.dataKey });
    }
  };

  const handleLegendMouseLeave = (e) => {
    setLineProps({ ...lineProps, hover: null });
  };

  const selectLine = (e) => {
    setLineProps({
      ...lineProps,
      [e.dataKey]: !lineProps[e.dataKey],
      hover: null,
    });
  };

  return (
    <div className="w-full p-4 bg-gray-800 rounded-lg shadow-md overflow-hidden">
      <h2 className="text-lg text-white mb-4 uppercase tracking-wider">
        {title}
      </h2>
      <ResponsiveContainer width="99%" height={400}>
        <LineChart
          data={data}
          margin={{
            top: 60,
            right: 30,
            left: 10,
            bottom: 0,
          }}
          padding={{ top: 20, right: 20, bottom: 20, left: 20 }}
        >
          <CartesianGrid strokeDasharray="3 3" stroke="#6b7280" />
          <XAxis
            dataKey="timestamp"
            stroke="#d1d5db"
            tick={{ fill: "#d1d5db" }}
          />
          <YAxis
            stroke="#d1d5db"
            tick={{ fill: "#d1d5db" }}
            domain={["auto", "auto"]}
          >
            <Label
              value={symbol}
              angle={-90}
              position="insideLeft"
              style={{ textAnchor: "middle", fill: "#d1d5db" }}
            />
          </YAxis>
          <Tooltip
            contentStyle={{
              backgroundColor: "#4B5563",
              color: "#E5E7EB",
              border: "none",
            }}
            labelStyle={{ fontSize: 16, color: "#E5E7EB" }}
            itemStyle={{ fontSize: 14, color: "#E5E7EB" }}
          />
          <Legend
            wrapperStyle={{
              top: 0,
              left: 30,
              padding: 10,
              backgroundColor: "#4B5563",
              borderRadius: "5px",
              color: "#E5E7EB",
            }}
            onMouseEnter={handleLegendMouseEnter}
            onMouseLeave={handleLegendMouseLeave}
            onClick={selectLine}
          />
          {lines.map((line, index) => (
            <Line
              key={index}
              type="monotone"
              dataKey={line.key}
              stroke={line.colour}
              strokeWidth={3}
              activeDot={{ r: 8 }}
              hide={lineProps[line.key] === true}
              strokeOpacity={Number(
                lineProps.hover === line.key || !lineProps.hover ? 1 : 0.2
              )}
            />
          ))}
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
};

export default Graph;
