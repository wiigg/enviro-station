import { memo } from "react";
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis
} from "recharts";

const AXIS_TICK_STYLE = { fill: "#647184", fontSize: 11 };
const TOOLTIP_CONTENT_STYLE = {
  borderRadius: "8px",
  border: "1px solid rgba(19, 28, 43, 0.12)",
  background: "rgba(255,255,255,0.98)",
  boxShadow: "0 12px 30px rgba(19, 28, 43, 0.12)"
};
const PARTICULATE_TICK_INTERVALS = 5;
const PARTICULATE_MIN_AXIS_MAX = 4;
const PARTICULATE_MIN_TICK_STEP = 2;

const TREND_PANELS = [
  {
    title: "Particulate matter",
    ariaLabel: "Particulate trend chart",
    lineDataKey: "pm2",
    lineStroke: "#c95727",
    averageDataKey: "pm2Average",
    averageStroke: "#4f6278",
    tooltipName: "PM2.5",
    tooltipUnit: "µg/m³",
    useParticulateYAxis: true
  },
  {
    title: "Temperature",
    ariaLabel: "Temperature trend chart",
    lineDataKey: "temperature",
    lineStroke: "#1f8a78",
    averageDataKey: "temperatureAverage",
    averageStroke: "#4f6278",
    tooltipName: "Temperature",
    tooltipUnit: "°C",
    useTemperatureDomain: true
  }
];

function formatChartLabel(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  return date.toLocaleString([], {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit"
  });
}

function particulateAxisTicks(chartData, keys) {
  let max = 0;
  for (const point of chartData) {
    for (const key of keys) {
      const value = point[key];
      if (Number.isFinite(value) && value > max) {
        max = value;
      }
    }
  }

  const paddedMax = Math.max(max * 1.15, PARTICULATE_MIN_AXIS_MAX);
  const step = Math.max(
    PARTICULATE_MIN_TICK_STEP,
    niceAxisStep(paddedMax / PARTICULATE_TICK_INTERVALS)
  );
  const axisMax = Math.ceil(paddedMax / step) * step;
  const tickCount = Math.round(axisMax / step);
  return Array.from({ length: tickCount + 1 }, (_item, index) => step * index);
}

function niceAxisStep(value) {
  const magnitude = 10 ** Math.floor(Math.log10(value));
  const fraction = value / magnitude;
  if (fraction <= 1.5) {
    return magnitude;
  }
  if (fraction <= 3) {
    return 2 * magnitude;
  }
  if (fraction <= 7) {
    return 5 * magnitude;
  }
  return 10 * magnitude;
}

const TrendPanel = memo(function TrendPanel({
  title,
  ariaLabel,
  chartData,
  axisTickFormatter,
  yAxisDomain,
  useParticulateYAxis = false,
  lineDataKey,
  lineStroke,
  averageDataKey,
  averageStroke,
  tooltipName,
  tooltipUnit
}) {
  const yAxisTicks = useParticulateYAxis
    ? particulateAxisTicks(chartData, [lineDataKey, averageDataKey])
    : undefined;
  const resolvedYAxisDomain = yAxisTicks
    ? [0, yAxisTicks[yAxisTicks.length - 1]]
    : yAxisDomain;

  return (
    <article className="card panel">
      <div className="panelHead">
        <h3>{title}</h3>
        <div className="chartLegend" aria-hidden="true">
          <span>
            <i style={{ backgroundColor: lineStroke }} />
            Reading
          </span>
          <span>
            <i className="averageLegend" style={{ backgroundColor: averageStroke }} />
            Avg
          </span>
        </div>
      </div>
      {chartData.length ? (
        <div className="chart" role="img" aria-label={ariaLabel}>
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={chartData} margin={{ top: 8, right: 10, left: 0, bottom: 0 }}>
              <CartesianGrid strokeDasharray="2 6" stroke="rgba(19, 28, 43, 0.1)" />
              <XAxis
                dataKey="timestamp"
                type="number"
                scale="time"
                domain={["dataMin", "dataMax"]}
                tickFormatter={axisTickFormatter}
                minTickGap={26}
                tick={AXIS_TICK_STYLE}
                axisLine={false}
                tickLine={false}
              />
              <YAxis
                allowDataOverflow={useParticulateYAxis}
                domain={resolvedYAxisDomain}
                ticks={yAxisTicks}
                width={44}
                tick={AXIS_TICK_STYLE}
                axisLine={false}
                tickLine={false}
              />
              <Tooltip
                labelFormatter={formatChartLabel}
                formatter={(value, name) => [
                  `${Number(value).toFixed(1)} ${tooltipUnit}`,
                  name
                ]}
                contentStyle={TOOLTIP_CONTENT_STYLE}
              />
              <Line
                type="linear"
                dataKey={averageDataKey}
                name={`${tooltipName} avg`}
                stroke={averageStroke}
                strokeWidth={2}
                strokeDasharray="5 5"
                dot={false}
                connectNulls
                isAnimationActive={false}
              />
              <Line
                type="linear"
                dataKey={lineDataKey}
                name={tooltipName}
                stroke={lineStroke}
                strokeWidth={2.5}
                dot={false}
                isAnimationActive={false}
              />
            </LineChart>
          </ResponsiveContainer>
        </div>
      ) : (
        <p className="emptyState">No data in selected window yet.</p>
      )}
    </article>
  );
});

export default memo(function TrendPanels({
  axisTickFormatter,
  chartData,
  temperatureDomain
}) {
  return (
    <section className="monitoringSection reveal" aria-labelledby="monitoring-title">
      <div className="sectionHeading">
        <h2 id="monitoring-title">Live monitoring</h2>
      </div>
      <div className="trendGrid">
        {TREND_PANELS.map((panel) => (
          <TrendPanel
            key={panel.lineDataKey}
            title={panel.title}
            ariaLabel={panel.ariaLabel}
            chartData={chartData}
            axisTickFormatter={axisTickFormatter}
            yAxisDomain={panel.useTemperatureDomain ? temperatureDomain : panel.yAxisDomain}
            useParticulateYAxis={panel.useParticulateYAxis}
            lineDataKey={panel.lineDataKey}
            lineStroke={panel.lineStroke}
            averageDataKey={panel.averageDataKey}
            averageStroke={panel.averageStroke}
            tooltipName={panel.tooltipName}
            tooltipUnit={panel.tooltipUnit}
          />
        ))}
      </div>
    </section>
  );
});
