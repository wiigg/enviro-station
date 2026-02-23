import DashboardView from "./components/DashboardView";
import { useDashboardData } from "./hooks/useDashboardData";

export default function App() {
  const viewProps = useDashboardData();
  return <DashboardView {...viewProps} />;
}
