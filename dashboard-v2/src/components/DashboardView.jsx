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
        >
          <WindowControls
            onSelectWindow={onSelectWindow}
            selectedWindowId={selectedWindow.id}
            windowOptions={windowOptions}
          />
        </DashboardHeader>

        <KpiGrid kpis={kpis} />

        <TrendPanels
          axisTickFormatter={axisTickFormatter}
          chartData={chartData}
          temperatureDomain={temperatureDomain}
        />

        <section className="intelligenceSection reveal" aria-label="Insights and diagnostics">
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
        </section>
      </main>
    </div>
  );
}
