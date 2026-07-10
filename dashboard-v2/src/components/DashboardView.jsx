import DashboardHeader from "./dashboard/DashboardHeader";
import InsightsCard from "./dashboard/InsightsCard";
import KpiGrid from "./dashboard/KpiGrid";
import OpsFeedCard from "./dashboard/OpsFeedCard";
import TrendPanels from "./dashboard/TrendPanels";
import WindowControls from "./dashboard/WindowControls";

export default function DashboardView({
  axisTickFormatter,
  chartData,
  connectionStatus,
  deviceLabel,
  feedError,
  feedItems,
  insightSource,
  insights,
  insightsError,
  isLoadingFeed,
  isLoadingInsights,
  kpis,
  lastError,
  lastReadingAt,
  onSelectWindow,
  selectedWindow,
  temperatureDomain,
  windowOptions
}) {
  return (
    <div className="shell">
      <main className="layout">
        <DashboardHeader
          connectionStatus={connectionStatus}
          deviceLabel={deviceLabel}
          kpis={kpis}
          lastError={lastError}
          lastReadingAt={lastReadingAt}
          selectedWindow={selectedWindow}
        />

        <WindowControls
          onSelectWindow={onSelectWindow}
          selectedWindowId={selectedWindow.id}
          windowOptions={windowOptions}
        />

        <KpiGrid kpis={kpis} />

        <section className="dashboardGrid reveal">
          <TrendPanels
            axisTickFormatter={axisTickFormatter}
            chartData={chartData}
            temperatureDomain={temperatureDomain}
          />

          <div className="sideStack">
            <InsightsCard
              insightSource={insightSource}
              insights={insights}
              insightsError={insightsError}
              isLoadingInsights={isLoadingInsights}
              lastError={lastError}
            />
            <OpsFeedCard
              feedError={feedError}
              feedItems={feedItems}
              isLoadingFeed={isLoadingFeed}
            />
          </div>
        </section>
      </main>
    </div>
  );
}
